package kugou

import (
	"encoding/json"
	"testing"
)

// The scenes arrive as a list of cards with a label, a picture and how many
// songs each holds.
func TestScenesReadTheList(t *testing.T) {
	body := `{"errcode":0,"status":1,"data":{"list":[
		{"scene_id":1,"title":"助眠专区","popup":"助眠","label":"催眠/纯音/雨声",
		 "pic":"http://imge.kugou.com/stdmusic/{size}/a.jpg",
		 "bg_pic":"http://imge.kugou.com/stdmusic/{size}/b.jpg",
		 "extra":{"song_count":1133,"view_num":18469376},"sort":1},
		{"scene_id":17,"title":"车载专区","popup":"车载","label":"DJ/流行/嗨歌",
		 "pic":"","bg_pic":"http://imge.kugou.com/stdmusic/{size}/c.jpg",
		 "extra":{"song_count":627799},"sort":2}
	]}}`
	var res map[string]any
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	list := scenesFrom(res)
	if len(list) != 2 {
		t.Fatalf("read %d scenes, want 2", len(list))
	}
	first := list[0]
	if first.ID != 1 || first.Title != "助眠专区" || first.Popup != "助眠" || first.Label != "催眠/纯音/雨声" {
		t.Errorf("first scene: %+v", first)
	}
	if first.Songs != 1133 || first.Plays != 18469376 {
		t.Errorf("first scene's counts: %+v", first)
	}
	// A card with no picture of its own is drawn with the one behind it.
	if list[1].Pic != list[1].BG || list[1].Pic == "" {
		t.Errorf("second scene's picture is %q, its backdrop %q", list[1].Pic, list[1].BG)
	}
	// The {size} the services leave in a picture is filled in.
	if first.Pic == "" || first.Pic == "http://imge.kugou.com/stdmusic/{size}/a.jpg" {
		t.Errorf("first scene's picture was not sized: %q", first.Pic)
	}
}

// A scene answers with a stream: the songs, each with what a listener said
// about it, and entries that are not songs are left out.
func TestSceneSongsReadTheStream(t *testing.T) {
	body := `{"status":1,"data":{"has_next":1,"list":[
		{"id":"1","content":"晚安","song":{"album_audio_id":345585127,"album_id":49886906,
		 "album_cover":"http://imge.kugou.com/stdmusic/{size}/cover.jpg",
		 "author_name":"大喜有声","songname":"别被那些干扰","hash":"C91B5AEB",
		 "timelength":290377,"filesize":4646696,"privilege_128":0}},
		{"id":"2","content":"效果不错","collection":{"name":"专业深度睡眠曲","count":20}},
		{"id":"3","song":{"album_audio_id":0,"songname":"没有编号"}}
	]}}`
	var res map[string]any
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	songs := sceneSongsFrom(res)
	if len(songs) != 1 {
		t.Fatalf("read %d songs, want 1", len(songs))
	}
	got := songs[0]
	if got.AlbumAudioID != 345585127 || got.Name != "别被那些干扰" || got.Singer != "大喜有声" {
		t.Errorf("song: %+v", got)
	}
	if got.Duration != 290377 || got.Size != 4646696 {
		t.Errorf("song's length and size: %+v", got)
	}
	if got.Hash != "c91b5aeb" {
		t.Errorf("hash is %q", got.Hash)
	}
	if got.Image == "" || got.Image == "http://imge.kugou.com/stdmusic/{size}/cover.jpg" {
		t.Errorf("cover was not sized: %q", got.Image)
	}
}

// A record says which platforms a song charted on, how high it got and how
// long it stayed.
func TestRankingReadsThePlatforms(t *testing.T) {
	body := `{"status":1,"errcode":0,"data":{"album_audio_id":32100650,
		"platform_num":6,"platform_total":17,"title1":"已上榜 6/17",
		"title2":"收录全球海内外17个音乐平台上榜信息，该歌曲上榜6个",
		"info":[
			{"platform_name":"酷狗音乐","platform_logo":"https://mobileservicebssdl.kugou.com/1.png",
			 "ranking_num":12,"ranking_times":6500,"last_time":"2026-11-18"},
			{"platform_name":"QQ音乐","ranking_num":4,"ranking_times":2502,"last_time":"2026-11-18"}
		]}}`
	var res map[string]any
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	rec := rankingFrom(res)
	if rec.On != 6 || rec.Total != 17 || rec.Title != "已上榜 6/17" {
		t.Errorf("record: %+v", rec)
	}
	if len(rec.Platforms) != 2 {
		t.Fatalf("read %d platforms, want 2", len(rec.Platforms))
	}
	if p := rec.Platforms[0]; p.Name != "酷狗音乐" || p.Rank != 12 || p.Times != 6500 || p.Last != "2026-11-18" {
		t.Errorf("first platform: %+v", p)
	}
	// A song no chart has seen says so without words of its own.
	empty := rankingFrom(map[string]any{"data": map[string]any{}})
	if empty.Title != "" || len(empty.Platforms) != 0 {
		t.Errorf("an unseen song's record: %+v", empty)
	}
	// And one that charted without a headline gets one.
	plain := rankingFrom(map[string]any{"data": map[string]any{"platform_num": float64(2), "platform_total": float64(5)}})
	if plain.Title != "已上榜 2/5" {
		t.Errorf("headline is %q", plain.Title)
	}
}

// A chart's past numbers come nested under a group, each with its date, and
// a number that appears twice is listed once.
func TestRankVolsReadTheNumbers(t *testing.T) {
	body := `{"status":1,"data":{"info":[{"vols":[
		{"volid":179137,"volname":"322期","voltime":"2026-11-18 08:30:01"},
		{"volid":177447,"volname":"321期","voltime":"2026-11-17 08:30:01"}
	]},{"vols":[
		{"volid":177447,"volname":"321期","voltime":"2026-11-17 08:30:01"},
		{"volid":175907,"volname":"","voltitle":"320期","voltime":"2026-11-16 08:30:00"}
	]}]}}`
	var res map[string]any
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	list := rankVolsFrom(res)
	if len(list) != 3 {
		t.Fatalf("read %d numbers, want 3", len(list))
	}
	if list[0].ID != 179137 || list[0].Name != "322期" || list[0].Date != "2026-11-18" {
		t.Errorf("newest number: %+v", list[0])
	}
	if list[2].ID != 175907 || list[2].Name != "320期" {
		t.Errorf("third number: %+v", list[2])
	}
}

// The words listeners keep using come with the comments, each with how many
// comments it is in.
func TestHotWordsReadTheList(t *testing.T) {
	body := `{"err_code":0,"count":711860,"hot_word_list":[
		{"content":"晴天","count":23605},
		{"content":"快乐","count":15130},
		{"content":"","count":9}
	]}`
	var res map[string]any
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	words := hotWordsFrom(res)
	if len(words) != 2 {
		t.Fatalf("read %d words, want 2", len(words))
	}
	if words[0].Word != "晴天" || words[0].Count != 23605 {
		t.Errorf("first word: %+v", words[0])
	}
}
