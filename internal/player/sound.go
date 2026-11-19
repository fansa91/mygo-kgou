package player

import (
	"errors"
	"io"
	"sync"

	"github.com/egoist/mygo/audio"
)

// soundStream decodes a download with one of package audio's decoders: the
// formats mygo reads itself, so that the app keeps no decoder of its own but
// Opus's, which mygo has none of. FLAC, an M4A's AAC and ALAC, and an Ogg's
// Vorbis all come through here.
type soundStream struct {
	audio.Decoder
	dl    *download
	total int64     // frames the song holds, from its file or its tags
	pos   int64     // the frame Read returns next
	spec  *spectrum // what the visualizers look up, or nil for a second stream
	buf   []float32 // stereo of what decoded, for a file of one channel
	drop  func()    // lets the download go, as the stream closes
	once  sync.Once
}

// openSound opens dl with open, the decoder of its format, and measures the
// spectrum of what it decodes. A song still coming in is read as it arrives,
// by the decoders that read it forward: the bytes they ask for beyond it
// wait for it.
func openSound(dl *download, durationMs int64, open func(io.ReadSeeker) (audio.Decoder, error)) (*soundStream, error) {
	dec, err := open(&seeker{dl: dl})
	if err != nil {
		return nil, err
	}
	rate, ch := dec.SampleRate(), dec.Channels()
	if rate <= 0 || ch <= 0 {
		return nil, errors.New("player: no sound in the file")
	}
	s := &soundStream{Decoder: dec, dl: dl, drop: dl.Close}
	// What the file says, or what the API says of the song: an Ogg Vorbis
	// download still coming in says no length until its end is in.
	s.total = dec.Len()
	if s.total <= 0 && durationMs > 0 {
		s.total = durationMs * int64(rate) / 1000
	}
	if s.total <= 0 {
		s.total = -1
	}
	s.spec = newSpectrum(rate, max(s.total, 0))
	return s, nil
}

func (s *soundStream) Len() int64 { return s.total }

func (s *soundStream) Read(dst []float32) (int, error) {
	n, err := s.Decoder.Read(dst)
	if n > 0 {
		ch := s.Channels()
		s.spec.feed(s.pos, asStereo(dst[:n], ch, &s.buf))
		s.pos += int64(n / ch)
	}
	return n, err
}

func (s *soundStream) SetPosition(frame int64) error {
	if err := s.Decoder.SetPosition(frame); err != nil {
		return err
	}
	s.pos = frame
	return nil
}

func (s *soundStream) source() (*download, *spectrum) { return s.dl, s.spec }

func (s *soundStream) Close() error {
	s.once.Do(s.drop)
	return nil
}
