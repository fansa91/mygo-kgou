package kugou

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Search asks KuGou for the songs matching a query, from a page of results.
// The complex search service answers, the one the phone's search box calls.
func Search(c *Client, query string, page, size int) (SearchResult, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 30
	}
	raw, err := c.retry("GET", call{
		Router: "complexsearch.kugou.com",
		Path:   "/v2/search/song",
		Params: map[string]string{
			"keyword":         query,
			"page":            strconv.Itoa(page),
			"pagesize":        strconv.Itoa(size),
			"platform":        "AndroidFilter",
			"iscorrection":    "1",
			"privilegefilter": "0",
			"area_code":       "1",
			"dopicfull":       "1",
		},
	})
	if err != nil {
		return SearchResult{}, err
	}
	var res struct {
		Data struct {
			Total int              `json:"total"`
			Lists []map[string]any `json:"lists"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return SearchResult{}, err
	}
	out := SearchResult{Total: res.Data.Total}
	for _, l := range res.Data.Lists {
		out.Songs = append(out.Songs, trackFrom(l))
	}
	return out, nil
}

// trackFrom reads a song out of any of the shapes KuGou sends it in: the
// search service, the day's recommendations, a singer's songs and the radio
// differ in capitalisation more than in content.
func trackFrom(l map[string]any) Track {
	t := Track{
		AlbumAudioID: pickInt(l, "EMixSongID", "MixSongID", "Audioid", "album_audio_id", "audio_id"),
		AlbumID:      pickInt(l, "AlbumID", "album_id"),
		Name:         clean(pickString(l, "SongName", "OriSongName", "FileName", "songname", "filename", "audio_name", "name")),
		Singer:       clean(pickString(l, "SingerName", "singername", "author_name")),
		Album:        clean(pickString(l, "AlbumName", "album_name", "remark")),
		Duration:     duration(l),
		Size:         pickInt(l, "FileSize", "file_size", "filesize", "filesize_128", "size"),
		Image:        coverOf(l),
		Hash:         strings.ToLower(pickString(l, "FileHash", "filehash", "hash", "hash_128")),
		HashHigh:     strings.ToLower(pickString(l, "HQFileHash", "hash_320")),
		HashLossl:    strings.ToLower(pickString(l, "SQFileHash", "hash_flac", "hash_ape")),
		HashHires:    strings.ToLower(pickString(l, "ResFileHash", "hash_high", "hash_super")),
		PayType:      int(pickInt(l, "PayType", "pay_type", "pay_type_128")),
		Privilege:    int(pickInt(l, "Privilege", "privilege")),
		Singers:      singersOf(fieldOf(l, "Singers", "singers", "singerinfo", "authors")),
	}
	// A song that is sold says so as a fail the purchase would clear.
	if t.PayType == 0 && pickInt(l, "fail_process", "fail_process_128") == 4 {
		t.PayType = 3
	}
	// The name comes as the credits read, with the singers in front.
	if i := strings.Index(t.Name, " - "); i > 0 {
		t.Name = clean(t.Name[i+3:])
	}
	return t
}

// clean strips the highlighting the search service wraps matches in when its
// caller asks for it, and the spaces KuGou pads names with.
func clean(s string) string {
	s = strings.ReplaceAll(s, "<em>", "")
	s = strings.ReplaceAll(s, "</em>", "")
	return strings.TrimSpace(s)
}

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

// coverOf finds a song's album picture in whatever the service calls it: the
// search service sends it as Image, the recommendations as sizable_cover with
// the size left as "{size}", and the lists nest it under trans_param or
// album_info. Each is only taken when it points at the CDN's cover path, as
// the same names elsewhere hold the singer's portrait or a video's still.
func coverOf(l map[string]any) string {
	for _, k := range []string{"sizable_cover", "Image", "union_cover", "flexible_cover", "album_image", "AlbumImage", "img", "image", "cover", "pic"} {
		if v := pickString(l, k); isCoverURL(v) {
			return sized(v)
		}
	}
	for _, k := range []string{"trans_param", "album_info", "audio_info"} {
		if m, ok := l[k].(map[string]any); ok {
			if v := coverOf(m); v != "" {
				return v
			}
		}
	}
	return ""
}

// isCoverURL reports whether v is a picture of an album rather than one of a
// singer or a video, which KuGou keeps under other paths of the same host.
func isCoverURL(v string) bool {
	return strings.Contains(v, "imge.kugou.com/stdmusic") || strings.Contains(v, "imge.kugou.com/commendpic")
}

// SearchRaw asks for one kind of thing — "album", "author", "special" — and
// hands back what the service found as it sent it, for the caller to read.
func SearchRaw(c *Client, kind, query string, page, size int) ([]map[string]any, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 30
	}
	path := "/v1/search/" + kind
	params := map[string]string{
		"keyword": query, "page": strconv.Itoa(page), "pagesize": strconv.Itoa(size),
		"platform": "AndroidFilter",
	}
	if kind == "album" {
		params["sorttype"] = "0"
	}
	raw, err := c.retry("GET", call{Router: "complexsearch.kugou.com", Path: path, Params: params})
	if err != nil {
		return nil, 0, err
	}
	var res struct {
		Data struct {
			Total int              `json:"total"`
			Lists []map[string]any `json:"lists"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, 0, err
	}
	return res.Data.Lists, res.Data.Total, nil
}
