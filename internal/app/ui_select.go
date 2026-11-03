package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Selecting songs of a list, to download them together: on a page that
// shows a list, a song's menu offers the select; then the list's rows
// become checkboxes, Shift ranges between two taps, and the bar over the
// player takes the marked songs off to the downloads.

// selectModePages are the pages whose lists offer the select.
func (a *app) selectModePages() map[pageKind]bool {
	return map[pageKind]bool{
		pageAlbum: true, pageArtist: true, pageDaily: true,
		pagePlaylist: true, pageSearch: true,
	}
}

// selectPage says the page's list may be selected: one of selectModePages
// that has songs to mark.
func (a *app) selectPage() bool {
	return a.selectModePages()[a.route.page] && len(a.selectTracks) > 0
}

// toggleSelectMode moves the page's list between playing and selecting.
func (a *app) toggleSelectMode() {
	a.selecting = !a.selecting
	a.selected = map[int64]*netease.Track{}
	a.selectAnchor = -1
	a.redraw()
}

// toggleSelect marks or unmarks tr.
func (a *app) toggleSelect(tr *netease.Track) {
	if a.selected[tr.ID] != nil {
		delete(a.selected, tr.ID)
	} else {
		a.selected[tr.ID] = tr
	}
}

// rangeSelect marks the list's songs between the anchored row and this one,
// inclusive, so a Shift click selects a stretch of the list at once.
func (a *app) rangeSelect(from, to int) {
	if from > to {
		from, to = to, from
	}
	for i := from; i <= to && i < len(a.selectTracks); i++ {
		a.selected[a.selectTracks[i].ID] = &a.selectTracks[i]
	}
}

// selectAll marks the list's songs, or none, whichever it does not hold.
func (a *app) selectAll() {
	// Marked all already: clear them.
	all := len(a.selectTracks) > 0
	for _, tr := range a.selectTracks {
		if a.selected[tr.ID] == nil {
			all = false
			break
		}
	}
	if all {
		a.selected = map[int64]*netease.Track{}
		return
	}
	// Else mark them all, by the list's pointers, as the rows hold them.
	for i := range a.selectTracks {
		a.selected[a.selectTracks[i].ID] = &a.selectTracks[i]
	}
}

// downloadSelected takes the marked songs to the downloads.
func (a *app) downloadSelected() {
	if len(a.selected) == 0 {
		return
	}
	tracks := make([]netease.Track, 0, len(a.selected))
	for _, tr := range a.selected {
		tracks = append(tracks, *tr)
	}
	a.downloads().add(tracks)
	a.selecting = false
	a.selected = map[int64]*netease.Track{}
	a.selectAnchor = -1
}

// selectBar is the bar over the player that shows, and takes, the select:
// how many are marked, select all or none, and download them at once. It
// comes with the select, which a song's menu offers, and goes with it.
func (a *app) selectBar(c *ui.Context) {
	if !a.selectPage() || !a.selecting {
		return
	}
	p := a.pal
	ui.Row(c.Key("select-bar")).Absolute().Left(0).Right(0).Bottom(playerBarHeight).
		Height(48).PaddingX(20).Gap(14).AlignItems(ui.Center).
		Background(p.sidebar).BorderWidth(0, 1, 0, 0).BorderColor(p.hairline).Children(func() {
		// Selecting: what is marked, how to mark it all, and where it goes.
		n := len(a.selected)
		if n > 0 {
			ui.Textf(c, "已选 %d 首", n).FontSize(13).FontWeight(600)
		} else {
			ui.Text(c, "点歌曲勾选，或全选").FontSize(13).TextColor(p.muted)
		}
		// All or none of the list's songs.
		markedAll := false
		if len(a.selectTracks) > 0 {
			markedAll = true
			for _, tr := range a.selectTracks {
				if a.selected[tr.ID] == nil {
					markedAll = false
					break
				}
			}
		}
		allLabel := "取消全选"
		if !markedAll {
			allLabel = "全选"
		}
		if pillButton(c.Key("select-all"), iconCheck, allLabel, p.hover.Over(p.bg), p.text).Clicked() {
			a.selectAll()
		}
		// The marked songs to the downloads.
		dl := pillButton(c.Key("select-download"), iconDown, "下载所选", p.accent, p.onAccent)
		if n == 0 {
			dl.Disabled(true).Opacity(0.5)
		}
		if dl.Clicked() {
			a.downloadSelected()
		}
		// Back to the list playing.
		if pillButton(c.Key("select-cancel"), iconX, "取消", p.hover.Over(p.bg), p.text).Clicked() {
			a.toggleSelectMode()
		}
	})
}

// selectMark draws a row's check while the list selects: a filled ring when
// the song is marked, an empty one to mark it.
func (a *app) selectMark(c *ui.Context, tr *netease.Track) ui.Element {
	p := a.pal
	checked := a.selected[tr.ID] != nil
	box := ui.Box(c).Size(20, 20).Radius(999).Center()
	if checked {
		box.Background(p.accent).Children(func() {
			ui.Icon(c, iconCheck).Size(13, 13).TextColor(p.onAccent)
		})
	} else {
		box.Border(1.5, p.faint)
	}
	return box
}
