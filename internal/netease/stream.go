package netease

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"netease-music-mygo/internal/kugou"
)

// Playing a song: where it streams from, what it is, and its words.

// UserAgent is what the app calls itself when fetching a picture.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

var httpClient = &http.Client{}

// SongDetails returns the tracks of ids, in their order, from what this app
// remembers of them. A song that was never seen cannot be named, and is left
// out.
func SongDetails(ids []int64) ([]Track, error) {
	if _, err := kuGou(); err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(ids))
	for _, id := range ids {
		k, ok := app.index.Lookup(id)
		if !ok {
			continue
		}
		t := Track{
			ID:       id,
			Name:     k.Name,
			Duration: 0,
		}
		if k.Singer != "" {
			t.Artists = []Artist{{Name: k.Singer}}
		}
		out = append(out, t)
	}
	return out, nil
}

// SongURL returns where to stream a track, MP3 or FLAC, at level (one of
// Levels) or the best below it the listener may play.
func SongURL(id int64, level string) (*Stream, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	t, ok := app.index.Track(id)
	if !ok {
		return nil, ErrUnavailable
	}
	st, err := kugou.URL(c, &t, level)
	if err != nil {
		if errors.Is(err, kugou.ErrUnavailable) {
			return nil, ErrUnavailable
		}
		return nil, err
	}
	return &Stream{URL: st.URL, Size: st.Size, Type: st.Type, Level: st.Level}, nil
}

// Lyrics returns a track's words: the plain ones, a translation when KuGou
// has one, and the word-timed lines the app scrolls by.
func Lyrics(id int64) (lrc, translation, yrc string, err error) {
	c, err := kuGou()
	if err != nil {
		return "", "", "", err
	}
	k, ok := app.index.Lookup(id)
	if !ok {
		return "", "", "", fmt.Errorf("netease: no song %d", id)
	}
	l, err := kugou.Lyrics(c, k.Hash, k.AlbumAudioID)
	if err != nil {
		return "", "", "", err
	}
	return l.LRC, l.Translation, l.KRC, nil
}

// Sized asks the image CDN for a picture of n×n pixels.
func Sized(pic string, n int) string {
	if pic == "" {
		return ""
	}
	pic = strings.ReplaceAll(pic, "{size}", fmt.Sprint(n))
	pic = strings.Replace(pic, "http://", "https://", 1)
	// NetEase's CDN resizes on demand; KuGou's pictures carry their size in
	// the path, which the services already fill in.
	if strings.Contains(pic, "music.126.net") {
		return fmt.Sprintf("%s?param=%dy%d", pic, n, n)
	}
	return pic
}

// Fetch returns what is at url, as a cover the image CDN serves.
func Fetch(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New(res.Status)
	}
	return io.ReadAll(res.Body)
}
