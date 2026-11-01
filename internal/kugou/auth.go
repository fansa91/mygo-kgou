package kugou

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Signing in by text message: KuGou texts a code to the number, and the code
// and the number are traded for a token every other call then carries. The
// body of the login is AES under a key of this run's own, and the key itself
// travels RSA-encrypted, so the code is never on the wire in the clear.

// The services that text the code and take it, and the app they believe is
// asking: the login service answers only the phone client's user agent.
//
// Two of them are reached over plain HTTP on purpose. Their names are three
// labels deep, and KuGou's certificate covers "*.kugou.com" alone, so TLS to
// them fails on the name; the reference client sends to the same two hosts
// the same way. Nothing secret travels there: a number, and a request.
// The two keys the 概念版 seals its t1 and t2 with, which are fixed rather
// than made per run as the body's key is.
const (
	liteT1Key = "5e4ef500e9597fe004bd09a46d8add98"
	liteT1IV  = "04bd09a46d8add98"
	liteT2Key = "fd14b35e3f81af3817a20ae7adae7020"
	liteT2IV  = "17a20ae7adae7020"
)

const (
	smsHost   = "http://login.user.kugou.com"
	loginHost = "https://loginserviceretry.kugou.com"
	userHost   = "http://relation.user.kugou.com"
	// userRouter is the same service behind the HTTPS gateway. The plain
	// host answers 403 with an empty head to a desktop app's request — a
	// local network policy rather than KuGou's, since the same call succeeds
	// from a command-line build — and the gateway is how the name and the
	// picture are still read.
	userRouter = "relation.user.kugou.com"
	loginUA    = "Android16-1070-11440-130-0-LOGIN-wifi"
)

// Session is what a login hands back.
type Session struct {
	Token    string
	UserID   int64
	Nickname string
	Avatar   string
	// VIPType is the subscription KuGou says the account holds, as a number
	// it defines; 0 is no subscription. VIPToken is what the tracker wants
	// beside it before it serves a subscriber their file.
	VIPType  int
	VIPToken string
	// Lite says the login was made as the 概念版, which is a different
	// client with listening of its own.
	Lite bool
}

// ErrNoSession is what an account call answers before anyone signed in.
var ErrNoSession = errors.New("kugou: nobody is signed in")

// SendSMSCode asks KuGou to text a login code to mobile.
func SendSMSCode(c *Client, mobile string) error {
	if !isMobile(mobile) {
		return errors.New("kugou: that is not a phone number")
	}
	var res struct {
		Status int `json:"status"`
		Data   struct {
			// A code sent too often is refused with how long is left.
			Interval int    `json:"interval"`
			Msg      string `json:"msg"`
		} `json:"data"`
	}
	err := c.send("POST", request{
		Base: smsHost,
		Path: "/v7/send_mobile_code",
		Body: map[string]any{"businessid": 5, "mobile": mobile, "plat": 3},
	}, &res)
	if err != nil {
		return err
	}
	if res.Status != 1 {
		if res.Data.Msg != "" {
			return errors.New("kugou: " + res.Data.Msg)
		}
		return errors.New("kugou: the code was not sent")
	}
	return nil
}

// LoginByCode trades the number and the code texted to it for a token.
func LoginByCode(c *Client, mobile, code string) (*Session, error) {
	if !isMobile(mobile) {
		return nil, errors.New("kugou: that is not a phone number")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("kugou: the code is empty")
	}
	ms := nowMS()
	sec, err := sealHex([]byte(fmt.Sprintf(`{"mobile":%s,"code":%s}`, quote(mobile), quote(code))))
	if err != nil {
		return nil, err
	}
	pk, err := c.rsaSealRawKey([]byte(fmt.Sprintf(`{"clienttime_ms":%d,"key":%s}`, ms, quote(sec.Key))))
	if err != nil {
		return nil, err
	}
	var res struct {
		Status  int    `json:"status"`
		ErrCode int    `json:"error_code"`
		Error   string `json:"error"`
		Data    struct {
			SecuParams string `json:"secu_params"`
			Token      string `json:"token"`
			UserID     int64  `json:"userid"`
			Nickname   string `json:"nickname"`
			Pic        string `json:"user_pic"`
			VIPType    int    `json:"vip_type"`
			VIPToken   string `json:"vip_token"`
		} `json:"data"`
	}
	body := map[string]any{
		"plat": 1, "support_multi": 1, "t1": 0, "t2": 0,
		"clienttime_ms": ms,
		// The number goes masked; the code is in params.
		"mobile": maskMobile(mobile),
		"key":    c.signKey(strconv.FormatInt(ms, 10)),
		"pk":     pk,
		"params": sec.Body,
		"t3":     "MCwwLDAsMCwwLDAsMCwwLDA=",
	}
	if c.Lite {
		// The 概念版 names its device to the login service, and seals the
		// moment of asking under two keys of its own.
		body["dfid"] = c.Dev.DFID
		body["dev"] = c.Dev.Dev
		body["gitversion"] = "5f0b7c4"
		body["t1"] = sealFixed(fmt.Sprintf("|%d", ms), liteT1Key, liteT1IV)
		body["t2"] = sealFixed(fmt.Sprintf("%s|0f607264fc6318a92b9e13c65db7cd3c|%s|%s|%d",
			c.Dev.GUID, c.Dev.MAC, c.Dev.Dev, ms), liteT2Key, liteT2IV)
	}
	err = c.send("POST", request{
		Base: loginHost,
		Path: "/v7/login_by_verifycode",
		Hdr:  map[string]string{"support-calm": "1", "User-Agent": loginUA},
		Body: body,
	}, &res)
	if err != nil {
		return nil, err
	}
	if res.Status != 1 {
		if res.Error != "" {
			return nil, errors.New("kugou: " + res.Error)
		}
		if msg := codeText[res.ErrCode]; msg != "" {
			return nil, errors.New("kugou: " + msg)
		}
		return nil, fmt.Errorf("kugou: the login was refused (%d)", res.ErrCode)
	}
	s := &Session{Token: res.Data.Token, UserID: res.Data.UserID, Nickname: res.Data.Nickname,
		Avatar: res.Data.Pic, VIPType: res.Data.VIPType, VIPToken: res.Data.VIPToken, Lite: c.Lite}
	// What the token is worth is sealed under the key this run made.
	if res.Data.SecuParams != "" {
		if plain, err := openHex(res.Data.SecuParams, sec.Key); err == nil {
			readSecrets(s, plain)
		}
	}
	if s.Token == "" {
		return nil, errors.New("kugou: the login answered without a token")
	}
	c.SetUser(s.UserID, s.Token)
	// The nickname, the picture and the subscription are not in the
	// login's answer; the account service has them.
	if acc, err := Profile(c); err == nil {
		if acc.Nickname != "" {
			s.Nickname = acc.Nickname
		}
		if acc.Avatar != "" {
			s.Avatar = acc.Avatar
		}
		if acc.VIPType != 0 {
			s.VIPType = acc.VIPType
		}
		if s.VIPType != 0 || acc.Concept {
			c.SetVIP(s.VIPType, s.VIPToken)
		}
	}
	// An account that will not say its name is still the one that signed
	// in, and the number is what the listener knows it by.
	if s.Nickname == "" {
		s.Nickname = maskMobile(mobile)
	}
	return s, nil
}

// readSecrets reads what the login sealed: an object holding the token and
// the account, or the token alone as a plain string.
func readSecrets(s *Session, plain []byte) {
	var obj map[string]any
	if json.Unmarshal(plain, &obj) == nil {
		if v := pickString(obj, "token", "Token"); v != "" {
			s.Token = v
		}
		if v := pickInt(obj, "userid", "user_id", "uid"); v != 0 {
			s.UserID = v
		}
		if v := pickString(obj, "nickname", "nick_name"); v != "" {
			s.Nickname = v
		}
		return
	}
	text := strings.Trim(strings.TrimSpace(string(plain)), `"`)
	if text != "" && !strings.ContainsAny(text, "{}\n") {
		s.Token = text
	}
}

// SignedIn reports whether the client carries a session.
func (c *Client) SignedIn() bool { return c.Token != "" }

func isMobile(s string) bool {
	if len(s) != 11 || s[0] != '1' {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// maskMobile is how the login sends the number: its head, and its last digit.
func maskMobile(s string) string {
	if len(s) != 11 {
		return s
	}
	return s[:2] + "*****" + s[10:11]
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
