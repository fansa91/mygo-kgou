package netease

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// KuGou knows a song inside a playlist by the "fileid" it handed it there,
// which is not the song's number and not its hash. Taking a song out of a
// playlist asks for that number, so the playlists the app reads leave it
// here, keyed by the playlist and the song.

type fileTable struct {
	mu    sync.Mutex
	path  string
	dirty bool
	byKey map[string]int64 // playlist and song, as "12:34", to the file's id
}

func openFileTable(dir string) (*fileTable, error) {
	t := &fileTable{path: filepath.Join(dir, "kugou-files.json"), byKey: map[string]int64{}}
	if b, err := os.ReadFile(t.path); err == nil {
		json.Unmarshal(b, &t.byKey)
	}
	return t, nil
}

func fileKey(listid, song int64) string {
	return strconv.FormatInt(listid, 10) + ":" + strconv.FormatInt(song, 10)
}

// set notes where a song sits in a playlist.
func (t *fileTable) set(listid, song, file int64) {
	if file == 0 || listid == 0 || song == 0 {
		return
	}
	k := fileKey(listid, song)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byKey[k] == file {
		return
	}
	t.byKey[k] = file
	t.dirty = true
}

// get finds the number a song was given in a playlist.
func (t *fileTable) get(listid, song int64) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.byKey[fileKey(listid, song)]
	return v, ok
}

// forget drops what a song was given, once it is out of the playlist.
func (t *fileTable) forget(listid, song int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.byKey[fileKey(listid, song)]; ok {
		delete(t.byKey, fileKey(listid, song))
		t.dirty = true
	}
}

// save writes the table if it changed.
func (t *fileTable) save() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.dirty {
		return
	}
	b, err := json.Marshal(t.byKey)
	if err != nil {
		return
	}
	os.WriteFile(t.path, b, 0o600)
	t.dirty = false
}
