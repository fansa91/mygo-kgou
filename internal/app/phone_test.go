package app

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// phoneApp is an app in the phone shell, at a phone's size, without the
// window's desktop bars: the layout can be tested on any development
// machine.
func phoneApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp()
	a.phone = true
	a.pageStates = map[route]pageState{}
	a.searches = map[route]*searchData{}
	a.route = route{page: pageHome}
	return a
}

// The phone keeps the four destinations in its bottom bar and the title
// of the page in its top bar, where the sidebar used to be.
func TestPhoneShellShowsTabsAndTitle(t *testing.T) {
	a := phoneApp(t)
	tt := ui.NewTester(a.view, 390, 844)
	tt.Frame()
	for _, label := range []string{"首页", "发现", "乐库", "我的"} {
		if !tt.HasText(label) {
			t.Errorf("the bottom bar has no %q: %v", label, tt.Texts())
		}
	}
	if !tt.HasText("为你推荐") {
		t.Errorf("the top bar has no title: %v", tt.Texts())
	}
}

// Tapping a destination opens its page, and the destination stays lit.
func TestPhoneTabsOpenPages(t *testing.T) {
	a := phoneApp(t)
	a.profile = &netease.Profile{UserID: 42, Nickname: "听歌的人"}
	tt := ui.NewTester(a.view, 390, 844)
	tt.Frame()
	if err := tt.Click("乐库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.phoneTab != 2 || a.route.page != pageLocal {
		t.Fatalf("after 乐库: tab %d, page %v", a.phoneTab, a.route.page)
	}
	if !tt.HasText("本地音乐") {
		t.Errorf("the local page's title is missing: %v", tt.Texts())
	}

	if err := tt.Click("我的"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route.page != pageMine {
		t.Fatalf("after 我的: page %v", a.route.page)
	}
	// Settings is reached from the account home, not the tab itself.
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route.page != pageSettings {
		t.Fatalf("after 设置: page %v", a.route.page)
	}
}

// Going back from a page opened within a tab restores the page before it,
// as the desktop back button does.
func TestPhoneBackPopsThePage(t *testing.T) {
	a := phoneApp(t)
	tt := ui.NewTester(a.view, 390, 844)
	tt.Frame()
	a.open(route{page: pageCharts})
	tt.Frame()
	if !tt.HasText("排行榜") {
		t.Fatalf("the charts page is missing: %v", tt.Texts())
	}
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route.page != pageHome {
		t.Fatalf("after back: page %v", a.route.page)
	}
}

// A page's list and scroll are kept per route, so a back finds the row
// it left rather than starting the list again.
func TestPhoneKeepsPageStatePerRoute(t *testing.T) {
	a := phoneApp(t)
	a.trackList.ScrollTo(3, ui.Start)
	a.pageStates[a.route] = pageState{a.trackList, a.pageScroll, a.navScroll}
	before := a.pageStates[a.route]
	a.open(route{page: pageCharts})
	tt := ui.NewTester(a.view, 390, 844)
	tt.Frame()
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.pageStates[a.route]; got.trackList.Handle != before.trackList.Handle {
		t.Errorf("back lost the list state: %+v, want %+v", got, before)
	}
}

// The phone's search tab starts with its own field, so it can be searched
// without a query from the desktop's sidebar.
func TestPhoneSearchTabHasAField(t *testing.T) {
	a := phoneApp(t)
	tt := ui.NewTester(a.view, 390, 844)
	tt.Frame()
	if err := tt.Click("发现"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route.page != pageSearch || a.searches[a.route] == nil {
		t.Fatalf("the search tab has no page: %v", a.route)
	}
	// The empty search tab is the discover page: a field to start from,
	// not an empty result list.
	if !tt.HasText("发现音乐") {
		t.Errorf("the discover page is missing: %v", tt.Texts())
	}
}

// The discover tab ends with the listener's own searches instead of the
// featured lists the home page already shows. A chip searches at a tap,
// and the bin clears the history.
func TestPhoneDiscoverShowsSearchHistory(t *testing.T) {
	searchResults = func(string, netease.SearchType, int, int) (netease.SearchResult, error) {
		return netease.SearchResult{}, errors.New("offline")
	}
	t.Cleanup(func() { searchResults = netease.SearchPage })
	hotWords = func() ([]string, error) { return nil, errors.New("offline") }
	t.Cleanup(func() { hotWords = netease.HotWords })

	a := phoneApp(t)
	a.settings = defaultSettings()
	a.settings.SearchHistory = []string{"周杰伦", "富士山下"}
	a.featured = []netease.Playlist{{ID: 7, Name: "不该出现在发现页"}}
	tt := ui.NewTester(a.view, 390, 844)
	tt.Frame()
	if err := tt.Click("发现"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"搜索历史", "周杰伦", "富士山下"} {
		if !tt.HasText(want) {
			t.Errorf("the discover page does not show %q: %v", want, tt.Texts())
		}
	}
	for _, gone := range []string{"推荐歌单", "不该出现在发现页"} {
		if tt.HasText(gone) {
			t.Errorf("the discover page still shows %q: %v", gone, tt.Texts())
		}
	}

	if err := tt.Click("清空搜索历史"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.settings.SearchHistory) != 0 || !tt.HasText("还没有搜索记录") {
		t.Errorf("clearing left %q on the page: %v", a.settings.SearchHistory, tt.Texts())
	}

	a.settings.SearchHistory = []string{"周杰伦", "富士山下"}
	tt.Frame()
	if err := tt.Click("富士山下"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route.page != pageSearch || a.route.query != "富士山下" {
		t.Fatalf("a history chip did not search: %v", a.route)
	}
}

// Frames keeps pictures of the phone shell for looking at by hand.
func TestPhoneFrames(t *testing.T) {
	if os.Getenv("NM_FRAMES") == "" {
		t.Skip("set NM_FRAMES to write frames")
	}
	a := phoneApp(t)
	dir := filepath.Join("testdata", "frames")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 390, 844)
	frames := []struct {
		name string
		open func()
	}{
		{"phone-home", func() {}},
		{"phone-search", func() { a.phoneSelectTab(1) }},
		{"phone-local", func() { a.phoneSelectTab(2) }},
		{"phone-settings", func() { a.phoneSelectTab(3) }},
	}
	for _, f := range frames {
		f.open()
		tt.Frame()
		out, err := os.Create(filepath.Join(dir, f.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(out, tt.Image()); err != nil {
			out.Close()
			t.Fatal(err)
		}
		out.Close()
	}
}
