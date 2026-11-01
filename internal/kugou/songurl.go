package kugou

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Stream is where a song streams from, and what the file is.
type Stream struct {
	URL     string
	Type    string // mp3 or flac
	BitRate int    // kbps
	Size    int64  // bytes
	Level   string // the level served, which may be below the one asked for
}

// ErrUnavailable is a song KuGou will not stream to this listener: it is sold,
// the listener has no subscription, or the quality was never uploaded.
var ErrUnavailable = errors.New("kugou: this song isn't available to stream")

// Levels are the levels this app plays, from the lowest.
var Levels = []string{"standard", "higher", "exhigh", "lossless", "hires"}

// quality is what KuGou calls each level.
var quality = map[string]string{
	"standard": "128",
	"higher":   "320",
	"exhigh":   "320",
	"lossless": "flac",
	"hires":    "high",
}

// Lossless reports whether level streams FLAC.
func Lossless(level string) bool { return level == "lossless" || level == "hires" }

// URL returns where to stream a song at level (one of Levels), or the best
// below it this listener may have. KuGou sells songs whole: what comes back is
// either the file or nothing, and a preview is no song.
func URL(c *Client, t *Track, level string) (*Stream, error) {
	at := 0
	for i, l := range Levels {
		if l == level {
			at = i
		}
	}
	var last error
	for i := at; i >= 0; i-- {
		st, err := stream(c, t, Levels[i])
		if err == nil {
			return st, nil
		}
		last = err
	}
	if last == nil {
		last = ErrUnavailable
	}
	return nil, last
}

func stream(c *Client, t *Track, level string) (*Stream, error) {
	if t.Hash == "" {
		return nil, ErrUnavailable
	}
	if err := c.ensureDFID(); err != nil {
		return nil, err
	}
	// The player's URL endpoint takes this device's registered ID, and makes
	// one up only when the device never registered.
	dfid := c.Dev.DFID
	if dfid == "" {
		dfid = randomKey(24)
	}
	q, ok := quality[level]
	if !ok {
		q = level
	}
	params := map[string]string{
		"album_id":       strconv.FormatInt(t.AlbumID, 10),
		"area_code":      "1",
		"hash":           t.HashFor(level),
		"ssa_flag":       "is_fromtrack",
		"version":        "11430",
		"page_id":        "151369488",
		"quality":        q,
		"album_audio_id": strconv.FormatInt(t.AlbumAudioID, 10),
		"behavior":       "play",
		"pid":            "2",
		"cmd":            "26",
		"pidversion":     "3001",
		"IsFreePart":     "0",
		"ppage_id":       "463467626,350369493,788954147",
		"cdnBackup":      "1",
		"module":         "",
		"clientver":      "11430",
	}
	if c.Lite {
		// The 概念版 asks as itself: its own page and player IDs. Its
		// player is handed the ogg file of the quality it asked for, which
		// it names with need_ogg; an MP3 is served too, and this app
		// prefers it, as only the ogg is a format its decoder had to learn.
		params["pid"] = "411"
		params["page_id"] = "967177915"
		params["ppage_id"] = "356753938,823673182,967485191"
	}
	st, err := ask(c, params, dfid, t, level)
	if err != nil && c.Lite {
		// A song the 概念版 sells is served at standard quality as ogg,
		// which is what its player really streams; that is what is left
		// when the MP3 is refused.
		params["need_ogg"] = "1"
		if ogg, oggErr := ask(c, params, dfid, t, level); oggErr == nil {
			return ogg, nil
		}
	}
	return st, err
}

// ask puts one request to the player's URL service, and reads the answer.
func ask(c *Client, params map[string]string, dfid string, t *Track, level string) (*Stream, error) {
	raw, err := c.do("GET", call{
		Router: "trackercdn.kugou.com",
		Path:   "/v5/url",
		Params: params,
		Key:    true,
		DFID:   dfid,
	})
	if err != nil {
		return nil, err
	}
	// The tracker answers flat: the file itself, or why there is none.
	var res struct {
		URL        []string `json:"url"` // the CDN in front of the listener, then mirrors
		ExtName    string   `json:"extName"`
		BitRate    int      `json:"bitRate"` // bits per second
		FileSize   int64    `json:"fileSize"`
		TimeLength int64    `json:"timeLength"` // seconds served
		Status     int      `json:"status"`
		Msg        string   `json:"msg"`
		// HashOffset is present for a song the listener may only hear a part
		// of: how far in that part runs.
		HashOffset *struct {
			EndMS int64 `json:"end_ms"`
		} `json:"hash_offset"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	if len(res.URL) == 0 || res.URL[0] == "" {
		if res.Msg != "" {
			return nil, errors.New("kugou: " + res.Msg)
		}
		return nil, fmt.Errorf("%w (answer %s)", ErrUnavailable, snippet(raw))
	}
	// A preview of a sold song is no song: the server trims it, and says how.
	if off := res.HashOffset; off != nil && t.Duration > 0 && off.EndMS > 0 && off.EndMS < t.Duration-2000 {
		return nil, ErrUnavailable
	}
	st := &Stream{
		URL:     strings.Replace(res.URL[0], "http://", "https://", 1),
		BitRate: res.BitRate / 1000,
		Size:    res.FileSize,
		Level:   level,
		Type:    strings.TrimPrefix(strings.ToLower(res.ExtName), "."),
	}
	if st.Type == "" {
		if Lossless(level) {
			st.Type = "flac"
		} else {
			st.Type = "mp3"
		}
	}
	return st, nil
}
