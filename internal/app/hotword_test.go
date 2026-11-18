package app

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Empty, the search field lists the day's searches, and a click on one
// searches for it.
func TestHotWordsSearchFromTheField(t *testing.T) {
	a, asked := liveSearchApp(t, map[string][]string{"富士山下": {"富士山下"}})
	a.hot = []string{"晴天", "富士山下", "春泥"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.Click("搜索"); err != nil {
		t.Fatalf("the search field cannot be focused: %v", err)
	}
	tt.Frame()
	if _, ok := tt.Find("晴天"); !ok {
		t.Fatal("the empty field's popup does not list the day's searches")
	}
	if _, ok := tt.Find("热搜"); !ok {
		t.Error("the day's searches are not told apart from the searches before")
	}
	if err := tt.Click("富士山下"); err != nil {
		t.Fatalf("the day's searches cannot be chosen: %v", err)
	}
	tt.Frame()

	if got := wait(t, asked, "the search"); got.query != "富士山下" {
		t.Errorf("searched %+v", got)
	}
	if a.route.page != pageSearch || a.route.query != "富士山下" {
		t.Errorf("on %+v, want the search for 富士山下", a.route)
	}
	if a.query != "富士山下" {
		t.Errorf("the field holds %q", a.query)
	}
}

// The day's searches are asked for once, and what fails or comes back
// empty leaves the popup to the history.
func TestHotWordsAreAskedForOnce(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	var asked int32
	hotWords = func() ([]string, error) {
		atomic.AddInt32(&asked, 1)
		return []string{"晴天"}, nil
	}
	t.Cleanup(func() { hotWords = netease.HotWords })

	a.loadHotWords()
	a.loadHotWords()
	for i := 0; i < 200 && atomic.LoadInt32(&asked) == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if got := atomic.LoadInt32(&asked); got != 1 {
		t.Fatalf("asked %d times", got)
	}

	a.hotWordsLoaded(nil, errors.New("offline"))
	a.hotWordsLoaded(nil, nil)
	if len(a.hot) != 0 {
		t.Errorf("kept %q from a failed answer", a.hot)
	}
	a.hotWordsLoaded([]string{"晴天", "春泥"}, nil)
	if len(a.hot) != 2 {
		t.Errorf("kept %q", a.hot)
	}
}
