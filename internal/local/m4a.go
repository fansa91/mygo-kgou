package local

import (
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Reading M4A: the boxes of an MP4, of which one says how long the song plays
// and, in ilst under meta, what it is — the tags Apple's players write, and
// the pictures they keep.

// readM4A reads a file of the MP4 family: AAC as most .m4a are, and ALAC as
// the lossless ones are.
func readM4A(f *os.File, size int64) (meta, error) {
	m := meta{format: FormatM4A}
	moov, err := m4aMoov(f, size)
	if err != nil {
		return m, err
	}
	if m4aSound(moov) == "alac" {
		m.format = FormatALAC
	}
	m.length = m4aLength(moov)
	if ilst := m4aILST(moov); len(ilst) > 0 {
		m4aTags(ilst, &m)
	}
	return m, nil
}

// m4aMoov returns the body of a file's moov box, which says what the file
// holds and how long it plays. The boxes before it — ftyp, free, and the
// sound itself — are passed over by their own lengths.
func m4aMoov(f *os.File, size int64) ([]byte, error) {
	for off := int64(0); off+8 <= size; {
		typ, body, end, ok := m4aBoxAt(f, off, size)
		if !ok || end <= off {
			break
		}
		if typ == "moov" {
			if n := end - body; n > 0 && n <= 64<<20 {
				return readBlock(f, body, n), nil
			}
			return nil, ErrFormat
		}
		off = end
	}
	return nil, ErrFormat
}

// m4aBoxAt reads the header of the box at off and answers its type, where its
// body starts, and where the box ends.
func m4aBoxAt(f *os.File, off, size int64) (typ string, body, end int64, ok bool) {
	var h [16]byte
	if n, _ := f.ReadAt(h[:8], off); n < 8 {
		return "", 0, 0, false
	}
	length := int64(binary.BigEndian.Uint32(h[:4]))
	typ, head := string(h[4:8]), int64(8)
	switch length {
	case 0:
		length = size - off // the box runs to the end of the file
	case 1:
		if n, _ := f.ReadAt(h[8:16], off+8); n < 8 {
			return "", 0, 0, false
		}
		length, head = int64(binary.BigEndian.Uint64(h[8:16])), 16
	}
	if length < head || off+length > size {
		return "", 0, 0, false
	}
	return typ, off + head, off + length, true
}

// m4aChild returns the body of the first box of this type in b, or nil.
func m4aChild(b []byte, typ string) []byte {
	var found []byte
	m4aEach(b, typ, func(body []byte) bool {
		found = body
		return false
	})
	return found
}

// m4aEach calls fn for every box of this type in b, in the order they lie,
// until fn answers false.
func m4aEach(b []byte, typ string, fn func(body []byte) bool) {
	for len(b) >= 8 {
		length := int64(binary.BigEndian.Uint32(b[:4]))
		kind, head := string(b[4:8]), int64(8)
		switch length {
		case 0:
			length = int64(len(b))
		case 1:
			if len(b) < 16 {
				return
			}
			length, head = int64(binary.BigEndian.Uint64(b[8:16])), 16
		}
		if length < head || length > int64(len(b)) {
			return
		}
		if kind == typ && !fn(b[head:length]) {
			return
		}
		b = b[length:]
	}
}

// m4aSound returns the kind of sample the sound track holds, as the sample
// entry of its first track names it: "mp4a" for AAC, "alac" for ALAC.
func m4aSound(moov []byte) string {
	var kind string
	m4aEach(moov, "trak", func(trak []byte) bool {
		mdia := m4aChild(trak, "mdia")
		stbl := m4aChild(m4aChild(mdia, "minf"), "stbl")
		stsd := m4aChild(stbl, "stsd")
		// stsd is a full box: four bytes of version and flags, then the
		// count of the samples it lists, then the entries themselves.
		if len(stsd) < 16 {
			return true
		}
		switch entry := string(stsd[12:16]); entry {
		case "mp4a", "alac", "Opus", "fLaC", "ac-3":
			kind = entry
			return false
		}
		return true
	})
	return kind
}

// m4aLength returns how long the movie plays, as its header says.
func m4aLength(moov []byte) time.Duration {
	return m4aSpan(m4aChild(moov, "mvhd"))
}

// m4aSpan reads the timescale and the length from an mvhd or mdhd box, which
// hold them the same way: two 64-bit times in a file of the second version,
// and two 32-bit ones before that.
func m4aSpan(b []byte) time.Duration {
	var scale, length uint64
	switch {
	case len(b) >= 32 && b[0] == 1:
		scale, length = uint64(binary.BigEndian.Uint32(b[20:24])), binary.BigEndian.Uint64(b[24:32])
	case len(b) >= 20:
		scale, length = uint64(binary.BigEndian.Uint32(b[12:16])), uint64(binary.BigEndian.Uint32(b[16:20]))
	}
	if scale == 0 || length == 0 || length == 0xffff_ffff || length == 0xffff_ffff_ffff_ffff {
		return 0
	}
	return time.Duration(float64(length) / float64(scale) * float64(time.Second))
}

// m4aILST returns the list of tags: it sits under meta, at the top of moov or
// under its udta.
func m4aILST(moov []byte) []byte {
	meta := m4aChild(moov, "meta")
	if len(meta) == 0 {
		meta = m4aChild(m4aChild(moov, "udta"), "meta")
	}
	// meta is a full box, with four bytes of version and flags before the
	// boxes it holds.
	if len(meta) < 4 {
		return nil
	}
	return m4aChild(meta[4:], "ilst")
}

// m4aTags reads the items of an ilst box: each an item of one name, holding
// a data box with its value after the type and the locale.
func m4aTags(ilst []byte, m *meta) {
	for len(ilst) >= 8 {
		size := int64(binary.BigEndian.Uint32(ilst[:4]))
		if size < 8 || size > int64(len(ilst)) {
			return
		}
		name := string(ilst[4:8])
		data := m4aChild(ilst[8:size], "data")
		ilst = ilst[size:]
		if len(data) < 8 {
			continue
		}
		value := data[8:] // past the type of what it holds, and the locale
		switch name {
		case "\xa9nam":
			m.title = m4aText(value)
		case "\xa9ART":
			m.artist = m4aText(value)
		case "aART":
			m.albumArtist = m4aText(value)
		case "\xa9alb":
			m.album = m4aText(value)
		case "\xa9day":
			if m.year == "" {
				m.year = m4aText(value)
			}
		case "trkn":
			m.track = m4aNumber(value, 2)
		case "disk":
			m.disc = m4aNumber(value, 2)
		case "\xa9lyr":
			if strings.TrimSpace(m.lyrics) == "" {
				m.lyrics = m4aText(value)
			}
		case "covr":
			if len(m.cover) == 0 {
				m.cover = append([]byte(nil), value...)
			}
		}
	}
}

// m4aText reads the words of an item, which are UTF-8 as Apple's players
// write them.
func m4aText(b []byte) string {
	b = []byte(strings.TrimRight(string(b), "\x00"))
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return strings.TrimSpace(string(b))
	}
	return strings.TrimSpace(legacyText(b))
}

// m4aNumber reads a number an item keeps as a pair of 16-bit counts, at off
// bytes in: a track's number, or a disc's.
func m4aNumber(b []byte, off int) string {
	if len(b) < off+2 {
		return ""
	}
	n := binary.BigEndian.Uint16(b[off : off+2])
	if n == 0 {
		return ""
	}
	return strconv.Itoa(int(n))
}
