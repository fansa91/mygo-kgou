package netease

import (
	"testing"

	"netease-music-mygo/internal/kugou"
)

// The badge beside the account says which build it signed in as and what it
// holds, and nothing when it holds nothing.
func TestVIPLabel(t *testing.T) {
	cases := []struct {
		s    session
		want string
	}{
		{session{}, ""},
		{session{VIPType: 2}, "VIP"},
		// Signed in as the 概念版 without its subscription: the build is
		// still worth saying.
		{session{Lite: true}, "概念版"},
		{session{Lite: true, VIPType: 2}, "概念版"}, // the standard line does not make it a 概念版 VIP
		// The 概念版's own subscription, which the account service does
		// not report at all.
		{session{Concept: true}, "概念版VIP"},
		{session{Lite: true, Concept: true}, "概念版VIP"},
	}
	for _, c := range cases {
		if got := vipLabel(&c.s); got != c.want {
			t.Fatalf("%+v: label = %q, want %q", c.s, got, c.want)
		}
		acc := c.s.Account()
		if acc.Label != c.want {
			t.Fatalf("%+v: account label = %q, want %q", c.s, acc.Label, c.want)
		}
		// A subscription is either kind: the standard one, or the
		// 概念版's own.
		if want := c.s.VIPType != 0 || c.s.Concept; acc.VIP != want {
			t.Fatalf("%+v: VIP = %v, want %v", c.s, acc.VIP, want)
		}
	}
}

// A search's albums, singers and playlists carry a picture and a number the
// app can open them by. Each service spells both its own way, and the names
// the app read before found neither: the cards came up blank, and every
// playlist found had the same number.
func TestSearchCardsCarryCoverAndID(t *testing.T) {
	dir := t.TempDir()
	lists, err := openListIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	was := app.lists
	app.lists = lists
	defer func() { app.lists = was }()

	album := albumFrom(map[string]any{
		"albumid":      float64(966846),
		"albumname":    "叶惠美",
		"img":          "http://imge.kugou.com/stdmusic/240/20230920/20230920142503632013.jpg",
		"singers":      []any{map[string]any{"name": "周杰伦"}},
		"songcount":    float64(11),
		"publish_time": "2003-07-31",
	})
	if album.ID != 966846 || album.PicURL == "" || album.Size != 11 {
		t.Fatalf("album = %+v", album)
	}

	artist := artistFrom(map[string]any{
		"AuthorId":   float64(3520),
		"AuthorName": "周杰伦",
		"Avatar":     "http://singerimg.kugou.com/uploadpic/softhead/240/20260324/x.jpg",
	})
	if artist.ID != 3520 || artist.Avatar == "" {
		t.Fatalf("artist = %+v", artist)
	}

	one := playlistFrom(map[string]any{
		"gid":         "collection_3_671372005_130_0",
		"specialname": "华语经典：晴天一响",
		"img":         "http://c1.kgimg.com/custom/150/20250815/x.jpg",
		"song_count":  float64(189),
		"nickname":    "你在乎我么",
	})
	two := playlistFrom(map[string]any{
		"gid":         "collection_3_1583126326_32_0",
		"specialname": "致命好听的欧美英文歌",
		"img":         "http://c1.kgimg.com/custom/150/20260518/y.jpg",
		"song_count":  float64(50),
	})
	if one.CoverURL == "" || one.TrackCount != 189 {
		t.Fatalf("playlist = %+v", one)
	}
	if one.ID == two.ID {
		t.Fatal("two playlists found share a number, so both open the same one")
	}
}

// A song's singers come with the number each one's page answers to, which
// is what makes their names links: a credit named by name alone opens
// nothing when it is clicked.
func TestSongSingersCarryTheirNumbers(t *testing.T) {
	dir := t.TempDir()
	idx, err := kugou.OpenIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	was := app.index
	app.index = idx
	defer func() { app.index = was }()

	tr := toTrack(kugou.Track{
		Name: "富士山下", Singer: "陈奕迅",
		Singers: []kugou.Singer{{ID: 420, Name: "陈奕迅"}},
	})
	if len(tr.Artists) != 1 || tr.Artists[0].ID != 420 || tr.Artists[0].Name != "陈奕迅" {
		t.Fatalf("artists = %+v, want the singer and the number of their page", tr.Artists)
	}
	// A song credited by name alone is still named, but opens nothing.
	plain := toTrack(kugou.Track{Name: "无主", Singer: "佚名"})
	if len(plain.Artists) != 1 || plain.Artists[0].ID != 0 || plain.Artists[0].Name != "佚名" {
		t.Fatalf("artists = %+v", plain.Artists)
	}
}

// A comment the service serves reads as the app shows it, with the singer's
// reply kept as a quote, and the service takes no like of its own: the app
// only shows how many thumbs a comment got.
func TestCommentsReadTheReplyAndTakeNoLike(t *testing.T) {
	cm := toComment(kugou.Comment{
		ID: 9, Content: "好听", UserName: "不懂事", UserPic: "https://a/b.jpg",
		Location: "广东", Likes: 5028, Reply: &kugou.Comment{UserName: "周柏豪", Content: "谢谢"},
	})
	if cm.User.Nickname != "不懂事" || cm.LikedCount != 5028 || cm.IPLocation.Location != "广东" {
		t.Fatalf("comment = %+v", cm)
	}
	if len(cm.BeReplied) != 1 || cm.BeReplied[0].User.Nickname != "周柏豪" || cm.BeReplied[0].Content != "谢谢" {
		t.Fatalf("reply = %+v", cm.BeReplied)
	}
	if CommentsLikeable() {
		t.Fatal("KuGou takes no like of a comment")
	}
}

// One of the listener's own lists is known by its listid and by the
// collection id the public service calls it, which is where its songs'
// qualities come from; a later call that brings no collection id — a rename
// through an index read before it was kept — leaves the one it has.
func TestRememberCloudKeepsTheCollectionID(t *testing.T) {
	x := &listIndex{byID: map[int64]listRef{}}
	id := x.rememberCloud("cloud", 15, 0, "my days", "pic", "collection_3_1_15_0")

	if r, _ := x.lookup(id); r.GID != "collection_3_1_15_0" {
		t.Fatalf("the list kept %q, want its collection id", r.GID)
	}
	x.rememberCloud("cloud", 15, 0, "改了个名", "pic", "")

	r, _ := x.lookup(id)
	if r.GID != "collection_3_1_15_0" {
		t.Errorf("a call without the collection id dropped it: %q", r.GID)
	}
	if r.Name != "改了个名" {
		t.Errorf("the name is %q, want the renamed one", r.Name)
	}
}
