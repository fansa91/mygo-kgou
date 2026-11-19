package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"
)

// The phone shell keeps the app's one window and its pages: the sidebar
// becomes a tab bar, the player bar a strip above it, and Now Playing
// still covers everything. Pages themselves are unchanged: only where
// they are put moves.

var phoneTabRoot = [4]route{
	{page: pageHome},
	{page: pageSearch},
	{page: pageLocal},
	{page: pageMine},
}

// phoneTabOf says which destination a page belongs to, so that a page
// opened from anywhere and a system back still light the right tab.
func (a *app) phoneTabOf(r route) int {
	switch r.page {
	case pageSearch, pageUser, pagePodcast, pagePodcasts:
		return 1
	case pageLocal, pageCloud, pageDownloads:
		return 2
	case pageMine, pageSettings:
		return 3
	default:
		return 0
	}
}

// isPhoneTabRoot reports whether r is the first page of one of the bottom
// tabs, which has no back button of its own.
func (a *app) isPhoneTabRoot(r route) bool {
	for _, root := range phoneTabRoot {
		if r.page != root.page {
			continue
		}
		if r.page == pageSearch {
			return r.query == ""
		}
		return true
	}
	return false
}

func (a *app) phoneTitle() string {
	switch a.route.page {
	case pageHome:
		return "为你推荐"
	case pageDaily:
		return "每日推荐"
	case pageFM:
		return "私人 FM"
	case pageCharts, pageRanking:
		return "排行榜"
	case pagePlaylist:
		return "歌单"
	case pageSearch:
		if a.route.query == "" {
			return "发现"
		}
		return "搜索结果"
	case pageArtist:
		return "歌手"
	case pageAlbum:
		return "专辑"
	case pageUser:
		return "我的主页"
	case pagePodcast:
		return "节目"
	case pagePodcasts:
		return "听书"
	case pageDownloads:
		return "下载管理"
	case pageLocal:
		return "本地音乐"
	case pageCloud:
		return "音乐云盘"
	case pageMine:
		return "我的"
	case pageSettings:
		return "设置"
	}
	return ""
}

// phoneSelectTab opens the tab's first page. A search tab with nothing
// searched yet still needs its page's data: its own field searches as it
// is typed, and the page reads that data as it builds.
func (a *app) phoneSelectTab(i int) {
	if i < 0 || i >= len(phoneTabRoot) {
		return
	}
	r := phoneTabRoot[i]
	if r.page == pageSearch {
		if a.searches == nil {
			a.searches = map[route]*searchData{}
		}
		if a.searches[r] == nil {
			d := &searchData{}
			d.page.loaded = true
			a.searches[r] = d
		}
	}
	if r.page == pageMine && a.profile != nil {
		// The account home loads the listener's own page.
		r.id = a.profile.UserID
	}
	a.phoneTab = i
	a.open(r)
}

// phoneView builds the shell around the pages, above which Now Playing
// rises. It is built instead of the sidebar-and-window layout on phones.
func (a *app) phoneView(c *ui.Context, npOpen, npSettled bool) {
	// A view dragged down shows the page under it, so it is built while the
	// finger holds it clear of the top; otherwise, settled over the pages,
	// it leaves them unbuilt: they keep their scroll and cost nothing per
	// frame.
	if npOpen && npSettled && a.npSwipe.offset.Y <= 0 {
		return
	}
	// The bar writes the destination it was tapped into selected; following
	// it is what opens that destination's page.
	selected := a.phoneTabOf(a.route)
	ui.Column(c).Fill().Children(func() {
		a.phoneTop(c)
		a.phonePages(c)
		a.phonePlayer(c)
		mobile.BottomBar(c, &selected, phoneBarButtons...)
	})
	if selected != a.phoneTabOf(a.route) {
		a.phoneSelectTab(selected)
	}
}

// npPageTurn is how long a page takes to slide away and the next to arrive,
// and npTabFade the crossfade a tab switch makes in its place: the page
// arriving goes over the one leaving, which fades out a little slower, so
// that the two are never both half seen.
const (
	npPageTurn   = 240 * time.Millisecond
	npTabFadeIn  = 140 * time.Millisecond
	npTabFadeOut = 220 * time.Millisecond
)

// npPageShift is how far a page comes from, and goes to, as the pages turn:
// far enough to be seen going, near enough that the page beside it is never
// shown empty.
const npPageShift = 56

// phonePages puts the page of the route where a phone shows pages, and
// slides it in as it changes: forward from the right, back from the left,
// with the page before going the other way. A tab switch is no step from
// the page it leaves, so the two cross-fade where they are.
func (a *app) phonePages(c *ui.Context) {
	// One parent for every page, so that a page leaving is still there for
	// the copy that fades or slides away; the page itself is keyed by what
	// it shows, so that its state comes back to it.
	box := ui.Box(c.Key("phone-pages")).Grow(1).Basis(0).FillWidth().Clip()
	dir := float32(a.navDir) * npPageShift
	turn := ui.ElementTransition{
		Duration: npPageTurn,
		Enter:    &ui.Motion{X: dir},
		Exit:     &ui.Motion{X: -dir},
	}
	if a.navDir == 0 {
		turn.Duration, turn.ExitDuration = npTabFadeIn, npTabFadeOut
		turn.Enter, turn.Exit = &ui.Motion{}, &ui.Motion{}
	}
	// The page going leaves the way the coming one does not: the parent
	// says so, in the frame it goes.
	box.ChildExit(ui.Motion{X: -dir}).Children(func() {
		ui.Column(c.Key(a.route)).Absolute().Fill().Transition(turn).Children(func() { a.content(c) })
	})
}

func (a *app) phoneTop(c *ui.Context) {
	mobile.TopBar(c, mobile.TopBarOptions{
		Title: a.phoneTitle(),
		Leading: func() {
			// The root page of a tab has no back button, even when earlier
			// tab switches left entries in the history.
			if len(a.history) == 0 || a.isPhoneTabRoot(a.route) {
				return
			}
			b := ui.ButtonBase(c.Key("phone-back")).Size(40, 40).Center().
				Radius(20).FocusRing(false).Label("返回")
			b.Children(func() { ui.Icon(c, iconBack).Size(20, 20) })
			if b.Clicked() {
				a.back()
			}
		},
		Trailing: func() {
			if a.route.page == pageSearch {
				return
			}
			b := ui.ButtonBase(c.Key("phone-search")).Size(40, 40).Center().
				Radius(20).FocusRing(false).Label("搜索")
			b.Children(func() { ui.Icon(c, iconSearch).Size(19, 19) })
			if b.Clicked() {
				a.phoneSelectTab(1)
			}
		},
	})
}

// phonePlayer is the small strip above the tab bar: what plays, and the
// two controls a finger reaches for most. Its whole row opens Now Playing.
func (a *app) phonePlayer(c *ui.Context) {
	p := a.pal
	tr := a.pl.Current()
	row := ui.ButtonBase(c.Key("phone-player")).FillWidth().Height(58).
		Padding(0, 12).Gap(10).AlignItems(ui.Center).FocusRing(false).
		Background(p.sidebar).Label("打开正在播放")
	row.Children(func() {
		if tr == nil {
			ui.Box(c).Size(38, 38).Radius(6).Background(p.hover).Center().Children(func() {
				ui.Icon(c, iconNote).Size(17, 17).TextColor(p.faint)
			})
			ui.Text(c, "未在播放").FontSize(13).TextColor(p.muted)
			return
		}
		ui.Box(c).Size(38, 38).Shrink(0).Children(func() {
			a.cover(c, tr.Album.PicURL, 80, 38, 6)
		})
		names := make([]string, len(tr.Artists))
		for i, ar := range tr.Artists {
			names[i] = ar.Name
		}
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, tr.Name).FontSize(13).FontWeight(600).SingleLine()
			ui.Text(c, strings.Join(names, " / ")).FontSize(11).TextColor(p.muted).SingleLine()
		})
		// playButton turns its icon with the playing state, and its click
		// comes before the row's: a tap on play plays, a tap elsewhere in
		// the strip opens Now Playing.
		if playButton(c.Key("phone-play"), a.pl.Playing(), 18, p.text).Clicked() {
			a.pl.TogglePlay()
		}
		if iconButton(c.Key("phone-next"), iconNext, 18, p.muted, "下一首").Clicked() {
			a.pl.Next()
		}
	})
	if row.Clicked() {
		a.nowPlaying = true
	}
}

// stroke draws a tab icon in the color the bar gives it.
func phoneStroke(paths ...func(p *ui.Path, r ui.Rect)) func(*ui.Painter, ui.Rect, ui.Color) {
	return func(pn *ui.Painter, r ui.Rect, color ui.Color) {
		var path ui.Path
		for _, draw := range paths {
			draw(&path, r)
		}
		pn.StrokePath(&path, 1.8, color)
	}
}

var phoneBarButtons = []mobile.BarButton{
	{Label: "首页", Icon: phoneStroke(func(p *ui.Path, r ui.Rect) {
		p.MoveTo(r.X+3, r.Y+11).LineTo(r.X+12, r.Y+3).LineTo(r.X+21, r.Y+11).
			LineTo(r.X+21, r.Y+21).LineTo(r.X+3, r.Y+21).Close()
	})},
	{Label: "发现", Icon: phoneStroke(func(p *ui.Path, r ui.Rect) {
		p.Circle(r.X+11, r.Y+11, 6)
	}, func(p *ui.Path, r ui.Rect) {
		p.MoveTo(r.X+16, r.Y+16).LineTo(r.X+21, r.Y+21)
	})},
	{Label: "乐库", Icon: phoneStroke(func(p *ui.Path, r ui.Rect) {
		p.MoveTo(r.X+9, r.Y+17).LineTo(r.X+9, r.Y+5).LineTo(r.X+20, r.Y+3).LineTo(r.X+20, r.Y+15)
	}, func(p *ui.Path, r ui.Rect) {
		p.Circle(r.X+6.5, r.Y+18, 2.5)
	}, func(p *ui.Path, r ui.Rect) {
		p.Circle(r.X+17.5, r.Y+16, 2.5)
	})},
	{Label: "我的", Icon: phoneStroke(func(p *ui.Path, r ui.Rect) {
		p.Circle(r.X+12, r.Y+8, 4)
	}, func(p *ui.Path, r ui.Rect) {
		p.MoveTo(r.X+4, r.Y+21).LineTo(r.X+5, r.Y+16).LineTo(r.X+19, r.Y+16).LineTo(r.X+20, r.Y+21)
	})},
}
