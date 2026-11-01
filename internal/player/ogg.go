package player

import (
	"cmp"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync"

	"github.com/jfreymuth/oggvorbis"
)

// oggStream decodes an Ogg Vorbis download for package audio. It is what the
// 概念版 streams: asked for its standard quality, its player is handed the
// ogg file of that quality rather than an MP3, and an MP3 decoder reads
// nothing of it.
//
// An ogg file is a run of pages, each carrying the sample its last packet
// ends at. Seeking goes to the page before the sample asked for and decodes
// from there with the three header packets in front of it, as the format
// allows: a page starts where a packet does, and a vorbis decoder needs the
// headers once rather than the audio before them.
type oggStream struct {
	dl   *download
	rd   *oggvorbis.Reader
	rate int
	ch   int
	pos  int64 // the sample Read returns next
	spec *spectrum
	drop func()
	once sync.Once

	// data is where the audio starts, after the three header packets, total
	// the frames the song holds, and pages what has been read of the page
	// index a seek goes by.
	data   int64
	total  int64
	pages  []oggPage
	scanned int64
}

// oggPage is a page of an ogg file: where it starts, how long it is, the
// sample its last packet ends at, and whether a packet starts on it.
type oggPage struct {
	off     int64
	size    int64
	granule int64
	fresh   bool
}

const (
	oggHeader = 27 // a page's header, before its segment table
)

func openOggStream(dl *download, durationMs int64) (*oggStream, error) {
	if dl.waitFor(4) < 4 || string(dl.peek(0, 4)) != "OggS" {
		return nil, errors.New("ogg: not an Ogg file")
	}
	rd, err := oggvorbis.NewReader(&downloadReader{d: dl, pos: 0})
	if err != nil {
		return nil, err
	}
	s := &oggStream{dl: dl, rd: rd, rate: rd.SampleRate(), ch: rd.Channels(), drop: dl.Close}
	if s.rate == 0 || s.ch == 0 {
		return nil, errors.New("ogg: no stream info")
	}
	s.data = oggAudioStart(dl)
	s.total = -1
	if durationMs > 0 {
		s.total = durationMs * int64(s.rate) / 1000
	}
	s.spec = newSpectrum(s.rate, s.total)
	return s, nil
}

func (s *oggStream) SampleRate() int { return s.rate }
func (s *oggStream) Channels() int   { return s.ch }

func (s *oggStream) Len() int64 { return s.total }

func (s *oggStream) Read(dst []float32) (int, error) {
	n, err := s.rd.Read(dst)
	if n > 0 {
		s.spec.feed(s.pos, dst[:n])
		s.pos += int64(n / s.ch)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF
	}
	return n, err
}

// SetPosition goes to frame: to the page holding it, then drops the samples
// between that page and it.
func (s *oggStream) SetPosition(frame int64) error {
	if frame < 0 {
		frame = 0
	}
	off, from := s.data, int64(0)
	if frame > 0 {
		if page, start, ok := s.pageFor(frame); ok {
			off, from = page, start
		}
	}
	if err := s.restart(off); err != nil {
		return err
	}
	s.pos = from
	// What lies between the page and the sample asked for is decoded and
	// dropped a little at a time, so a seek costs no memory.
	buf := make([]float32, 2048*s.ch)
	for left := frame - from; left > 0; {
		want := int(min(left, 2048))
		n, err := s.rd.Read(buf[:want*s.ch])
		left -= int64(n / s.ch)
		if n == 0 || err != nil {
			break
		}
	}
	s.pos = frame
	return nil
}

// restart opens the song again with its headers in front of a page, as
// though the file were the headers and then this page: all the decoder needs
// to play from here.
func (s *oggStream) restart(off int64) error {
	rd, err := oggvorbis.NewReader(&ranged{
		d:    s.dl,
		segs: [][2]int64{{0, s.data}, {off, -1}},
	})
	if err != nil {
		return err
	}
	s.rd = rd
	return nil
}

// pageFor returns the page to start a seek to frame from, and the sample
// that page's decoding starts at: the page before the one holding frame, as
// a page's granule is where its last packet ends.
func (s *oggStream) pageFor(frame int64) (off, from int64, ok bool) {
	if s.data <= 0 {
		return 0, 0, false
	}
	s.scan(frame)
	best := -1
	for i, p := range s.pages {
		if !p.fresh || p.granule > frame {
			continue
		}
		best = i
	}
	if best < 0 {
		return 0, 0, false
	}
	start := int64(0)
	if best > 0 {
		start = s.pages[best-1].granule
	}
	return s.pages[best].off, min(start, frame), true
}

// scan reads the pages after the last one it read, as far as they have come
// in, until a page ends past frame. The pages are walked in order rather than
// searched for: "OggS" turns up in the audio itself, and a page found by that
// would send a seek anywhere.
func (s *oggStream) scan(frame int64) {
	off := s.scanned
	if off == 0 {
		off = s.data
	}
	for {
		if off <= 0 || (s.dl.size > 0 && off >= s.dl.size) {
			return
		}
		if s.dl.waitFor(off+oggHeader) < off+oggHeader {
			return
		}
		b := s.dl.peek(off, oggHeader)
		if len(b) < oggHeader || string(b[:4]) != "OggS" {
			return
		}
		segs := int(b[26])
		if segs == 0 || s.dl.waitFor(off+oggHeader+int64(segs)) < off+oggHeader+int64(segs) {
			return
		}
		size := int64(oggHeader + segs)
		for _, x := range s.dl.peek(off+oggHeader, segs) {
			size += int64(x)
		}
		p := oggPage{
			off:     off,
			size:    size,
			granule: int64(binary.LittleEndian.Uint64(b[6:14])),
			fresh:   b[5]&0x01 == 0,
		}
		s.pages = append(s.pages, p)
		s.scanned = off + size
		off = s.scanned
		if frame > 0 && p.granule >= frame {
			return
		}
	}
}

// oggAudioStart returns where the audio starts: the end of the page the
// third header packet ends on. The three headers come first in every ogg
// vorbis file, and a packet ends at a segment shorter than 255 bytes.
func oggAudioStart(dl *download) int64 {
	packets, off := 0, int64(0)
	for off < 1<<20 {
		if dl.waitFor(off+oggHeader) < off+oggHeader {
			return off
		}
		b := dl.peek(off, oggHeader)
		if string(b[:4]) != "OggS" {
			return off
		}
		segs := int(b[26])
		if dl.waitFor(off+oggHeader+int64(segs)) < off+oggHeader+int64(segs) {
			return off
		}
		size := int64(oggHeader + segs)
		for _, s := range dl.peek(off+oggHeader, segs) {
			size += int64(s)
			if s < 255 {
				packets++
			}
		}
		off += size
		if packets >= 3 {
			return off
		}
	}
	return off
}

func (s *oggStream) Close() error {
	s.once.Do(s.drop)
	return nil
}

func (s *oggStream) source() (*download, *spectrum) { return s.dl, s.spec }

// ranged reads a download in pieces: the song's headers, then from a byte of
// its audio, as a seek does. It reads forward only, so the decoder that reads
// it takes it for a stream and never asks for the end.
type ranged struct {
	d    *download
	segs [][2]int64 // start, end; an end below zero runs to the end
	i    int
	off  int64
}

func (r *ranged) Read(p []byte) (int, error) {
	for r.i < len(r.segs) {
		start, end := r.segs[r.i][0], r.segs[r.i][1]
		if end < 0 {
			end = math.MaxInt64
		}
		if r.off < start {
			r.off = start
		}
		if r.off >= end {
			r.next()
			continue
		}
		d := r.d
		d.mu.Lock()
		for d.n <= r.off && !d.done {
			d.cond.Wait()
		}
		if d.n <= r.off {
			err := d.err
			d.mu.Unlock()
			if r.next() {
				continue
			}
			if err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		n, err := d.readAt(p, r.off)
		d.mu.Unlock()
		if n == 0 {
			return 0, cmp.Or(err, io.ErrUnexpectedEOF)
		}
		r.off += int64(n)
		return n, err
	}
	return 0, io.EOF
}

// next moves to the piece after this one, and reports whether there is one.
func (r *ranged) next() bool {
	r.i++
	if r.i >= len(r.segs) {
		return false
	}
	r.off = r.segs[r.i][0]
	return true
}
