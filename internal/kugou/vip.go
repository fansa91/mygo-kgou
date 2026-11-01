package kugou

import (
	"fmt"
	"strconv"
	"time"
)

// What an account is besides its name: the subscription it holds, and
// whether it signed in as the 概念版, which listens on terms of its own.

// Product is one of the subscriptions an account pays for, and the day it
// runs out. The 概念版 sells two — 超级会员 (svip) and 豪华会员 (tvip) — and
// an account may hold both at once, each ending on a day of its own.
type Product struct {
	Type string // svip, tvip; the business line's own name for it
	End  string // "2006-01-02 15:04:05"
}

// Name is what the app calls a product.
func (p Product) Name() string {
	switch p.Type {
	case "svip":
		return "超级会员"
	case "tvip":
		return "豪华会员"
	}
	return "会员"
}

// Account is who is signed in.
type Account struct {
	Nickname string
	Avatar   string
	// VIPType is the subscription KuGou's account service reports for the
	// standard client, 0 for none.
	VIPType int
	VIPEnd  string
	// Concept is an active subscription on the 概念版's own business line.
	// It is sold apart from the standard one — the account's "is_vip" can
	// read 0 while this is in force — and it is what the app's 概念版 mode
	// is for.
	Concept    bool
	ConceptEnd string
	// ConceptProducts are the 概念版's products held, in the order the
	// service lists them; ConceptEnd is the last day any of them runs.
	ConceptProducts []Product
}

// Subscriber reports whether the account holds any subscription.
func (a Account) Subscriber() bool { return a.VIPType != 0 || a.Concept }

// Label is what the badge beside the name says, in as few words as fit.
func (a Account) Label() string {
	switch {
	case a.Concept:
		return "概念版VIP"
	case a.VIPType != 0:
		return "VIP"
	}
	return ""
}

// Profile returns who is signed in: the name, the picture and the
// subscription the account service states.
func Profile(c *Client) (Account, error) {
	var out Account
	if c.Token == "" {
		return out, ErrNoSession
	}
	ms := nowMS()
	// The account service checks "p" the way rsaEncrypt2 builds it, with
	// PKCS#1 padding. The login services' raw form is refused here, and it
	// is why the name and the picture were missing.
	p, err := c.rsaSealPKCS1Key([]byte(fmt.Sprintf(`{"clienttime":%d,"token":%s}`, ms, quote(c.Token))))
	if err != nil {
		return out, err
	}
	var res struct {
		Data map[string]any `json:"data"`
	}
	err = c.send("POST", request{
		Router: userRouter,
		Path:   "/v1/get_my_userinfo",
		Body: map[string]any{
			"p": p, "appid": c.appID(), "mid": c.Dev.MID, "clientver": c.clientVer(),
			"source": 0, "clienttime": ms, "uuid": "-", "userid": c.UserID,
			"key": c.signKey(strconv.FormatInt(ms, 10)),
		},
	}, &res)
	if err != nil {
		return out, err
	}
	out.Nickname = pickString(res.Data, "nickname", "nick_name", "username")
	out.Avatar = pickString(res.Data, "pic", "user_pic", "pic_300", "img")
	if out.Avatar != "" {
		out.Avatar = sized(out.Avatar)
	}
	// This service reports the standard subscription only, and thinly: a
	// number, and the day it runs out.
	out.VIPType = int(pickInt(res.Data, "vip_type", "viptype", "vipType"))
	out.VIPEnd = pickString(res.Data, "vip_end_time", "vip_endtime", "vip_expire_time", "vip_end_date")
	return out, nil
}

// unionVIP is the subscription service's answer: the standard line at the
// top, and every business line the account holds under busi_vip.
type unionVIP struct {
	Data struct {
		IsVIP   int    `json:"is_vip"`
		VIPType int    `json:"vip_type"`
		VIPEnd  string `json:"vip_end_time"`
		SVIPEnd string `json:"su_vip_end_time"`
		BusyVIP []struct {
			IsVIP       int    `json:"is_vip"`
			BusiType    string `json:"busi_type"`
			ProductType string `json:"product_type"`
			VIPEnd      string `json:"vip_end_time"`
		} `json:"busi_vip"`
	} `json:"data"`
}

// UnionVIP asks the subscription service what the account holds. The account
// service does not carry the 概念版's own subscription at all, so this is the
// only place the app can learn that a listener pays for it.
func UnionVIP(c *Client) (Account, error) {
	var out Account
	if c.Token == "" {
		return out, ErrNoSession
	}
	var res unionVIP
	// The service answers about the 概念版's business line only to a client
	// that is one, and it lists the products asked for. The standard client
	// asks plainly and is refused with "验证码错误或已过期", which is this
	// endpoint's way of saying the token is not one of its own.
	params := map[string]string{"busi_type": "concept"}
	if c.Lite {
		params["opt_product_types"] = "svip,tvip,dvip,qvip"
	}
	err := c.send("GET", request{
		Base:   "https://kugouvip.kugou.com",
		Path:   "/v1/get_union_vip",
		Params: params,
	}, &res)
	if err != nil {
		return out, err
	}
	d := res.Data
	if d.IsVIP != 0 || d.VIPType != 0 {
		out.VIPType = d.VIPType
		if out.VIPType == 0 {
			out.VIPType = 1
		}
		out.VIPEnd = firstFilled(d.VIPEnd, d.SVIPEnd)
	}
	// Every line in busi_vip is a product of the business asked about; the
	// ones that count are those paid for and not yet over, each ending on
	// a day of its own.
	for _, b := range d.BusyVIP {
		if b.BusiType != "" && b.BusiType != "concept" {
			continue
		}
		if b.IsVIP == 0 || !after(b.VIPEnd) {
			continue
		}
		out.Concept = true
		if b.VIPEnd > out.ConceptEnd {
			out.ConceptEnd = b.VIPEnd
		}
		if b.ProductType != "" {
			out.ConceptProducts = append(out.ConceptProducts, Product{Type: b.ProductType, End: b.VIPEnd})
		}
	}
	// The service also states a 概念版 end at the top, for an account whose
	// busi_vip list came empty.
	if !out.Concept && d.IsVIP != 0 && after(d.VIPEnd) {
		out.Concept = true
		out.ConceptEnd = d.VIPEnd
	}
	return out, nil
}

// after reports whether a date the service writes as "2006-01-02 15:04:05"
// has not passed. A date written some other way is taken as still running:
// the service sets is_vip for a reason, and guessing otherwise would hide a
// subscription the listener is paying for.
func after(stamp string) bool {
	if stamp == "" {
		return false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", stamp, time.Local)
	if err != nil {
		t, err = time.ParseInLocation("2006-01-02", stamp, time.Local)
	}
	if err != nil {
		return true
	}
	return t.After(time.Now())
}

func firstFilled(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
