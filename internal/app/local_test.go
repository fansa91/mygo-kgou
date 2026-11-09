package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/tag"
)

// localSongs puts the songs of a library in dir, as the app downloads them:
// each tagged with what it is.
func localSongs(t *testing.T, dir string) {
	t.Helper()
	for _, song := range []struct{ file, title, artist, album string }{
		{"01 晴天.mp3", "晴天", "周杰伦", "叶惠美"},
		{"02 江南.flac", "江南", "林俊杰", "第二天堂"},
	} {
		fixture := "../player/testdata/seek.mp3"
		if strings.HasSuffix(song.file, ".flac") {
			fixture = "../player/testdata/seek.flac"
		}
		b, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, song.file)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		err = tag.Write(path, tag.Meta{Title: song.title, Artist: song.artist, Album: song.album, Track: "1", Year: "2003"})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The local page lists the songs of the folder it reads, and plays the one
// clicked from its file.
func TestLocalPagePlaysTheFiles(t *testing.T) {
	dir := t.TempDir()
	localSongs(t, dir)
	tracks, skipped, err := local.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.LocalDir = dir
	a.setLocal(tracks, skipped)
	a.route = route{page: pageLocal}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if !tt.HasText("晴天") || !tt.HasText("江南") {
		t.Fatal("the page does not list the songs of the folder")
	}
	if !tt.HasText("2 首") {
		t.Error("the page does not say how many songs it holds")
	}
	// The folder goes, so that what the click does is seen without a
	// decoder holding the file the test clears: that the song plays from
	// its file is the player's own test.
	gone := filepath.Join(t.TempDir(), "晴天.mp3")
	a.setLocal([]local.Track{{Path: gone, Name: "晴天", Artist: "周杰伦", Length: 4 * time.Second}}, 0)
	tt.Frame()
	if err := tt.Click("晴天"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	cur := a.pl.Current()
	if cur == nil || cur.Name != "晴天" || !cur.Local() {
		t.Fatalf("playing %+v", cur)
	}
	if cur.Path != gone {
		t.Errorf("playing %q, want %q", cur.Path, gone)
	}
	if cur.Duration != 4000 || cur.ID >= 0 {
		t.Errorf("the song has %d ms and id %d", cur.Duration, cur.ID)
	}
	// The queue is the page's list, and plays from the disk too.
	if q := a.pl.Queue(); len(q) != 1 || !q[0].Local() {
		t.Errorf("queue = %+v", q)
	}
}

// The filter keeps the songs whose name, artist or album holds what is
// typed, and clearing it brings them all back.
func TestLocalPageFilters(t *testing.T) {
	dir := t.TempDir()
	localSongs(t, dir)
	tracks, skipped, err := local.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.LocalDir = dir
	a.setLocal(tracks, skipped)
	a.route = route{page: pageLocal}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	a.localFilter = "林俊杰"
	tt.Frame()
	if tt.HasText("晴天") {
		t.Error("the filter kept a song by another artist")
	}
	if !tt.HasText("江南") {
		t.Error("the filter dropped the song it matches")
	}
	a.localFilter = "没有这首"
	tt.Frame()
	if !tt.HasText("没有符合的歌") {
		t.Error("the page does not say the filter keeps nothing")
	}
}

// A song of the local library is named by its file, and shows what the file
// holds: its picture from the file, and the format it is.
func TestLocalTrack(t *testing.T) {
	tr := localTrack(local.Track{
		Path: `C:\music\晴天.flac`, Name: "晴天", Artist: "周杰伦", Album: "叶惠美",
		Length: 4*time.Second + 520*time.Millisecond, Year: "2003", TrackNo: 3, Covered: true,
	})
	if !tr.Local() || tr.ID >= 0 {
		t.Errorf("local %v, id %d", tr.Local(), tr.ID)
	}
	if tr.Duration != 4520 {
		t.Errorf("duration = %d ms", tr.Duration)
	}
	if tr.Album.PicURL != localCoverURL(`C:\music\晴天.flac`) {
		t.Errorf("picture = %q", tr.Album.PicURL)
	}
	if got := localFormat(&tr); got != local.FormatFLAC {
		t.Errorf("format = %q", got)
	}
	// The same file is the same song from one run to the next.
	if again := localTrack(local.Track{Path: `C:\music\晴天.flac`}); again.ID != tr.ID {
		t.Errorf("the same file is %d and then %d", tr.ID, again.ID)
	}
	if other := localTrack(local.Track{Path: `C:\music\江南.flac`}); other.ID == tr.ID {
		t.Error("two files are one song")
	}
	// A song of KuGou's says its quality, and no format.
	remote := netease.Track{ID: 1, HashLossl: "abc"}
	if got := localFormat(&remote); got != "" {
		t.Errorf("a song of KuGou's shows %q", got)
	}
}

// The song playing from a file has no comments to read: Now Playing shows
// its lyrics instead of the tab, and falls back to them when the tab was
// the one showing.
func TestLocalSongHasNoCommentsTab(t *testing.T) {
	a := newTestApp()
	tr := localTrack(local.Track{Path: `/music/a.mp3`, Name: "晴天", Length: 4 * time.Second})
	a.pl.Cue([]netease.Track{tr})
	a.npPane = paneComments
	tt := ui.NewTester(a.view, 1200, 800)
	a.nowPlaying = true
	tt.Frame()
	if a.npPane == paneComments {
		t.Error("Now Playing kept the comments of a file")
	}
	for _, tab := range a.npTabs() {
		if tab.pane == paneComments {
			t.Error("the tabs offer comments for a file")
		}
	}
}

// A song's picture loads from its file, through the cache the CDN's covers
// go through.
func TestLocalCoverLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp3")
	b, err := os.ReadFile("../player/testdata/seek.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 0x40
	}
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var pic bytes.Buffer
	if err := png.Encode(&pic, img); err != nil {
		t.Fatal(err)
	}
	if err := tag.Write(path, tag.Meta{Title: "晴天", Cover: pic.Bytes(), MIME: "image/png"}); err != nil {
		t.Fatal(err)
	}
	bmp, decoded, size, err := loadArt(localCoverURL(path))
	if err != nil {
		t.Fatal(err)
	}
	if bmp == nil || decoded == nil || size == 0 {
		t.Fatalf("cover = %v, %v, %d bytes", bmp, decoded, size)
	}
	if got := decoded.Bounds().Dx(); got != 8 {
		t.Errorf("the picture is %d pixels wide", got)
	}
	// A cover on the CDN is not one of the library's.
	if _, ok := localCoverPath("https://p1.music.126.net/x.jpg"); ok {
		t.Error("a CDN cover was taken for a file's")
	}
}
