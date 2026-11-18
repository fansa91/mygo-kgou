package netease

import (
	"errors"
	"strings"

	"netease-music-mygo/internal/kugou"
)

// The listener's music cloud: the files they put there themselves. KuGou
// keeps them for the account and streams them back like any other song, so
// the app shows them as songs — of the same account the phone app writes to.

// CloudSongs returns a page of the cloud's songs and how many there are.
func CloudSongs(page, size int) ([]Track, int, error) {
	c, err := kuGou()
	if err != nil {
		return nil, 0, err
	}
	songs, total, err := kugou.CloudSongs(c, page, size)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Track, 0, len(songs))
	for _, s := range songs {
		out = append(out, cloudTrack(s))
	}
	return out, total, nil
}

// cloudTrack is a file of the cloud as a song of the app: its number comes
// from its hash, as a catalogue song's does, and the index keeps the rest,
// so it plays by number once it is out of sight.
func cloudTrack(s kugou.CloudSong) Track {
	id := app.index.RememberCloud(s)
	t := Track{
		ID:        id,
		Name:      s.Title(),
		Duration:  s.Duration,
		Hash:      s.Hash,
		Cloud:     true,
		CloudName: s.Name,
		Size:      s.Size,
	}
	if t.Name == "" {
		t.Name = s.Name
	}
	if s.Singer != "" {
		t.Artists = []Artist{{Name: s.Singer}}
		// The files are named "歌手 - 歌名" as often as not: the singer is
		// not said again in the title.
		if rest, ok := strings.CutPrefix(t.Name, s.Singer+" - "); ok && rest != "" {
			t.Name = rest
		}
	} else if name, singer := splitFileName(s.Name); singer != "" {
		// An untagged file uploaded by its name still reads as "歌手 - 歌名".
		t.Name, t.Artists = name, []Artist{{Name: singer}}
	}
	return t
}

// splitFileName reads "歌手 - 歌名" out of a file name, which is how the
// files that carry no tags are named.
func splitFileName(name string) (title, singer string) {
	name = strings.TrimSuffix(name, pathExt(name))
	parts := strings.SplitN(name, " - ", 2)
	if len(parts) != 2 {
		return name, ""
	}
	if strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return name, ""
	}
	return strings.TrimSpace(parts[1]), strings.TrimSpace(parts[0])
}

// pathExt returns a file's extension, dot included, or "" when it has none.
func pathExt(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 || i == len(name)-1 {
		return ""
	}
	return name[i:]
}

// CloudUpload is a file to put in the cloud: where it is, and what its tags
// say it is. An empty name is taken from the file's own.
type CloudUpload struct {
	Path     string
	Name     string
	Singer   string
	Duration int64 // milliseconds
	Bitrate  int
}

// CloudUploadFile puts a file in the cloud and returns the song it made.
func CloudUploadFile(f CloudUpload) (Track, error) {
	c, err := kuGou()
	if err != nil {
		return Track{}, err
	}
	s, err := kugou.CloudUpload(c, kugou.CloudFile{
		Path: f.Path, Name: f.Name, Singer: f.Singer,
		Duration: f.Duration, Bitrate: f.Bitrate,
	})
	if err != nil {
		return Track{}, err
	}
	return cloudTrack(s), nil
}

// CloudDelete takes the songs of ids out of the cloud.
func CloudDelete(ids []int64) error {
	c, err := kuGou()
	if err != nil {
		return err
	}
	songs := make([]kugou.CloudSong, 0, len(ids))
	for _, id := range ids {
		if s, ok := app.index.CloudFile(id); ok {
			songs = append(songs, s)
		}
	}
	if len(songs) == 0 {
		return ErrUnavailable
	}
	return kugou.CloudDelete(c, songs)
}

// cloudStream returns where the cloud streams a song of id from, for the
// player: the tracker's answer, shaped as the catalogue's streams are.
func cloudStream(c *kugou.Client, id int64) (*Stream, bool, error) {
	s, ok := app.index.CloudFile(id)
	if !ok {
		return nil, false, nil
	}
	st, err := kugou.CloudURL(c, s)
	if err != nil {
		if errors.Is(err, kugou.ErrUnavailable) {
			return nil, true, ErrUnavailable
		}
		return nil, true, err
	}
	return &Stream{URL: st.URL, Size: st.Size, Type: st.Type, Level: st.Level}, true, nil
}
