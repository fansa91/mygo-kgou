package player

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/tag"
)

// copyFixture puts the audio of a fixture where a library holds it.
func copyFixture(t *testing.T, src, dst string) string {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// writeWAV writes a second of a tone, as a file of the listener's own.
func writeWAV(t *testing.T, path string) {
	t.Helper()
	const rate, seconds = 44100, 1
	data := make([]byte, rate*seconds*4)
	for i := range rate * seconds {
		v := int16(math.Sin(2*math.Pi*440*float64(i)/rate) * 8000)
		binary.LittleEndian.PutUint16(data[i*4:], uint16(v))
		binary.LittleEndian.PutUint16(data[i*4+2:], uint16(v))
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(2)) // stereo
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*4))
	binary.Write(&b, binary.LittleEndian, uint16(4))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A song of the local library plays from its file: MP3 and FLAC through the
// streams of the service's songs, and WAV — which they do not read — through
// the file's own decoder.
func TestOpenFileDecodes(t *testing.T) {
	for _, tc := range []struct{ name, fixture string }{
		{"a.mp3", "testdata/seek.mp3"},
		{"a.flac", "testdata/seek.flac"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := copyFixture(t, tc.fixture, filepath.Join(t.TempDir(), tc.name))
			s, err := openFile(netease.Track{Path: path, Duration: 5040})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			buf := make([]float32, 2048)
			n, err := s.Read(buf)
			if n == 0 || err != nil {
				t.Fatalf("read %d samples, %v", n, err)
			}
			// The fixtures open with a moment of silence of their own.
			if !carriesSound(s) {
				t.Error("what decoded is silent")
			}
			// A seek lands where it says, and the song plays on from there.
			if err := s.SetPosition(int64(s.SampleRate())); err != nil {
				t.Fatal(err)
			}
			if n, err = s.Read(buf); n == 0 || err != nil {
				t.Fatalf("after a seek read %d samples, %v", n, err)
			}
			if _, spec := s.source(); spec == nil {
				t.Error("no spectrum to draw")
			}
		})
	}
}

func TestOpenFileDecodesWAV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.wav")
	writeWAV(t, path)
	s, err := openFile(netease.Track{Path: path, Duration: 1000})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.SampleRate() != 44100 || s.Channels() != 2 {
		t.Errorf("the file plays at %d Hz in %d channels", s.SampleRate(), s.Channels())
	}
	buf := make([]float32, 4096)
	n, err := s.Read(buf)
	if n == 0 || err != nil {
		t.Fatalf("read %d samples, %v", n, err)
	}
	if !carriesSound(s) {
		t.Error("what decoded is silent")
	}
}

// The words and the picture of a song of the library come from its file,
// where the service's come from KuGou.
func TestLocalWordsAndPicture(t *testing.T) {
	path := copyFixture(t, "testdata/seek.mp3", filepath.Join(t.TempDir(), "a.mp3"))
	pic := []byte("\x89PNG\r\n\x1a\n0123456789abcdef")
	err := tag.Write(path, tag.Meta{
		Title: "晴天", Lyrics: "[00:00.00]故事的小黄花",
		Cover: pic, MIME: "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	tr := netease.Track{Path: path, ID: -1}
	cover, err := trackCover(tr)
	if err != nil || !bytes.Equal(cover, pic) {
		t.Errorf("cover = %d bytes, %v", len(cover), err)
	}
	lrc, _, _, _, err := trackLyrics(tr)
	if err != nil || !bytes.Contains([]byte(lrc), []byte("故事的小黄花")) {
		t.Errorf("lyrics = %q, %v", lrc, err)
	}
}

// A song of the local library that has gone from the disk says why it does
// not play.
func TestOpenFileMissing(t *testing.T) {
	_, err := openFile(netease.Track{Path: filepath.Join(t.TempDir(), "gone.mp3")})
	if err == nil {
		t.Fatal("a file that is gone opened")
	}
}

// carriesSound reports whether the stream brings sound within its first
// second.
func carriesSound(s stream) bool {
	buf := make([]float32, 2048)
	for range 20 {
		n, err := s.Read(buf)
		for _, v := range buf[:n] {
			if v > 0.001 || v < -0.001 {
				return true
			}
		}
		if err != nil {
			return false
		}
	}
	return false
}
