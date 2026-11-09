package local

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"os"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// meta is what a file says about itself, before it is made a Track.
type meta struct {
	title, artist, album, albumArtist string
	year, track, disc, lyrics         string
	cover                             []byte
	format                            string
	length                            time.Duration
}

// Reading MP3: an ID3v2 tag at the start, the frames after it, and a
// v1 tag of 128 bytes at the very end for the files that carry one.

var (
	bitratesV1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	bitratesV2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	sampleRate = [4]int{44100, 48000, 32000, 0}
)

func readMP3(f *os.File, size int64) (meta, error) {
	m := meta{format: FormatMP3}
	audio := int64(0)
	head := make([]byte, 10)
	if n, _ := f.ReadAt(head, 0); n == len(head) && string(head[:3]) == "ID3" {
		body := make([]byte, syncSafe(head[6:10]))
		if _, err := f.ReadAt(body, 10); err == nil {
			readID3(body, head[3], head[5], &m)
		}
		audio = 10 + int64(len(body))
		if head[3] >= 4 && head[5]&0x10 != 0 {
			audio += 10 // the tag's footer
		}
	}
	end := size
	if size > 128 {
		tail := make([]byte, 128)
		if _, err := f.ReadAt(tail, size-128); err == nil && string(tail[:3]) == "TAG" {
			readID3v1(tail, &m)
			end -= 128
		}
	}
	m.length = mp3Length(f, audio, end)
	return m, nil
}

// mp3Length returns how long the MP3 from audio to end plays: by the count
// of frames a Xing or Info header keeps, as the files KuGou serves and the
// ones encoded at a variable rate do, and otherwise by their bit rate.
func mp3Length(f *os.File, audio, end int64) time.Duration {
	if end <= audio {
		return 0
	}
	// The first frame, which may follow some junk after the tag.
	buf := make([]byte, 8<<10)
	n, _ := f.ReadAt(buf, audio)
	buf = buf[:n]
	start := -1
	for i := 0; i+4 <= len(buf); i++ {
		if frameLen(buf[i:]) > 0 {
			start, buf = i, buf[i:]
			break
		}
	}
	if start < 0 {
		return 0
	}
	_, rate, bitrate, spf := mp3Frame(buf)
	if rate == 0 {
		return 0
	}
	// A frame of silence after the seek in the buffer holds the count.
	head := buf[:min(len(buf), 256)]
	for _, name := range []string{"Xing", "Info"} {
		if i := bytes.Index(head, []byte(name)); i >= 0 && i+12 <= len(head) {
			if frames, ok := xingFrames(head[i:]); ok && frames > 0 {
				return time.Duration(float64(frames) * float64(spf) / float64(rate) * float64(time.Second))
			}
		}
	}
	if bitrate <= 0 {
		return 0
	}
	bytes := end - audio - int64(start)
	return time.Duration(float64(bytes) * 8 / float64(bitrate) * float64(time.Second))
}

// xingFrames returns the frames an Xing or Info header counts, if it does.
func xingFrames(b []byte) (int64, bool) {
	if len(b) < 12 {
		return 0, false
	}
	flags := binary.BigEndian.Uint32(b[4:8])
	if flags&1 == 0 {
		return 0, false
	}
	return int64(binary.BigEndian.Uint32(b[8:12])), true
}

// frameLen returns the length of the MPEG layer III frame whose header b
// starts with, or 0 if it starts with none.
func frameLen(b []byte) int {
	if len(b) < 4 || b[0] != 0xff || b[1]&0xe0 != 0xe0 {
		return 0
	}
	version := (b[1] >> 3) & 3 // 3: MPEG 1, 2: MPEG 2, 0: MPEG 2.5
	layer := (b[1] >> 1) & 3   // 1: layer III
	if version == 1 || layer != 1 {
		return 0
	}
	bi, ri := b[2]>>4, (b[2]>>2)&3
	if bi == 0 || bi == 15 || ri == 3 {
		return 0
	}
	pad := int(b[2]>>1) & 1
	rate := sampleRate[ri]
	switch version {
	case 3:
		return 144*bitratesV1[bi]*1000/rate + pad
	case 2:
		return 72*bitratesV2[bi]*1000/(rate/2) + pad
	default:
		return 72*bitratesV2[bi]*1000/(rate/4) + pad
	}
}

// mp3Frame returns the frame's length, its sample rate, its bit rate and
// the samples it holds: 1152 of MPEG 1, and half of that of MPEG 2.
func mp3Frame(b []byte) (size, rate, bitrate, samples int) {
	size = frameLen(b)
	if size == 0 {
		return 0, 0, 0, 0
	}
	version := (b[1] >> 3) & 3
	bi, ri := b[2]>>4, (b[2]>>2)&3
	samples = 1152
	if version != 3 {
		samples = 576
	}
	if version == 3 {
		bitrate = bitratesV1[bi] * 1000
	} else {
		bitrate = bitratesV2[bi] * 1000
	}
	return size, sampleRate[ri], bitrate, samples
}

// readID3 reads an ID3v2 tag body into m.
func readID3(b []byte, version, flags byte, m *meta) {
	if len(b) < 6 {
		return
	}
	if flags&0x80 != 0 {
		b = unsync(b)
	}
	if version == 2 {
		readID3v22(b, m)
		return
	}
	if flags&0x40 != 0 {
		if len(b) < 4 {
			return
		}
		n := 4 + int(binary.BigEndian.Uint32(b[:4]))
		if version >= 4 {
			n = syncSafe(b[:4])
		}
		if n > 0 && n < len(b) {
			b = b[n:]
		}
	}
	for len(b) >= 10 {
		id := string(b[:4])
		if id[0] == 0 {
			return // the padding after the frames
		}
		n := int(binary.BigEndian.Uint32(b[4:8]))
		if version >= 4 {
			n = syncSafe(b[4:8])
		}
		fflags := binary.BigEndian.Uint16(b[8:10])
		b = b[10:]
		if n <= 0 || n > len(b) {
			return
		}
		payload := b[:n]
		b = b[n:]
		if fflags&0x0c != 0 {
			continue // compressed or encrypted: read by no one
		}
		if fflags&0x01 != 0 && len(payload) >= 4 {
			payload = payload[4:] // the length of what is not compressed
		}
		switch id {
		case "TIT2":
			m.title = text(payload)
		case "TPE1":
			m.artist = text(payload)
		case "TALB":
			m.album = text(payload)
		case "TPE2":
			m.albumArtist = text(payload)
		case "TRCK":
			m.track = text(payload)
		case "TPOS":
			m.disc = text(payload)
		case "TYER", "TDRC":
			if m.year == "" {
				m.year = text(payload)
			}
		case "APIC":
			coverMP3(payload, m)
		case "USLT":
			lyricsMP3(payload, m)
		}
	}
}

// readID3v22 reads a tag of the first version, whose frames are shorter.
func readID3v22(b []byte, m *meta) {
	for len(b) >= 6 {
		id := string(b[:3])
		if id[0] == 0 {
			return
		}
		n := int(b[3])<<16 | int(b[4])<<8 | int(b[5])
		b = b[6:]
		if n <= 0 || n > len(b) {
			return
		}
		p := b[:n]
		b = b[n:]
		switch id {
		case "TT2":
			m.title = text(p)
		case "TP1":
			m.artist = text(p)
		case "TAL":
			m.album = text(p)
		case "TP2":
			m.albumArtist = text(p)
		case "TRK":
			m.track = text(p)
		case "TPA":
			m.disc = text(p)
		case "TYE":
			m.year = text(p)
		case "ULT":
			lyricsMP3(p, m)
		}
	}
}

// readID3v1 reads the tag at the end of the file, which says less than the
// first version's and fills only what that did not.
func readID3v1(b []byte, m *meta) {
	field := func(from, to int) string { return legacyText(b[from:to]) }
	if m.title == "" {
		m.title = field(3, 33)
	}
	if m.artist == "" {
		m.artist = field(33, 63)
	}
	if m.album == "" {
		m.album = field(63, 93)
	}
	if m.year == "" {
		m.year = field(93, 97)
	}
}

// unsync undoes the tag's unsynchronisation, which puts a zero after every
// byte that looks like a frame's start.
func unsync(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		out = append(out, b[i])
		if b[i] == 0xff && i+1 < len(b) && b[i+1] == 0x00 {
			i++
		}
	}
	return out
}

// text returns the words of a text frame.
func text(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	s := decodeString(b[0], b[1:])
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i] // a frame may hold several values
	}
	return strings.TrimSpace(s)
}

// decodeString reads text as a tag's encoding byte says it is.
func decodeString(enc byte, b []byte) string {
	switch enc {
	case 1:
		return utf16Text(b, false)
	case 2:
		return utf16Text(b, true)
	case 3:
		return strings.TrimRight(string(b), "\x00")
	default:
		return legacyText(b)
	}
}

// utf16Text reads UTF-16, little- or big-endian, as the byte order mark
// says where there is one.
func utf16Text(b []byte, bigEndian bool) string {
	if len(b) >= 2 {
		switch {
		case b[0] == 0xff && b[1] == 0xfe:
			bigEndian, b = false, b[2:]
		case b[0] == 0xfe && b[1] == 0xff:
			bigEndian, b = true, b[2:]
		}
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return strings.TrimRight(string(utf16.Decode(u)), "\x00")
}

// legacyText reads text of a tag that says nothing of its encoding: UTF-8
// as it is written now, GBK as the tags of Chinese songs were, and Latin-1
// otherwise.
func legacyText(b []byte) string {
	b = bytes.TrimRight(b, "\x00")
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	if s, err := simplifiedchinese.GBK.NewDecoder().Bytes(b); err == nil && utf8.Valid(s) {
		return string(s)
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// coverMP3 reads the picture of an APIC frame, preferring the front cover
// of the album to the others.
func coverMP3(b []byte, m *meta) {
	if len(b) < 2 {
		return
	}
	enc := b[0]
	b = b[1:]
	i := bytes.IndexByte(b, 0) // the end of the MIME type
	if i < 0 || i+2 > len(b) {
		return
	}
	b = b[i+1:]
	kind := b[0]
	b = b[1:]
	b = pastDescription(b, enc)
	if len(b) == 0 {
		return
	}
	if len(m.cover) > 0 && kind != 3 {
		return // another picture, and not the front cover
	}
	m.cover = append([]byte(nil), b...)
}

// lyricsMP3 reads the words of a USLT frame, which come after the language
// and a description no one fills.
func lyricsMP3(b []byte, m *meta) {
	if len(b) < 4 || m.lyrics != "" {
		return
	}
	enc := b[0]
	b = pastDescription(b[4:], enc) // past the language
	s := strings.TrimRight(decodeString(enc, b), "\x00")
	if strings.TrimSpace(s) != "" {
		m.lyrics = s
	}
}

// pastDescription returns b after the null-ended description it starts
// with, of the encoding enc.
func pastDescription(b []byte, enc byte) []byte {
	if enc == 1 || enc == 2 {
		for i := 0; i+1 < len(b); i += 2 {
			if b[i] == 0 && b[i+1] == 0 {
				return b[i+2:]
			}
		}
		return nil
	}
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return b[i+1:]
	}
	return nil
}

// syncSafe reads a size as ID3v2 keeps it: seven bits a byte.
func syncSafe(b []byte) int {
	n := 0
	for _, c := range b {
		n = n<<7 | int(c&0x7f)
	}
	return n
}

// Reading FLAC: its metadata blocks, of which one says how long the song
// plays, one what it is, and one holds its picture.

func readFLAC(f *os.File, size int64) (meta, error) {
	m := meta{format: FormatFLAC}
	magic := make([]byte, 4)
	if n, _ := f.ReadAt(magic, 0); n != 4 || string(magic) != "fLaC" {
		return m, ErrFormat
	}
	off := int64(4)
	head := make([]byte, 4)
	for off+4 <= size {
		if _, err := f.ReadAt(head, off); err != nil {
			break
		}
		last := head[0]&0x80 != 0
		typ := head[0] & 0x7f
		n := int64(head[1])<<16 | int64(head[2])<<8 | int64(head[3])
		off += 4
		if n < 0 || off+n > size {
			break
		}
		switch typ {
		case 0:
			if b := readBlock(f, off, min(n, 34)); len(b) >= 18 {
				rate := int(b[10])<<12 | int(b[11])<<4 | int(b[12])>>4
				samples := int64(b[13]&0x0f)<<32 | int64(binary.BigEndian.Uint32(b[14:18]))
				if rate > 0 && samples > 0 {
					m.length = time.Duration(float64(samples) / float64(rate) * float64(time.Second))
				}
			}
		case 4:
			vorbisTags(readBlock(f, off, n), &m)
		case 6:
			if len(m.cover) == 0 {
				picture(readBlock(f, off, n), &m)
			}
		}
		off += n
		if last {
			break
		}
	}
	return m, nil
}

// readBlock returns n bytes of f at off, or as many as there are.
func readBlock(f *os.File, off, n int64) []byte {
	if n <= 0 {
		return nil
	}
	b := make([]byte, n)
	got, _ := f.ReadAt(b, off)
	return b[:got]
}

// vorbisTags reads a Vorbis comment into m, as FLAC and Ogg both keep it.
func vorbisTags(b []byte, m *meta) {
	if len(b) < 4 {
		return
	}
	b = b[4+int(le32(b)):] // past the vendor
	if len(b) < 4 {
		return
	}
	count := int(le32(b))
	b = b[4:]
	for i := 0; i < count && len(b) >= 4; i++ {
		n := int(le32(b))
		b = b[4:]
		if n <= 0 || n > len(b) {
			return
		}
		k, v, ok := strings.Cut(string(b[:n]), "=")
		b = b[n:]
		if !ok {
			continue
		}
		switch strings.ToUpper(k) {
		case "TITLE":
			m.title = v
		case "ARTIST":
			m.artist = joinNames(m.artist, v)
		case "ALBUM":
			m.album = v
		case "ALBUMARTIST", "ALBUM ARTIST":
			m.albumArtist = v
		case "TRACKNUMBER":
			m.track = v
		case "DISCNUMBER":
			m.disc = v
		case "DATE", "YEAR":
			if m.year == "" {
				m.year = v
			}
		case "LYRICS", "UNSYNCEDLYRICS":
			if strings.TrimSpace(m.lyrics) == "" {
				m.lyrics = v
			}
		case "METADATA_BLOCK_PICTURE":
			if len(m.cover) == 0 {
				if data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v)); err == nil {
					picture(data, m)
				}
			}
		}
	}
}

// joinNames adds a name to the ones a tag already has: a song sung by two
// keeps two values under ARTIST.
func joinNames(have, add string) string {
	add = strings.TrimSpace(add)
	switch {
	case add == "":
		return have
	case have == "":
		return add
	case strings.Contains(have, add):
		return have
	}
	return have + ", " + add
}

// picture reads a FLAC picture block into m.
func picture(b []byte, m *meta) {
	if len(b) < 8 {
		return
	}
	n := int(binary.BigEndian.Uint32(b[4:8]))
	if 8+n+8 > len(b) {
		return
	}
	b = b[8+n:] // past the MIME type
	n = int(binary.BigEndian.Uint32(b[:4]))
	if 4+n+16 > len(b) {
		return
	}
	b = b[4+n:] // past the description
	b = b[16:]  // past the size, the depth and the colors
	if len(b) < 4 {
		return
	}
	n = int(binary.BigEndian.Uint32(b[:4]))
	if n <= 0 || 4+n > len(b) {
		return
	}
	m.cover = append([]byte(nil), b[4:4+n]...)
}

// Reading Ogg: pages of packets, of which the first names the format and
// the sample rate, and the second holds what the song is.

func readOgg(f *os.File, size int64) (meta, error) {
	m := meta{format: FormatOgg}
	head := readBlock(f, 0, min(size, 128<<10))
	if !bytes.HasPrefix(head, []byte("OggS")) {
		return m, ErrFormat
	}
	pkts := packets(head, 2)
	if len(pkts) == 0 || !bytes.HasPrefix(pkts[0], []byte("\x01vorbis")) {
		// Not Vorbis: Opus is kept in the same kind of file, and the app
		// plays none of it.
		return m, ErrFormat
	}
	rate := 0
	if len(pkts[0]) >= 16 {
		rate = int(le32(pkts[0][12:16]))
	}
	if len(pkts) > 1 && bytes.HasPrefix(pkts[1], []byte("\x03vorbis")) {
		vorbisTags(pkts[1][7:], &m)
	}
	if rate > 0 {
		if samples, ok := oggSamples(f, size); ok {
			m.length = time.Duration(float64(samples) / float64(rate) * float64(time.Second))
		}
	}
	return m, nil
}

// packets returns the first want packets of an Ogg stream, as far as they
// are in b.
func packets(b []byte, want int) [][]byte {
	var out [][]byte
	var cur []byte
	for len(b) >= 27 {
		if string(b[:4]) != "OggS" {
			return out
		}
		nsegs := int(b[26])
		if len(b) < 27+nsegs {
			return out
		}
		segs := b[27 : 27+nsegs]
		body := b[27+nsegs:]
		total := 0
		for _, s := range segs {
			total += int(s)
		}
		if len(body) < total {
			return out
		}
		for _, s := range segs {
			cur = append(cur, body[:int(s)]...)
			body = body[int(s):]
			if s < 255 {
				out = append(out, cur)
				cur = nil
				if len(out) >= want {
					return out
				}
			}
		}
		b = body
	}
	return out
}

// oggSamples returns the sample the stream ends at, as the granule
// position of its last page says.
func oggSamples(f *os.File, size int64) (int64, bool) {
	n := min(size, 64<<10)
	buf := readBlock(f, size-n, n)
	for i := len(buf) - 27; i >= 0; i-- {
		if string(buf[i:i+4]) != "OggS" {
			continue
		}
		g := int64(le64(buf[i+6 : i+14]))
		if g > 0 {
			return g, true
		}
	}
	return 0, false
}

// Reading WAV: its chunks, of which one says the format, one holds the
// sound, and one may hold the tags.

func readWAV(f *os.File, size int64) (meta, error) {
	m := meta{format: FormatWAV}
	head := make([]byte, 12)
	if n, _ := f.ReadAt(head, 0); n != 12 || string(head[:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return m, ErrFormat
	}
	var rate, channels, bits int
	data := int64(-1)
	chunk := make([]byte, 8)
	for off := int64(12); off+8 <= size; {
		if _, err := f.ReadAt(chunk, off); err != nil {
			break
		}
		id := string(chunk[:4])
		n := int64(le32(chunk[4:8]))
		body := off + 8
		if n < 0 || body+n > size {
			n = size - body
		}
		switch id {
		case "fmt ":
			if b := readBlock(f, body, min(n, 16)); len(b) >= 16 {
				channels = int(le16(b[2:4]))
				rate = int(le32(b[4:8]))
				bits = int(le16(b[14:16]))
			}
		case "data":
			data = n
		case "LIST":
			if n <= 4096 {
				info(readBlock(f, body, n), &m)
			}
		}
		off = body + n + n&1 // the chunks are word-aligned
	}
	if data > 0 && rate > 0 && channels > 0 && bits > 0 {
		m.length = time.Duration(float64(data) / float64(rate*channels*bits/8) * float64(time.Second))
	}
	return m, nil
}

// info reads the tags of a LIST chunk, as a recorder writes them.
func info(b []byte, m *meta) {
	if len(b) < 4 || string(b[:4]) != "INFO" {
		return
	}
	b = b[4:]
	for len(b) >= 8 {
		id := string(b[:4])
		n := int(le32(b[4:8]))
		b = b[8:]
		if n < 0 || n > len(b) {
			return
		}
		v := strings.TrimSpace(legacyText(b[:n]))
		b = b[n:]
		if n&1 != 0 && len(b) > 0 {
			b = b[1:]
		}
		switch id {
		case "INAM":
			m.title = v
		case "IART":
			m.artist = v
		case "IPRD":
			m.album = v
		case "ICRD":
			if m.year == "" {
				m.year = v
			}
		}
	}
}

// decodeFile reads a text file of the listener's own, as a .lrc is: UTF-8
// as it is written now, UTF-16 as Windows writes it, and GBK as the ones
// downloaded years ago are.
func decodeFile(b []byte) string {
	switch {
	case len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe:
		return utf16Text(b, false)
	case len(b) >= 2 && b[0] == 0xfe && b[1] == 0xff:
		return utf16Text(b, true)
	case len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf:
		return string(b[3:])
	}
	return legacyText(b)
}

func le16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func le64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }
