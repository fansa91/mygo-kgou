package kugou

import (
	"strconv"
	"strings"
)

// KuGou has no podcasts, but it has long audio: audio books and talk shows
// sold and streamed one episode at a time. An album there is the channel a
// podcast is here, and an album's audios are its episodes, which is as close
// as the catalogue comes.

// AudioAlbum is a long audio album: a show, or an audio book.
type AudioAlbum struct {
	ID       int64
	Name     string
	Pic      string
	Intro    string
	Episodes int
	Played   int64
	Pay      bool   // sold, or free to hear
	Tag      string // its category, as 有声小说, 自然疗愈
	Author   string
}

// PodcastAlbums returns the albums picked to listen to today.
func PodcastAlbums(c *Client) ([]AudioAlbum, error) {
	var res struct {
		Data struct {
			Albums []map[string]any `json:"albums"`
		} `json:"data"`
	}
	err := c.send("POST", request{
		Path:   "/longaudio/v1/home_new/daily_recommend",
		Params: map[string]string{"module_id": "1", "size": "30", "page": "1"},
	}, &res)
	if err != nil {
		return nil, err
	}
	return audioAlbums(res.Data.Albums), nil
}

// PodcastLibrary returns a page of the albums in a category, free ones only
// when free is set, and whether more follow.
func PodcastLibrary(c *Client, tagID int, page, size int, free bool) ([]AudioAlbum, bool, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 30
	}
	if tagID == 0 {
		tagID = 906 // 有声小说, the whole of it
	}
	p := map[string]string{
		"api_ver": "2", "gender": "0", "sort": "1", "tag_id": strconv.Itoa(tagID),
		"status": "0", "page": strconv.Itoa(page), "page_size": strconv.Itoa(size),
		// The version the free shelf answers to.
		"clientver": "20789",
	}
	if free {
		p["free"] = "1"
	}
	var res struct {
		Data struct {
			List []map[string]any `json:"data_list"`
			End  int              `json:"is_end"`
		} `json:"data"`
	}
	err := c.send("GET", request{Path: "/longaudio/v1/album/list", Params: p}, &res)
	if err != nil {
		return nil, false, err
	}
	return audioAlbums(res.Data.List), res.Data.End == 0, nil
}

// PodcastAlbum returns one album.
func PodcastAlbum(c *Client, id int64) (*AudioAlbum, error) {
	var res struct {
		Data []map[string]any `json:"data"`
	}
	err := c.send("POST", request{
		Path: "/openapi/v2/broadcast",
		Hdr:  map[string]string{"kg-tid": "78"},
		Body: map[string]any{
			"data":           []map[string]string{{"album_id": strconv.FormatInt(id, 10)}},
			"show_album_tag": 1,
			"fields": "album_name,album_id,category,authors,sizable_cover,intro,author_name," +
				"trans_param,album_tag,mix_intro,full_intro,is_publish",
		},
	}, &res)
	if err != nil {
		return nil, err
	}
	if len(res.Data) == 0 {
		return nil, ErrUnavailable
	}
	one := audioAlbums(res.Data)
	a := one[0]
	if a.ID == 0 {
		a.ID = id
	}
	return &a, nil
}

// PodcastEpisodes returns a page of an album's episodes, newest first, and
// whether more follow.
func PodcastEpisodes(c *Client, albumID int64, page, size int) ([]Track, bool, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 100
	}
	var res struct {
		Total int              `json:"total"`
		Data  []map[string]any `json:"data"`
	}
	err := c.send("POST", request{
		Router: "openapi.kugou.com",
		Path:   "/longaudio/v2/album_audios",
		Hdr:    map[string]string{"kg-tid": "78"},
		Body: map[string]any{
			"album_id": strconv.FormatInt(albumID, 10), "area_code": 1,
			"tagid": 0, "page": page, "pagesize": size,
		},
	}, &res)
	if err != nil {
		return nil, false, err
	}
	out := make([]Track, 0, len(res.Data))
	for _, l := range res.Data {
		out = append(out, episodeFrom(l, albumID))
	}
	return out, len(res.Data) == size, nil
}

// episodeFrom reads one episode: its own audio, named and dated as an
// episode, not as a song.
func episodeFrom(l map[string]any, albumID int64) Track {
	t := Track{
		AlbumAudioID: pickInt(l, "album_audio_id", "mixsongid"),
		AlbumID:      pickInt(l, "album_id"),
		Name:         clean(pickString(l, "audio_name", "name")),
		Singer:       clean(pickString(l, "author_name")),
		Album:        clean(pickString(l, "album_name")),
		Duration:     duration(l),
		Image:        coverOf(l),
		Hash:         strings.ToLower(pickString(l, "hash", "hash_128")),
		HashHigh:     strings.ToLower(pickString(l, "hash_320")),
		HashLossl:    strings.ToLower(pickString(l, "hash_flac")),
		HashHires:    strings.ToLower(pickString(l, "hash_high", "hash_super")),
		PayType:      int(pickInt(l, "pay_type", "pay_type_128")),
		Privilege:    int(pickInt(l, "privilege", "privilege_128")),
	}
	if t.AlbumID == 0 {
		t.AlbumID = albumID
	}
	if who := singers(l["authors"]); who != "" {
		t.Singer = who
	}
	return t
}

func audioAlbums(list []map[string]any) []AudioAlbum {
	out := make([]AudioAlbum, 0, len(list))
	for _, l := range list {
		a := AudioAlbum{
			ID:       pickInt(l, "album_id", "id"),
			Name:     clean(pickString(l, "album_name", "name")),
			Pic:      sized(pickString(l, "sizable_cover", "cover", "sizable_cover_h5")),
			Intro:    clean(pickString(l, "intro", "full_intro", "mix_intro")),
			Episodes: int(pickInt(l, "audio_total", "audio_num")),
			Played:   pickInt(l, "play_count"),
			Pay:      pickInt(l, "is_pay") == 1,
			Author:   clean(pickString(l, "author_name")),
		}
		if tag, ok := l["tag_info"].(map[string]any); ok {
			a.Tag = clean(pickString(tag, "tag_name"))
		}
		if a.Tag == "" {
			a.Tag = clean(pickString(l, "category", "album_label"))
		}
		if a.Name == "" {
			continue
		}
		out = append(out, a)
	}
	return out
}
