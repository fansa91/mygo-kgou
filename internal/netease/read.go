package netease

import (
	"strconv"
	"strings"
	"time"

	"netease-music-mygo/internal/kugou"
)

// KuGou's services answer in maps of loosely typed fields, with the same
// thing under different names and types in different answers. These read
// them the same way however they come.

func pickString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case float64:
			return strconv.FormatInt(int64(v), 10)
		}
	}
	return ""
}

func pickInt(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return int64(v)
		case string:
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				return n
			}
		}
	}
	return 0
}

// clean strips the highlighting a search wraps its matches in, and .mp3 suffixes.
func clean(s string) string {
	s = strings.ReplaceAll(s, "<em>", "")
	s = strings.ReplaceAll(s, "</em>", "")
	s = strings.TrimSpace(s)
	// Remove .mp3 suffix (case-insensitive)
	if strings.HasSuffix(strings.ToLower(s), ".mp3") {
		s = s[:len(s)-4]
	}
	return s
}

// sized asks the image CDN for a picture big enough to show.
func sized(pic string) string {
	if pic == "" {
		return ""
	}
	pic = strings.ReplaceAll(pic, "{size}", "480")
	if i := strings.Index(pic, "stdmusic//"); i >= 0 {
		pic = pic[:i] + "stdmusic/480/" + pic[i+len("stdmusic//"):]
	}
	return strings.Replace(pic, "http://", "https://", 1)
}

// parseDate reads the dates KuGou writes: "2016-07-18", or "2016-07-18
// 10:00:00".
func parseDate(s string) (int64, error) {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02", "2006"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(s), time.Local); err == nil {
			return t.UnixMilli(), nil
		}
	}
	return 0, ErrUnsupported
}

// toPodcast turns a long audio album into the podcast the app knows: its
// author is the host, and how many episodes it holds is its count.
func toPodcast(a kugou.AudioAlbum) Podcast {
	p := Podcast{
		ID:           a.ID,
		Name:         a.Name,
		PicURL:       a.Pic,
		Description:  a.Intro,
		Category:     a.Tag,
		ProgramCount: a.Episodes,
		PlayCount:    float64(a.Played),
	}
	p.DJ.Nickname = a.Author
	if p.DJ.Nickname == "" {
		p.DJ.Nickname = a.Tag
	}
	return p
}

// toProgram turns an episode into the program the app knows: its audio is
// what plays.
func toProgram(t Track, podID int64, desc string) Program {
	createTime := t.PublishTime
	if createTime == 0 {
		createTime = time.Now().UnixMilli()
	}
	return Program{
		ID:          t.ID,
		Name:        t.Name,
		CoverURL:    t.Album.PicURL,
		Duration:    t.Duration,
		CreateTime:  createTime,
		Description: desc,
		MainSong: legacyTrack{
			ID:       t.ID,
			Name:     t.Name,
			Artists:  t.Artists,
			Album:    t.Album,
			Duration: t.Duration,
		},
	}
}
