package kugou

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestAccountProbe tries the account service the few ways it may want to be
// asked, to find which one it accepts. KG_TOKEN and KG_UID name the account.
func TestAccountProbe(t *testing.T) {
	token, uid := os.Getenv("KG_TOKEN"), os.Getenv("KG_UID")
	if token == "" {
		t.Skip("set KG_TOKEN and KG_UID to ask about an account")
	}
	c, err := New(os.Getenv("KG_CACHE"))
	if err != nil {
		t.Fatal(err)
	}
	c.SetUser(atoiSafe(uid), token)

	ms := nowMS()
	raw := func(name string, r call) {
		if r.Body != "" {
			r.Type = "application/json"
		}
		if r.Body != "" {
			r.Type = "application/json"
		}
		out, err := c.do("POST", r)
		if err != nil {
			fmt.Printf("%-14s err %v\n", name, err)
			return
		}
		var body map[string]any
		json.Unmarshal(out, &body)
		keys := ""
		if d, ok := body["data"].(map[string]any); ok {
			for k := range d {
				keys += k + " "
			}
			if len(keys) > 160 {
				keys = keys[:160]
			}
		}
		fmt.Printf("%-14s status=%v code=%v data=[%s]\n", name, body["status"], body["error_code"], keys)
		fmt.Printf("%-14s raw %.160s\n", "", out)
	}

	p, _ := c.rsaSealRawKey([]byte(fmt.Sprintf(`{"clienttime":%d,"token":%s}`, ms, quote(token))))
	pkcs, _ := rsaSealPKCS1([]byte(fmt.Sprintf(`{"clienttime":%d,"token":%s}`, ms, quote(token))))
	body := func(pv string) string {
		b, _ := json.Marshal(map[string]any{
			"p": pv, "appid": appID, "mid": c.Dev.MID, "clientver": clientVer,
			"source": 0, "clienttime": ms, "uuid": "-", "userid": c.UserID,
			"key": signParamsKey(fmtInt(ms)),
		})
		return string(b)
	}

	raw("signed", call{Base: userHost, Path: "/v1/get_my_userinfo", Body: body(p)})
	raw("unsigned", call{Base: userHost, Path: "/v1/get_my_userinfo", Body: body(p), NoSign: true})
	raw("pkcs1", call{Base: userHost, Path: "/v1/get_my_userinfo", Body: body(pkcs)})
	noKey, _ := json.Marshal(map[string]any{
		"p": p, "appid": appID, "mid": c.Dev.MID, "clientver": clientVer,
		"source": 0, "clienttime": ms, "uuid": "-", "userid": c.UserID,
	})
	raw("no-key", call{Base: userHost, Path: "/v1/get_my_userinfo", Body: string(noKey)})
	// The same question over the gateway, which is how most services want it.
	raw("gateway", call{Router: "usercenter.kugou.com", Path: "/v1/get_my_userinfo", Body: body(p)})
}

func atoiSafe(s string) int64 {
	var n int64
	fmt.Sscan(s, &n)
	return n
}

func fmtInt(n int64) string {
	return fmt.Sprintf("%d", n)
}
