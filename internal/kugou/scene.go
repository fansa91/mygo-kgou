package kugou

import (
	"strconv"
	"strings"
)

// The scenes KuGou builds its radio from — 助眠, 车载, 工作学习 and the rest —
// each a stream of songs picked for that place rather than for the listener.
// The service lists eleven of them here; each answers with its own songs, a
// page at a time, and never runs out.

// Scene is one of them, as the page shows it: what it is called, what it is
// for, and the picture it is drawn with.
type Scene struct {
	ID     int64
	Title  string
	Popup  string // the word on its card: 助眠, 车载…
	Label  string // what it holds: 催眠/纯音/雨声
	Intro  string
	Pic    string // the card's picture
	BG     string // the picture behind the page
	Icon   string
	Color  string
	Songs  int
	Plays  int64
	SortAt int
}

// Scenes returns the scenes the radio is built from, in the order KuGou
// puts them in.
func Scenes(c *Client) ([]Scene, error) {
	var res map[string]any
	if err := c.get(call{Path: "/scene/v1/scene/list"}, &res); err != nil {
		return nil, err
	}
	return scenesFrom(res), nil
}

// scenesFrom reads the scenes out of the service's answer.
func scenesFrom(res map[string]any) []Scene {
	data, _ := res["data"].(map[string]any)
	list := maps(firstField(data, "list"))
	out := make([]Scene, 0, len(list))
	for _, l := range list {
		id := pickInt(l, "scene_id", "id")
		if id == 0 {
			continue
		}
		extra, _ := l["extra"].(map[string]any)
		s := Scene{
			ID:     id,
			Title:  clean(pickString(l, "title")),
			Popup:  clean(pickString(l, "popup")),
			Label:  clean(pickString(l, "label")),
			Intro:  clean(pickString(l, "intro")),
			Pic:    sized(pickString(l, "pic")),
			BG:     sized(pickString(l, "bg_pic")),
			Icon:   sized(pickString(l, "icon")),
			Color:  pickString(l, "color"),
			Songs:  int(pickInt(extra, "song_count")),
			Plays:  pickInt(extra, "view_num"),
			SortAt: int(pickInt(l, "sort")),
		}
		// A scene with no card picture is drawn with the one behind its page.
		if s.Pic == "" {
			s.Pic = s.BG
		}
		out = append(out, s)
	}
	return out
}

// SceneSongs returns a page of a scene's radio. The service answers with a
// stream rather than a list: each entry is a song, with what a listener said
// about it. What the page plays is the songs.
func SceneSongs(c *Client, sceneID int64, page, size int) ([]Track, error) {
	if sceneID == 0 {
		return nil, ErrUnavailable
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	var res map[string]any
	err := c.send("POST", request{
		Path: "/genesisapi/v1/scene_music/rec_music",
		Params: map[string]string{
			"scene_id": strconv.FormatInt(sceneID, 10),
			"page":     strconv.Itoa(page),
			"pagesize": strconv.Itoa(size),
		},
		Body: map[string]any{"exposure": []any{}},
	}, &res)
	if err != nil {
		return nil, err
	}
	return sceneSongsFrom(res), nil
}

// sceneSongsFrom reads the songs a scene's answer carries: each entry is a
// song with what a listener said about it, and the page plays the songs.
func sceneSongsFrom(res map[string]any) []Track {
	data, _ := res["data"].(map[string]any)
	items := maps(firstField(data, "list"))
	out := make([]Track, 0, len(items))
	for _, item := range items {
		song, _ := item["song"].(map[string]any)
		if len(song) == 0 {
			continue
		}
		if t := trackFromScene(song); t.AlbumAudioID != 0 {
			out = append(out, t)
		}
	}
	return out
}

// trackFromScene reads the song a scene's entry carries, which names its
// qualities flat rather than in audio_info, and its cover album_cover.
func trackFromScene(l map[string]any) Track {
	t := trackFrom(l)
	if t.Name == "" {
		t.Name = clean(pickString(l, "songname", "audio_name"))
	}
	if t.Image == "" {
		if pic := sized(pickString(l, "album_cover")); isCoverURL(pic) {
			t.Image = pic
		}
	}
	if t.Hash == "" {
		t.Hash = strings.ToLower(pickString(l, "hash"))
	}
	return t
}
