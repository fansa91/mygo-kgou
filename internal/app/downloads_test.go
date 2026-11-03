package app

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The downloads page opens the folder downloads go to, reveals a file that
// was saved, and forgets what ended when asked to.
func TestDownloadsPage(t *testing.T) {
	var opened, revealed string
	realOpen, realReveal := openPath, revealPath
	openPath = func(p string) error { opened = p; return nil }
	revealPath = func(p string) { revealed = p }
	defer func() { openPath, revealPath = realOpen, realReveal }()

	a := newTestApp()
	a.settings = defaultSettings()
	dir := t.TempDir()
	a.settings.DownloadDir = dir
	saved := filepath.Join(dir, "周杰伦 - 晴天.flac")
	a.downloads().log = []*downloadTask{
		{id: 1, track: netease.Track{ID: 1, Name: "晴天", Artists: []netease.Artist{{Name: "周杰伦"}}}, state: dlDone, level: "lossless", name: saved},
		{id: 2, track: netease.Track{ID: 2, Name: "江南", Artists: []netease.Artist{{Name: "林俊杰"}}}, state: dlFailed, err: "下载中断，请稍后再试"},
	}
	a.route = route{page: pageDownloads}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.Click("打开文件夹"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if opened != dir {
		t.Errorf("opened %q, want the download folder %q", opened, dir)
	}

	if err := tt.Click("在文件夹中显示"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if revealed != saved {
		t.Errorf("revealed %q, want %q", revealed, saved)
	}

	// Clearing takes away what ended.
	if err := tt.Click("清空已完成"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := len(a.downloads().entries()); got != 0 {
		t.Errorf("the page still holds %d downloads", got)
	}
}
