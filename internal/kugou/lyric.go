package kugou

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// Lyrics are a song's words: the plain timed lyrics, and the word-timed ones
// (KRC) the phone itself scrolls by, which this app draws as its "律动" view.
//
// The KRC carries the translation and the pronunciation of the lines in a
// tag of its own; the app reads them from there, in the lyrics package.
type Lyric struct {
	LRC string
	KRC string
}

const lyricsHost = "https://lyrics.kugou.com"

// Lyrics finds a song's words. KuGou keeps them apart from the catalogue: a
// search by hash names them, then they are downloaded by that name and key.
func Lyrics(c *Client, hash string, albumAudioID int64) (*Lyric, error) {
	raw, err := c.do("GET", call{
		Base: lyricsHost,
		Path: "/v1/search",
		// Signed over its own parameters alone: this service knows nothing of
		// the gateway's device parameters, but does check the signature.
		Bare: true,
		Params: map[string]string{
			"album_audio_id": strconv.FormatInt(albumAudioID, 10),
			"appid":          c.appID(),
			"clientver":      c.clientVer(),
			"duration":       "0",
			"hash":           hash,
			"keyword":        "",
			"lrctxt":         "1",
			"man":            "no",
		},
	})
	if err != nil {
		return nil, err
	}
	var found struct {
		Candidates []struct {
			ID        string `json:"id"`
			AccessKey string `json:"accesskey"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &found); err != nil {
		return nil, err
	}
	if len(found.Candidates) == 0 {
		return nil, errors.New("kugou: no lyrics for this song")
	}
	id := found.Candidates[0].ID
	key := found.Candidates[0].AccessKey

	out := &Lyric{}
	if lrc, err := download(c, id, key, "lrc"); err == nil {
		out.LRC = lrc
	}
	if krc, err := download(c, id, key, "krc"); err == nil {
		out.KRC = krc
	}
	if out.LRC == "" && out.KRC == "" {
		return nil, errors.New("kugou: no lyrics for this song")
	}
	return out, nil
}

// download fetches one of a song's lyric files, decoding the KRC the phone
// keeps compressed.
func download(c *Client, id, key, format string) (string, error) {
	raw, err := c.do("GET", call{
		Base: lyricsHost,
		Path: "/download",
		Params: map[string]string{
			"ver":       "1",
			"client":    "android",
			"id":        id,
			"accesskey": key,
			"fmt":       format,
			"charset":   "utf8",
		},
	})
	if err != nil {
		return "", err
	}
	var res struct {
		Content     string `json:"content"`
		ContentType int    `json:"contenttype"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	if res.Content == "" {
		return "", errors.New("kugou: the lyric file is empty")
	}
	// A plain LRC comes base64-encoded; a KRC only when its content type says
	// so, and otherwise it is the covered, deflated file.
	if format == "lrc" || res.ContentType != 0 {
		plain, err := base64.StdEncoding.DecodeString(strings.TrimSpace(res.Content))
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}
	return DecodeKRC(res.Content)
}
