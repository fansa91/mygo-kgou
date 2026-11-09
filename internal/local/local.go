// Package local reads the music in the listener's own folders: what each
// file says it is, how long it plays, the picture it holds and the words
// that sit beside it. It is where the app plays a song from when the song
// is a file rather than a stream.
package local

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The formats the app plays from a file, as a page shows them.
const (
	FormatMP3  = "MP3"
	FormatFLAC = "FLAC"
	FormatOgg  = "OGG"
	FormatWAV  = "WAV"
)

// ErrFormat reports a file of a format the app does not read, as one of
// the formats Apple sells is.
var ErrFormat = errors.New("local: a file of a format the app does not play")

// A Track is a song found in a folder of the listener's.
type Track struct {
	// Path is where the song's file is.
	Path string
	// Name is the song's title, or its file's name when it is tagged with
	// none, as the files of a library that was never tagged are.
	Name        string
	Artist      string
	Album       string
	AlbumArtist string
	// Year is when the song came out, "2006" or "2006-11-23" as the file
	// says it; TrackNo its number on the album, DiscNo the disc it is on.
	Year            string
	TrackNo, DiscNo int
	// Length is how long it plays, as its file says.
	Length time.Duration
	// Format is the file's format: FormatMP3, FormatFLAC, FormatOgg or
	// FormatWAV.
	Format string
	// Cover is the album's picture, when the file holds one. Scan leaves
	// it out: a library of thousands would keep a hundred pictures each.
	Cover []byte
	// Covered says the song has a picture to show, in its file or beside
	// it.
	Covered bool
	// Lyrics is the song's words as its file keeps them, if it does.
	Lyrics string
}

// Read reads the song at path. It fails with ErrFormat for a file of a
// format the app does not play.
func Read(path string) (Track, error) {
	f, err := os.Open(path)
	if err != nil {
		return Track{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Track{}, err
	}
	var m meta
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		m, err = readMP3(f, st.Size())
	case ".flac":
		m, err = readFLAC(f, st.Size())
	case ".ogg", ".oga":
		m, err = readOgg(f, st.Size())
	case ".wav":
		m, err = readWAV(f, st.Size())
	default:
		return Track{}, ErrFormat
	}
	if err != nil {
		return Track{}, err
	}
	t := Track{
		Path:        path,
		Name:        strings.TrimSpace(m.title),
		Artist:      strings.TrimSpace(m.artist),
		Album:       strings.TrimSpace(m.album),
		AlbumArtist: strings.TrimSpace(m.albumArtist),
		Year:        yearOf(m.year),
		Length:      m.length,
		Format:      m.format,
		Cover:       m.cover,
		Covered:     len(m.cover) > 0,
		Lyrics:      m.lyrics,
	}
	t.TrackNo = number(m.track)
	t.DiscNo = number(m.disc)
	if t.Name == "" {
		t.Name, t.Artist = nameOfFile(path, t.Artist)
	}
	if t.Artist == "" && t.AlbumArtist != "" {
		t.Artist = t.AlbumArtist
	}
	return t, nil
}

// Scan reads every song in dir and the folders under it, in the order a
// library lists them: by artist, then album, then the album's order. It
// skips the folders and the files that start with a dot, and counts the
// songs it cannot play, which are returned as skipped; a folder it cannot
// read is passed over rather than failing the scan.
func Scan(dir string) (tracks []Track, skipped int, err error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, 0, err
	}
	if !st.IsDir() {
		return nil, 0, fmt.Errorf("local: %s is not a folder", dir)
	}
	var paths []string
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if playable(d.Name()) {
			paths = append(paths, path)
			return nil
		}
		if audioName(d.Name()) {
			skipped++
		}
		return nil
	})
	if err != nil {
		return nil, skipped, err
	}
	tracks, failed := readAll(paths)
	skipped += failed
	// The pictures beside the songs: the ones whose own file holds none.
	covers := map[string]bool{}
	for i := range tracks {
		if tracks[i].Covered {
			continue
		}
		dir := filepath.Dir(tracks[i].Path)
		has, seen := covers[dir]
		if !seen {
			has = folderCover(dir) != nil
			covers[dir] = has
		}
		tracks[i].Covered = has
	}
	sortLib(tracks)
	return tracks, skipped, nil
}

// readAll reads the tags of every path, a few at a time, and returns the
// songs it read and how many it could not.
func readAll(paths []string) ([]Track, int) {
	if len(paths) == 0 {
		return nil, 0
	}
	out := make([]Track, len(paths))
	bad := make([]bool, len(paths))
	var next atomic.Int64
	var wg sync.WaitGroup
	workers := min(runtime.NumCPU(), 6)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= len(paths) {
					return
				}
				t, err := Read(paths[i])
				if err != nil {
					bad[i] = true
					continue
				}
				// The picture stays out of the library: it loads again
				// when the row or the song shows it.
				t.Cover = nil
				out[i] = t
			}
		}()
	}
	wg.Wait()
	tracks := make([]Track, 0, len(paths))
	failed := 0
	for i, t := range out {
		if bad[i] {
			failed++
			continue
		}
		tracks = append(tracks, t)
	}
	return tracks, failed
}

// sortLib puts the songs in a library's order: by artist, then album,
// then the album's own order, then title.
func sortLib(tracks []Track) {
	sort.SliceStable(tracks, func(i, j int) bool {
		a, b := &tracks[i], &tracks[j]
		if c := strings.Compare(fold(a.Artist), fold(b.Artist)); c != 0 {
			return c < 0
		}
		if c := strings.Compare(fold(a.Album), fold(b.Album)); c != 0 {
			return c < 0
		}
		if a.DiscNo != b.DiscNo {
			return a.DiscNo < b.DiscNo
		}
		if a.TrackNo != b.TrackNo {
			return a.TrackNo < b.TrackNo
		}
		return strings.Compare(fold(a.Name), fold(b.Name)) < 0
	})
}

func fold(s string) string { return strings.ToLower(s) }

// Cover returns the song's picture: the one in its file, or else the one
// beside it.
func Cover(path string) ([]byte, error) {
	t, err := Read(path)
	if err == nil && len(t.Cover) > 0 {
		return t.Cover, nil
	}
	if b := folderCover(filepath.Dir(path)); b != nil {
		return b, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("local: the song has no picture")
}

// Lyrics returns the song's words: as its file keeps them, or else from
// the .lrc file beside it, which a library of lyrics downloads has.
func Lyrics(path string) string {
	if t, err := Read(path); err == nil && strings.TrimSpace(t.Lyrics) != "" {
		return t.Lyrics
	}
	b, err := os.ReadFile(strings.TrimSuffix(path, filepath.Ext(path)) + ".lrc")
	if err != nil {
		return ""
	}
	return decodeFile(b)
}

// coverNames are the names a folder's picture goes by.
var coverNames = []string{"cover.jpg", "folder.jpg", "front.jpg", "cover.png", "folder.png", "albumartsmall.jpg"}

// folderCover returns the picture stored beside the songs of dir, or nil.
func folderCover(dir string) []byte {
	for _, name := range coverNames {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil && len(b) > 0 {
			return b
		}
	}
	return nil
}

// playable reports whether the file of this name is one the app plays.
func playable(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp3", ".flac", ".ogg", ".oga", ".wav":
		return true
	}
	return false
}

// audioName reports whether the file of this name is a song, of a format
// the app plays or of one it does not, as a skip counts.
func audioName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp3", ".flac", ".ogg", ".oga", ".wav", ".m4a", ".aac", ".wma", ".ape", ".opus", ".aiff", ".dsf":
		return true
	}
	return false
}

// nameOfFile returns the song's name and artist as its file's name has
// them, for a file that is tagged with neither: "01 - 歌手 - 歌名" and
// "歌名" are what a library of them holds.
func nameOfFile(path, artist string) (name, who string) {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = stripNumber(base)
	if artist == "" {
		if a, n, ok := strings.Cut(base, " - "); ok && strings.TrimSpace(a) != "" && strings.TrimSpace(n) != "" {
			return strings.TrimSpace(n), strings.TrimSpace(a)
		}
	}
	return strings.TrimSpace(base), artist
}

// stripNumber takes a leading track number off a file's name, as a ripper
// writes it: "01 - ", "02. " and "03_".
func stripNumber(base string) string {
	i := 0
	for i < len(base) && base[i] >= '0' && base[i] <= '9' {
		i++
	}
	if i == 0 || i == len(base) {
		return base
	}
	j := i
	for j < len(base) {
		switch base[j] {
		case '.', '-', '_', ' ':
			j++
		default:
			if j > i && j < len(base) {
				return strings.TrimSpace(base[j:])
			}
			return base
		}
	}
	return base
}

// number reads the first number of s, as "3" and "3/12" both are, and 0
// for none.
func number(s string) int {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// yearOf returns the year of s as a page shows it: "2006" for "2006" and
// for "2006-11-23", and "" for anything else, as a tag that holds a
// timestamp, or a date written the other way round, is no year.
func yearOf(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return ""
	}
	for _, c := range s[:4] {
		if c < '0' || c > '9' {
			return ""
		}
	}
	n, err := strconv.Atoi(s[:4])
	if err != nil || n < 1900 || n > 2100 {
		return ""
	}
	return s[:4]
}
