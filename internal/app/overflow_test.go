package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/netease"
)

// phoneFits checks that every label of a page lands within the screen's
// width. A page whose content is wider than the phone pushes its text off
// the edge, which is easy to do with the desktop's fixed widths and easy
// to miss until a narrow device shows it.
func phoneFits(t *testing.T, a *app, w, h int, paged ...bool) {
	t.Helper()
	tt := ui.NewTester(a.view, w, h)
	tt.Frame()
	for _, text := range tt.Texts() {
		r, ok := tt.Find(text)
		if !ok {
			continue
		}
		// A PageView keeps its neighbouring page to the right, off screen.
		// Its labels are not overflow: they belong to the page a swipe
		// would bring in.
		if len(paged) > 0 && paged[0] && r.X >= float32(w)-0.5 {
			continue
		}
		if r.X < -0.5 || r.X+r.W > float32(w)+0.5 {
			t.Errorf("%q runs off the %dpx screen: x=%.1f width=%.1f", text, w, r.X, r.W)
		}
	}
}

// TestPhonePagesFit checks the pages at the narrowest common phone width.
// The shell's pages keep the desktop's fixed widths, so without the
// phone-specific sizing they put the FM song and Now Playing's pane off
// the screen.
func TestPhonePagesFit(t *testing.T) {
	const w, h = 390, 844
	// Now Playing keeps a second, off-screen page to the right for lyrics.
	paged := map[string]bool{"nowplaying": true}
	cases := []struct {
		name string
		open func(a *app)
	}{
		{"home", func(a *app) {}},
		{"search", func(a *app) { a.phoneSelectTab(1) }},
		{"local", func(a *app) { a.phoneSelectTab(2) }},
		{"localArtists", func(a *app) {
			a.settings = defaultSettings()
			a.setLocal([]local.Track{
				{Path: `C:\晴天.mp3`, Name: "晴天", Artist: "周杰伦", Album: "叶惠美", Length: 4 * time.Second},
				{Path: `C:\七里香.flac`, Name: "七里香", Artist: "周杰伦", Album: "七里香", Length: 5 * time.Second},
				{Path: `C:\江南.flac`, Name: "江南", Artist: "林俊杰", Album: "第二天堂", Length: 4 * time.Second},
			}, 0)
			a.phoneSelectTab(2)
			a.localTab = localTabArtists
		}},
		{"localAlbums", func(a *app) {
			a.settings = defaultSettings()
			a.setLocal([]local.Track{
				{Path: `C:\晴天.mp3`, Name: "晴天", Artist: "周杰伦", Album: "叶惠美", Length: 4 * time.Second},
				{Path: `C:\七里香.flac`, Name: "七里香", Artist: "周杰伦", Album: "七里香", Length: 5 * time.Second},
			}, 0)
			a.phoneSelectTab(2)
			a.localTab = localTabAlbums
		}},
		{"settings", func(a *app) { a.phoneSelectTab(3) }},
		{"daily", func(a *app) { a.open(route{page: pageDaily}) }},
		{"charts", func(a *app) { a.open(route{page: pageCharts}) }},
		{"fm", func(a *app) { a.open(route{page: pageFM}) }},
		{"cloud", func(a *app) { a.open(route{page: pageCloud}) }},
		{"downloads", func(a *app) { a.open(route{page: pageDownloads}) }},
		{"podcasts", func(a *app) { a.open(route{page: pagePodcasts}) }},
		{"ranking", func(a *app) { a.open(route{page: pageRanking, id: 1}) }},
		{"playlist", func(a *app) { a.open(route{page: pagePlaylist, id: 1}) }},
		{"nowplaying", func(a *app) {
			a.nowPlaying = true
			a.pl.Cue([]netease.Track{{ID: 7, Name: "宝船", Duration: 200000}})
			a.pl.SetLyrics(nil)
			out := audio.NewPlayer(&silence{})
			out.SetVolume(0)
			out.Play()
			a.pl.Attach(out)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := phoneApp(t)
			tc.open(a)
			phoneFits(t, a, w, h, paged[tc.name])
		})
	}
}
