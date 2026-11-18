package player

import (
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"

	alac "github.com/mycophonic/saprobe-alac"
	aacm4a "github.com/tphakala/go-m4a/aacm4a"
)

// m4aStream decodes an M4A file: the container Apple's players and phones
// keep their music in, with the sound in AAC-LC as most of them hold it, or
// in ALAC as the lossless ones do. The container is the same either way, so
// the decoder is the one the file's track turns out to need.
//
// Both decoders read the file by seeking through it, which a song of the
// local library allows: it is whole before it plays.
type m4aStream struct {
	dl  *download
	dec m4aTrack

	rate, ch, bps int
	total, pos    int64 // frames of the song, and the frame Read returns next
	skip          int64 // frames decoded but not played: a codec's priming

	raw  []byte    // what a decoder hands back, as it comes
	pend []byte    // whole frames of it, not yet read
	at   int       // the byte of pend Read takes next
	tail []byte    // the bytes of a frame that has not all come in
	buf  []float32 // stereo of what decoded, for a file of one channel

	spec *spectrum
	drop func()
	once sync.Once
	err  error // what ended the song, to answer with once the rest is read
}

// m4aTrack is a decoder of an M4A file: the sound of one track, read as bytes
// of little-endian PCM, and moved to a frame.
type m4aTrack interface {
	Read(p []byte) (int, error)
	// seekFrame goes to frame, as the frame of the song a page counts, and
	// answers how many frames must be dropped from what it decodes next: a
	// decoder starts at the packet holding the frame, not at the frame.
	seekFrame(frame int64) (drop int64, err error)
}

// aacFrameSize is what an access unit of AAC-LC decodes to, in frames.
const aacFrameSize = 1024

// openM4AStream opens an M4A file for the song whose tags say it plays
// durationMs. It fails when the file holds neither AAC nor ALAC, as one of
// Opus or FLAC in an MP4 does.
func openM4AStream(dl *download, durationMs int64) (*m4aStream, error) {
	// The box that says where the sound is stands at the end of a file as
	// often as at its start, and a song still coming in is read by waiting
	// for bytes rather than seeking to them: one that is not whole yet is
	// not read, rather than spinning on what has not come.
	if !dl.whole() {
		return nil, errors.New("m4a: the file must be whole to be read")
	}
	rs := &seeker{dl: dl}
	s := &m4aStream{dl: dl, drop: dl.Close}
	if dec, info, err := aacm4a.NewSeekableDecoder(rs); err == nil {
		s.dec = &aacTrack{dec: dec, delay: info.EncoderDelay, count: int64(info.FrameCount)}
		s.rate, s.ch, s.bps = info.SampleRate, info.Channels, 2
		s.skip = info.EncoderDelay
		s.total = framesOf(info.Duration, s.rate)
	} else if aerr := alacStream(s, rs); aerr != nil {
		return nil, err
	}
	if s.rate <= 0 || s.ch <= 0 {
		return nil, errors.New("m4a: no sound in the file")
	}
	if s.total <= 0 && durationMs > 0 {
		s.total = durationMs * int64(s.rate) / 1000
	}
	if s.total <= 0 {
		s.total = -1
	}
	s.spec = newSpectrum(s.rate, max(s.total, 0))
	return s, nil
}

// alacStream sets s to decode rs as ALAC, which the AAC reader refused.
func alacStream(s *m4aStream, rs *seeker) error {
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return err
	}
	dec, err := alac.NewDecoder(rs)
	if err != nil {
		return err
	}
	format := dec.Format()
	s.dec = &alacTrack{dec: dec, rate: format.SampleRate}
	s.rate, s.ch = format.SampleRate, format.Channels
	s.bps = max(1, (format.BitDepth+7)/8)
	s.total = framesOf(dec.Duration(), s.rate)
	return nil
}

// framesOf returns how many frames a length of time holds at rate.
func framesOf(d time.Duration, rate int) int64 {
	if d <= 0 || rate <= 0 {
		return 0
	}
	return int64(d / time.Second * time.Duration(rate))
}

// aacTrack reads the access units of an AAC track. Its output is every sample
// the codec decodes, the priming at the start included: the delay an edit list
// names is where the song is heard to begin.
type aacTrack struct {
	dec   *aacm4a.Decoder
	delay int64
	count int64
}

func (t *aacTrack) Read(p []byte) (int, error) { return t.dec.Read(p) }

func (t *aacTrack) seekFrame(frame int64) (int64, error) {
	at := frame + t.delay
	idx := at / aacFrameSize
	if last := t.count - 1; idx > last {
		idx = max(last, 0)
	}
	if err := t.dec.SeekSample(int(idx)); err != nil {
		return 0, err
	}
	return at - idx*aacFrameSize, nil
}

// alacTrack reads an ALAC track, which seeks to a packet rather than a sample.
type alacTrack struct {
	dec  *alac.Decoder
	rate int
}

func (t *alacTrack) Read(p []byte) (int, error) { return t.dec.Read(p) }

func (t *alacTrack) seekFrame(frame int64) (int64, error) {
	at, err := t.dec.Seek(framesAsTime(frame, t.rate))
	if err != nil {
		return 0, err
	}
	// The decoder answers where it landed, in the whole packets it counts:
	// the frames between there and the one asked for are dropped.
	from := framesAt(at, t.rate)
	if from > frame {
		from = frame
	}
	return frame - from, nil
}

// framesAsTime returns the length of time frame frames play at rate, and
// framesAt the frames a length of time holds, to the nearest one: a decoder
// answers where it landed in time, and the frame it landed on is the one
// that time was made from.
func framesAsTime(frame int64, rate int) time.Duration {
	return time.Duration(frame) * time.Second / time.Duration(rate)
}

func framesAt(d time.Duration, rate int) int64 {
	if rate <= 0 {
		return 0
	}
	return (int64(d)*int64(rate) + int64(time.Second)/2) / int64(time.Second)
}

func (s *m4aStream) SampleRate() int { return s.rate }
func (s *m4aStream) Channels() int   { return s.ch }
func (s *m4aStream) Len() int64      { return s.total }

// bytesPerFrame is what one frame of the song is, as its decoder writes it.
func (s *m4aStream) bytesPerFrame() int { return s.ch * s.bps }

func (s *m4aStream) Read(dst []float32) (int, error) {
	if len(dst) < s.ch {
		return 0, nil
	}
	limit := len(dst) - len(dst)%s.ch
	n := s.drain(dst[:limit])
	for n < limit && s.err == nil {
		if err := s.fill(); err != nil {
			s.err = err
			break
		}
		n += s.drain(dst[n:limit])
	}
	if n > 0 {
		s.feed(dst[:n])
		s.pos += int64(n / s.ch)
		return n, nil
	}
	if s.err == nil {
		s.err = io.EOF
	}
	return 0, s.err
}

// fill decodes until whole frames are pending, or the song ends.
func (s *m4aStream) fill() error {
	if s.raw == nil {
		s.raw = make([]byte, 64<<10)
	}
	fb := s.bytesPerFrame()
	for {
		n, err := s.dec.Read(s.raw)
		if n > 0 {
			buf := append(s.pend[:0], s.tail...)
			buf = append(buf, s.raw[:n]...)
			// A decoder splits where it likes: what is not a frame yet waits
			// for the next read to complete it.
			keep := len(buf) % fb
			s.tail = append(s.tail[:0], buf[len(buf)-keep:]...)
			s.pend, s.at = buf[:len(buf)-keep], 0
			s.trim()
			if len(s.pend) > 0 {
				return nil
			}
		}
		if err != nil {
			return err
		}
	}
}

// trim takes the priming of the codec off what has just decoded.
func (s *m4aStream) trim() {
	fb := int64(s.bytesPerFrame())
	if s.skip <= 0 || int64(len(s.pend)) < fb {
		return
	}
	frames := int64(len(s.pend)) / fb
	if dropped := min(s.skip, frames); dropped > 0 {
		s.pend = s.pend[dropped*fb:]
		s.skip -= dropped
	}
}

// drain reads whole frames of what is pending into dst, as the samples the
// player takes: one float a sample, from -1 to 1.
func (s *m4aStream) drain(dst []float32) int {
	fb := s.bytesPerFrame()
	frames := min(len(dst)/s.ch, (len(s.pend)-s.at)/fb)
	if frames <= 0 {
		return 0
	}
	out := dst[:frames*s.ch]
	for i := range out {
		out[i] = s.sample(s.pend[s.at+i*s.bps:])
	}
	s.at += frames * fb
	if s.at >= len(s.pend) {
		s.pend, s.at = s.pend[:0], 0
	}
	return len(out)
}

// sample reads the PCM sample b starts with, of the depth the file decodes to.
func (s *m4aStream) sample(b []byte) float32 {
	switch s.bps {
	case 2:
		return float32(int16(binary.LittleEndian.Uint16(b))) / (1 << 15)
	case 3:
		// 24 bits, and the 20-bit samples some ALAC files hold left-aligned
		// in them.
		v := int32(b[0])<<8 | int32(b[1])<<16 | int32(b[2])<<24
		return float32(v>>8) / (1 << 23)
	case 4:
		return float32(int32(binary.LittleEndian.Uint32(b))) / (1 << 31)
	}
	return 0
}

// feed measures the spectrum of what decoded, as two channels.
func (s *m4aStream) feed(b []float32) {
	s.spec.feed(s.pos, asStereo(b, s.ch, &s.buf))
}

func (s *m4aStream) SetPosition(frame int64) error {
	if frame < 0 {
		frame = 0
	}
	drop, err := s.dec.seekFrame(frame)
	if err != nil {
		return err
	}
	s.pend, s.at, s.tail = s.pend[:0], 0, s.tail[:0]
	s.skip, s.pos, s.err = drop, frame, nil
	return nil
}

func (s *m4aStream) source() (*download, *spectrum) { return s.dl, s.spec }

func (s *m4aStream) Close() error {
	s.once.Do(s.drop)
	return nil
}
