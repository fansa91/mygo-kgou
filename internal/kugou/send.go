package kugou

import (
	"encoding/json"
	"errors"
	"time"
)

// request is a call to one of KuGou's services, as the pages of the app make
// it: the device's parameters are added, a body is encoded and signed, and the
// answer is decoded into out.
type request struct {
	Base   string // where to send it; the gateway unless a service lives elsewhere
	Router string // the service behind the gateway, for x-router
	Path   string
	Params map[string]string
	Body   any               // encoded as JSON when set
	Hdr    map[string]string // headers the service itself wants
	Web    bool              // sign as the H5 client rather than the Android one
}

// send makes the request and decodes the answer.
func (c *Client) send(method string, q request, out any) error {
	r := call{Base: q.Base, Router: q.Router, Path: q.Path, Params: q.Params,
		Hdr: q.Hdr, Web: q.Web}
	if q.Body != nil {
		b, err := json.Marshal(q.Body)
		if err != nil {
			return err
		}
		r.Body, r.Type = string(b), "application/json"
	}
	return c.decode(method, r, out)
}

// retry sends a call again when the service answers "status 0" with nothing
// to say: the search services do that when asked too often, and answer
// properly a moment later.
func (c *Client) retry(method string, r call) ([]byte, error) {
	var raw []byte
	var err error
	for _, wait := range []time.Duration{0, 400 * time.Millisecond, time.Second, 2 * time.Second} {
		if wait > 0 {
			time.Sleep(wait)
		}
		raw, err = c.do(method, r)
		var e *Error
		if err == nil || !errors.As(err, &e) || e.Code != 0 || e.Msg != "" || e.HTTP != 0 {
			return raw, err
		}
	}
	return raw, err
}

func now() int64 { return time.Now().Unix() }

func nowMS() int64 { return time.Now().UnixMilli() }

// SendWeb sends a request signed the way the H5 client signs, to a service
// that lives on a host of its own rather than behind the gateway. The login
// services are the ones that do.
func (c *Client) SendWeb(method, base, path string, params map[string]string, body, out any) error {
	r := call{Base: base, Path: path, Params: params, Web: true}
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r.Body, r.Type = string(b), "application/json"
	}
	return c.decode(method, r, out)
}
