package player

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sync"

	"github.com/thesyncim/gopus"
	gopusogg "github.com/thesyncim/gopus/container/ogg"
)

// oggStream decodes an Ogg file of Opus: what a library of the listener's
// own holds as .opus, the one format of the app's that mygo has no decoder
// for. An Ogg of Vorbis goes through package audio's decoder, as m4a.go's
// does.
//
// An ogg file is a run of pages, each carrying the sample its last packet
// ends at. Seeking goes to the page before the sample asked for and decodes
// from there, the headers in front of it, as the format allows: a page
// starts where a packet does, and a decoder needs the headers once rather
// than the audio before them.
type oggStream struct {
	dl      *download
	dec     *gopus.Decoder
	pcm     []float32 // what a packet decodes to, reused
	headers [][]byte  // the packets a decoder needs before any audio
	dem     *oggPackets
	serial  uint32

	rate  int
	ch    int
	total int64 // frames the song holds, or -1 when its tags say nothing
	pos   int64 // the frame Read returns next
	skip  int64 // frames decoded but not played: the stream's pre-skip

	pend []float32 // decoded, and not yet read
	buf  []float32 // stereo of what decoded, for a file of one channel

	spec *spectrum
	drop func()
	once sync.Once
	err  error // what ended the song, to answer with once the rest is read

	// data is where the audio starts, after the headers, and pages what has
	// been read of the page index a seek goes by.
	data    int64
	pages   []oggPage
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

	// opusRate is what Opus decodes to, whatever rate its file names.
	opusRate = 48000
)

// oggOpus reports whether the Ogg file dl starts with an Opus stream, which
// the app decodes itself; one that starts with Vorbis is mygo's.
func oggOpus(dl *download) bool {
	dem := &oggPackets{r: &downloadReader{d: dl}}
	p, err := dem.ReadPacket()
	return err == nil && bytes.HasPrefix(p.data, []byte("OpusHead"))
}

func openOggStream(dl *download, durationMs int64) (*oggStream, error) {
	if dl.waitFor(4) < 4 || string(dl.peek(0, 4)) != "OggS" {
		return nil, errors.New("ogg: not an Ogg file")
	}
	headers, serial, err := oggHeaders(dl)
	if err != nil {
		return nil, err
	}
	head, err := gopusogg.ParseOpusHead(headers[0])
	if err != nil {
		return nil, err
	}
	ch := int(head.Channels)
	if ch != 1 && ch != 2 {
		return nil, errors.New("ogg: Opus of more than two channels is not played")
	}
	// Opus decodes at 48 kHz whatever rate its file names as its input's.
	dec, err := gopus.NewDecoder(gopus.DefaultDecoderConfig(opusRate, ch))
	if err != nil {
		return nil, err
	}
	s := &oggStream{
		dl: dl, dec: dec, headers: headers, serial: serial, ch: ch, rate: opusRate,
		dem:  &oggPackets{r: &downloadReader{d: dl}},
		drop: dl.Close,
	}
	// An Opus stream's first samples are the encoder's priming.
	s.skip = int64(head.PreSkip)
	s.data = oggDataStart(dl, len(headers))
	s.total = -1
	if durationMs > 0 {
		s.total = durationMs * int64(s.rate) / 1000
	}
	s.spec = newSpectrum(s.rate, max(s.total, 0))
	return s, nil
}

// oggHeaders reads the two packets an Opus stream needs before any audio:
// the head that says what it is, and the tags. It answers the serial of the
// stream they belong to, which the rest of the file is read by.
func oggHeaders(dl *download) (headers [][]byte, serial uint32, err error) {
	dem := &oggPackets{r: &downloadReader{d: dl}}
	for len(headers) < 2 {
		p, rerr := dem.ReadPacket()
		if rerr != nil {
			return nil, 0, rerr
		}
		if len(headers) == 0 {
			if !bytes.HasPrefix(p.data, []byte("OpusHead")) {
				return nil, 0, errors.New("ogg: a codec the app does not play")
			}
			serial = p.serial
		} else if p.serial != serial {
			continue // another stream of a multiplexed file
		}
		headers = append(headers, append([]byte(nil), p.data...))
	}
	return headers, serial, nil
}

// An oggPacket is a packet of an ogg file, and the stream it belongs to.
type oggPacket struct {
	data   []byte // the packet, which the next read takes back
	serial uint32
}

// oggPackets reads the packets of an ogg file a page at a time: a page holds
// the lacing values that say how long its packets are, then their bytes, and
// a packet runs on into the next page while the last of its values is 255.
type oggPackets struct {
	r      io.Reader
	head   [oggHeader]byte // the page's header, before its segment table
	seg    []byte          // the page's lacing values
	body   []byte          // the bytes of its packets
	segAt  int             // the lacing value read next
	bodyAt int             // the byte of the body read next
	serial uint32          // the stream the page belongs to
	pkt    []byte          // the packet being read, reused
}

func (p *oggPackets) ReadPacket() (oggPacket, error) {
	p.pkt = p.pkt[:0]
	for {
		if p.segAt >= len(p.seg) {
			// The page is spent: on to the next, for the packet this one
			// has only started.
			if err := p.readPage(); err != nil {
				return oggPacket{}, err
			}
			continue
		}
		n := int(p.seg[p.segAt])
		p.segAt++
		p.pkt = append(p.pkt, p.body[p.bodyAt:p.bodyAt+n]...)
		p.bodyAt += n
		if n < 255 {
			return oggPacket{data: p.pkt, serial: p.serial}, nil
		}
	}
}

// readPage reads the page that comes next in r.
func (p *oggPackets) readPage() error {
	if _, err := io.ReadFull(p.r, p.head[:]); err != nil {
		return err
	}
	if string(p.head[:4]) != "OggS" {
		return errors.New("ogg: not a page")
	}
	p.serial = binary.LittleEndian.Uint32(p.head[14:18])
	n := int(p.head[26])
	if cap(p.seg) < n {
		p.seg = make([]byte, n)
	}
	p.seg = p.seg[:n]
	if _, err := io.ReadFull(p.r, p.seg); err != nil {
		return err
	}
	size := 0
	for _, s := range p.seg {
		size += int(s)
	}
	if cap(p.body) < size {
		p.body = make([]byte, size)
	}
	p.body = p.body[:size]
	if _, err := io.ReadFull(p.r, p.body); err != nil {
		return err
	}
	p.segAt, p.bodyAt = 0, 0
	return nil
}

func (s *oggStream) SampleRate() int { return s.rate }
func (s *oggStream) Channels() int   { return s.ch }
func (s *oggStream) Len() int64      { return s.total }

func (s *oggStream) Read(dst []float32) (int, error) {
	if len(dst) < s.ch {
		return 0, nil
	}
	limit := len(dst) - len(dst)%s.ch
	n := s.take(dst[:limit])
	for n < limit && s.err == nil {
		if err := s.fill(); err != nil {
			s.err = err
			break
		}
		n += s.take(dst[n:limit])
	}
	if n > 0 {
		s.spec.feed(s.pos, asStereo(dst[:n], s.ch, &s.buf))
		s.pos += int64(n / s.ch)
		return n, nil
	}
	if s.err == nil {
		s.err = io.EOF
	}
	return 0, s.err
}

// fill decodes packets until samples are pending, or the song ends.
func (s *oggStream) fill() error {
	for len(s.pend) == 0 {
		p, err := s.dem.ReadPacket()
		if err != nil {
			return err
		}
		if p.serial != s.serial {
			continue // another stream of a multiplexed file
		}
		if oggIsHeader(p.data) {
			continue // a chained stream's headers; this one follows after them
		}
		need := s.ch * 5760 // 120 ms at 48 kHz, more than a packet holds
		if cap(s.pcm) < need {
			s.pcm = make([]float32, need)
		}
		pcm := s.pcm[:need]
		n, err := s.dec.Decode(p.data, pcm)
		if err != nil {
			return err
		}
		s.pend = append(s.pend[:0], pcm[:n*s.ch]...)
		s.trim()
	}
	return nil
}

// trim takes the codec's priming off what has just decoded.
func (s *oggStream) trim() {
	if s.skip <= 0 || len(s.pend) < s.ch {
		return
	}
	frames := min(s.skip, int64(len(s.pend)/s.ch))
	s.pend = s.pend[frames*int64(s.ch):]
	s.skip -= frames
}

// take reads pending samples into dst.
func (s *oggStream) take(dst []float32) int {
	n := copy(dst, s.pend)
	s.pend = s.pend[n:]
	if len(s.pend) == 0 {
		s.pend = s.pend[:0]
	}
	return n
}

// oggIsHeader reports whether a packet is one of an Opus stream's headers
// rather than sound.
func oggIsHeader(p []byte) bool {
	return bytes.HasPrefix(p, []byte("OpusHead")) || bytes.HasPrefix(p, []byte("OpusTags"))
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
	s.dec.Reset()
	s.dem = &oggPackets{r: &downloadReader{d: s.dl, pos: off}}
	s.pend, s.err = nil, nil
	s.pos, s.skip = frame, frame-from
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

// oggDataStart returns where the audio starts: the end of the page the last
// header packet ends on. A packet ends at a segment shorter than 255 bytes.
func oggDataStart(dl *download, headers int) int64 {
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
		if packets >= headers {
			return off
		}
	}
	return off
}

func (s *oggStream) source() (*download, *spectrum) { return s.dl, s.spec }

func (s *oggStream) Close() error {
	s.once.Do(s.drop)
	return nil
}
