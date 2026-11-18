package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// searchResults and searchSuggest search NetEase; tests replace them.
var (
	searchResults = netease.SearchPage
	searchSuggest = netease.SearchSuggest
)

// searchLimit is how many results load at a time.
const searchLimit = 60

// searchKinds are the kinds of search, as their tabs show them.
var searchKinds = []struct {
	kind        netease.SearchType
	label, unit string
}{
	{netease.SearchSongs, "单曲", "首单曲"},
	{netease.SearchArtists, "歌手", "位歌手"},
	{netease.SearchAlbums, "专辑", "张专辑"},
	{netease.SearchPlaylists, "歌单", "个歌单"},
	{netease.SearchUsers, "用户", "位用户"},
	{netease.SearchPodcasts, "听书", "个听书"},
}

// searchData is what a search found, as far as its pages have loaded.
type searchData struct {
	res  netease.SearchResult
	page paged
	// gen counts the searches this page has been given by its own field.
	// A page that arrives after another one was asked for is dropped.
	gen int
}

// searchLiveDelay is how long typing pauses in the search page's own field
// before it searches for what was typed, as a search per letter would ask
// KuGou for "周", "周杰", "周杰伦" one after another.
const searchLiveDelay = 300 * time.Millisecond

// searchKeepDelay is how long a query searched as it was typed stays on the
// page before it is kept in the history: pausing on the way to "周杰伦" is
// not a search of its own.
const searchKeepDelay = 2 * time.Second

// liveSearch is the field atop a search's page, which searches as it is
// typed: what it holds, what the page shows, and the pauses between them.
type liveSearch struct {
	draft   string      // what the field holds
	query   string      // what the page's results are for
	timer   *time.Timer // waiting for typing to pause
	keep    *time.Timer // waiting to keep the query in the history
	pending bool        // whether a search for draft is waiting to start
}

// reset shows q in the page's field and in its results, as opening a search
// made elsewhere does, and gives up whatever was being typed.
func (l *liveSearch) reset(q string) {
	if l.timer != nil {
		l.timer.Stop()
	}
	if l.keep != nil {
		l.keep.Stop()
	}
	*l = liveSearch{draft: q, query: q}
}

// liveTyped asks for the results of what the page's field holds, once
// typing pauses. It runs as the text is typed, so it only sets a loose
// timer and lets that search for it.
func (a *app) liveTyped() {
	l := &a.live
	if a.route.page != pageSearch {
		return
	}
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	q := strings.TrimSpace(l.draft)
	if q == "" || q == l.query {
		l.pending = false
		return
	}
	l.pending = true
	l.timer = time.AfterFunc(searchLiveDelay, func() {
		a.post(func() { a.liveFire(q) })
	})
}

// liveFire searches for q once typing has paused on it, unless the field
// has moved on or the page was left.
func (a *app) liveFire(q string) {
	if strings.TrimSpace(a.live.draft) != q || a.route.page != pageSearch {
		return
	}
	a.liveSearch(q)
}

// liveSearch gives the page over to q: the results showing stay until the
// new ones come, and the query is left out of the history until it stays
// put, so a name typed letter by letter is remembered as one search.
func (a *app) liveSearch(q string) {
	l := &a.live
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	l.pending = false
	if q == "" || q == l.query || a.route.page != pageSearch {
		return
	}
	old := a.route
	r := route{page: pageSearch, query: q, kind: old.kind}
	if r.kind == 0 {
		r.kind = netease.SearchSongs
	}
	l.query = q
	a.route, a.searched, a.query = r, r, q
	if d := a.searches[r]; d != nil && d.page.loaded {
		// Searched for before: what it found is still the answer.
		delete(a.searches, old)
		a.keepSearch(r)
		return
	}
	// The page keeps its data, so what it shows stays while the new
	// results come in, rather than a page of shimmer for each pause.
	d := a.searches[old]
	if d == nil {
		d = &searchData{}
	}
	delete(a.searches, old)
	a.searches[r] = d
	a.refreshSearch(r, d)
	a.keepSearch(r)
}

// liveSubmit searches at once for what the field holds, Enter having no
// reason to wait for typing to pause, and keeps it in the history.
func (a *app) liveSubmit() {
	q := strings.TrimSpace(a.live.draft)
	if q == "" {
		return
	}
	a.liveSearch(q)
	a.rememberSearch(q)
}

// refreshSearch loads the first page of a live search in place, replacing
// what the page shows only once the results come. A search asked for after
// this one, or a page of the last search still on its way, lands nowhere.
func (a *app) refreshSearch(r route, d *searchData) {
	d.gen++
	gen := d.gen
	d.page.loading, d.page.err = true, ""
	go func() {
		res, err := searchResults(r.query, r.kind, searchLimit, 0)
		a.post(func() { a.liveSearched(d, gen, res, err) })
	}()
}

// liveSearched takes what a live search found, unless the page has been
// given another query since.
func (a *app) liveSearched(d *searchData, gen int, res netease.SearchResult, err error) {
	if d.gen != gen {
		return
	}
	d.page.loading = false
	if err != nil {
		// The songs of the query before are not this query's: the page
		// goes to what it found, none, with the reason at its foot.
		d.res, d.page.loaded = netease.SearchResult{}, false
		d.page.err = "搜索失败，请稍后再试"
		return
	}
	d.res = res
	d.page.loaded, d.page.more = true, res.Len() > 0 && res.Len() < res.Total
}

// keepSearch remembers r's query once it has stayed on the page a while,
// which is when it was what the listener was after rather than a step on
// the way to it.
func (a *app) keepSearch(r route) {
	l := &a.live
	if l.keep != nil {
		l.keep.Stop()
	}
	l.keep = time.AfterFunc(searchKeepDelay, func() {
		a.post(func() { a.keepSearched(r) })
	})
}

// keepSearched remembers r's query if the page is still showing it.
func (a *app) keepSearched(r route) {
	if a.route == r && strings.TrimSpace(a.live.draft) == r.query {
		a.rememberSearch(r.query)
	}
}

// search searches for q, of the kind last searched for.
func (a *app) search(q string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return
	}
	kind := a.searched.kind
	if kind == 0 {
		kind = netease.SearchSongs
	}
	a.searchFor(route{page: pageSearch, query: q, kind: kind})
}

// searchFor opens the page of a search, remembering its query. Searched
// for again, it loads anew.
func (a *app) searchFor(r route) {
	a.rememberSearch(r.query)
	a.dismissSuggestions()
	if r == a.route {
		a.nowPlaying = false
		a.loadSearch(r, true)
		return
	}
	a.open(r)
}

// loadSearch loads the first page of a search, unless it has, or again.
func (a *app) loadSearch(r route, again bool) {
	if a.searches[r] != nil && !again {
		return
	}
	d := &searchData{}
	a.searches[r] = d
	a.moreResults(r, d)
}

// moreResults loads the next page of a search. A search done again has
// new data, so the last one's pages land where nothing shows them.
func (a *app) moreResults(r route, d *searchData) {
	if d.page.loading {
		return
	}
	d.page.loading, d.page.err = true, ""
	gen := d.gen
	offset := d.res.Len()
	go func() {
		res, err := searchResults(r.query, r.kind, searchLimit, offset)
		a.post(func() {
			// Typed on: this is a page of a query the page no longer shows.
			if d.gen != gen {
				return
			}
			d.page.loading = false
			if err != nil {
				d.page.err = "搜索失败，请稍后再试"
				return
			}
			d.res.Append(res)
			// A server can claim more while sending none.
			d.page.loaded, d.page.more = true, res.Len() > 0 && d.res.Len() < d.res.Total
		})
	}()
}

// searchHistoryLimit is how many searches the history keeps.
const searchHistoryLimit = 20

// rememberSearch puts q first in the history of searches, once.
func (a *app) rememberSearch(q string) {
	history := []string{q}
	for _, s := range a.settings.SearchHistory {
		if s != q && len(history) < searchHistoryLimit {
			history = append(history, s)
		}
	}
	a.settings.SearchHistory = history
	a.settings.save()
}

func (a *app) clearSearchHistory() {
	a.settings.SearchHistory = nil
	a.settings.save()
}

func (a *app) searchPage(c *ui.Context) {
	r := a.route
	d := a.searches[r]
	header := func(c *ui.Context) { a.searchHeader(c, d) }
	more := func() { a.moreResults(r, d) }
	if r.kind == netease.SearchSongs {
		if !d.page.loaded && d.page.loading {
			a.trackSkeleton(c, a.pageShimmer(c), true, header)
			return
		}
		a.trackRows(c, d.res.Songs, true, header, func(c *ui.Context) {
			a.pageFoot(c, &d.page, more)
			a.searchEmpty(c, d)
		})
		return
	}
	a.scrollPage(c, func() {
		header(c)
		res := &d.res
		switch r.kind {
		case netease.SearchArtists:
			ui.Grid(c).Columns(a.columns(c, 132)).GapX(20).GapY(28).Children(func() {
				for i := range res.Artists {
					a.artistCard(c.Key(res.Artists[i].ID), &res.Artists[i])
				}
			})
		case netease.SearchAlbums:
			a.albumGrid(c, res.Albums)
		case netease.SearchPlaylists:
			ui.Grid(c).Columns(a.columns(c, 156)).GapX(20).GapY(28).Children(func() {
				for i := range res.Playlists {
					a.foundPlaylistCard(c.Key(res.Playlists[i].ID), &res.Playlists[i])
				}
			})
		case netease.SearchUsers:
			for i := range res.Users {
				a.userRow(c.Key(res.Users[i].UserID), &res.Users[i])
			}
		case netease.SearchPodcasts:
			for i := range res.Podcasts {
				a.podcastRow(c.Key(res.Podcasts[i].ID), &res.Podcasts[i])
			}
		}
		if !d.page.loaded && d.page.loading {
			a.playlistCardsSkeleton(c, a.pageShimmer(c), a.columns(c, 156), 156, false)
			return
		}
		a.pageFoot(c, &d.page, more)
		a.searchEmpty(c, d)
	})
}

func (a *app) searchEmpty(c *ui.Context, d *searchData) {
	if d.page.loaded && !d.page.loading && d.res.Len() == 0 {
		a.empty(c, "没有找到相关结果")
	}
}

// searchHeader is what was searched for, how many it found, and the tabs
// of what it may look for instead.
func (a *app) searchHeader(c *ui.Context, d *searchData) {
	r := a.route
	sub := "搜索"
	at := 0
	for i, k := range searchKinds {
		if k.kind == r.kind {
			at = i
			switch {
			case d.page.loaded && (d.page.loading || a.live.pending):
				// The results shown are the last query's, on their way
				// out: the count they came with would be wrong.
				sub = "搜索 · 正在搜索…"
			case d.page.loaded:
				sub = fmt.Sprintf("搜索 · 找到 %d %s", d.res.Total, k.unit)
			}
		}
	}
	a.heading(c, "“"+r.query+"”", sub)
	// The page searches on its own as the field is typed, so refining a
	// query does not mean going back to the sidebar's field.
	in := ui.TextInput(c.Key("search-live"), &a.live.draft).
		Placeholder("搜索歌曲、歌手、听书").Label("搜索").FontSize(14).
		Width(400).Height(36).Margin(0, 0, 14, 0)
	if in.Changed() {
		a.liveTyped()
	}
	if in.Submitted() {
		a.liveSubmit()
	}
	ui.Row(c).Margin(0, 0, 18, 0).Children(func() {
		labels := make([]string, len(searchKinds))
		for i, k := range searchKinds {
			labels[i] = k.label
		}
		if segmented(c.Key("kind"), &at, labels...) {
			r.kind = searchKinds[at].kind
			a.show(r)
		}
	})
}

// artistCard is an artist's round photo over their name, opening their page.
func (a *app) artistCard(c *ui.Context, ar *netease.ArtistProfile) {
	p := a.pal
	card := ui.ButtonBase(c).Column().Gap(10).AlignItems(ui.Stretch).FocusRing(false).Label(ar.Name)
	hovered := card.Hovered()
	card.Children(func() {
		photo := ar.Avatar
		if photo == "" {
			photo = ar.PicURL
		}
		art := ui.Box(c).AspectRatio(1).Radius(999)
		if hovered {
			art.Shadow(0, 12, 28, -6, p.cardShade)
		} else {
			art.Shadow(0, 2, 8, -2, p.cardShade)
		}
		art.Children(func() { a.coverFill(c, photo, 400, 999) })
		ui.Text(c, ar.Name).FontSize(13).FontWeight(500).SingleLine().AlignSelf(ui.Center)
	})
	if card.Clicked() {
		a.open(route{page: pageArtist, id: ar.ID})
	}
}

// foundPlaylistCard is a playlist a search found: its card, with how many
// songs it has and who made it under its name, as strangers' playlists
// are told apart by their makers.
func (a *app) foundPlaylistCard(c *ui.Context, pl *netease.Playlist) {
	sub := fmt.Sprintf("%d 首", pl.TrackCount)
	if pl.Creator.Nickname != "" {
		sub += " · " + pl.Creator.Nickname
	}
	if a.coverCard(c, pl.Name, pl.Cover(), pl.PlayCount, sub, func() { a.loadPlaylist(pl.ID, true) }) {
		a.open(route{page: pagePlaylist, id: pl.ID})
	}
}
