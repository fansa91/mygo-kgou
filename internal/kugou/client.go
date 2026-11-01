package kugou

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// KuGou's gateway: every request goes to one host, and the x-router header
// names the service behind it. The parameters and the signature are the
// client's own, ported from KuGouMusicApi's util/request.js.

const (
	Gateway   = "https://gateway.kugou.com"
	appID     = "1005"
	clientVer = "20489"
	// The 概念版 (lite) is KuGou's other client: another app ID, another
	// build, and its own salt, so everything it sends is signed as it and
	// not as the standard player. It is the client that may hear a sold
	// song's standard quality without a subscription.
	liteAppID     = "3116"
	liteClientVer = "11440"
	UserAgent     = "Android15-1070-11083-46-0-DiscoveryDRADProtocol-wifi"
)

// Client speaks to KuGou as a signed-in Android player does.
type Client struct {
	HTTP *http.Client
	Dir  string

	Dev    device
	Token  string
	UserID int64
	// VIPType and VIPToken are the subscription the account holds, as the
	// login or the VIP service stated it. The tracker serves a subscriber
	// their file only when both come along.
	VIPType  int
	VIPToken string
	// Lite is whether this client presents itself as the 概念版.
	Lite bool
}

// appID is the app this client signs as.
func (c *Client) appID() string {
	if c.Lite {
		return liteAppID
	}
	return appID
}

// AppID is the app this client signs as, for the callers outside this
// package: the login services take it as a parameter of their own, and a
// token is only good for the appid that asked for it.
func (c *Client) AppID() string { return c.appID() }

// clientVer is the build this client signs as.
func (c *Client) clientVer() string {
	if c.Lite {
		return liteClientVer
	}
	return clientVer
}

// salt is the signing salt of the build this client signs as.
func (c *Client) salt() string {
	if c.Lite {
		return saltAndroidLite
	}
	return saltAndroid
}

// keySalt is the "key" salt of the build this client signs as.
func (c *Client) keySalt() string {
	if c.Lite {
		return saltSignKeyLite
	}
	return saltSignKey
}

// signKey is signParamsKey as this build sends it.
func (c *Client) signKey(data string) string {
	return signParamsKeyFor(c.appID(), c.clientVer(), c.salt(), data)
}

// New loads the device this install presents, making one on first run, from
// dir (the app's user data directory).
func New(dir string) (*Client, error) {
	d, err := loadDevice(dir)
	if err != nil {
		return nil, err
	}
	return &Client{
		HTTP: &http.Client{Timeout: 20 * time.Second},
		Dir:  dir,
		Dev:  d,
	}, nil
}

// call is one request to KuGou.
type call struct {
	Base   string            // where to send it; Gateway by default
	Path   string            // the route, as "/v2/search/song"
	Router string            // the service behind the gateway, for x-router
	Params map[string]string // what the endpoint itself wants
	Body   string            // a POST body, already encoded
	Type   string            // the body's content type
	Raw    bool              // answer the bytes, whatever they hold
	Web    bool              // sign as the H5 client rather than the Android one
	Key    bool              // add the "key" parameter the player's URLs want
	DFID   string            // a device ID this call makes up
	Bare   bool              // send only what the endpoint asks for, no device
	NoSign bool              // leave the signature off
	Hdr    map[string]string // headers the endpoint itself wants, as "kg-tid"
}

// Error is KuGou refusing or failing a request.
type Error struct {
	Path string
	HTTP int
	Code int
	Msg  string
}

func (e *Error) Error() string {
	if e.HTTP != 0 && e.HTTP != http.StatusOK {
		return fmt.Sprintf("kugou: %s: HTTP %d", e.Path, e.HTTP)
	}
	if e.Msg != "" {
		return "kugou: " + e.Msg
	}
	return fmt.Sprintf("kugou: %s: status %d", e.Path, e.Code)
}

// codeText are the refusals KuGou gives by number alone, without a word to
// go with them. The login services are the ones that do.
var codeText = map[int]string{
	20006: "验证码错误或已过期",
	20007: "这个手机号还没有酷狗账号",
	20010: "操作太频繁，请稍后再试",
}

// do sends one call and returns the body's bytes, or an *Error if KuGou said no.
func (c *Client) do(method string, r call) ([]byte, error) {
	params := map[string]string{}
	if !r.Bare {
		params = map[string]string{
			"dfid":       c.Dev.DFID,
			"mid":        c.Dev.MID,
			"uuid":       "-",
			"appid":      c.appID(),
			"clientver":  c.clientVer(),
			"clienttime": strconv.FormatInt(time.Now().Unix(), 10),
		}
		if r.DFID != "" {
			params["dfid"] = r.DFID
		}
		if c.Token != "" {
			params["token"] = c.Token
		}
		if c.UserID != 0 {
			params["userid"] = strconv.FormatInt(c.UserID, 10)
		}
	}
	for k, v := range r.Params {
		params[k] = v
	}
	if r.Key {
		userid, ok := params["userid"]
		if !ok {
			userid = "0"
		}
		params["key"] = keyForWith(params["hash"], params["mid"], userid, params["appid"], c.keySalt())
	}
	// Every call is signed, as the client's is: it signs whatever it sends,
	// bodies included. The lyrics search is the exception, and asks for it.
	if !r.NoSign {
		if r.Web {
			params["signature"] = signWeb(params, r.Body)
		} else {
			params["signature"] = signAndroidWith(params, r.Body, c.salt())
		}
	}

	base := r.Base
	if base == "" {
		base = Gateway
	}
	query := url.Values{}
	for k, v := range params {
		query.Set(k, v)
	}
	target := base + r.Path + "?" + query.Encode()

	var body io.Reader
	if r.Body != "" {
		body = strings.NewReader(r.Body)
	}
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("dfid", header(params, "dfid", c.Dev.DFID))
	req.Header.Set("mid", header(params, "mid", c.Dev.MID))
	req.Header.Set("clienttime", header(params, "clienttime", strconv.FormatInt(time.Now().Unix(), 10)))
	req.Header.Set("kg-rc", "1")
	req.Header.Set("kg-thash", "5d816a0")
	req.Header.Set("kg-rec", "1")
	req.Header.Set("kg-rf", "B9EDA08A64250DEFFBCADDEE00F8F25F")
	if r.Router != "" {
		req.Header.Set("x-router", r.Router)
	}
	for k, v := range r.Hdr {
		req.Header.Set(k, v)
	}
	if r.Body != "" {
		ct := r.Type
		if ct == "" {
			ct = "application/x-www-form-urlencoded"
		}
		req.Header.Set("Content-Type", ct)
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	// KG_DEBUG shows what went out and what came back, for the calls that
	// answer "status 0" and nothing else.
	if os.Getenv("KG_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "%s %s\n-> %s\n<- %d %s\n", method, target, r.Body, res.StatusCode, raw)
		if res.StatusCode >= 400 {
			fmt.Fprintf(os.Stderr, "   head %v\n   sent %v\n", res.Header, req.Header)
		}
	}
	if r.Raw {
		if res.StatusCode != http.StatusOK {
			return nil, &Error{Path: r.Path, HTTP: res.StatusCode}
		}
		return raw, nil
	}
	return raw, failure(r.Path, res.StatusCode, raw)
}

// failure reports why KuGou's answer is not a success. Its services say
// "status: 0" or a non-zero "error_code", and the rest of the body is data.
// A refusal carries its reason in error_code, and the login services send
// nothing else: status 0 beside error_code 20006 is a wrong code, not a
// silent failure.
func failure(path string, status int, raw []byte) error {
	if status != http.StatusOK {
		return &Error{Path: path, HTTP: status}
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return &Error{Path: path, HTTP: status, Msg: "the answer is not JSON"}
	}
	refused := false
	if v, ok := body["status"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil && n == 0 {
			refused = true
		}
	}
	code := 0
	if v, ok := body["error_code"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil && n != 0 {
			refused, code = true, n
		}
	}
	if refused {
		return &Error{Path: path, HTTP: status, Code: code, Msg: refuse(body, code)}
	}
	return nil
}

// refuse says why KuGou said no: in its own words when it gave any, and in
// the ones learned from it when it did not.
func refuse(body map[string]json.RawMessage, code int) string {
	if msg := message(body); msg != "" {
		return msg
	}
	return codeText[code]
}

func message(body map[string]json.RawMessage) string {
	for _, k := range []string{"error", "msg", "message"} {
		if v, ok := body[k]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil && s != "" {
				return s
			}
		}
	}
	// The login services put the complaint where the data would be.
	if v, ok := body["data"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			return s
		}
	}
	return ""
}

// header is a value from the request's parameters, or the device's own when
// the request sent none.
func header(params map[string]string, key, fallback string) string {
	if v := params[key]; v != "" {
		return v
	}
	return fallback
}

// get sends a signed GET and decodes the answer into out.
func (c *Client) get(r call, out any) error {
	return c.decode("GET", r, out)
}

func (c *Client) decode(method string, r call, out any) error {
	raw, err := c.do(method, r)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("kugou: %s: unexpected answer: %w", r.Path, err)
	}
	return nil
}
