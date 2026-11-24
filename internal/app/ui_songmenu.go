package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// copyText puts text on the clipboard; tests replace it.
var copyText = func(text string) error { return mygo.Clipboard.Write(transfer.TextData(text)) }

// songItems is the desktop's menu of what can be done with tr. The
// actions are shared with the phone's sheet, so the two stay in step.
func (a *app) songItems(tr *netease.Track, pid int64) func(m *ui.Menu) {
	actions := a.songActions(tr, pid)
	return func(m *ui.Menu) { a.songMenuItems(m, actions) }
}

// localItems builds the menu of a song of the listener's own folders: it has
// no page on KuGou to copy a link to, and no playlist of the service to be
// added to; what it has is the file it plays from.
func (a *app) localItems(m *ui.Menu, tr *netease.Track) {
	if m.Item("复制歌名").Chosen() {
		copyText(tr.Name)
	}
	if m.Item("复制歌名和歌手").Chosen() {
		copyText(tr.Name + " - " + tr.ArtistNames())
	}
	m.Separator()
	if m.Item("在文件夹中显示").Chosen() {
		revealPath(tr.Path)
	}
}

// programItems builds the menu of an episode: copying its title or the
// link to its page, or opening its podcast's.
func (a *app) programItems(m *ui.Menu, tr *netease.Track) {
	if m.Item("复制节目名").Chosen() {
		copyText(tr.Name)
	}
	if m.Item("复制链接").Chosen() {
		copyText(tr.URL())
	}
	m.Separator()
	if m.Item("查看听书").Chosen() {
		a.openPage(route{page: pagePodcast, id: tr.PodcastID})
	}
}
