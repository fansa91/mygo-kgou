package app

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// This file makes the phone pictures in the README. The app's real cover
// loader works on goroutines whose callbacks are queued through post, which
// a headless Tester does not run; and it fetches the pictures from the CDN.
// So, rather than depend on a network, the frames below fill the cover cache
// with colorful placeholders worked out here, alongside made-up catalogue
// data. Only a picture, never shipped in the app, is made.

// shotPalette is the set of cover colours, warm to cool. Each card and row
// takes one, so the page looks as varied as a real library.
var shotPalette = []color.RGBA{
	{0xE8, 0x6A, 0x5A, 0xFF}, {0x3E, 0x7C, 0xC4, 0xFF}, {0xE0, 0xA4, 0x3C, 0xFF},
	{0x54, 0xA8, 0x86, 0xFF}, {0x9A, 0x6F, 0xC8, 0xFF}, {0xD0, 0x5E, 0x92, 0xFF},
	{0x4E, 0x9C, 0xB8, 0xFF}, {0xC8, 0x84, 0x4A, 0xFF}, {0x74, 0x8C, 0x4E, 0xFF},
	{0xB0, 0x60, 0x70, 0xFF}, {0x5A, 0x68, 0xB0, 0xFF}, {0xD8, 0x9A, 0x7A, 0xFF},
}

// shotArt makes a square cover of col with a soft diagonal shade, so the
// placeholders read as album art rather than flat swatches.
func shotArt(col color.RGBA) (*ui.Bitmap, image.Image) {
	const n = 96
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			t := (float32(x) + float32(y)) / (2 * n)
			img.Pix[img.PixOffset(x, y)] = uint8(float32(col.R)*(1-t) + float32(col.G)*t*0.25 + 18*t)
			img.Pix[img.PixOffset(x, y)+1] = uint8(float32(col.G)*(1-t) + float32(col.B)*t*0.35 + 14*t)
			img.Pix[img.PixOffset(x, y)+2] = uint8(float32(col.B)*(1-t) + float32(col.R)*t*0.30 + 22*t)
			img.Pix[img.PixOffset(x, y)+3] = 255
		}
	}
	return ui.NewBitmap(img), img
}

// shotCover fills every size a given URL is asked at, because the cache key
// folds the requested size into the URL. A backdrop is kept alongside for
// covers used behind the daily card and Now Playing.
func (a *app) shotCover(url string, col color.RGBA, backdrop bool) {
	for _, px := range []int{40, 80, 96, 120, 160, 200, 240, 400, 500, 600} {
		bmp, img := shotArt(col)
		art := &artwork{bmp: bmp, tint: dominantColor(img), used: cNow(), size: 1}
		a.art.art[netease.Sized(url, px)] = art
		if backdrop {
			bd := *art
			bd.backdrop = ui.NewBitmap(blurImage(img))
			a.art.art[netease.Sized(url, px)+"#backdrop"] = &bd
		}
	}
}

// cNow is a stable clock for the cache's bookkeeping; the pictures do not
// need the wall clock, only distinct artworks under distinct keys.
func cNow() time.Time { return time.Unix(1e9, 0) }

var shotTitles = []string{
	"晴天", "稻香", "夜曲", "七里香", "青花瓷", "简单爱", "告白气球", "双截棍",
	"枫", "蒲公英的约定", "听妈妈的话", "千里之外", "烟花易冷", "红尘客栈", "给我一首歌的时间",
}
var shotArtists = []string{"周杰伦", "林俊杰", "陈奕迅", "五月天", "薛之谦", "邓紫棋", "李荣浩", "毛不易"}
var shotAlbums = []string{"叶惠美", "依然范特西", "我很想爱他", "范特西", "十二新作", "魔杰座", "跨时代", "哎呦，不错哦"}

// shotTrack makes a song with a cover drawn from the palette by i.
func shotTrack(i int) netease.Track {
	url := fmt.Sprintf("shot://cover/%d", i)
	return netease.Track{
		ID:       int64(1000 + i),
		Name:     shotTitles[i%len(shotTitles)],
		Artists:  []netease.Artist{{ID: int64(10 + i%8), Name: shotArtists[i%len(shotArtists)]}},
		Album:    netease.Album{ID: int64(200 + i), Name: shotAlbums[i%len(shotAlbums)], PicURL: url},
		Duration: int64(180000 + i%6*27000),
		Fee:      0,
	}
}

// shotPlaylist makes a playlist or chart card with a cover and a few lines.
func shotPlaylist(i int, chart bool) netease.Playlist {
	url := fmt.Sprintf("shot://playlist/%d", i)
	names := []string{"华语流行精选", "深夜循环 · 一个人的夜晚", "8090 经典回忆", "开车必听的歌",
		"周杰伦：青春的声音", "治愈系 · 温柔入耳", "飙升榜", "新歌榜", "热歌榜", "原创榜"}
	pl := netease.Playlist{
		ID:         int64(3000 + i),
		Name:       names[i%len(names)],
		CoverURL:   url,
		PlayCount:  float64(120000 + i*37000),
		TrackCount: 60 + i*13,
	}
	pl.Creator.Nickname = "酷狗音乐"
	if chart {
		pl.UpdateFreq = "每日更新"
		pl.Preview = []netease.ChartLine{
			{First: shotTitles[i%len(shotTitles)], Second: shotArtists[i%len(shotArtists)]},
			{First: shotTitles[(i+1)%len(shotTitles)], Second: shotArtists[(i+2)%len(shotArtists)]},
			{First: shotTitles[(i+3)%len(shotTitles)], Second: shotArtists[(i+5)%len(shotArtists)]},
		}
	}
	return pl
}

// richPhoneApp is a phone full of made-up, colorful catalogue data, with a
// song cued so the mini player and Now Playing have something to show.
func richPhoneApp(t *testing.T) *app {
	t.Helper()
	a := phoneApp(t)
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 42, Nickname: "爱听歌的人", VIP: true, Label: "概念版"}
	a.users[42] = &userData{
		user:      &netease.User{Nickname: "爱听歌的人", Follows: 128, Followeds: 512},
		playlists: []netease.Playlist{shotPlaylist(1, false), shotPlaylist(2, false), shotPlaylist(3, false)},
	}
	a.hot = []string{"晴天", "周杰伦", "稻香", "夜曲", "告白气球", "七里香", "青花瓷", "双截棍"}

	// The day's songs, the recommendations, the charts and the playlists.
	a.daily = make([]netease.Track, 0, 6)
	for i := 0; i < 6; i++ {
		tr := shotTrack(i)
		tr.Reason = []string{"根据你的喜好推荐", "最近常听", "和你喜欢的相似", "本周热门"}[i%4]
		a.daily = append(a.daily, tr)
	}
	for i := 0; i < 4; i++ {
		a.charts = append(a.charts, shotPlaylist(i+6, true))
		a.recPlaylists = append(a.recPlaylists, shotPlaylist(i, false))
	}
	for i := 0; i < 8; i++ {
		a.featured = append(a.featured, shotPlaylist(i+2, false))
	}
	// Fill every cover used on the home page; the day's first cover also
	// needs a blurred copy for the card behind it.
	for i := 0; i < 16; i++ {
		col := shotPalette[i%len(shotPalette)]
		a.shotCover(fmt.Sprintf("shot://cover/%d", i), col, i < 6)
		a.shotCover(fmt.Sprintf("shot://playlist/%d", i), shotPalette[(i*3+1)%len(shotPalette)], false)
	}

	// A search with real-looking results.
	r := route{page: pageSearch, query: "晴天", kind: netease.SearchSongs}
	a.searches[r] = &searchData{
		res:  netease.SearchResult{Songs: []netease.Track{shotTrack(0), shotTrack(7), shotTrack(3), shotTrack(10), shotTrack(5)}, Total: 128},
		page: paged{loaded: true},
	}

	// A local library with songs, artists and albums.
	a.localScanned = time.Now()
	a.localScannedDir = "C:\\Users\\Administrator\\Music"
	a.localArtistN, a.localAlbumN = 5, 6
	for i := 0; i < 12; i++ {
		tr := shotTrack(i)
		tr.Path = fmt.Sprintf("C:\\Users\\Administrator\\Music\\%s - %s.mp3", tr.ArtistNames(), tr.Name)
		tr.Format = "MP3"
		a.localTracks = append(a.localTracks, tr)
	}

	// Cue the day's songs through a silent output, so the mini strip and
	// Now Playing show aplaying song, a cover and lyrics.
	a.pl.Cue(a.daily)
	a.pl.SetLyrics(lyrics.Build(
		"[00:00.00]晴天\n[00:05.00]故事的小黄花\n[00:10.00]从出生那年就飘着\n[00:15.00]童年的荡秋千\n[00:20.00]随记忆一直晃到现在\n[00:25.00]吹着前奏望着天空", "", "", ""))
	out := audio.NewPlayer(&silence{})
	out.SetVolume(0)
	out.Play()
	a.pl.Attach(out)
	t.Cleanup(func() { out.Close() })
	return a
}

// TestPhoneShots writes the README's phone pictures. Set NM_PHONE_SHOTS to
// the directory to save them in. It is skipped in an ordinary test run.
func TestPhoneShots(t *testing.T) {
	dir := os.Getenv("NM_PHONE_SHOTS")
	if dir == "" {
		t.Skip("set NM_PHONE_SHOTS to write phone frames")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := func(name string, tt *ui.Tester) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()
	}

	// Home, with the day's card, the recommendations and a playing strip.
	a := richPhoneApp(t)
	tt := ui.NewTester(a.view, 390, 844)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	save("m-home", tt)

	// Search results.
	a2 := richPhoneApp(t)
	a2.phoneSelectTab(1)
	a2.route = route{page: pageSearch, query: "晴天", kind: netease.SearchSongs}
	tt2 := ui.NewTester(a2.view, 390, 844)
	tt2.Frame()
	tt2.Advance(time.Second)
	tt2.Frame()
	save("m-search", tt2)

	// The local library's songs view.
	a3 := richPhoneApp(t)
	a3.phoneSelectTab(2)
	tt3 := ui.NewTester(a3.view, 390, 844)
	tt3.Frame()
	tt3.Advance(time.Second)
	tt3.Frame()
	save("m-local", tt3)

	// Now Playing, with the controls that stay visible on a touch screen.
	a4 := richPhoneApp(t)
	a4.phoneSelectTab(0)
	a4.nowPlaying = true
	tt4 := ui.NewTester(a4.view, 390, 844)
	tt4.SetInsets(44, 0, 34, 0)
	tt4.Frame()
	tt4.Advance(time.Second)
	tt4.Frame()
	save("m-nowplaying", tt4)

	// Now Playing, swiped left to the lyrics page.
	a4b := richPhoneApp(t)
	a4b.phoneSelectTab(0)
	a4b.nowPlaying = true
	tt4b := ui.NewTester(a4b.view, 390, 844)
	tt4b.SetInsets(44, 0, 34, 0)
	tt4b.Frame()
	tt4b.Advance(time.Second)
	tt4b.Frame()
	tt4b.FingerDown(320, 400)
	tt4b.Advance(16 * time.Millisecond)
	for i := 1; i <= 8; i++ {
		tt4b.FingerMove(320-float32(i)*32, 400)
		tt4b.Frame()
		tt4b.Advance(16 * time.Millisecond)
	}
	tt4b.FingerUp(64, 400)
	tt4b.Advance(time.Second)
	tt4b.Frame()
	save("m-nowplaying-lyrics", tt4b)

	// The account home, under the 我的 tab.
	a5 := richPhoneApp(t)
	a5.phoneSelectTab(3)
	tt5 := ui.NewTester(a5.view, 390, 844)
	tt5.Frame()
	tt5.Advance(time.Second)
	tt5.Frame()
	save("m-mine", tt5)

	// Discover, the 发现 tab with nothing searched yet.
	a6 := richPhoneApp(t)
	a6.phoneSelectTab(1)
	tt6 := ui.NewTester(a6.view, 390, 844)
	tt6.Frame()
	tt6.Advance(time.Second)
	tt6.Frame()
	save("m-discover", tt6)

	// Settings, pushed from the account home.
	a7 := richPhoneApp(t)
	a7.phoneSelectTab(3)
	a7.open(route{page: pageSettings})
	tt7 := ui.NewTester(a7.view, 390, 844)
	tt7.Frame()
	tt7.Advance(time.Second)
	tt7.Frame()
	save("m-settings", tt7)
}
