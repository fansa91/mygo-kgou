package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/netease"
)

// localLibrary makes a library of three songs by two artists, on three
// albums, and opens the page that lists them.
func localLibrary(t *testing.T) *app {
	t.Helper()
	a := newTestApp()
	a.settings = defaultSettings()
	a.setLocal([]local.Track{
		{Path: `C:\music\晴天.mp3`, Name: "晴天", Artist: "周杰伦", Album: "叶惠美", Length: 4 * time.Second},
		{Path: `C:\music\七里香.flac`, Name: "七里香", Artist: "周杰伦", Album: "七里香", Length: 5 * time.Second},
		{Path: `C:\music\江南.flac`, Name: "江南", Artist: "林俊杰", Album: "第二天堂", Length: 4 * time.Second},
	}, 0)
	a.route = route{page: pageLocal}
	return a
}

// The local page lists its songs, and, on the other views, the artists they
// are by and the albums they are on.
func TestLocalPageShowsArtistsAndAlbums(t *testing.T) {
	a := localLibrary(t)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("晴天") || !tt.HasText("3 首 · 2 位歌手 · 3 张专辑") {
		t.Fatal("the page does not list the songs, and what the library holds")
	}

	if err := tt.Click("歌手"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.localTab != localTabArtists {
		t.Fatalf("the page is on tab %d", a.localTab)
	}
	if !tt.HasText("周杰伦") || !tt.HasText("林俊杰") || !tt.HasText("2 首") {
		t.Errorf("the artists view lists %v", tt.Texts())
	}
	if tt.HasText("晴天") {
		t.Error("the artists view lists a song")
	}

	if err := tt.Click("专辑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.localTab != localTabAlbums {
		t.Fatalf("the page is on tab %d", a.localTab)
	}
	if !tt.HasText("叶惠美") || !tt.HasText("七里香") || !tt.HasText("第二天堂") {
		t.Errorf("the albums view lists %v", tt.Texts())
	}
}

// An artist picked from that view lists their songs alone, and the way back
// to the whole library brings them all.
func TestLocalPageOpensAnArtist(t *testing.T) {
	a := localLibrary(t)
	a.localTab = localTabArtists
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("周杰伦"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.localPickRef != "artist:周杰伦" {
		t.Fatalf("picked %q", a.localPickRef)
	}
	if !tt.HasText("晴天") || !tt.HasText("七里香") {
		t.Error("the artist's songs are not listed")
	}
	if tt.HasText("江南") {
		t.Error("a song of another artist is listed")
	}
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.localPickRef != "" {
		t.Errorf("still picked %q after going back", a.localPickRef)
	}
	if !tt.HasText("林俊杰") {
		t.Error("back on the artists view, another artist is missing")
	}
}

// An album picked from its view lists the songs on it, which are only the
// ones of that album by that artist.
func TestLocalPageOpensAnAlbum(t *testing.T) {
	a := localLibrary(t)
	a.localTab = localTabAlbums
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("叶惠美"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got, want := a.localPickRef, "album:叶惠美\x1f周杰伦"; got != want {
		t.Fatalf("picked %q, want %q", got, want)
	}
	if !tt.HasText("晴天") || !tt.HasText("专辑 · 周杰伦 · 1 首") {
		t.Errorf("the album's page says %v", tt.Texts())
	}
	if tt.HasText("七里香") {
		t.Error("a song of another album is listed")
	}
	// Playing the album plays its songs, from their files.
	if err := tt.Click("播放全部"); err != nil {
		t.Fatal(err)
	}
	if q := a.pl.Queue(); len(q) != 1 || q[0].Name != "晴天" || !q[0].Local() {
		t.Errorf("queue = %+v", q)
	}
}

// The filter keeps the songs, the artists and the albums its words match.
func TestLocalPageFiltersTheOtherViews(t *testing.T) {
	a := localLibrary(t)
	a.localTab = localTabAlbums
	a.localFilter = "林俊杰"
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("第二天堂") {
		t.Error("the album of the song the filter matches is missing")
	}
	if tt.HasText("叶惠美") {
		t.Error("the filter kept an album it does not match")
	}
}

// A song of the library shows the format of its file, which the new ones are
// told apart by.
func TestLocalFormats(t *testing.T) {
	for path, want := range map[string]string{
		`C:\music\a.m4a`:  local.FormatM4A,
		`C:\music\a.opus`: local.FormatOpus,
		`C:\music\a.ogg`:  local.FormatOgg,
		`C:\music\a.wav`:  local.FormatWAV,
	} {
		tr := localTrack(local.Track{Path: path, Name: "a"})
		if got := localFormat(&tr); got != want {
			t.Errorf("%s shows %q, want %q", path, got, want)
		}
	}
	// What the file's own tags said wins over what its name says, which is
	// how a lossless file in an .m4a shows as such.
	tr := localTrack(local.Track{Path: `C:\music\a.m4a`, Name: "a", Format: local.FormatALAC})
	if got := localFormat(&tr); got != local.FormatALAC {
		t.Errorf("an ALAC file shows %q", got)
	}
	// A song from the site has no file of its own, so it shows no format.
	var remote netease.Track
	remote.Name, remote.Hash, remote.Album.Name = "a", "h", "b"
	if got := localFormat(&remote); got != "" {
		t.Errorf("a song of KuGou's shows %q", got)
	}
}

// With NM_FRAMES set, saves the three views of the local page.
func TestLocalViewFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, view := range []struct {
		name string
		tab  int
		pick string
	}{
		{"local-songs.png", localTabSongs, ""},
		{"local-artists.png", localTabArtists, ""},
		{"local-albums.png", localTabAlbums, ""},
		{"local-album.png", localTabAlbums, "album:叶惠美\x1f周杰伦"},
	} {
		a := localLibrary(t)
		a.localTab, a.localPickRef = view.tab, view.pick
		tt := ui.NewTester(a.view, 1200, 800)
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, view.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
