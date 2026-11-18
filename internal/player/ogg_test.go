package player

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/faide/goxiph/audio"
	"github.com/faide/goxiph/vorbis"
	"github.com/faide/goxiph/vorbiscomment"
	"github.com/thesyncim/gopus"
	gopusogg "github.com/thesyncim/gopus/container/ogg"
)

// wave returns n frames of a sine as the channels a codec's encoder takes:
// one slice of samples each.
func wave(n, rate, channels int, freq float64) [][]float32 {
	out := make([][]float32, channels)
	for c := range out {
		out[c] = make([]float32, n)
		for i := range n {
			out[c][i] = float32(math.Sin(2*math.Pi*freq*float64(i)/float64(rate))) * 0.5
		}
	}
	return out
}

// interleaved returns channels as one stream of samples, as a decoder hands
// them back.
func interleaved(chans [][]float32) []float32 {
	n := len(chans[0])
	out := make([]float32, 0, n*len(chans))
	for i := range n {
		for _, plane := range chans {
			out = append(out, plane[i])
		}
	}
	return out
}

// vorbisFile writes a song as an Ogg Vorbis file, with the tags asked for.
func vorbisFile(t *testing.T, chans [][]float32, rate int) string {
	t.Helper()
	var tags vorbiscomment.Tags
	tags.Add("TITLE", "晴天")
	tags.Add("ARTIST", "周杰伦")
	tags.Add("ALBUM", "叶惠美")
	tags.Add("TRACKNUMBER", "3")
	tags.Add("DATE", "2003")
	enc, err := vorbis.NewEncoder(audio.Format{SampleRate: rate, Channels: len(chans)}, tags, vorbis.EncoderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := enc.Encode(&buf, chans, 0x5eed); err != nil {
		t.Fatalf("encode vorbis: %v", err)
	}
	path := filepath.Join(t.TempDir(), "song.ogg")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// opusFile writes a song as an Ogg Opus file.
func opusFile(t *testing.T, chans [][]float32, rate int) string {
	t.Helper()
	ch := len(chans)
	enc, err := gopus.NewEncoder(gopus.EncoderConfig{
		SampleRate: rate, Channels: ch, Application: gopus.ApplicationAudio,
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w, err := gopusogg.NewWriter(&buf, uint32(rate), uint8(ch))
	if err != nil {
		t.Fatal(err)
	}
	pcm := interleaved(chans)
	packet := make([]byte, 4000)
	for at := 0; at+enc.FrameSize()*ch <= len(pcm); at += enc.FrameSize() * ch {
		n, err := enc.Encode(pcm[at:at+enc.FrameSize()*ch], packet)
		if err != nil {
			t.Fatalf("encode opus: %v", err)
		}
		if err := w.WritePacket(packet[:n], enc.FrameSize()); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "song.opus")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// An Ogg Vorbis file plays — the format the 概念版 streams, and the one the
// player used to decode with another library — and a seek carries on in the
// sound.
func TestOggVorbisPlays(t *testing.T) {
	const rate = 44100
	path := vorbisFile(t, wave(rate*2, rate, 2, 440), rate)
	s := openFileStream(t, path, 2000)
	if s.SampleRate() != rate || s.Channels() != 2 {
		t.Fatalf("stream = %d Hz, %d channels", s.SampleRate(), s.Channels())
	}
	buf := make([]float32, 4096)
	n, err := s.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 || peakOf(buf[:n]) < 0.1 {
		t.Fatalf("read %d frames, peak %v", n, peakOf(buf[:n]))
	}
	if err := s.SetPosition(rate); err != nil {
		t.Fatal(err)
	}
	n, err = s.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if peak := peakOf(buf[:n]); peak < 0.1 {
		t.Errorf("a second in, peak %v", peak)
	}
}

// An Ogg Opus file — what a library of the listener's own holds — plays, and
// seeks, as its format allows.
func TestOggOpusPlays(t *testing.T) {
	const rate = 48000
	path := opusFile(t, wave(rate*2, rate, 1, 440), rate)
	s := openFileStream(t, path, 2000)
	if s.SampleRate() != rate || s.Channels() != 1 {
		t.Fatalf("stream = %d Hz, %d channels", s.SampleRate(), s.Channels())
	}
	buf := make([]float32, 4096)
	n, err := s.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 || peakOf(buf[:n]) < 0.05 {
		t.Fatalf("read %d frames, peak %v", n, peakOf(buf[:n]))
	}
	// The song goes on where it is sent, and carries on from there.
	if err := s.SetPosition(rate); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		n, err = s.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Fatal("nothing decoded after the seek")
		}
		if peak := peakOf(buf[:n]); peak < 0.05 {
			t.Errorf("after the seek, peak %v", peak)
		}
	}
}

// The headers of an Ogg file say which codec it holds, and a file of one the
// app does not play is refused rather than played as noise.
func TestOggCodecs(t *testing.T) {
	if n, err := oggHeaderCount([]byte("OpusHead\x01")); err != nil || n != 2 {
		t.Errorf("Opus: %d headers, %v", n, err)
	}
	if n, err := oggHeaderCount([]byte("\x01vorbis")); err != nil || n != 3 {
		t.Errorf("Vorbis: %d headers, %v", n, err)
	}
	if _, err := oggHeaderCount([]byte("\x7fFLAC")); err == nil {
		t.Error("a codec the app does not play was taken")
	}
}
