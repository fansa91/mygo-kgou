package app

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

func cloudTrackOf(id int64, name, singer string, size int64) netease.Track {
	tr := netease.Track{ID: id, Name: name, Duration: 200000, Cloud: true, CloudName: name + ".mp3", Size: size}
	if singer != "" {
		tr.Artists = []netease.Artist{{Name: singer}}
	}
	return tr
}

// The page lists what the cloud holds, and the filter keeps what it says.
func TestCloudPageListsWhatTheCloudHolds(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 1, Nickname: "listener"}
	a.setCloud([]netease.Track{
		cloudTrackOf(2, "晴天", "周杰伦", 4<<20),
		cloudTrackOf(3, "富士山下", "陈奕迅", 30<<20),
	}, 2, nil)
	a.open(route{page: pageCloud})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("音乐云盘") {
		t.Fatal("the page is not the cloud's")
	}
	for _, name := range []string{"晴天", "富士山下"} {
		if !tt.HasText(name) {
			t.Errorf("the page does not list %q", name)
		}
	}
	if !tt.HasText("2 首 · 34 MB") {
		t.Error("the page does not say how much the cloud holds")
	}

	a.cloudFilter = "富士"
	tt.Frame()
	if tt.HasText("晴天") || !tt.HasText("富士山下") {
		t.Error("the filter did not keep just the song it names")
	}
}

// Signed out, the page says what the cloud is for and offers the login.
func TestCloudPageAsksForALogin(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.open(route{page: pageCloud})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("登录后可以听云盘里的歌") {
		t.Fatal("the page does not ask for a login")
	}
	if tt.HasText("上传") {
		t.Error("the page offers an upload to a listener who is not signed in")
	}
}

// The cloud is read a page at a time until it is all there.
func TestCloudPagingReadsEveryPage(t *testing.T) {
	old := cloudSongs
	t.Cleanup(func() { cloudSongs = old })
	pages := 0
	cloudSongs = func(page, size int) ([]netease.Track, int, error) {
		pages++
		if page == 1 {
			out := make([]netease.Track, size)
			for i := range out {
				out[i] = cloudTrackOf(int64(i+1), fmt.Sprint("歌", i+1), "", 1<<20)
			}
			return out, 130, nil
		}
		out := make([]netease.Track, 30)
		for i := range out {
			out[i] = cloudTrackOf(int64(100+i), fmt.Sprint("歌", 100+i), "", 1<<20)
		}
		return out, 130, nil
	}
	tracks, total, err := fetchCloud()
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 130 || total != 130 {
		t.Errorf("read %d songs of %d", len(tracks), total)
	}
	if pages != 2 {
		t.Errorf("asked %d pages, want 2", pages)
	}
}

// A file with no tags is named as its file is.
func TestCloudFileInfoFallsBackToTheFileName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "周杰伦 - 晴天.mp3")
	f := cloudFileInfo(path)
	if f.Name != "周杰伦 - 晴天" {
		t.Errorf("name is %q", f.Name)
	}
	if f.Path != path {
		t.Errorf("path is %q", f.Path)
	}
}

// Deleting takes the song out of what the page shows.
func TestCloudDeleteForgetsTheSong(t *testing.T) {
	tracks := []netease.Track{
		cloudTrackOf(1, "一", "", 1),
		cloudTrackOf(2, "二", "", 1),
		cloudTrackOf(3, "三", "", 1),
	}
	left := removeTrack(tracks, 2)
	if len(left) != 2 {
		t.Fatalf("%d songs left", len(left))
	}
	for _, tr := range left {
		if tr.ID == 2 {
			t.Error("the deleted song is still listed")
		}
	}
}

func TestCloudSizeText(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{900, "900 B"},
		{2048, "2 KB"},
		{5 << 20, "5 MB"},
		{3 << 30, "3.0 GB"},
	}
	for _, c := range cases {
		if got := cloudSizeText(c.n); got != c.want {
			t.Errorf("%d is %q, want %q", c.n, got, c.want)
		}
	}
}
