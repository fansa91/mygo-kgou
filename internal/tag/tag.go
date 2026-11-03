// Package tag writes what a song is into its file, so that the music
// players that read the file, this app's included, show its name, its
// artists, its album and its cover rather than an untitled file.
//
// MP3 is tagged with ID3v2.3, which Windows and every player reads, FLAC
// with its Vorbis comment and a picture block. The tag goes before the
// audio, which is copied through as it was.
package tag

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// Meta is what a file is tagged with; an empty field is left out.
type Meta struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	// Track is the number of the song in its album, Disc the disc it is on
	// where the album has more than one, and Year the year it came out in,
	// all as text.
	Track string
	Disc  string
	Year  string
	// Lyrics is the song's words as LRC, with the translation of each line
	// right after it where the song has one.
	Lyrics string
	// Cover is the album's picture, of the MIME type given.
	Cover []byte
	MIME  string
}

// Write tags the file at path by what its extension says it is. A format
// this package cannot tag is left alone.
func Write(path string, m Meta) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return writeMP3(path, m)
	case ".flac":
		return writeFLAC(path, m)
	}
	return nil
}

// chunk is a metadata block of a FLAC file: its type and its contents.
type chunk struct {
	typ  byte
	data []byte
}

// replaceFile writes path anew through tmp, which the caller fills: the
// file is only replaced once it is whole, so a failed tag leaves the
// download as it was.
func replaceFile(path string, tmp *os.File, write func(w io.Writer) error) error {
	name := tmp.Name()
	if err := write(tmp); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	// Windows refuses to rename over a file that exists, so the old one
	// goes first: the new one is whole, and about to take its place.
	err := os.Rename(name, path)
	if err != nil && os.Remove(path) == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// writeMP3 replaces the ID3v2 tag the file starts with, if any, by one of
// these, and puts the audio after it.
func writeMP3(path string, m Meta) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	// Where the audio starts: after a tag the file already has, if it has
	// one, which this replaces.
	var audio int64
	head := make([]byte, 10)
	if n, _ := io.ReadFull(src, head); n == len(head) && string(head[:3]) == "ID3" {
		audio = 10 + int64(synchsafe(head[6:10]))
		// A v2.4 tag may carry a footer, another 10 bytes, after it.
		if head[5]&0x10 != 0 {
			audio += 10
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tag-*"+filepath.Ext(path))
	if err != nil {
		return err
	}
	return replaceFile(path, tmp, func(w io.Writer) error {
		// The file is closed before it is replaced: Windows will not
		// replace one that is open.
		defer src.Close()
		if _, err := w.Write(id3v2(m)); err != nil {
			return err
		}
		if _, err := src.Seek(audio, io.SeekStart); err != nil {
			return err
		}
		_, err := io.Copy(w, src)
		return err
	})
}

// id3v2 returns a v2.3 tag of m, padded so that a later tag of about the
// same size fits in it.
func id3v2(m Meta) []byte {
	var frames []byte
	for _, f := range []struct{ id, text string }{
		{"TIT2", m.Title},
		{"TPE1", m.Artist},
		{"TALB", m.Album},
		{"TPE2", m.AlbumArtist},
		{"TRCK", m.Track},
		{"TPOS", m.Disc},
		{"TYER", m.Year},
	} {
		frames = append(frames, textFrame(f.id, f.text)...)
	}
	frames = append(frames, pictureFrame(m)...)
	frames = append(frames, lyricsFrame(m)...)
	// Room for a tag of this size again, as tag writers leave.
	padding := len(frames) + 1024
	head := make([]byte, 10)
	copy(head, "ID3")
	head[3], head[4], head[5] = 3, 0, 0
	putSynchsafe(head[6:10], padding)
	out := make([]byte, 0, 10+padding)
	out = append(out, head...)
	out = append(out, frames...)
	return append(out, make([]byte, padding-len(frames))...)
}

// textFrame is a text frame of UTF-16 with a byte order mark, which every
// version of the tag and every player reads.
func textFrame(id, text string) []byte {
	if text == "" {
		return nil
	}
	payload := []byte{0x01, 0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(text)) {
		payload = append(payload, byte(u), byte(u>>8))
	}
	return frame(id, payload)
}

// pictureFrame is the album's cover, as the front cover of the file.
func pictureFrame(m Meta) []byte {
	if len(m.Cover) == 0 {
		return nil
	}
	mime := m.MIME
	if mime == "" {
		mime = "image/jpeg"
	}
	payload := []byte{0x00} // the text encoding of the tag's text: Latin-1
	payload = append(payload, mime...)
	payload = append(payload, 0x00)
	payload = append(payload, 0x03) // the picture of the front cover
	payload = append(payload, 0x00) // no description
	payload = append(payload, m.Cover...)
	return frame("APIC", payload)
}

// lyricsFrame is the song's words, as the tag keeps them: one frame of the
// unsynchronised lyrics, in UTF-16, under no description, which players
// show as the song plays.
func lyricsFrame(m Meta) []byte {
	if strings.TrimSpace(m.Lyrics) == "" {
		return nil
	}
	payload := []byte{0x01}                  // the text is UTF-16
	payload = append(payload, 'u', 'n', 'd') // the language: undetermined
	payload = append(payload, 0xFF, 0xFE)    // the descriptor's byte order mark
	payload = append(payload, 0x00, 0x00)    // no description
	payload = append(payload, 0xFF, 0xFE)    // the lyrics' byte order mark
	for _, u := range utf16.Encode([]rune(m.Lyrics)) {
		payload = append(payload, byte(u), byte(u>>8))
	}
	return frame("USLT", payload)
}

// frame returns an ID3v2.3 frame: its id, its size, no flags, and what it
// holds.
func frame(id string, payload []byte) []byte {
	b := make([]byte, 10, 10+len(payload))
	copy(b, id)
	binary.BigEndian.PutUint32(b[4:8], uint32(len(payload)))
	return append(b, payload...)
}

// synchsafe reads a size as ID3v2 keeps it: seven bits a byte.
func synchsafe(b []byte) int {
	n := 0
	for _, c := range b {
		n = n<<7 | int(c&0x7f)
	}
	return n
}

// putSynchsafe writes n as ID3v2 keeps a size.
func putSynchsafe(b []byte, n int) {
	b[0] = byte(n >> 21 & 0x7f)
	b[1] = byte(n >> 14 & 0x7f)
	b[2] = byte(n >> 7 & 0x7f)
	b[3] = byte(n & 0x7f)
}

// writeFLAC writes the file's Vorbis comment and its cover, keeping its
// stream information and the rest of its metadata as they were.
func writeFLAC(path string, m Meta) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	magic := make([]byte, 4)
	if _, err := io.ReadFull(src, magic); err != nil || string(magic) != "fLaC" {
		return errors.New("tag: not a FLAC file")
	}
	var chunks []chunk
	header := make([]byte, 4)
	for {
		if _, err := io.ReadFull(src, header); err != nil {
			return fmt.Errorf("tag: reading the FLAC metadata: %w", err)
		}
		last := header[0]&0x80 != 0
		size := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
		data := make([]byte, size)
		if _, err := io.ReadFull(src, data); err != nil {
			return fmt.Errorf("tag: reading the FLAC metadata: %w", err)
		}
		chunks = append(chunks, chunk{typ: header[0] & 0x7f, data: data})
		if last {
			break
		}
	}
	// The tags this writes take the place of those the file had.
	var kept []chunk
	for _, ch := range chunks {
		if ch.typ != 4 && ch.typ != 6 {
			kept = append(kept, ch)
		}
	}
	kept = append(kept, chunk{typ: 4, data: vorbisComment(m)})
	if pic := pictureBlock(m); pic != nil {
		kept = append(kept, chunk{typ: 6, data: pic})
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tag-*"+filepath.Ext(path))
	if err != nil {
		return err
	}
	return replaceFile(path, tmp, func(w io.Writer) error {
		// The file is closed before it is replaced: Windows will not
		// replace one that is open.
		defer src.Close()
		if _, err := w.Write([]byte("fLaC")); err != nil {
			return err
		}
		for i, ch := range kept {
			head := []byte{ch.typ, byte(len(ch.data) >> 16), byte(len(ch.data) >> 8), byte(len(ch.data))}
			if i == len(kept)-1 {
				head[0] |= 0x80 // the last block of the metadata
			}
			if _, err := w.Write(head); err != nil {
				return err
			}
			if _, err := w.Write(ch.data); err != nil {
				return err
			}
		}
		// What follows the metadata is the audio itself, unchanged.
		_, err := io.Copy(w, src)
		return err
	})
}

// vorbisComment returns the tags of m as FLAC keeps them, in UTF-8.
func vorbisComment(m Meta) []byte {
	var body bytes.Buffer
	count := uint32(0)
	for _, f := range []struct{ key, value string }{
		{"TITLE", m.Title},
		{"ARTIST", m.Artist},
		{"ALBUM", m.Album},
		{"ALBUMARTIST", m.AlbumArtist},
		{"TRACKNUMBER", m.Track},
		{"DISCNUMBER", m.Disc},
		{"DATE", m.Year},
		// The words, timed, under the name players look for; the value may
		// run over several lines, which the length prefix allows.
		{"LYRICS", m.Lyrics},
	} {
		if f.value == "" {
			continue
		}
		s := f.key + "=" + f.value
		var n [4]byte
		binary.LittleEndian.PutUint32(n[:], uint32(len(s)))
		body.Write(n[:])
		body.WriteString(s)
		count++
	}
	var out bytes.Buffer
	vendor := []byte("mygo-kgou")
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(len(vendor)))
	out.Write(n[:])
	out.Write(vendor)
	binary.LittleEndian.PutUint32(n[:], count)
	out.Write(n[:])
	out.Write(body.Bytes())
	return out.Bytes()
}

// pictureBlock returns the cover as a FLAC picture block, or nil when
// there is none.
func pictureBlock(m Meta) []byte {
	if len(m.Cover) == 0 {
		return nil
	}
	mime := m.MIME
	if mime == "" {
		mime = "image/jpeg"
	}
	width, height, depth := imageSize(m.Cover)
	var b bytes.Buffer
	var n [4]byte
	put := func(v uint32) {
		binary.BigEndian.PutUint32(n[:], v)
		b.Write(n[:])
	}
	str := func(s string) {
		put(uint32(len(s)))
		b.WriteString(s)
	}
	put(3) // the picture of the front cover
	str(mime)
	str("") // no description
	put(uint32(width))
	put(uint32(height))
	put(uint32(depth))
	put(0) // no palette: the picture is not indexed
	put(uint32(len(m.Cover)))
	b.Write(m.Cover)
	return b.Bytes()
}

// imageSize returns the size of a JPEG or PNG picture, and how many bits a
// pixel takes; zeros where it cannot tell.
func imageSize(b []byte) (width, height, depth int) {
	switch {
	case len(b) > 24 && bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		width = int(binary.BigEndian.Uint32(b[16:20]))
		height = int(binary.BigEndian.Uint32(b[20:24]))
		if len(b) > 25 {
			bits, color := int(b[24]), b[25]
			channels := map[byte]int{0: 1, 2: 3, 3: 1, 4: 2, 6: 4}[color]
			depth = bits * channels
		}
	case len(b) > 4 && b[0] == 0xFF && b[1] == 0xD8:
		// The frames that start with a size, up to the one that holds the
		// size of the picture.
		for i := 2; i+9 < len(b); {
			if b[i] != 0xFF {
				i++
				continue
			}
			marker := b[i+1]
			if marker == 0xD8 || marker == 0x01 || marker >= 0xD0 && marker <= 0xD7 {
				i += 2
				continue
			}
			size := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
			if size < 2 {
				break
			}
			if sof := marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC; sof {
				height = int(binary.BigEndian.Uint16(b[i+5 : i+7]))
				width = int(binary.BigEndian.Uint16(b[i+7 : i+9]))
				depth = 3 * int(b[i+4])
				return
			}
			i += 2 + size
		}
	}
	if depth == 0 {
		depth = 24
	}
	return
}
