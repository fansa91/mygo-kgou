package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// liveSearchApp stubs searching with what each query is said to find, so a
// search's page lands here rather than on KuGou's.
func liveSearchApp(t *testing.T, found map[string][]string) (*app, chan searchRequest) {
	t.Helper()
	asked := make(chan searchRequest, 16)
	searchResults = func(q string, kind netease.SearchType, _, offset int) (netease.SearchResult, error) {
		asked <- searchRequest{q, kind, offset}
		var res netease.SearchResult
		for i, name := range found[q] {
			res.Songs = append(res.Songs, netease.Track{ID: int64(100 + i), Name: name, Duration: 200000})
		}
		res.Total = len(res.Songs)
		return res, nil
	}
	t.Cleanup(func() { searchResults = netease.SearchPage })
	a := newTestApp()
	a.settings = defaultSettings()
	return a, asked
}

// Typing in the page's own field searches once typing pauses, and what it
// found replaces what the page showed rather than adding to it.
func TestSearchPageSearchesAsYouType(t *testing.T) {
	a, asked := liveSearchApp(t, map[string][]string{
		"晴天":     {"晴天", "晴天 (Live)"},
		"晴天 周杰伦": {"晴天"},
	})
	a.open(route{page: pageSearch, query: "晴天", kind: netease.SearchSongs})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if got := wait(t, asked, "the search"); got.query != "晴天" {
		t.Fatalf("searched %+v", got)
	}
	// The page's field starts on the query it was opened with.
	if a.live.draft != "晴天" || a.live.query != "晴天" {
		t.Fatalf("field %q, page %q", a.live.draft, a.live.query)
	}
	// What came back lands by hand: the tester has no window to post to.
	d := a.searches[a.route]
	a.liveSearched(d, d.gen, netease.SearchResult{Songs: d.res.Songs, Total: 2}, nil)
	d.res.Songs = []netease.Track{{ID: 1, Name: "晴天"}, {ID: 2, Name: "晴天 (Live)"}}
	d.page.loaded = true
	tt.Frame()
	if !tt.HasText("晴天 (Live)") {
		t.Fatal("the page does not list what the search found")
	}

	// Typed, but not yet paused on: nothing is asked for, and the page says
	// it is searching.
	a.live.draft = "晴天 周杰伦"
	a.liveTyped()
	tt.Frame()
	select {
	case got := <-asked:
		t.Fatalf("searched %+v before typing paused", got)
	default:
	}
	if !a.live.pending {
		t.Error("the page does not say it is searching")
	}

	a.liveFire("晴天 周杰伦")
	if got := wait(t, asked, "the typed search"); got.query != "晴天 周杰伦" {
		t.Fatalf("searched %+v", got)
	}
	if a.route.query != "晴天 周杰伦" || a.searched.query != "晴天 周杰伦" {
		t.Errorf("on %+v, sidebar back to %+v", a.route, a.searched)
	}
	// The old results stay until the new ones come.
	tt.Frame()
	if !tt.HasText("晴天 (Live)") {
		t.Error("the results went before the new ones came")
	}
	// And the new ones replace them, rather than joining them.
	d = a.searches[a.route]
	a.liveSearched(d, d.gen, netease.SearchResult{Songs: []netease.Track{{ID: 9, Name: "晴天"}}, Total: 1}, nil)
	tt.Frame()
	if d.res.Len() != 1 {
		t.Errorf("the page holds %d songs", d.res.Len())
	}
	if tt.HasText("晴天 (Live)") {
		t.Error("a song of the last query is still listed")
	}
}

// What is typed but not yet searched for is dropped when the page is left.
func TestTypingIsDroppedWhenThePageIsLeft(t *testing.T) {
	a, _ := liveSearchApp(t, map[string][]string{})
	a.open(route{page: pageSearch, query: "晴天", kind: netease.SearchSongs})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	a.live.draft = "稻香"
	a.liveTyped()
	// Off to another page, and back to the search.
	a.show(route{page: pageHome})
	if a.live.pending {
		t.Error("the page still says it is searching")
	}
	a.liveFire("稻香")
	if a.route.page != pageHome {
		t.Error("typing searched after the page was left")
	}
	a.show(route{page: pageSearch, query: "晴天", kind: netease.SearchSongs})
	if a.live.draft != "晴天" {
		t.Errorf("the field holds %q", a.live.draft)
	}
}

// A query typed letter by letter is kept in the history once, when it stays
// put, not once per letter.
func TestTypedSearchesAreKeptOnceSettled(t *testing.T) {
	a, _ := liveSearchApp(t, map[string][]string{
		"周": {"周一"}, "周杰": {"周杰"}, "周杰伦": {"周杰伦"},
	})
	a.open(route{page: pageSearch, query: "晴天", kind: netease.SearchSongs})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	for _, q := range []string{"周", "周杰", "周杰伦"} {
		a.live.draft = q
		a.liveTyped()
		a.liveFire(q)
	}
	if slices.Contains(a.settings.SearchHistory, "周杰伦") {
		t.Error("a query was kept before it stayed put")
	}
	// The pause after the last of them keeps that one alone.
	a.keepSearched(a.route)
	if !slices.Contains(a.settings.SearchHistory, "周杰伦") {
		t.Fatalf("history %q", a.settings.SearchHistory)
	}
	for _, gone := range []string{"周", "周杰"} {
		if slices.Contains(a.settings.SearchHistory, gone) {
			t.Errorf("%q was kept", gone)
		}
	}
	// Enter does not wait for a pause to keep what it searched.
	a.live.draft = "晴天"
	a.liveSubmit()
	if len(a.settings.SearchHistory) == 0 || a.settings.SearchHistory[0] != "晴天" {
		t.Errorf("history %q", a.settings.SearchHistory)
	}
}

// A page of the query before this one, arriving after it was replaced, is
// dropped rather than put over the newer results.
func TestStaleResultNeverReplacesTheNewer(t *testing.T) {
	a, _ := liveSearchApp(t, map[string][]string{})
	a.open(route{page: pageSearch, query: "晴天", kind: netease.SearchSongs})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	old := a.searches[a.route]
	oldGen := old.gen

	a.live.draft = "稻香"
	a.liveTyped()
	a.liveFire("稻香")
	d := a.searches[a.route]
	a.liveSearched(d, d.gen, netease.SearchResult{Songs: []netease.Track{{ID: 1, Name: "稻香"}}, Total: 1}, nil)
	// The first query's page comes in late.
	a.liveSearched(old, oldGen, netease.SearchResult{Songs: []netease.Track{{ID: 2, Name: "晴天"}}, Total: 1}, nil)

	if d.res.Len() != 1 || d.res.Songs[0].Name != "稻香" {
		t.Fatalf("the page holds %+v", d.res.Songs)
	}
	tt.Frame()
	if tt.HasText("晴天") {
		t.Error("a page of the last query was put over the new results")
	}
}

// A live search that fails does not leave the last query's songs under the
// name of the one that failed.
func TestFailedLiveSearchDropsTheLastQuery(t *testing.T) {
	a, _ := liveSearchApp(t, map[string][]string{})
	a.open(route{page: pageSearch, query: "晴天", kind: netease.SearchSongs})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	d := a.searches[a.route]
	a.liveSearched(d, d.gen, netease.SearchResult{Songs: []netease.Track{{ID: 1, Name: "晴天"}}, Total: 1}, nil)
	tt.Frame()
	if !tt.HasText("晴天") {
		t.Fatal("the first search found nothing")
	}

	a.live.draft = "稻香"
	a.liveTyped()
	a.liveFire("稻香")
	d = a.searches[a.route]
	a.liveSearched(d, d.gen, netease.SearchResult{}, errors.New("offline"))
	tt.Frame()
	if tt.HasText("晴天") {
		t.Error("the song of the last query is still under this one")
	}
	if !tt.HasText("搜索失败，请稍后再试") {
		t.Error("the page does not say why it is empty")
	}
}
