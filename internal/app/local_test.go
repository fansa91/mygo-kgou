package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/localfolder"
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

// The songs picked on a phone are copied into the app's folder, each under
// its own name: picking them again replaces the copies rather than adding
// more.
func TestCopyLocalSongsKeepsTheSongs(t *testing.T) {
	src := t.TempDir()
	want := map[string]string{
		"01 晴天.mp3":  "qingtian",
		"02 江南.flac": "jiangnan",
	}
	var paths []string
	for name, body := range want {
		path := filepath.Join(src, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}

	dst := filepath.Join(t.TempDir(), "Music")
	for round := 1; round <= 2; round++ {
		n, err := copyLocalSongs(paths, dst)
		if err != nil || n != len(paths) {
			t.Fatalf("round %d copied %d songs: %v", round, n, err)
		}
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(want) {
		t.Errorf("the folder holds %d files, want %d", len(entries), len(want))
	}
	for name, body := range want {
		got, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil || string(got) != body {
			t.Errorf("%s = %q, %v", name, got, err)
		}
	}
}

// Two songs that share a name are two songs: the second is kept under a
// number rather than replacing the first.
func TestCopyLocalSongsKeepsNamesApart(t *testing.T) {
	one, two := t.TempDir(), t.TempDir()
	first := filepath.Join(one, "track01.mp3")
	second := filepath.Join(two, "track01.mp3")
	if err := os.WriteFile(first, []byte("first song"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("a second song"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()
	if n, err := copyLocalSongs([]string{first, second}, dst); err != nil || n != 2 {
		t.Fatalf("copied %d songs: %v", n, err)
	}
	for name, want := range map[string]string{
		"track01.mp3":     "first song",
		"track01 (2).mp3": "a second song",
	} {
		got, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", name, got, err)
		}
	}
}

// A phone has no folder to pick: its page offers songs instead, which are
// copied into the app's own folder when picked.
func TestPhoneLocalAddsSongs(t *testing.T) {
	// A phone without the folder plugin can only be handed songs.
	folderInPlace = func() bool { return false }
	t.Cleanup(func() { folderInPlace = localfolder.Available })
	src := t.TempDir()
	song := filepath.Join(src, "晴天.mp3")
	if err := os.WriteFile(song, []byte("song"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Music")

	openLocalDialog = func(mygo.OpenDialogOptions) ([]string, error) { return []string{song}, nil }
	t.Cleanup(func() { openLocalDialog = mygo.Dialog.Open })
	localMusicDirPath = func() (string, error) { return dir, nil }
	t.Cleanup(func() { localMusicDirPath = localMusicDir })

	a := phoneApp(t)
	a.settings = defaultSettings()
	a.settings.LocalDir = t.TempDir()
	a.localScanned, a.localScannedDir = time.Now(), a.settings.LocalDir
	a.localTracks = []netease.Track{{ID: 1, Name: "晴天", Path: song}}
	a.route = route{page: pageLocal}
	tt := ui.NewTester(a.view, 390, 844)
	tt.Frame()
	if !tt.HasText("添加歌曲") {
		t.Fatalf("the phone's local page has no way to add songs: %v", tt.Texts())
	}
	if tt.HasText("选择文件夹") {
		t.Errorf("the phone's local page still picks a folder: %v", tt.Texts())
	}

	if err := tt.Click("添加歌曲"); err != nil {
		t.Fatal(err)
	}
	// The picker and the copy run on their own; the post that scans the
	// folder needs a window, which the tester has not.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(dir, "晴天.mp3")); err == nil && string(b) == "song" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the song picked was not copied into the app's folder")
}

// The desktop keeps picking a folder, whose songs the library reads where
// they are.
func TestDesktopLocalPicksAFolder(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.LocalDir = t.TempDir()
	a.localScanned, a.localScannedDir = time.Now(), a.settings.LocalDir
	a.route = route{page: pageLocal}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("选择文件夹") || tt.HasText("添加歌曲") {
		t.Errorf("the desktop's local page does not pick a folder: %v", tt.Texts())
	}
}

// A phone with the folder plugin reads a folder of its own storage where it
// is: the songs stay there, and the folders under it come with them.
func TestPhoneLocalReadsAFolderInPlace(t *testing.T) {
	dir := t.TempDir()
	// The songs sit under folders of their own, as a library of files does.
	sub := filepath.Join(dir, "周杰伦", "叶惠美")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	localSongs(t, sub)

	folderInPlace = func() bool { return true }
	t.Cleanup(func() { folderInPlace = localfolder.Available })
	picked := make(chan string, 1)
	pickLocalFolder = func(context.Context) (string, error) {
		picked <- dir
		return dir, nil
	}
	t.Cleanup(func() { pickLocalFolder = localfolder.Pick })
	// Songs are not picked one by one when a folder is read.
	openLocalDialog = func(mygo.OpenDialogOptions) ([]string, error) {
		t.Error("the phone opened the picker of songs")
		return nil, nil
	}
	t.Cleanup(func() { openLocalDialog = mygo.Dialog.Open })

	a := phoneApp(t)
	a.settings = defaultSettings()
	a.route = route{page: pageLocal}
	tt := ui.NewTester(a.view, 390, 844)
	tt.Frame()
	if !tt.HasText("选择文件夹") {
		t.Fatalf("the phone's local page does not pick a folder: %v", tt.Texts())
	}
	if err := tt.Click("选择文件夹"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-picked:
		if got != dir {
			t.Errorf("the picker asked for %q, not %q", got, dir)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the phone did not ask for a folder")
	}

	// The songs are read where they are, in folders of their own and all.
	songs, _, err := local.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 2 {
		t.Errorf("the folder under the one picked holds %d songs, not 2", len(songs))
	}
}

// A phone that added songs by copy keeps them: they live in the app's own
// folder, which needs no permission to be taken back.
func TestPhoneLocalKeepsTheSongsAdded(t *testing.T) {
	dir := t.TempDir()
	a := phoneApp(t)
	a.settings = defaultSettings()
	a.settings.LocalDir = dir

	// Nothing was picked in an earlier run, so nothing is taken back.
	restoreLocalFolder = func(context.Context) (localfolder.Restored, error) {
		t.Error("the page asked for a folder nothing picked")
		return localfolder.Restored{}, nil
	}
	t.Cleanup(func() { restoreLocalFolder = localfolder.Restore })
	folderInPlace = func() bool { return true }
	t.Cleanup(func() { folderInPlace = localfolder.Available })

	a.reopenLocalFolder()
	time.Sleep(20 * time.Millisecond)
	if a.settings.LocalDir != dir {
		t.Errorf("the songs added lost their folder: %q", a.settings.LocalDir)
	}
	if a.localLost {
		t.Error("the page asks for a folder it never had to take back")
	}
}

// A phone whose folder the system took the permission to back has none: the
// page says so, and offers picking it again.
func TestPhoneLocalLostTheFolder(t *testing.T) {
	// What the folder the app took back means for the library.
	if next := localReopened(localfolder.Restored{Saved: true}); !next.lost || !next.forget || next.path != "" {
		t.Errorf("a lost folder is still read: %+v", next)
	}
	if next := localReopened(localfolder.Restored{Path: "/music", Saved: true}); next.path != "/music" || next.lost {
		t.Errorf("a folder still there is not read: %+v", next)
	}
	if next := localReopened(localfolder.Restored{}); next.path != "" || next.lost || next.forget {
		t.Errorf("a run that never picked a folder has one to lose: %+v", next)
	}

	a := phoneApp(t)
	a.settings = defaultSettings()
	a.settings.LocalDir = t.TempDir()
	a.settings.LocalPicked = true
	a.localTracks = []netease.Track{{ID: 1, Name: "晴天"}}
	// The folder is gone: the library has none, and the page asks for one.
	a.settings.LocalDir, a.settings.LocalPicked = "", false
	a.localLost = true
	if a.localDir() != "" {
		t.Errorf("a lost folder is still read: %q", a.localDir())
	}
	a.scanLocal()
	if len(a.localTracks) != 0 {
		t.Errorf("a lost folder still lists %d songs", len(a.localTracks))
	}
	a.route = route{page: pageLocal}
	tt := ui.NewTester(a.view, 390, 844)
	tt.Frame()
	if !tt.HasText("重新选择") {
		t.Errorf("the page does not ask for the folder again: %v", tt.Texts())
	}
}

// A run takes the folder picked before back, before the page lists the songs
// it holds.
func TestPhoneLocalReopensTheFolder(t *testing.T) {
	dir := t.TempDir()
	folderInPlace = func() bool { return true }
	t.Cleanup(func() { folderInPlace = localfolder.Available })
	asked := make(chan bool, 1)
	restoreLocalFolder = func(context.Context) (localfolder.Restored, error) {
		asked <- true
		return localfolder.Restored{Path: dir, Saved: true}, nil
	}
	t.Cleanup(func() { restoreLocalFolder = localfolder.Restore })

	a := phoneApp(t)
	a.settings = defaultSettings()
	a.settings.LocalDir, a.settings.LocalPicked = dir, true
	a.reopenLocalFolder()
	select {
	case <-asked:
	case <-time.After(2 * time.Second):
		t.Fatal("the run did not take the folder back")
	}
}

// Android hands the app a document tree it cannot walk by path: the native
// plugin lists the files, and Go reads those files where they are.
func TestAndroidLocalReadsTheDocumentTree(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "周杰伦", "叶惠美")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	localSongs(t, sub)

	scansDocumentTree = true
	t.Cleanup(func() { scansDocumentTree = runtime.GOOS == "android" })
	called := false
	listLocalFolder = func(context.Context) ([]string, error) {
		called = true
		return []string{
			filepath.Join(sub, "01 晴天.mp3"),
			filepath.Join(sub, "02 江南.flac"),
			filepath.Join(dir, "cover.jpg"),
		}, nil
	}
	t.Cleanup(func() { listLocalFolder = localfolder.List })

	a := phoneApp(t)
	a.settings = defaultSettings()
	a.settings.LocalDir = dir
	a.settings.LocalPicked = true
	tracks, _, err := a.readLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("the document tree was not asked for its files")
	}
	if len(tracks) != 2 {
		t.Errorf("read %d songs from the tree: %v", len(tracks), tracks)
	}

	// A tree that cannot be listed is an error, not an empty library.
	listLocalFolder = func(context.Context) ([]string, error) {
		return nil, errors.New("permission gone")
	}
	if _, _, err := a.readLocal(dir); err == nil {
		t.Error("a folder that cannot be listed read as an empty library")
	}
}
