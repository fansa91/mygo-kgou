package tag

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// png returns a PNG header of the size asked for.
func png(width, height int) []byte {
	b := []byte("\x89PNG\r\n\x1a\n")
	chunk := make([]byte, 17)
	binary.BigEndian.PutUint32(chunk[0:4], 13) // the length of IHDR
	copy(chunk[4:8], "IHDR")
	binary.BigEndian.PutUint32(chunk[8:12], uint32(width))
	binary.BigEndian.PutUint32(chunk[12:16], uint32(height))
	chunk[16] = 8 // bits a channel
	b = append(b, chunk...)
	return append(b, 2) // RGB
}

// jpeg returns a JPEG header of the size asked for.
func jpeg(width, height int) []byte {
	// An APP0 segment, then the frame that holds the size.
	b := []byte{0xFF, 0xD8}
	app := []byte{0xFF, 0xE0, 0x00, 0x10}
	app = append(app, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")...)
	b = append(b, app...)
	sof := []byte{0xFF, 0xC0, 0x00, 0x11, 0x08, 0, 0, 0, 0, 0x03}
	binary.BigEndian.PutUint16(sof[5:7], uint16(height))
	binary.BigEndian.PutUint16(sof[7:9], uint16(width))
	b = append(b, sof...)
	return append(b, make([]byte, 32)...)
}

func TestImageSize(t *testing.T) {
	if w, h, d := imageSize(png(600, 600)); w != 600 || h != 600 || d != 24 {
		t.Errorf("png: got %dx%d at %d bits, want 600x600 at 24", w, h, d)
	}
	if w, h, d := imageSize(jpeg(300, 200)); w != 300 || h != 200 || d != 24 {
		t.Errorf("jpeg: got %dx%d at %d bits, want 300x200 at 24", w, h, d)
	}
}

// id3Frames returns the frames of an ID3v2.3 tag at the start of b, by id.
func id3Frames(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	if len(b) < 10 || string(b[:3]) != "ID3" {
		t.Fatal("no ID3 tag at the start of the file")
	}
	size := synchsafe(b[6:10])
	end := 10 + size
	if end > len(b) {
		end = len(b)
	}
	frames := map[string][]byte{}
	for i := 10; i+10 <= end; {
		id := string(b[i : i+4])
		if id == "\x00\x00\x00\x00" {
			break // the padding
		}
		n := int(binary.BigEndian.Uint32(b[i+4 : i+8]))
		if i+10+n > len(b) {
			break
		}
		frames[id] = b[i+10 : i+10+n]
		i += 10 + n
	}
	return frames
}

// textOf decodes a UTF-16 text frame of the tag.
func textOf(payload []byte) string {
	if len(payload) < 2 {
		return ""
	}
	// The first byte is the encoding, then the byte order mark.
	u := make([]uint16, 0, (len(payload)-2)/2)
	for i := 3; i+1 < len(payload); i += 2 {
		u = append(u, binary.LittleEndian.Uint16(payload[i:i+2]))
	}
	var sb strings.Builder
	for _, r := range u {
		sb.WriteRune(rune(r))
	}
	return sb.String()
}

// usltText decodes the words of an unsynchronised lyrics frame: the
// encoding, the language, an empty description and then the lyrics in
// UTF-16.
func usltText(t *testing.T, payload []byte) string {
	t.Helper()
	if len(payload) < 10 || payload[0] != 0x01 {
		t.Fatalf("not a UTF-16 USLT frame: % x", payload)
	}
	// 0x01, three bytes of language, the descriptor's byte order mark, the
	// terminator of no descriptor, the lyrics' byte order mark.
	u := make([]uint16, 0, (len(payload)-10)/2)
	for i := 10; i+1 < len(payload); i += 2 {
		u = append(u, binary.LittleEndian.Uint16(payload[i:i+2]))
	}
	var sb strings.Builder
	for _, r := range u {
		sb.WriteRune(rune(r))
	}
	return sb.String()
}

// A tagged MP3 keeps its audio after the new tag, and reads back the words
// it was tagged with.
func TestWriteMP3(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.mp3")
	audio := bytes.Repeat([]byte("mp3-audio-"), 100)
	// A tag the file already has, which the new one replaces.
	old := make([]byte, 10, 10+64)
	copy(old, "ID3")
	old[3] = 3
	putSynchsafe(old[6:10], 64)
	old = append(old, make([]byte, 64)...)
	if err := os.WriteFile(path, append(old, audio...), 0o644); err != nil {
		t.Fatal(err)
	}

	lyrics := "[00:00.00]晴天\n[00:00.00]A sunny day\n"
	m := Meta{Title: "晴天", Artist: "周杰伦", Album: "叶惠美", Lyrics: lyrics, Cover: jpeg(300, 300), MIME: "image/jpeg"}
	if err := Write(path, m); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(b, audio) {
		t.Error("the audio does not follow the tag as it was")
	}
	// One tag, not the old one under the new one.
	if bytes.Count(b, []byte("ID3")) != 1 {
		t.Errorf("the file holds %d tags, want 1", bytes.Count(b, []byte("ID3")))
	}
	frames := id3Frames(t, b)
	for id, want := range map[string]string{"TIT2": "晴天", "TPE1": "周杰伦", "TALB": "叶惠美"} {
		if got := textOf(frames[id]); got != want {
			t.Errorf("%s: got %q, want %q", id, got, want)
		}
	}
	if pic := frames["APIC"]; len(pic) == 0 || !bytes.Contains(pic, m.Cover) {
		t.Error("the cover is not in the tag")
	}
	if got := usltText(t, frames["USLT"]); got != lyrics {
		t.Errorf("the words in the tag are %q, want %q", got, lyrics)
	}
}

// A tagged FLAC keeps its stream information and its audio, and holds the
// tags and the cover.
func TestWriteFLAC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.flac")
	audio := bytes.Repeat([]byte("flac-audio-"), 100)
	streaminfo := make([]byte, 34)
	// A padding block the file already has, which stays.
	padding := make([]byte, 16)
	var b bytes.Buffer
	b.WriteString("fLaC")
	b.Write([]byte{0x00, 0x00, 0x00, 34})
	b.Write(streaminfo)
	b.Write([]byte{0x81, 0x00, 0x00, 16}) // the last block: padding
	b.Write(padding)
	b.Write(audio)
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	lyrics := "[00:00.00]晴天\n[00:00.00]A sunny day\n"
	m := Meta{Title: "晴天", Artist: "周杰伦", Album: "叶惠美", Lyrics: lyrics, Cover: png(600, 600), MIME: "image/png"}
	if err := Write(path, m); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(out, audio) {
		t.Error("the audio does not follow the metadata as it was")
	}
	if !bytes.HasPrefix(out, []byte("fLaC")) {
		t.Fatal("the file no longer starts with its marker")
	}
	var types []byte
	for i := 4; ; {
		typ := out[i] & 0x7f
		size := int(out[i+1])<<16 | int(out[i+2])<<8 | int(out[i+3])
		types = append(types, typ)
		if typ == 0 && string(out[i+4:i+4+size]) != string(streaminfo) {
			t.Error("the stream information changed")
		}
		if typ == 4 && !bytes.Contains(out[i+4:i+4+size], []byte("TITLE=晴天")) {
			t.Errorf("the title is not in the comment: %q", out[i+4:i+4+size])
		}
		if typ == 4 && !bytes.Contains(out[i+4:i+4+size], []byte("LYRICS="+lyrics)) {
			t.Errorf("the words are not in the comment: %q", out[i+4:i+4+size])
		}
		if out[i]&0x80 != 0 {
			break
		}
		i += 4 + size
	}
	for _, want := range []byte{0, 4, 6} {
		if !bytes.Contains(types, []byte{want}) {
			t.Errorf("the metadata has no block of type %d: %v", want, types)
		}
	}
}
