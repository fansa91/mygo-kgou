package player

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo/audio"

	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/netease"
)

// A song of the listener's own folders plays from its file: the whole of
// it is there, so it is read from where it lies rather than downloaded,
// and its words and its picture come from the file and the folder around
// it rather than from KuGou.

// trackCover returns the picture of t: the file's own, or the one beside
// it, for a song of the local library, and the CDN's otherwise.
func trackCover(t netease.Track) ([]byte, error) {
	if t.Local() {
		return local.Cover(t.Path)
	}
	return netease.Fetch(netease.Sized(t.Album.PicURL, 600))
}

// trackLyrics returns the words of t as netease.Lyrics gives them: the
// file's own, or its .lrc, for a song of the local library.
func trackLyrics(t netease.Track) (lrc, translation, yrc, krc string, err error) {
	if t.Local() {
		return local.Lyrics(t.Path), "", "", "", nil
	}
	return netease.Lyrics(t.ID)
}

// openFile opens the sound of a song of the local library.
func openFile(t netease.Track) (stream, error) {
	f, err := os.Open(t.Path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	dl := opened(f, st.Size())
	var s stream
	if strings.EqualFold(filepath.Ext(t.Path), ".wav") {
		s, err = openWAVStream(dl, t.Duration)
	} else {
		s, err = openStream(dl, t.Duration)
	}
	if err != nil {
		dl.Close()
		return nil, err
	}
	return s, nil
}

// A seeker reads a download that is whole, as the decoders of package
// audio read a file: from any offset, which it may move from.
type seeker struct {
	dl  *download
	pos int64
}

func (s *seeker) Read(p []byte) (int, error) {
	s.dl.mu.Lock()
	defer s.dl.mu.Unlock()
	if s.pos >= s.dl.n {
		if s.dl.err != nil {
			return 0, s.dl.err
		}
		return 0, io.EOF
	}
	n, err := s.dl.readAt(p, s.pos)
	s.pos += int64(n)
	return n, err
}

func (s *seeker) Seek(off int64, whence int) (int64, error) {
	s.dl.mu.Lock()
	defer s.dl.mu.Unlock()
	switch whence {
	case io.SeekStart:
		s.pos = off
	case io.SeekCurrent:
		s.pos += off
	case io.SeekEnd:
		s.pos = s.dl.n + off
	}
	if s.pos < 0 {
		s.pos = 0
	}
	return s.pos, nil
}

// wavStream decodes a WAV file for package audio, which the streams of the
// service's songs do not read. It measures the spectrum of what it decodes
// as they do, for the visualizers.
type wavStream struct {
	dl   *download
	dec  audio.Decoder
	rate int
	ch   int
	pos  int64 // the frame Read returns next
	spec *spectrum
	buf  []float32 // stereo of what decoded, for a file of one or of many channels
	once sync.Once
}

func openWAVStream(dl *download, durationMs int64) (*wavStream, error) {
	dec, name, err := audio.NewDecoder(&seeker{dl: dl})
	if err != nil {
		return nil, err
	}
	if name != "wav" {
		return nil, errors.New("player: not a WAV file")
	}
	s := &wavStream{dl: dl, dec: dec, rate: dec.SampleRate(), ch: dec.Channels()}
	if s.rate > 0 {
		s.spec = newSpectrum(s.rate, durationMs*int64(s.rate)/1000)
	}
	return s, nil
}

func (s *wavStream) SampleRate() int { return s.rate }
func (s *wavStream) Channels() int   { return s.ch }
func (s *wavStream) Len() int64      { return s.dec.Len() }

func (s *wavStream) Read(dst []float32) (int, error) {
	n, err := s.dec.Read(dst)
	if n > 0 {
		s.feed(dst[:n])
		s.pos += int64(n / max(s.ch, 1))
	}
	return n, err
}

// feed measures the spectrum of what decoded, as two channels: a file of
// one channel is heard twice, as the output plays it, and one of more than
// two plays the first two.
func (s *wavStream) feed(b []float32) {
	if s.ch == 2 {
		s.spec.feed(s.pos, b)
		return
	}
	frames := len(b) / max(s.ch, 1)
	if len(s.buf) < frames*2 {
		s.buf = make([]float32, frames*2)
	}
	stereo := s.buf[:frames*2]
	for i := range frames {
		if s.ch == 1 {
			v := b[i]
			stereo[2*i], stereo[2*i+1] = v, v
			continue
		}
		stereo[2*i], stereo[2*i+1] = b[i*s.ch], b[i*s.ch+1]
	}
	s.spec.feed(s.pos, stereo)
}

func (s *wavStream) SetPosition(frame int64) error {
	if err := s.dec.SetPosition(frame); err != nil {
		return err
	}
	s.pos = frame
	return nil
}

func (s *wavStream) source() (*download, *spectrum) { return s.dl, s.spec }

func (s *wavStream) Close() error {
	s.once.Do(s.dl.Close)
	return nil
}
