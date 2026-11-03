package kugou

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// The app names a song by a number: its download cache, its play queue, its
// liked songs and its comment threads all key on int64. KuGou names a song by
// a hash, so the adapter needs a number for it, and needs the same number
// every time or a cached song is lost on the next run.
//
// SongID gives that number: the first eight bytes of the hash's MD5, kept
// positive. It is stable across runs and machines, which is all the app asks.
// The rest of what a song needs to play — its album audio, its album — is
// remembered in an index beside the device, keyed by that number.

// SongID is the number this app calls a song, derived from its hash.
func SongID(hash string) int64 {
	sum := md5.Sum([]byte(hash))
	return int64(binary.BigEndian.Uint64(sum[:8]) >> 1)
}

// Known is what the index remembers about a song, beyond its hash.
type Known struct {
	Hash         string `json:"hash"`
	AlbumAudioID int64  `json:"album_audio_id"`
	AlbumID      int64  `json:"album_id"`
	Name         string `json:"name,omitempty"`
	Singer       string `json:"singer,omitempty"`
	Year         string `json:"year,omitempty"`
	TrackNo      int    `json:"track_no,omitempty"`
	DiscNo       int    `json:"disc_no,omitempty"`
}

// Index remembers the songs that have been seen, so a song can be played by
// number after the app restarts.
type Index struct {
	mu   sync.Mutex
	path string
	byID map[int64]Known
}

// OpenIndex loads the index from dir, which is the app's user data directory.
func OpenIndex(dir string) (*Index, error) {
	x := &Index{
		path: filepath.Join(dir, "kugou-songs.json"),
		byID: map[int64]Known{},
	}
	b, err := os.ReadFile(x.path)
	if err == nil {
		json.Unmarshal(b, &x.byID)
	}
	return x, nil
}

// Remember notes a song and answers its ID.
func (x *Index) Remember(t Track) int64 {
	id := SongID(t.Hash)
	x.mu.Lock()
	defer x.mu.Unlock()
	known := x.byID[id]
	known.Hash = t.Hash
	if t.AlbumAudioID != 0 {
		known.AlbumAudioID = t.AlbumAudioID
	}
	if t.AlbumID != 0 {
		known.AlbumID = t.AlbumID
	}
	if t.Name != "" {
		known.Name = t.Name
	}
	if t.Singer != "" {
		known.Singer = t.Singer
	}
	if t.Year != "" {
		known.Year = t.Year
	}
	if t.TrackNo != 0 {
		known.TrackNo = t.TrackNo
	}
	if t.DiscNo != 0 {
		known.DiscNo = t.DiscNo
	}
	if known != x.byID[id] {
		x.byID[id] = known
		x.save()
	}
	return id
}

// Lookup finds what a number names.
func (x *Index) Lookup(id int64) (Known, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	k, ok := x.byID[id]
	return k, ok
}

// Track turns what the index remembers into a song the other calls take.
func (x *Index) Track(id int64) (Track, bool) {
	k, ok := x.Lookup(id)
	if !ok {
		return Track{}, false
	}
	return Track{
		Hash:         k.Hash,
		AlbumAudioID: k.AlbumAudioID,
		AlbumID:      k.AlbumID,
		Name:         k.Name,
		Singer:       k.Singer,
		Year:         k.Year,
		TrackNo:      k.TrackNo,
		DiscNo:       k.DiscNo,
	}, true
}

func (x *Index) save() {
	b, err := json.Marshal(x.byID)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(x.path), 0o700)
	os.WriteFile(x.path, b, 0o600)
}
