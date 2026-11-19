package player

import (
	"errors"

	// AAC in MP4, which mygo's mp4 decoder reads through it.
	_ "github.com/egoist/mygo/audio/aac"
	"github.com/egoist/mygo/audio/mp4"
)

// openM4AStream opens an M4A file: the container Apple's players and phones
// keep their music in, with the sound in AAC-LC as most of them hold it, or
// in ALAC as the lossless ones do. mygo reads both.
//
// The decoder reads the file by seeking through it, and the box that says
// where the sound is stands at the end as often as at the start, which a
// song still coming in has not reached: one is refused rather than read as
// silence.
func openM4AStream(dl *download, durationMs int64) (stream, error) {
	if !dl.whole() {
		return nil, errors.New("m4a: the file must be whole to be read")
	}
	return openSound(dl, durationMs, mp4.NewDecoder)
}
