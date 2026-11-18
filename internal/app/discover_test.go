package app

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// What the six additions ask of the service; tests replace them.
func sceneTestApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp()
	a.settings = defaultSettings()
	a.route = route{page: pageFM}
	return a
}

// 听相似的歌 asks the personal FM for the songs that follow the one chosen,
// and the radio plays on from there rather than from the listener's taste.
func TestSimilarSongsGoOnAsARadio(t *testing.T) {
	asked := make(chan int64, 4)
	similarRadioSongs = func(id int64) ([]netease.Track, error) {
		asked <- id
		return []netease.Track{
			{ID: 9, Name: "反方向的钟", Hash: "aa", Duration: 200000},
			{ID: 10, Name: "回到过去", Hash: "bb", Duration: 200000},
		}, nil
	}
	t.Cleanup(func() { similarRadioSongs = netease.SimilarRadio })

	a := newTestApp()
	a.settings = defaultSettings()
	tr := netease.Track{ID: 7, Name: "晴天", Hash: "cc", Duration: 200000}
	a.pl.Cue([]netease.Track{tr})
	a.playSimilar(&tr)

	if got := wait(t, asked, "the songs like the one chosen"); got != 7 {
		t.Fatalf("asked for the songs like %d, want 7", got)
	}
	if a.radioStarting != similarRadio(7) {
		t.Errorf("the radio starting is %q, want %q", a.radioStarting, similarRadio(7))
	}
	if label := radioLabel(similarRadio(7)); label != "相似歌曲" {
		t.Errorf("the radio is named %q", label)
	}

	// Playing it already, a second click only takes the listener to it.
	a.pl.Cue([]netease.Track{tr})
	a.pl.AttachRadio(similarRadio(7), func(func([]netease.Track)) {})
	a.nowPlaying = false
	a.playSimilar(&tr)
	select {
	case id := <-asked:
		t.Fatalf("asked again for the songs like %d", id)
	case <-time.After(50 * time.Millisecond):
	}
	if !a.nowPlaying {
		t.Error("the second click did not open the song playing")
	}
}

// Songs the service cannot pick from say so rather than start an empty
// radio.
func TestSimilarSongsOfWhatCannotSeed(t *testing.T) {
	similarRadioSongs = func(int64) ([]netease.Track, error) {
		t.Error("asked for the songs like one it cannot pick from")
		return nil, nil
	}
	t.Cleanup(func() { similarRadioSongs = netease.SimilarRadio })

	a := newTestApp()
	a.settings = defaultSettings()
	for _, tr := range []netease.Track{
		{ID: 1, Name: "本地", Path: "/music/a.mp3"},
		{ID: 2, Name: "云盘", Cloud: true},
		{ID: 3, Name: "节目", ProgramID: 4},
	} {
		a.toast = ""
		a.playSimilar(&tr)
		if a.toast == "" {
			t.Errorf("%q started a radio", tr.Name)
		}
		if a.radioStarting != "" {
			t.Fatalf("%q left the radio starting %q", tr.Name, a.radioStarting)
		}
	}
}

// A scene's radio keeps its own page, asked for again each time the queue
// runs out, so it plays on page after page.
func TestSceneRadioTurnsItsPages(t *testing.T) {
	pages := make(chan int, 4)
	sceneRadioSongs = func(id int64, page int) ([]netease.Track, error) {
		if id != 17 {
			t.Errorf("asked scene %d", id)
		}
		pages <- page
		return []netease.Track{{ID: int64(100 + page), Name: "车载的歌", Hash: "hh", Duration: 200000}}, nil
	}
	t.Cleanup(func() { sceneRadioSongs = netease.SceneRadio })

	a := newTestApp()
	feed := a.radioFeed(sceneRadio(17))
	for i := 1; i <= 3; i++ {
		feed(func([]netease.Track) {})
		if got := wait(t, pages, "the scene's songs"); got != i {
			t.Fatalf("asked page %d, want %d", got, i)
		}
	}
	if label := radioLabel(sceneRadio(17)); label != "场景电台" {
		t.Errorf("the radio is named %q", label)
	}
}

// The FM offers the scenes, each with its picture and its name, and a click
// starts that scene's radio.
func TestSceneCardsStartTheRadio(t *testing.T) {
	scenes = func() ([]netease.Scene, error) {
		t.Error("asked for the scenes the page already has")
		return nil, nil
	}
	t.Cleanup(func() { scenes = netease.Scenes })

	asked := make(chan int64, 1)
	sceneRadioSongs = func(id int64, _ int) ([]netease.Track, error) {
		asked <- id
		return nil, errors.New("offline")
	}
	t.Cleanup(func() { sceneRadioSongs = netease.SceneRadio })

	a := sceneTestApp(t)
	a.scenes = []netease.Scene{
		{ID: 17, Title: "车载", Label: "路上的歌", Songs: 120},
		{ID: 4, Title: "助眠", Label: "入睡", Songs: 80},
	}
	// Tall enough for the scenes, which sit under the FM's card.
	tt := ui.NewTester(a.view, 1200, 1400)
	tt.Frame()
	for _, want := range []string{"场景电台", "车载", "助眠", "路上的歌 · 120 首"} {
		if !tt.HasText(want) {
			t.Errorf("the FM does not show %q", want)
		}
	}

	if err := tt.Click("车载"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := wait(t, asked, "the scene's songs"); got != 17 {
		t.Fatalf("played scene %d, want 17", got)
	}
	if a.radioStarting != sceneRadio(17) {
		t.Fatalf("the radio starting is %q", a.radioStarting)
	}
	if !tt.HasText("正在准备") {
		t.Error("the card does not say it is being prepared")
	}
}

// 歌曲成绩单 lists the platforms the song reached, the best place it took on
// each, and how long it stayed.
func TestRankingPageListsThePlatforms(t *testing.T) {
	asked := make(chan int64, 1)
	songRanking = func(id int64) (*netease.Ranking, error) {
		asked <- id
		return nil, errors.New("offline")
	}
	t.Cleanup(func() { songRanking = netease.SongRanking })

	a := newTestApp()
	a.settings = defaultSettings()
	tr := netease.Track{ID: 7, Name: "晴天", Artists: []netease.Artist{{Name: "周杰伦"}}, Duration: 200000}
	a.openRanking(&tr)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if got := wait(t, asked, "the record"); got != 7 {
		t.Fatalf("asked for the record of %d", got)
	}
	// As the answer lands, which post drops without a window.
	a.ranking = &netease.Ranking{
		Song: tr, On: 6, Total: 17,
		Title: "已上榜 6/17",
		Note:  "数据来自各平台榜单",
		Platforms: []netease.RankingPlatform{
			{Name: "酷狗", Rank: 12, Times: 9, Last: "2024-06-01"},
			{Name: "QQ音乐", Rank: 4, Times: 6, Last: "2024-05-30"},
		},
	}
	tt.Frame()
	for _, want := range []string{"歌曲成绩单", "晴天", "周杰伦", "已上榜 6/17",
		"上榜明细", "酷狗", "最高第 12 名", "在榜 9 次", "2024/06/01", "QQ音乐", "最高第 4 名"} {
		if !tt.HasText(want) {
			t.Errorf("the record does not show %q", want)
		}
	}

	// A song that never charted says so.
	a.ranking = &netease.Ranking{Song: tr, Platforms: nil}
	tt.Frame()
	if !tt.HasText("这首还没有上榜记录") {
		t.Error("a song that never charted does not say so")
	}
}

// A chart's page offers its past numbers, and shows the chart as the one
// chosen stood, until the listener goes back to the latest.
func TestRankVolsShowAPastNumber(t *testing.T) {
	// A chart's numbers are read with its page.
	read := make(chan int64, 1)
	rankVolumes = func(id int64) ([]netease.RankVol, error) {
		read <- id
		return []netease.RankVol{{ID: 177447, Name: "321期", Date: "2024-06-01"}}, nil
	}
	t.Cleanup(func() { rankVolumes = netease.RankVols })

	type ask struct {
		pl  int64
		vol int64
	}
	asked := make(chan ask, 1)
	rankVolSongs = func(pl, vol int64, _ string) (*netease.Playlist, error) {
		asked <- ask{pl, vol}
		return nil, errors.New("offline")
	}
	t.Cleanup(func() { rankVolSongs = netease.RankVolSongs })

	a := newTestApp()
	a.settings = defaultSettings()
	a.playlists[42] = &netease.Playlist{
		ID: 42, Name: "飙升榜", UpdateFreq: "每天更新",
		Tracks: []netease.Track{{ID: 1, Name: "今天的歌", Duration: 200000}},
	}
	a.route = route{page: pagePlaylist, id: 42}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if got := wait(t, read, "the chart's numbers"); got != 42 {
		t.Fatalf("asked for the numbers of chart %d", got)
	}
	// As the numbers land, which post drops without a window.
	delete(a.rankVolBusy, 42)
	a.rankVols[42] = []netease.RankVol{{ID: 177447, Name: "321期", Date: "2024-06-01"}}
	tt.Frame()
	if err := tt.Click("往期"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("321期 · 2024-06-01"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	if got := wait(t, asked, "the chart's past number"); got != (ask{42, 177447}) {
		t.Fatalf("asked for %+v", got)
	}
	// As the answer lands, which post drops without a window.
	delete(a.rankVolBusy, 42)
	a.rankVolPl[42] = &netease.Playlist{
		ID: 42, Name: "飙升榜", Description: "321期",
		Tracks: []netease.Track{{ID: 5, Name: "那天的歌", Duration: 200000}},
	}
	tt.Frame()
	for _, want := range []string{"榜单 · 321期", "那天的歌", "回到最新"} {
		if !tt.HasText(want) {
			t.Errorf("the past number does not show %q", want)
		}
	}
	if tt.HasText("今天的歌") {
		t.Error("the latest number is still listed")
	}

	if err := tt.Click("回到最新"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("今天的歌") || tt.HasText("那天的歌") {
		t.Error("going back did not show the chart as it stands")
	}
	if !tt.HasText("榜单 · 每天更新") {
		t.Error("the header does not name the chart as it stands")
	}
}

// The words a song's comments keep using are offered above them, and one
// filters the comments by it; again, or 全部, shows them all.
func TestCommentWordsFilterTheComments(t *testing.T) {
	commentWords = func(id int64) ([]string, error) {
		if id != 2 {
			t.Errorf("asked for the words of song %d", id)
		}
		return []string{"青春", "回忆"}, nil
	}
	t.Cleanup(func() { commentWords = netease.CommentHotWords })

	type ask struct {
		id   int64
		word string
	}
	asked := make(chan ask, 1)
	commentsByWord = func(id int64, word string, _, _ int) (*netease.Comments, error) {
		asked <- ask{id, word}
		return nil, errors.New("offline")
	}
	t.Cleanup(func() { commentsByWord = netease.CommentsByWord })

	a, tt, _ := coverApp(t)
	a.comments = commentState{
		track: 2, thread: "KG_2", active: commentTabHot,
		hot:   commentPage{loaded: true, total: 1, list: []netease.Comment{{ID: 7, Content: "好听"}}},
		words: []string{"青春", "回忆"}, wordsFor: 2,
	}
	a.npPane = paneComments
	tt.Frame()
	for _, want := range []string{"全部", "青春", "回忆"} {
		if !tt.HasText(want) {
			t.Fatalf("the pane does not offer %q", want)
		}
	}

	if err := tt.Click("青春"); err != nil {
		t.Fatal(err)
	}
	if got := wait(t, asked, "the comments with the word"); got != (ask{2, "青春"}) {
		t.Fatalf("asked for %+v", got)
	}
	if a.comments.word != "青春" {
		t.Fatalf("the pane keeps the word %q", a.comments.word)
	}
	// As the answer lands, which post drops without a window.
	a.comments.hot = commentPage{loaded: true, total: 3, list: []netease.Comment{{ID: 9, Content: "那年夏天"}}}
	tt.Frame()
	if !tt.HasText("含「青春」的评论 3") {
		t.Errorf("the page is not the one word's: %q", tt.Texts())
	}
	if !tt.HasText("那年夏天") {
		t.Error("the filtered comments do not show")
	}

	// Again on the same word shows them all.
	if err := tt.Click("青春"); err != nil {
		t.Fatal(err)
	}
	if a.comments.word != "" {
		t.Errorf("the word is still %q", a.comments.word)
	}
}

// A word that filters nothing out says so, and offers the way back.
func TestCommentWordWithNoComments(t *testing.T) {
	a, tt, _ := coverApp(t)
	a.comments = commentState{
		track: 2, thread: "KG_2", word: "青春",
		hot:   commentPage{loaded: true},
		words: []string{"青春"}, wordsFor: 2,
	}
	a.npPane = paneComments
	tt.Frame()
	if !tt.HasText("没有含「青春」的评论") {
		t.Fatal("a word with no comments does not say so")
	}
	if err := tt.Click("看全部评论"); err != nil {
		t.Fatal(err)
	}
	if a.comments.word != "" {
		t.Errorf("the word is still %q", a.comments.word)
	}
}

// 上移 and 下移 ask the service for the new order, and a refusal says so
// rather than reorder the sidebar.
func TestMovingAPlaylistAsksTheService(t *testing.T) {
	type move struct {
		id int64
		up bool
	}
	calls := make(chan move, 2)
	movePlaylist = func(id int64, up bool) ([]int64, error) {
		calls <- move{id, up}
		return nil, errors.New("操作太频繁，请稍后再试")
	}
	t.Cleanup(func() { movePlaylist = netease.MovePlaylist })

	a := editTestApp()
	a.homeLoading = true
	a.route = route{page: pageHome}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	for _, item := range []string{"上移", "下移"} {
		if err := tt.RightClick("睡前"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(item); err != nil {
			t.Fatalf("%v; the menu shows %q", err, tt.Menu())
		}
		if got := <-calls; got != (move{11, item == "上移"}) {
			t.Errorf("%s asked for %+v", item, got)
		}
	}
	// The refusal lands, which post drops without a window.
	a.toast = "排序失败，请稍后再试"
	tt.Frame()
	if !tt.HasText("排序失败") {
		t.Error("a refused move says nothing")
	}
}

// A new cover shows on the playlist it was given to, wherever the app lists
// it.
func TestNewCoverShowsEverywhereThePlaylistIs(t *testing.T) {
	a := editTestApp()
	a.playlists[10] = &netease.Playlist{ID: 10, Name: "通勤", CoverURL: "https://old/1.jpg"}
	a.playlists[10].PicURL = "https://old/1.jpg"
	const cover = "https://imge.kugou.com/stdmusic/480/20240601/cover.jpg"
	a.setCover(10, cover)
	if got := a.mine[1].CoverURL; got != cover {
		t.Errorf("the sidebar's cover is %q", got)
	}
	if got := a.playlists[10].PicURL; got != cover {
		t.Errorf("the page's cover is %q", got)
	}
	if got := a.saved[0].CoverURL; got != "" {
		t.Errorf("a playlist of another's cover changed to %q", got)
	}
}
