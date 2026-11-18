package local

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// box builds a box of an MP4: its type, and the pieces of its body.
func box(typ string, body ...[]byte) []byte {
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

func be16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

func be32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// The headers of an Ogg file, and the comments it holds, count in
// little-endian as the format says.

func u16le(v uint16) []byte {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, v)
	return b
}

func u32le(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// item is a tag of an M4A, as its ilst keeps one: its name, and the value in
// a data box after the type of what it holds and the locale.
func item(name string, value []byte) []byte {
	return box(name, box("data", be32(1), be32(0), value))
}

// ilstBook builds an ilst of the tags these tests look for, the picture among
// them, as Apple's players write them.
func ilstBook(pic []byte) []byte {
	track := []byte{0, 0, 0, 3, 0, 12, 0, 0}
	disc := []byte{0, 0, 0, 1, 0, 2, 0, 0}
	return box("ilst",
		item("\xa9nam", []byte("晴天")),
		item("\xa9ART", []byte("周杰伦")),
		item("\xa9alb", []byte("叶惠美")),
		item("aART", []byte("周杰伦")),
		item("\xa9day", []byte("2003-07-31")),
		item("trkn", track),
		item("disk", disc),
		item("\xa9lyr", []byte("[00:00.00]故事的小黄花")),
		box("covr", box("data", be32(13), be32(0), pic)),
	)
}

// m4aFile builds an M4A of the sample entry named — "mp4a" for AAC, "alac"
// for the lossless one — holding tags and a second and a half of silence.
func m4aFile(entry string, pic []byte) []byte {
	const (
		rate    = 44100
		samples = rate * 3 / 2
	)
	stsd := box("stsd", be32(0), be32(1), box(entry,
		make([]byte, 6), be16(1), be16(0), be16(0), make([]byte, 4),
		be16(2), be16(16), be16(0), be16(0), be32(rate<<16)))
	mdhd := box("mdhd", be32(0), be32(0), be32(0), be32(rate), be32(samples), be16(0x55c4), be16(0))
	stbl := box("stbl", stsd)
	mdia := box("mdia", mdhd, box("minf", stbl))
	mvhd := box("mvhd", be32(0), be32(0), be32(0), be32(rate), be32(samples),
		be32(0x00010000), be16(0x0100), be16(0), make([]byte, 8), make([]byte, 36), make([]byte, 24), be32(2))
	meta := box("meta", be32(0), ilstBook(pic))
	moov := box("moov", mvhd, box("trak", mdia), box("udta", meta))
	ftyp := box("ftyp", []byte("M4A "), be32(0x200), []byte("M4A "), []byte("isom"))
	out := append(ftyp, moov...)
	return append(out, box("mdat", nil)...)
}

// What an M4A says about itself — its tags, its picture and how long it plays
// — is read as the tags of any other file of the library are.
func TestM4AReadsTheTags(t *testing.T) {
	pic := coverPNG(t)
	for _, tc := range []struct{ entry, format string }{
		{"mp4a", FormatM4A},
		{"alac", FormatALAC},
	} {
		t.Run(tc.format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "song.m4a")
			if err := os.WriteFile(path, m4aFile(tc.entry, pic), 0o644); err != nil {
				t.Fatal(err)
			}
			tr, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			if tr.Name != "晴天" || tr.Artist != "周杰伦" || tr.Album != "叶惠美" || tr.AlbumArtist != "周杰伦" {
				t.Errorf("tags = %q / %q / %q / %q", tr.Name, tr.Artist, tr.Album, tr.AlbumArtist)
			}
			if tr.TrackNo != 3 || tr.DiscNo != 1 || tr.Year != "2003" {
				t.Errorf("number = %d/%d, year %q", tr.TrackNo, tr.DiscNo, tr.Year)
			}
			if tr.Format != tc.format {
				t.Errorf("format = %q, want %q", tr.Format, tc.format)
			}
			if tr.Length < time.Second || tr.Length > 2*time.Second {
				t.Errorf("length = %v", tr.Length)
			}
			if !tr.Covered || !bytes.Equal(tr.Cover, pic) {
				t.Errorf("cover = %d bytes, covered %v", len(tr.Cover), tr.Covered)
			}
			if !strings.Contains(tr.Lyrics, "故事的小黄花") {
				t.Errorf("lyrics = %q", tr.Lyrics)
			}
		})
	}
}

// oggPage writes one page of an Ogg file, with the packets it carries.
func oggPage(serial, seq uint32, flags byte, granule int64, packets ...[]byte) []byte {
	var segs []byte
	var payload []byte
	for _, p := range packets {
		for n := len(p); ; {
			if n >= 255 {
				segs = append(segs, 255)
				n -= 255
				continue
			}
			segs = append(segs, byte(n))
			break
		}
		payload = append(payload, p...)
	}
	p := &oggPageImage{
		headerType: flags, granule: granule, serial: serial, seq: seq,
		segs: segs, payload: payload,
	}
	return p.marshal()
}

// oggPageImage is a page being written, laid out as the format keeps it.
type oggPageImage struct {
	headerType byte
	granule    int64
	serial     uint32
	seq        uint32
	segs       []byte
	payload    []byte
}

func (p *oggPageImage) marshal() []byte {
	out := []byte("OggS")
	out = append(out, 0, p.headerType)
	out = binary.LittleEndian.AppendUint64(out, uint64(p.granule))
	out = binary.LittleEndian.AppendUint32(out, p.serial)
	out = binary.LittleEndian.AppendUint32(out, p.seq)
	out = append(out, make([]byte, 4)...) // the checksum, written below
	out = append(out, byte(len(p.segs)))
	out = append(out, p.segs...)
	out = append(out, p.payload...)
	binary.LittleEndian.PutUint32(out[22:26], oggCRC(out))
	return out
}

// oggCRC is the checksum a page carries, of the polynomial the format names.
func oggCRC(b []byte) uint32 {
	const poly = 0x04c11db7
	var crc uint32
	for _, c := range b {
		crc = crc<<8 ^ uint32(c)
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ poly
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// opusFile builds an Ogg Opus file of two seconds of sound — a page of each
// header, then one page of it — with the tags a comment header holds.
func opusFile(t *testing.T) []byte {
	t.Helper()
	head := []byte("OpusHead\x01\x02")
	head = append(head, u16le(312)...)   // the priming, at 48 kHz
	head = append(head, u32le(48000)...) // the rate of the sound before it was encoded
	head = append(head, u16le(0)...)     // gain
	head = append(head, 0)               // the channel mapping of mono and stereo
	tags := []byte("OpusTags")
	vendor := []byte("mygo")
	tags = append(tags, u32le(uint32(len(vendor)))...)
	tags = append(tags, vendor...)
	entries := []string{"TITLE=晴天", "ARTIST=周杰伦", "ALBUM=叶惠美", "TRACKNUMBER=3", "DATE=2003"}
	tags = append(tags, u32le(uint32(len(entries)))...)
	for _, e := range entries {
		tags = append(tags, u32le(uint32(len(e)))...)
		tags = append(tags, e...)
	}
	audio := []byte{0xf8, 0xff, 0xfe} // a packet of silence
	out := oggPage(7, 0, 0x02, 0, head)
	out = append(out, oggPage(7, 1, 0, 0, tags)...)
	out = append(out, oggPage(7, 2, 0x04, 48000*2+312, audio)...)
	return out
}

// An Ogg Opus file of the listener's own is read as the tags of any other
// file are: its comments, and how long it plays by its last page.
func TestOggOpusReadsTheTags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.opus")
	if err := os.WriteFile(path, opusFile(t), 0o644); err != nil {
		t.Fatal(err)
	}
	tracks, skipped, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(tracks) != 1 {
		t.Fatalf("scan = %d songs, %d skipped (%v)", len(tracks), skipped, err)
	}
	tr := tracks[0]
	if tr.Name != "晴天" || tr.Artist != "周杰伦" || tr.Album != "叶惠美" {
		t.Errorf("tags = %q / %q / %q", tr.Name, tr.Artist, tr.Album)
	}
	if tr.TrackNo != 3 || tr.Year != "2003" {
		t.Errorf("number = %d, year %q", tr.TrackNo, tr.Year)
	}
	if tr.Format != FormatOpus {
		t.Errorf("format = %q", tr.Format)
	}
	// Two seconds of sound, without the priming the header names.
	if tr.Length < 1900*time.Millisecond || tr.Length > 2100*time.Millisecond {
		t.Errorf("length = %v", tr.Length)
	}
}
