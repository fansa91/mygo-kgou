package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"netease-music-mygo/internal/netease"
)

func trForNaming() *netease.Track {
	return &netease.Track{
		ID:       1,
		Name:     "晴天",
		Artists:  []netease.Artist{{Name: "周杰伦"}},
		Album:    netease.Album{Name: "叶惠美"},
		Duration: 250_000, // 4m10s
	}
}

// downloadName keeps a template's tokens, its folders, and what a file name
// may hold, in the order the listener named them.
func TestDownloadName(t *testing.T) {
	tr := trForNaming()
	cases := []struct {
		format, level, ext, want string
	}{
		{"", "", "mp3", "周杰伦 - 晴天.mp3"}, // empty means the default
		{"{歌手} - {歌名}", "", "mp3", "周杰伦 - 晴天.mp3"},
		{"{专辑}/{歌名}", "", "flac", filepath.Join("叶惠美", "晴天") + ".flac"},
		{"{歌名}({音质}){时长}", "lossless", "mp3", "晴天(无损)4m10s.mp3"},
		{"a/b/../x/", "hires", "mp3", filepath.Join("a", "b", "x") + ".mp3"},
	}
	for _, c := range cases {
		got := downloadName(c.format, tr, c.level, c.ext)
		if got != c.want {
			t.Errorf("downloadName(%q) = %q, want %q", c.format, got, c.want)
		}
	}
}

// downloadName, for a file that could not hold, replaces what Windows will
// not keep in a file name, rather than the download failing on a bad byte.
func TestDownloadNameCleans(t *testing.T) {
	tr := &netease.Track{
		ID:      2,
		Name:    "你:我?\"星<>歌|号\\",
		Artists: []netease.Artist{{Name: "歌手/团"}},
		Album:   netease.Album{Name: "专:辑"},
	}
	got := downloadName("{专辑}/{歌手}/{歌名}", tr, "standard", "mp3")
	if strings.ContainsAny(got, `\/:*?"<>|`) {
		// A folder's slash is the separator, which is allowed; the check is
		// that no value left one of the forbidden marks in.
		for _, part := range strings.Split(got, string(filepath.Separator)) {
			if part == "" || part == "." || part == ".." {
				t.Errorf("downloadName made a bad path part: %q", got)
				return
			}
		}
		if strings.ContainsAny(got, `:*?"<>|`) {
			t.Errorf("downloadName left a forbidden mark: %q", got)
		}
	}
	if got == "" || strings.HasSuffix(got, "mp3") && !strings.Contains(got, ".mp3") {
		t.Errorf("downloadName dropped the extension: %q", got)
	}
}

// uniquePath leaves a free name as it is, and numbers one that is not.
func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	freename := filepath.Join(dir, "song.mp3")
	if got := uniquePath(freename); got != freename {
		t.Errorf("uniquePath of a free name moved it: %q", got)
	}
	if err := os.WriteFile(freename, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := uniquePath(freename); got != filepath.Join(dir, "song (2).mp3") {
		t.Errorf("uniquePath of a taken name: got %q, want song (2).mp3", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "song (2).mp3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := uniquePath(freename); got != filepath.Join(dir, "song (3).mp3") {
		t.Errorf("uniquePath of two taken names: got %q, want song (3).mp3", got)
	}
}
