package player

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// peakOf returns the loudest sample of a read, which a song of silence has
// none of.
func peakOf(b []float32) float32 {
	var peak float32
	for _, v := range b {
		peak = max(peak, float32(math.Abs(float64(v))))
	}
	return peak
}

// openFileStream opens a file of the local library as the player does.
func openFileStream(t *testing.T, path string, durationMs int64) stream {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	dl := opened(f, st.Size())
	s, err := openStream(dl, durationMs)
	if err != nil {
		dl.Close()
		t.Fatalf("open %s: %v", filepath.Base(path), err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// An M4A of AAC — what a phone keeps its music in — plays, and goes where it
// is asked to as well as any other song. The file is testdata/tone.m4a: two
// seconds of a 440 Hz tone, 44.1 kHz, two channels.
func TestM4AAACPlays(t *testing.T) {
	const rate = 44100
	s := openFileStream(t, filepath.Join("testdata", "tone.m4a"), 2000)
	if s.SampleRate() != rate || s.Channels() != 2 {
		t.Fatalf("stream = %d Hz, %d channels", s.SampleRate(), s.Channels())
	}
	if total := s.Len(); total < rate*19/10 || total > rate*21/10 {
		t.Errorf("length = %d frames, want about %d", total, rate*2)
	}
	buf := make([]float32, 4096)
	n, err := s.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 || peakOf(buf[:n]) < 0.1 {
		t.Fatalf("read %d frames, peak %v", n, peakOf(buf[:n]))
	}
	// A second in, the sound is still there, and the position says so.
	if err := s.SetPosition(rate); err != nil {
		t.Fatal(err)
	}
	n, err = s.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if peak := peakOf(buf[:n]); peak < 0.1 {
		t.Errorf("after the seek, peak %v", peak)
	}
	if got := s.Len() - rate; got <= 0 {
		t.Errorf("length %d, seeked a second in", s.Len())
	}
}

// alacM4A builds an M4A whose sound is ALAC: the samples written as the
// format's uncompressed frames, in the boxes the decoder reads them from.
func alacM4A(t *testing.T, samples []int16, rate int) []byte {
	t.Helper()
	const frameLen = 4096
	packets := alacPackets(samples, frameLen)
	var mdat []byte
	for _, p := range packets {
		mdat = append(mdat, p...)
	}
	cookie := alacCookie(frameLen, rate)
	// The sample entry: six bytes the format reserves, then the index of the
	// data reference, and what the sound is.
	entry := m4aBox("alac", make([]byte, 6), u16(1), u16(0), u16(0), make([]byte, 4),
		u16(1), u16(16), u16(0), u16(0), u32(uint32(rate)<<16), m4aBox("alac", u32(0), cookie))
	stsd := m4aBox("stsd", u32(0), u32(1), entry)
	stts := m4aBox("stts", u32(0), u32(1), u32(uint32(len(packets))), u32(frameLen))
	stsc := m4aBox("stsc", u32(0), u32(1), u32(1), u32(uint32(len(packets))), u32(1))
	sizes := append(append(u32(0), u32(0)...), u32(uint32(len(packets)))...)
	for _, p := range packets {
		sizes = append(sizes, u32(uint32(len(p)))...)
	}
	stsz := m4aBox("stsz", sizes)
	ftyp := m4aBox("ftyp", []byte("M4A "), u32(0x200), []byte("M4A "), []byte("isom"))
	moov := func(offset uint32) []byte {
		stco := m4aBox("stco", u32(0), u32(1), u32(offset))
		stbl := m4aBox("stbl", stsd, stts, stsc, stsz, stco)
		mdhd := m4aBox("mdhd", u32(0), u32(0), u32(0), u32(uint32(rate)), u32(uint32(len(samples))), u16(0x55c4), u16(0))
		hdlr := m4aBox("hdlr", u32(0), u32(0), []byte("soun"), make([]byte, 12))
		mdia := m4aBox("mdia", mdhd, hdlr, m4aBox("minf", stbl))
		mvhd := m4aBox("mvhd", u32(0), u32(0), u32(0), u32(uint32(rate)), u32(uint32(len(samples))),
			u32(0x00010000), u16(0x0100), u16(0), make([]byte, 8), make([]byte, 36), make([]byte, 24), u32(2))
		return m4aBox("moov", mvhd, m4aBox("trak", mdia))
	}
	// The chunk's offset is where mdat's sound starts, which needs the length
	// of the moov that holds it: the boxes are the same either way.
	offset := uint32(len(ftyp) + len(moov(0)) + 8)
	out := append(ftyp, moov(offset)...)
	return append(out, m4aBox("mdat", mdat)...)
}

// alacPackets writes samples as ALAC frames: each a channel element whose
// samples are stored as they are, which the format calls an escape.
func alacPackets(samples []int16, frameLen int) [][]byte {
	var out [][]byte
	for from := 0; from < len(samples); from += frameLen {
		to := min(from+frameLen, len(samples))
		w := &bitWriter{}
		w.write(0, 3)       // a single channel element
		w.write(0, 4)       // the element's instance
		w.write(0, 12)      // the bits the format leaves unused
		header := uint32(1) // escape: the samples are not compressed
		if n := to - from; n != frameLen {
			header |= 1 << 3 // a frame that holds fewer than its length
		}
		w.write(header, 4)
		if n := to - from; n != frameLen {
			w.write(uint32(n)>>16, 16)
			w.write(uint32(n)&0xffff, 16)
		}
		for _, s := range samples[from:to] {
			w.write(uint32(uint16(s)), 16)
		}
		out = append(out, w.buf)
	}
	return out
}

// alacCookie is the 24 bytes an ALAC sample entry keeps its configuration in.
func alacCookie(frameLen, rate int) []byte {
	c := make([]byte, 24)
	binary.BigEndian.PutUint32(c[0:4], uint32(frameLen))
	c[4] = 0  // the version of the format
	c[5] = 16 // bits a sample
	c[6] = 40 // the rice parameters the decoder uses
	c[7] = 10
	c[8] = 14
	c[9] = 1 // channels
	binary.BigEndian.PutUint16(c[10:12], 255)
	binary.BigEndian.PutUint32(c[20:24], uint32(rate))
	return c
}

// An M4A of ALAC — the lossless one — decodes to the very samples it was
// written from, and seeks to them.
func TestM4AALACPlays(t *testing.T) {
	const rate = 44100
	samples := make([]int16, rate) // a second
	for i := range samples {
		samples[i] = int16(math.Sin(2*math.Pi*440*float64(i)/rate) * 0.5 * 32767)
	}
	path := filepath.Join(t.TempDir(), "lossless.m4a")
	if err := os.WriteFile(path, alacM4A(t, samples, rate), 0o644); err != nil {
		t.Fatal(err)
	}

	s := openFileStream(t, path, 1000)
	// mygo's MP4 decoder hands back stereo even for a mono track: the
	// samples of one channel in both.
	if s.SampleRate() != rate || s.Channels() != 2 {
		t.Fatalf("stream = %d Hz, %d channels", s.SampleRate(), s.Channels())
	}
	buf := make([]float32, 4096)
	n, err := s.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("nothing decoded")
	}
	// Lossless says the samples come back as they went in.
	for i := 0; i < min(n/2, 1000); i++ {
		want := float32(samples[i]) / (1 << 15)
		if d := math.Abs(float64(buf[2*i] - want)); d > 1.0/(1<<15) {
			t.Fatalf("frame %d = %v, want %v", i, buf[2*i], want)
		}
	}
	// A seek lands where it is asked to, which for a lossless file is exact.
	if err := s.SetPosition(20000); err != nil {
		t.Fatal(err)
	}
	n, err = s.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("nothing decoded after the seek")
	}
	want := float32(samples[20000]) / (1 << 15)
	if d := math.Abs(float64(buf[0] - want)); d > 1.0/(1<<15) {
		t.Errorf("after the seek, %v, want %v", buf[0], want)
	}
}

// m4aBox builds a box of an MP4, of its type and the pieces of its body.
func m4aBox(typ string, body ...[]byte) []byte {
	n := 8
	for _, b := range body {
		n += len(b)
	}
	out := make([]byte, 8, n)
	binary.BigEndian.PutUint32(out, uint32(n))
	copy(out[4:8], typ)
	for _, b := range body {
		out = append(out, b...)
	}
	return out
}

func u16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// bitWriter appends the bits of a stream in the order ALAC keeps them: the
// highest of a field first.
type bitWriter struct {
	buf []byte
	n   int // the bits of the last byte written
}

func (w *bitWriter) write(v uint32, bits int) {
	for i := bits - 1; i >= 0; i-- {
		if w.n == 0 || w.n == 8 {
			w.buf = append(w.buf, 0)
			w.n = 0
		}
		if (v>>uint(i))&1 != 0 {
			w.buf[len(w.buf)-1] |= 1 << uint(7-w.n)
		}
		w.n++
	}
}

// An M4A is read by seeking through it, so it is played once it is whole: a
// song still coming in is refused rather than read as silence.
func TestM4APlaysOnlyWholeFiles(t *testing.T) {
	song, err := os.ReadFile(filepath.Join("testdata", "tone.m4a"))
	if err != nil {
		t.Fatal(err)
	}
	half := int64(len(song) / 2)
	coming := &download{data: song[:half], n: half, size: int64(len(song))}
	coming.cond.L = &coming.mu
	if _, err := openM4AStream(coming, 1000); err == nil {
		t.Fatal("a file still coming in was read as M4A")
	}
	whole := &download{data: song, n: int64(len(song)), size: int64(len(song)), done: true}
	whole.cond.L = &whole.mu
	s, err := openM4AStream(whole, 1000)
	if err != nil {
		t.Fatalf("a file that came in whole: %v", err)
	}
	if s.SampleRate() != 44100 || s.Channels() != 2 {
		t.Errorf("opened as %d Hz, %d channels", s.SampleRate(), s.Channels())
	}
}
