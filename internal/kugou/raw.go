package kugou

import "encoding/json"

// RawGet sends one signed GET and answers it untouched, for the endpoints this
// package has no types for yet, and for the CLI to see what KuGou sends.
func RawGet(c *Client, router, path string, params map[string]string) ([]byte, error) {
	return c.do("GET", call{Router: router, Path: path, Params: params})
}

// RawPost sends one signed POST with body as JSON, and answers it untouched,
// for the same reason RawGet does.
func RawPost(c *Client, router, path string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return c.do("POST", call{Router: router, Path: path, Body: string(b), Type: "application/json"})
}
