package media

import (
	"context"
	"encoding/base64"
	"testing"
	"time"
)

type sent struct {
	method string
	args   any
}

// stub takes what the package would tell the native code.
func stub(t *testing.T) <-chan sent {
	t.Helper()
	ch := make(chan sent, 16)
	oldCall, oldAvailable := call, available
	call = func(ctx context.Context, method string, args any) error {
		ch <- sent{method, args}
		return nil
	}
	available = func() bool { return true }
	held.mu.Lock()
	held.info, held.sent, held.sentArt = Info{}, false, 0
	held.mu.Unlock()
	t.Cleanup(func() {
		call, available = oldCall, oldAvailable
		held.mu.Lock()
		held.info, held.sent, held.sentArt = Info{}, false, 0
		held.mu.Unlock()
	})
	return ch
}

func wait(t *testing.T, ch <-chan sent) sent {
	t.Helper()
	select {
	case c := <-ch:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("the native code was not told")
		return sent{}
	}
}

// The track shows with its cover, and what the controls may do with it.
func TestSetShowsTheTrack(t *testing.T) {
	ch := stub(t)
	cover := []byte{0x89, 'P', 'N', 'G'}
	Set(Info{
		Title: "晴天", Artist: "周杰伦", Album: "叶惠美",
		Artwork: cover, Duration: 4*time.Minute + 29*time.Second, Position: 3 * time.Second,
		Playing: true, CanNext: true, CanPrev: true, CanSeek: true,
	})
	c := wait(t, ch)
	if c.method != "set" {
		t.Fatalf("the app sent %q, not set", c.method)
	}
	p, ok := c.args.(payload)
	if !ok {
		t.Fatalf("set went as %T", c.args)
	}
	if p.Title != "晴天" || p.Artist != "周杰伦" || p.Album != "叶惠美" {
		t.Errorf("the track shows as %q / %q / %q", p.Title, p.Artist, p.Album)
	}
	if p.DurationMs != 269000 || p.PositionMs != 3000 {
		t.Errorf("the track shows %d ms long, at %d ms", p.DurationMs, p.PositionMs)
	}
	if !p.Playing || !p.CanNext || !p.CanPrev || !p.CanSeek {
		t.Errorf("the controls are offered as %+v", p)
	}
	if p.Artwork == nil {
		t.Fatal("the cover was not sent")
	}
	if got, err := base64.StdEncoding.DecodeString(*p.Artwork); err != nil || string(got) != string(cover) {
		t.Errorf("the cover went as %q (%v)", *p.Artwork, err)
	}
}

// A cover is sent once: the system keeps it while the same track plays, and
// a paused track sends where it is without it.
func TestSetPlaybackKeepsTheCover(t *testing.T) {
	ch := stub(t)
	cover := []byte("cover")
	Set(Info{Title: "晴天", Artwork: cover, Duration: time.Minute, Playing: true})
	first := wait(t, ch).args.(payload)
	if first.Artwork == nil {
		t.Fatal("the first set left the cover out")
	}
	SetPlayback(false, 5*time.Second)
	p := wait(t, ch).args.(payload)
	if p.Artwork != nil {
		t.Error("the same cover was sent again")
	}
	if p.Playing || p.PositionMs != 5000 || p.Title != "晴天" || p.DurationMs != 60000 {
		t.Errorf("the paused track shows as %+v", p)
	}
}

// Another cover is sent, and a track with none clears the one before.
func TestSetSendsAnotherCover(t *testing.T) {
	ch := stub(t)
	Set(Info{Title: "晴天", Artwork: []byte("one")})
	wait(t, ch)
	Set(Info{Title: "七里香", Artwork: []byte("two")})
	p := wait(t, ch).args.(payload)
	if p.Artwork == nil {
		t.Fatal("another cover was left out")
	}
	if got, _ := base64.StdEncoding.DecodeString(*p.Artwork); string(got) != "two" {
		t.Errorf("another cover went as %q", *p.Artwork)
	}
	Set(Info{Title: "以父之名"})
	p = wait(t, ch).args.(payload)
	if p.Artwork == nil || *p.Artwork != "" {
		t.Errorf("a track without a cover kept the last one: %v", p.Artwork)
	}
}

// Clear takes the app out of the system's controls.
func TestClearTakesTheAppOut(t *testing.T) {
	ch := stub(t)
	Set(Info{Title: "晴天", Artwork: []byte("cover")})
	wait(t, ch)
	Clear()
	if c := wait(t, ch); c.method != "clear" {
		t.Errorf("the app sent %q, not clear", c.method)
	}
	// What comes next is a track new to the system, cover and all.
	Set(Info{Title: "晴天", Artwork: []byte("cover")})
	p := wait(t, ch).args.(payload)
	if p.Artwork == nil {
		t.Error("the cover of a track cleared before was left out")
	}
}

// The system's controls reach the app, with where a seek goes.
func TestCommandsReachTheApp(t *testing.T) {
	type signal struct {
		cmd Command
		pos time.Duration
	}
	got := make(chan signal, 4)
	off := OnCommand(func(cmd Command, pos time.Duration) { got <- signal{cmd, pos} })
	defer off()

	heard([]byte(`{"command":"seek","positionMs":42000}`))
	select {
	case h := <-got:
		if h.cmd != Seek || h.pos != 42*time.Second {
			t.Errorf("the app heard %q at %v", h.cmd, h.pos)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the app did not hear the seek")
	}

	heard([]byte(`{"command":"toggle"}`))
	select {
	case h := <-got:
		if h.cmd != Toggle {
			t.Errorf("the app heard %q, not a toggle", h.cmd)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the app did not hear the toggle")
	}

	// What the app cannot read is dropped, not a command.
	heard([]byte(`not json`))
	select {
	case h := <-got:
		t.Errorf("the app heard %q of nothing", h.cmd)
	case <-time.After(50 * time.Millisecond):
	}

	// One that stopped hearing hears nothing more.
	off()
	heard([]byte(`{"command":"next"}`))
	select {
	case h := <-got:
		t.Errorf("a listener that stopped heard %q", h.cmd)
	case <-time.After(50 * time.Millisecond):
	}
}
