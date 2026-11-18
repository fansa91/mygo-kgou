package player

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sync"

	"github.com/faide/goxiph/audio"
	"github.com/faide/goxiph/ogg"
	"github.com/faide/goxiph/vorbis"
	"github.com/thesyncim/gopus"
	gopusogg "github.com/thesyncim/gopus/container/ogg"
)

// oggStream decodes an Ogg file: Vorbis, which is what the 概念版 streams
// rather than an MP3 at the qualities its player is given, and Opus, which a
// library of the listener's own holds as .opus.
//
// An ogg file is a run of pages, each carrying the sample its last packet
// ends at. Seeking goes to the page before the sample asked for and decodes
// from there, the headers in front of it, as the format allows: a page starts
// where a packet does, and a decoder needs the headers once rather than the
// audio before them.
type oggStream struct {
	dl      *download
	codec   oggCodec
	headers [][]byte // the packets a decoder needs before any audio
	dem     *ogg.Demuxer
	serial  uint32

	rate  int
	ch    int
	total int64 // frames the song holds, or -1 when its tags say nothing
	pos   int64 // the frame Read returns next
	skip  int64 // frames decoded but not played: an Opus stream's pre-skip

	pend []float32
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

// oggCodec decodes the packets of one codec into the samples the player takes.
type oggCodec interface {
	decode(packet []byte, out []float32) ([]float32, error)
}

func openOggStream(dl *download, durationMs int64) (*oggStream, error) {
	if dl.waitFor(4) < 4 || string(dl.peek(0, 4)) != "OggS" {
		return nil, errors.New("ogg: not an Ogg file")
	}
	headers, serial, err := oggHeaders(dl)
	if err != nil {
		return nil, err
	}
	codec, err := newOggCodec(headers)
	if err != nil {
		return nil, err
	}
	s := &oggStream{
		dl: dl, codec: codec, headers: headers, serial: serial,
		dem:  ogg.NewDemuxer(&downloadReader{d: dl, pos: 0}),
		drop: dl.Close,
	}
	s.rate, s.ch = oggFormat(headers)
	if s.rate <= 0 || s.ch <= 0 {
		return nil, errors.New("ogg: no stream info")
	}
	// An Opus stream's first samples are the encoder's priming.
	s.skip = oggPreSkip(headers)
	s.data = oggDataStart(dl, len(headers))
	s.total = -1
	if durationMs > 0 {
		s.total = durationMs * int64(s.rate) / 1000
	}
	s.spec = newSpectrum(s.rate, max(s.total, 0))
	return s, nil
}

// oggHeaders reads as many packets as the codec at the front of the file
// needs before any audio: three for Vorbis, two for Opus. It answers the
// serial of the stream they belong to, which the rest of the file is read by.
func oggHeaders(dl *download) (headers [][]byte, serial uint32, err error) {
	dem := ogg.NewDemuxer(&downloadReader{d: dl, pos: 0})
	want := 0
	for want == 0 || len(headers) < want {
		p, rerr := dem.ReadPacket()
		if rerr != nil {
			return nil, 0, rerr
		}
		if len(headers) == 0 {
			serial = p.Serial
			if want, err = oggHeaderCount(p.Data); err != nil {
				return nil, 0, err
			}
		}
		if p.Serial == serial {
			headers = append(headers, p.Data)
		}
	}
	return headers, serial, nil
}

// oggHeaderCount returns how many header packets the codec of this packet
// has, and fails for a codec the app does not read.
func oggHeaderCount(first []byte) (int, error) {
	switch {
	case bytes.HasPrefix(first, []byte("OpusHead")):
		return 2, nil
	case bytes.HasPrefix(first, []byte("\x01vorbis")):
		return 3, nil
	}
	return 0, errors.New("ogg: a codec the app does not play")
}

// oggFormat returns the rate and the channels the headers name.
func oggFormat(headers [][]byte) (rate, ch int) {
	if len(headers) == 0 {
		return 0, 0
	}
	if info, err := vorbis.ParseInfo(headers[0]); err == nil {
		return info.SampleRate, info.Channels
	}
	if head, err := gopusogg.ParseOpusHead(headers[0]); err == nil {
		return opusRate, int(head.Channels)
	}
	return 0, 0
}

// oggPreSkip returns how many samples of silence the codec's headers say come
// before the song.
func oggPreSkip(headers [][]byte) int64 {
	if len(headers) == 0 {
		return 0
	}
	if head, err := gopusogg.ParseOpusHead(headers[0]); err == nil {
		return int64(head.PreSkip)
	}
	return 0
}

// newOggCodec makes the decoder of the codec these headers belong to, with
// the headers already in it.
func newOggCodec(headers [][]byte) (oggCodec, error) {
	if len(headers) == 0 {
		return nil, errors.New("ogg: no headers")
	}
	if bytes.HasPrefix(headers[0], []byte("OpusHead")) {
		return newOpusCodec(headers)
	}
	return newVorbisCodec(headers)
}

// vorbisCodec decodes Vorbis packets: the decoder wants the three headers
// once, then one packet at a time.
type vorbisCodec struct {
	dec *vorbis.Decoder
	buf *audio.Buffer
}

func newVorbisCodec(headers [][]byte) (*vorbisCodec, error) {
	if len(headers) < 3 {
		return nil, errors.New("ogg: the Vorbis headers are missing")
	}
	info, err := vorbis.ParseInfo(headers[0])
	if err != nil {
		return nil, err
	}
	setup, err := vorbis.ParseSetup(headers[2], info)
	if err != nil {
		return nil, err
	}
	dec := vorbis.NewDecoder()
	dec.SetInfo(info)
	if err := dec.SetSetup(setup); err != nil {
		return nil, err
	}
	buf, err := audio.NewBuffer(info.Format(), 0)
	if err != nil {
		return nil, err
	}
	return &vorbisCodec{dec: dec, buf: buf}, nil
}

func (v *vorbisCodec) decode(packet []byte, out []float32) ([]float32, error) {
	v.buf.Resize(0)
	if err := v.dec.DecodePacket(packet, v.buf); err != nil {
		return out, err
	}
	frames := v.buf.Frames()
	if len(v.buf.Data) == 1 {
		return append(out, v.buf.Data[0][:frames]...), nil
	}
	for i := range frames {
		for _, plane := range v.buf.Data {
			out = append(out, plane[i])
		}
	}
	return out, nil
}

// opusCodec decodes Opus packets. Opus decodes at 48 kHz whatever its file
// names as its input rate.
type opusCodec struct {
	dec    *gopus.Decoder
	ch     int
	packet []float32
}

func newOpusCodec(headers [][]byte) (*opusCodec, error) {
	if len(headers) < 2 {
		return nil, errors.New("ogg: the Opus headers are missing")
	}
	head, err := gopusogg.ParseOpusHead(headers[0])
	if err != nil {
		return nil, err
	}
	ch := int(head.Channels)
	if ch != 1 && ch != 2 {
		return nil, errors.New("ogg: Opus of more than two channels is not played")
	}
	dec, err := gopus.NewDecoder(gopus.DefaultDecoderConfig(opusRate, ch))
	if err != nil {
		return nil, err
	}
	return &opusCodec{dec: dec, ch: ch}, nil
}

func (o *opusCodec) decode(packet []byte, out []float32) ([]float32, error) {
	need := o.ch * 5760 // 120 ms at 48 kHz, more than a packet holds
	if cap(o.packet) < need {
		o.packet = make([]float32, need)
	}
	pcm := o.packet[:need]
	n, err := o.dec.Decode(packet, pcm)
	if err != nil {
		return out, err
	}
	return append(out, pcm[:n*o.ch]...), nil
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
		if p.Serial != s.serial {
			continue // another stream of a multiplexed file
		}
		if oggIsHeader(p.Data) {
			continue // a chained stream's headers; this one follows after them
		}
		if s.pend, err = s.codec.decode(p.Data, s.pend[:0]); err != nil {
			return err
		}
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

// oggIsHeader reports whether a packet is one of a codec's headers rather
// than sound.
func oggIsHeader(p []byte) bool {
	switch {
	case bytes.HasPrefix(p, []byte("OpusHead")), bytes.HasPrefix(p, []byte("OpusTags")):
		return true
	case len(p) > 7 && p[0] < 0x08 && string(p[1:7]) == "vorbis":
		return true
	}
	return false
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
	codec, err := newOggCodec(s.headers)
	if err != nil {
		return err
	}
	s.codec = codec
	s.dem = ogg.NewDemuxer(&downloadReader{d: s.dl, pos: off})
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
