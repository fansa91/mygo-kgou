package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"

	"netease-music-mygo/internal/netease"
)

// songSheetView shows the song the listener long-pressed, in the phone's
// bottom sheet rather than the desktop's big system menu. A second sheet
// asks which playlist to add it to, since a sheet has no submenus.
func (a *app) songSheetView(c *ui.Context) {
	if !a.phone {
		return
	}
	s := &a.songSheet
	if s.open {
		tr := s.track
		acts := a.songActions(&tr, s.pid)
		toSheet := make([]mobile.Action, 0, len(acts))
		for i := range acts {
			act := acts[i]
			if act.label == "" {
				continue
			}
			if act.sub != nil {
				toSheet = append(toSheet, mobile.Action{Label: act.label, OnTap: func() {
					s.open, s.askPlaylist = false, true
				}})
				continue
			}
			toSheet = append(toSheet, mobile.Action{Label: act.label, OnTap: act.run})
		}
		mobile.ActionSheet(c, &s.open, mobile.ActionSheetOptions{
			Title: s.track.Name, Cancel: "取消",
		}, toSheet...)
	}
	if s.askPlaylist {
		tr := s.track
		acts := a.addToPlaylists(&tr, s.pid)
		toSheet := make([]mobile.Action, 0, len(acts))
		for i := range acts {
			act := acts[i]
			toSheet = append(toSheet, mobile.Action{Label: act.label, OnTap: act.run})
		}
		mobile.ActionSheet(c, &s.askPlaylist, mobile.ActionSheetOptions{
			Title: "添加到歌单", Cancel: "取消",
		}, toSheet...)
	}
}

// songContext is how a song row asks for its actions: a phone opens the
// bottom sheet on a long press, a desktop keeps its context menu.
func (a *app) songContext(row ui.Element, tr *netease.Track, pid int64) {
	if !a.phone {
		row.ContextMenu(a.songItems(tr, pid))
		return
	}
	if !row.LongPressed() {
		return
	}
	s := &a.songSheet
	s.track, s.pid, s.open = *tr, pid, true
}

// songMenu is the three dots of a song, on a phone opening the same sheet
// a long press does.
func (a *app) songMenu(c *ui.Context, tr *netease.Track, size float32, color ui.Color) ui.Element {
	b := iconButton(c, iconMore, size, color, "更多")
	if a.phone {
		if b.Clicked() {
			s := &a.songSheet
			s.track, s.pid, s.open = *tr, 0, true
		}
	} else {
		b.Menu(a.songItems(tr, 0))
	}
	return b
}
