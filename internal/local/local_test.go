package local

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

	"netease-music-mygo/internal/tag"
)

// coverPNG returns a small picture, as a file's tag holds one.
func coverPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// copyFile puts the audio of a fixture at dst, as a library holds it.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A song downloaded by the app is tagged by it: the library reads back what
// the download wrote, its picture and its words included.
func TestReadWhatTheDownloadWrote(t *testing.T) {
	for _, tc := range []struct{ name, fixture, format string }{
		{"song.mp3", "../player/testdata/seek.mp3", FormatMP3},
		{"song.flac", "../player/testdata/seek.flac", FormatFLAC},
	} {
		t.Run(tc.format, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tc.name)
			copyFile(t, tc.fixture, path)
			pic := coverPNG(t)
			err := tag.Write(path, tag.Meta{
				Title: "晴天", Artist: "周杰伦", Album: "叶惠美", AlbumArtist: "周杰伦",
				Track: "3", Disc: "1", Year: "2003",
				Lyrics: "[00:00.00]故事的小黄花\n[00:04.00]从出生那年就飘着",
				Cover:  pic, MIME: "image/png",
			})
			if err != nil {
				t.Fatal(err)
			}
			tr, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			if tr.Name != "晴天" || tr.Artist != "周杰伦" || tr.Album != "叶惠美" {
				t.Errorf("tags = %q / %q / %q", tr.Name, tr.Artist, tr.Album)
			}
			if tr.TrackNo != 3 || tr.DiscNo != 1 || tr.Year != "2003" {
				t.Errorf("number = %d/%d year %q", tr.TrackNo, tr.DiscNo, tr.Year)
			}
			if tr.Format != tc.format {
				t.Errorf("format = %q", tr.Format)
			}
			if tr.Length < 4*time.Second || tr.Length > 6*time.Second {
				t.Errorf("length = %v", tr.Length)
			}
			if !tr.Covered || !bytes.Equal(tr.Cover, pic) {
				t.Errorf("cover = %d bytes, covered %v", len(tr.Cover), tr.Covered)
			}
			if !strings.Contains(tr.Lyrics, "故事的小黄花") {
				t.Errorf("lyrics = %q", tr.Lyrics)
			}
		})
	}
}

// A library of files that were never tagged, as one downloaded by a browser
// is, takes its names from the files' own.
func TestNamesFromFiles(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, "../player/testdata/seek.mp3", filepath.Join(dir, "01 - 陈奕迅 - 富士山下.mp3"))
	copyFile(t, "../player/testdata/seek.mp3", filepath.Join(dir, "02. 浮夸.mp3"))
	copyFile(t, "../player/testdata/seek.mp3", filepath.Join(dir, "单车.mp3"))
	tracks, skipped, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(tracks) != 3 {
		t.Fatalf("scan = %d songs, %d skipped (%v)", len(tracks), skipped, err)
	}
	by := map[string]Track{}
	for _, tr := range tracks {
		by[tr.Name] = tr
	}
	if tr, ok := by["富士山下"]; !ok || tr.Artist != "陈奕迅" {
		t.Errorf("01 - 陈奕迅 - 富士山下 read as %q by %q", tr.Name, tr.Artist)
	}
	if tr, ok := by["浮夸"]; !ok || tr.Artist != "" {
		t.Errorf("02. 浮夸 read as %q by %q", tr.Name, tr.Artist)
	}
	if _, ok := by["单车"]; !ok {
		t.Error("单车 not read")
	}
}

// The scan walks the folders under the one it is given, skips the hidden
// ones, and counts the songs of formats the app does not play.
func TestScanWalksAndSkips(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"rock", ".cache"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	copyFile(t, "../player/testdata/seek.mp3", filepath.Join(dir, "rock", "a.mp3"))
	copyFile(t, "../player/testdata/seek.flac", filepath.Join(dir, "b.flac"))
	copyFile(t, "../player/testdata/seek.mp3", filepath.Join(dir, ".cache", "c.mp3"))
	if err := os.WriteFile(filepath.Join(dir, "d.m4a"), []byte("not a song"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	tracks, skipped, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 {
		t.Errorf("scan read %d songs: %v", len(tracks), tracks)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	// Sorted by their names, as a library lists them: a.flac? no: a, b.
	if tracks[0].Name != "a" || tracks[1].Name != "b" {
		t.Errorf("order = %q, %q", tracks[0].Name, tracks[1].Name)
	}
}

// The picture and the words a file does not hold are the ones beside it.
func TestCoverAndLyricsBesideTheSong(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.mp3")
	copyFile(t, "../player/testdata/seek.mp3", path)
	pic := coverPNG(t)
	if err := os.WriteFile(filepath.Join(dir, "cover.png"), pic, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "song.lrc"), []byte("[00:00.00]故事的小黄花\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tracks, _, err := Scan(dir)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("scan = %d (%v)", len(tracks), err)
	}
	if !tracks[0].Covered {
		t.Error("the song beside a picture is not marked covered")
	}
	got, err := Cover(path)
	if err != nil || !bytes.Equal(got, pic) {
		t.Errorf("cover = %d bytes, %v", len(got), err)
	}
	if lrc := Lyrics(path); !strings.Contains(lrc, "故事的小黄花") {
		t.Errorf("lyrics = %q", lrc)
	}
}

func TestYearOf(t *testing.T) {
	for in, want := range map[string]string{
		"2006":       "2006",
		"2006-11-23": "2006",
		" 1999 ":     "1999",
		"":           "",
		"11/23/2006": "",
		"1136214245": "",
	} {
		if got := yearOf(in); got != want {
			t.Errorf("yearOf(%q) = %q, want %q", in, got, want)
		}
	}
}
