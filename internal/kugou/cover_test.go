package kugou

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// drawPNG is a picture of w by h, in the format that carries its size.
func drawPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A picture smaller than the service keeps is scaled up to its side, and one
// it keeps goes up as it came.
func TestCoverFitScalesTheSmallPicture(t *testing.T) {
	small, ext := coverFit(drawPNG(t, 100, 60), ".png")
	if ext != ".jpg" {
		t.Errorf("the small picture goes up as %q", ext)
	}
	img, err := jpeg.Decode(bytes.NewReader(small))
	if err != nil {
		t.Fatalf("the scaled picture does not decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 800 || b.Dy() != 480 {
		t.Errorf("the scaled picture is %dx%d, want 800x480", b.Dx(), b.Dy())
	}

	big := drawPNG(t, 600, 600)
	same, ext := coverFit(big, ".png")
	if ext != ".png" || !bytes.Equal(same, big) {
		t.Errorf("a picture the service keeps was changed to %q", ext)
	}
	// Something that is not a picture at all is sent as it is, for the
	// service to say what it makes of it.
	junk := []byte("not a picture")
	if out, _ := coverFit(junk, ".bin"); !bytes.Equal(out, junk) {
		t.Error("a file that does not decode was changed")
	}
}

// The upload names the day it was made on, and answers what the service
// stored the picture as.
func TestUploadCoverNamesTheFile(t *testing.T) {
	var asked string
	old := coverUploadHost
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.RawQuery
		if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("sent as %q", got)
		}
		body := make([]byte, 1)
		r.Body.Read(body)
		json.NewEncoder(w).Encode(map[string]any{"IsSuccess": true, "FileName": "20261118/pic.jpg"})
	}))
	defer srv.Close()
	coverUploadHost = srv.URL
	defer func() { coverUploadHost = old }()

	name, err := UploadCover(&Client{HTTP: srv.Client()}, drawPNG(t, 500, 500), "png")
	if err != nil {
		t.Fatal(err)
	}
	if name != "20261118/pic.jpg" {
		t.Errorf("stored as %q", name)
	}
	day := time.Now().Format("20060102")
	for _, want := range []string{"type=custom", "extendName=.png", "jsonResponse=1", "md5=" + md5Hex(day+coverUploadSalt)} {
		if !bytes.Contains([]byte(asked), []byte(want)) {
			t.Errorf("the upload asks %q, without %q", asked, want)
		}
	}
}

// A refused picture says why, and no picture at all says that.
func TestUploadCoverRefusesWhatTheServiceRefuses(t *testing.T) {
	old := coverUploadHost
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"IsSuccess": false, "Message": "Bad width"})
	}))
	defer srv.Close()
	coverUploadHost = srv.URL
	defer func() { coverUploadHost = old }()

	c := &Client{HTTP: srv.Client()}
	if _, err := UploadCover(c, drawPNG(t, 500, 500), "png"); err == nil {
		t.Fatal("a refused picture was taken")
	} else if got := err.Error(); !bytes.Contains([]byte(got), []byte("Bad width")) {
		t.Errorf("the refusal reads %q", got)
	}
	if _, err := UploadCover(c, nil, "png"); err == nil {
		t.Error("an empty picture was taken")
	}
}
