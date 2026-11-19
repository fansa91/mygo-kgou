package app

import (
	"testing"

	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// phonePlayingApp is a phone with a song playing through a silent output,
// so the player strip and Now Playing have something to show.
func phonePlayingApp(t *testing.T) *app {
	t.Helper()
	a := phoneApp(t)
	a.pl.Cue([]netease.Track{{ID: 7, Name: "晴天", Duration: 200000,
		Artists: []netease.Artist{{Name: "周杰伦"}}, Album: netease.Album{Name: "叶惠美"}}})
	a.pl.SetLyrics(nil)
	out := audio.NewPlayer(&silence{})
	out.SetVolume(0)
	out.Play()
	a.pl.Attach(out)
	return a
}

// On a phone the mini strip's play button toggles without opening Now
// Playing, while a tap on the song itself does open it. The buttons are
// nested in the row that opens the view, so a click that reached the row
// would both play and cover the page in one tap.
func TestPhoneMiniPlayer(t *testing.T) {
	a := phonePlayingApp(t)
	tt := ui.NewTester(a.view, 390, 844)
	tt.Frame()
	if !tt.HasText("晴天") {
		t.Fatalf("the mini strip has no song: %v", tt.Texts())
	}
	before := a.pl.Playing()
	if err := tt.Click("播放/暂停"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.pl.Playing() == before {
		t.Error("tapping the mini play button did not toggle")
	}
	if a.nowPlaying {
		t.Error("tapping the mini play button opened Now Playing")
	}
	if err := tt.Click("晴天"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.nowPlaying {
		t.Error("tapping the mini strip did not open Now Playing")
	}
}

// Now Playing's close chevron and pane tabs are revealed only while the
// pointer is in the window. A phone has no pointer to hover, so they must
// stay shown: otherwise a listener can neither leave the view nor reach
// the lyrics or the queue.
func TestPhoneNowPlayingControls(t *testing.T) {
	a := phonePlayingApp(t)
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 390, 844)
	tt.SetInsets(44, 0, 34, 0) // a phone's bars, which also turns on touch
	tt.Frame()
	if err := tt.Click("待播清单"); err != nil {
		t.Fatalf("Now Playing has no tappable pane tab: %v", err)
	}
	tt.Frame()
	if a.npPane != paneQueue {
		t.Errorf("tapping 待播清单 left pane %d, want queue", a.npPane)
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatalf("Now Playing has no tappable close: %v", err)
	}
	tt.Frame()
	if a.nowPlaying {
		t.Error("the close chevron did not leave Now Playing")
	}
}
