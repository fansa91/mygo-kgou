package kugou

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// A playlist's own cover: the service takes a picture from its image host,
// keeps it, and names it by a path a list may then point at. The picture
// goes up here, unsealed and unsigned — it is not the gateway's service, and
// knows nothing of devices.

// coverUploadHost is where a picture is uploaded; a test points it at a
// server of its own.
var coverUploadHost = "http://imgphp.kugou.com/imageupload/stream.php"

// coverUploadSalt is the salt the phone's upload signs the day with.
const coverUploadSalt = "hewry678WEK23D"

// coverMin is the shortest side the picture service keeps: it refuses a
// narrower one for its width and a squatter one for its height, so a
// smaller picture is scaled up before it goes.
const coverMin = 400

// coverSide is the side a picture smaller than that is scaled to, which is
// the size the app shows a list's cover in.
const coverSide = 480

// UploadCover puts a picture on KuGou's image host and returns the name it
// keeps it under, which SetListCover takes. ext is the file's extension,
// with or without its dot.
func UploadCover(c *Client, data []byte, ext string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("kugou: the picture is empty")
	}
	data, ext = coverFit(data, ext)
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		ext = ".jpg"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	// The phone names the picture by the day it uploaded it.
	day := time.Now().Format("20060102")
	q := url.Values{
		"type":         {"custom"},
		"extendName":   {ext},
		"md5":          {md5Hex(day + coverUploadSalt)},
		"jsonResponse": {"1"},
	}
	req, err := http.NewRequest("POST", coverUploadHost+"?"+q.Encode(), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	var out struct {
		IsSuccess bool   `json:"IsSuccess"`
		FileName  string `json:"FileName"`
		Message   string `json:"Message"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("kugou: the picture service answered %q", snippet(raw))
	}
	if !out.IsSuccess || out.FileName == "" {
		if out.Message != "" {
			return "", fmt.Errorf("kugou: %s", out.Message)
		}
		return "", fmt.Errorf("kugou: the picture was refused")
	}
	return out.FileName, nil
}

// coverFit scales a picture up to the side the service keeps, keeping what
// it shows, and answers it as a JPEG. A picture the service keeps as it is
// goes up as it came.
func coverFit(data []byte, ext string) ([]byte, string) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, ext
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w >= coverMin && h >= coverMin {
		return data, ext
	}
	short := min(w, h)
	if short <= 0 {
		return data, ext
	}
	scale := float64(coverSide) / float64(short)
	dst := image.NewRGBA(image.Rect(0, 0, int(float64(w)*scale+0.5), int(float64(h)*scale+0.5)))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 92}); err != nil {
		return data, ext
	}
	return out.Bytes(), ".jpg"
}
