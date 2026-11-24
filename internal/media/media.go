// Package media shows what the app plays on a phone where the system shows
// media: the lock screen, the notification shade and Control Center, and
// reports their commands — play, pause, next, previous and seek — so the
// headphones' buttons, a car's controls and the lock screen drive the player.
//
// The system's code is beside this package, as mygo-plugin.json names it:
// Swift on iOS, which is MPRemoteCommandCenter and MPNowPlayingInfoCenter, and
// Kotlin on Android, which is a MediaSession and its notification. A desktop
// shows what it plays with mygo.NowPlaying, and reports false from Available.
package media

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// plugin is the id the native code is registered under, as mygo-plugin.json
// says.
const plugin = "media"

// Available reports whether the app has the plugin's native code: on iOS and
// Android, in a build that imports this package.
func Available() bool { return available() }

// call and available are what the package has of the plugin; tests replace
// them.
var (
	call = func(ctx context.Context, method string, args any) error {
		return mygo.Native(plugin).Call(ctx, method, args, nil)
	}
	available = func() bool { return mygo.Native(plugin).Available() }
)

// Command is what a system's media controls ask the app to do.
type Command string

const (
	// Play and Pause play and pause what the app plays.
	Play  Command = "play"
	Pause Command = "pause"
	// Toggle pauses when playing, and plays when not: it is what a
	// headphone's single press sends.
	Toggle Command = "toggle"
	// Next and Prev change the track.
	Next Command = "next"
	Prev Command = "prev"
	// Stop ends playback.
	Stop Command = "stop"
	// Seek moves to the position the command comes with.
	Seek Command = "seek"
)

// Info is what the system shows of the track that plays, and what its
// controls may do with it.
type Info struct {
	Title, Artist, Album string
	// Artwork is the cover, as PNG or JPEG. Empty keeps what the system
	// shows, until another track with none clears it.
	Artwork []byte
	// Duration is how long the track is; 0 leaves its progress out.
	Duration time.Duration
	// Position is where playback is as the info is set.
	Position time.Duration
	// Playing is whether the track plays: the system shows the pause
	// button, and moves the position on itself.
	Playing bool
	// CanNext, CanPrev and CanSeek are whether the controls offer those:
	// the commands the app does something with.
	CanNext, CanPrev, CanSeek bool
}

// Set replaces what the system shows, as a track starts, its cover loads, or
// its length is known.
func Set(info Info) {
	held.mu.Lock()
	held.info = info
	held.mu.Unlock()
	enqueue("set")
}

// SetPlayback says whether the track plays, and where it is, after the app
// played, paused or moved it.
func SetPlayback(playing bool, position time.Duration) {
	held.mu.Lock()
	held.info.Playing, held.info.Position = playing, position
	held.mu.Unlock()
	enqueue("set")
}

// Clear takes the app out of the system's media controls: nothing shows, and
// the headphones' buttons go to other apps.
func Clear() {
	held.mu.Lock()
	held.info = Info{}
	held.sent, held.sentArt = false, 0
	held.mu.Unlock()
	enqueue("clear")
}

// payload is what the native code takes: the info, and the cover as base64
// when it changed.
type payload struct {
	Title      string  `json:"title"`
	Artist     string  `json:"artist"`
	Album      string  `json:"album"`
	Artwork    *string `json:"artwork,omitempty"`
	DurationMs int64   `json:"durationMs"`
	PositionMs int64   `json:"positionMs"`
	Playing    bool    `json:"playing"`
	CanNext    bool    `json:"canNext"`
	CanPrev    bool    `json:"canPrev"`
	CanSeek    bool    `json:"canSeek"`
}

// held is what the app last said, and what of it the system was told.
type holder struct {
	mu   sync.Mutex
	info Info
	// sent says the system was told something, and sentArt is the hash of
	// the cover it was told: the same cover is not sent again.
	sent    bool
	sentArt uint64
}

var held holder

// snapshot takes the info as what the native code shows, with the cover when
// it is not the one the system has.
func (h *holder) snapshot() (payload, uint64) {
	h.mu.Lock()
	info := h.info
	h.mu.Unlock()
	p := payload{
		Title:      info.Title,
		Artist:     info.Artist,
		Album:      info.Album,
		DurationMs: info.Duration.Milliseconds(),
		PositionMs: info.Position.Milliseconds(),
		Playing:    info.Playing,
		CanNext:    info.CanNext,
		CanPrev:    info.CanPrev,
		CanSeek:    info.CanSeek,
	}
	hash := artHash(info.Artwork)
	h.mu.Lock()
	changed := !h.sent || hash != h.sentArt
	h.mu.Unlock()
	if changed {
		art := base64.StdEncoding.EncodeToString(info.Artwork)
		p.Artwork = &art
	}
	return p, hash
}

// markSent notes the cover the system now shows.
func (h *holder) markSent(hash uint64) {
	h.mu.Lock()
	h.sent, h.sentArt = true, hash
	h.mu.Unlock()
}

func artHash(b []byte) uint64 {
	if len(b) == 0 {
		return 0
	}
	sum := sha256.Sum256(b)
	var h uint64
	for i := 0; i < 8; i++ {
		h = h<<8 | uint64(sum[i])
	}
	return h
}

// sender sends the latest of what the app said, one call at a time: the
// native code holds the system's state, so an older set is worth nothing
// once a newer one comes.
type sender struct {
	mu      sync.Mutex
	method  string
	running bool
}

var queue sender

func enqueue(method string) {
	if !available() {
		return
	}
	queue.mu.Lock()
	queue.method = method
	if !queue.running {
		queue.running = true
		go queue.loop()
	}
	queue.mu.Unlock()
}

func (q *sender) loop() {
	for {
		q.mu.Lock()
		method := q.method
		q.method = ""
		if method == "" {
			q.running = false
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()
		var args any
		var hash uint64
		if method == "set" {
			var p payload
			p, hash = held.snapshot()
			args = p
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := call(ctx, method, args)
		cancel()
		if err == nil && method == "set" {
			held.markSent(hash)
		}
	}
}

// listener is one who hears the system's controls, so that it can stop.
type listener struct{ fn func(Command, time.Duration) }

// commands holds who hears the system's controls; the plugin is asked for
// them once.
var commands struct {
	mu    sync.Mutex
	fns   []*listener
	wired bool
}

// OnCommand calls fn on the main thread with each command of the system's
// media controls, and where a seek goes. The app plays, pauses or moves as it
// asks, and says so with SetPlayback.
func OnCommand(fn func(Command, time.Duration)) (off func()) {
	l := &listener{fn}
	commands.mu.Lock()
	commands.fns = append(commands.fns, l)
	first := !commands.wired
	commands.wired = true
	commands.mu.Unlock()
	if first {
		mygo.Native(plugin).On("command", heard)
	}
	return func() {
		commands.mu.Lock()
		defer commands.mu.Unlock()
		for i, x := range commands.fns {
			if x == l {
				commands.fns = append(commands.fns[:i:i], commands.fns[i+1:]...)
				return
			}
		}
	}
}

// event is what the native code sends for a command.
type event struct {
	Command    string `json:"command"`
	PositionMs int64  `json:"positionMs"`
}

func heard(data []byte) {
	var e event
	if err := json.Unmarshal(data, &e); err != nil || e.Command == "" {
		return
	}
	commands.mu.Lock()
	fns := append([]*listener(nil), commands.fns...)
	commands.mu.Unlock()
	pos := time.Duration(e.PositionMs) * time.Millisecond
	for _, l := range fns {
		l.fn(Command(e.Command), pos)
	}
}
