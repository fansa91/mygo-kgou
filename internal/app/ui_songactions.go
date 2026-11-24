package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// songAction is what can be done with a song, told apart from how it is
// asked for: the desktop puts these in a system menu, a phone in a sheet
// from the bottom. A submenu holds the playlists the song can go into.
type songAction struct {
	label string
	run   func()
	sep   bool // a separator goes before this action
	sub   []songAction
}

// songActions lists what can be done with tr, in the order the desktop's
// menu shows it. pid is the playlist tr is listed in, if any.
func (a *app) songActions(tr *netease.Track, pid int64) []songAction {
	if tr.Local() {
		return []songAction{
			{label: "复制歌名", run: func() { copyText(tr.Name) }},
			{label: "复制歌名和歌手", run: func() { copyText(tr.Name + " - " + tr.ArtistNames()) }},
			{label: "在文件夹中显示", sep: true, run: func() { revealPath(tr.Path) }},
		}
	}
	if tr.Cloud {
		return []songAction{
			{label: "复制歌名", run: func() { copyText(tr.Name) }},
			{label: "复制歌名和歌手", run: func() { copyText(tr.Name + " - " + tr.ArtistNames()) }},
			{label: "下载", sep: true, run: func() { a.downloads().add([]netease.Track{*tr}) }},
			{label: "从云盘删除", sep: true, run: func() { a.deleteCloud(tr) }},
		}
	}
	if tr.ProgramID != 0 {
		return []songAction{
			{label: "复制节目名", run: func() { copyText(tr.Name) }},
			{label: "复制链接", run: func() { copyText(tr.URL()) }},
			{label: "查看听书", sep: true, run: func() { a.openPage(route{page: pagePodcast, id: tr.PodcastID}) }},
		}
	}

	out := []songAction{
		{label: "复制歌名", run: func() { copyText(tr.Name) }},
		{label: "复制歌名和歌手", run: func() { copyText(tr.Name + " - " + tr.ArtistNames()) }},
		{label: "复制链接", sep: true, run: func() { copyText(tr.URL()) }},
		{label: "下载", sep: true, run: func() { a.downloads().add([]netease.Track{*tr}) }},
	}
	if a.selectModePages()[a.route.page] {
		label := "多选"
		if a.selecting {
			label = "退出多选"
		}
		out = append(out, songAction{label: label, run: func() { a.toggleSelectMode() }})
	}
	out = append(out,
		songAction{label: "听相似的歌", sep: true, run: func() { a.playSimilar(tr) }},
		songAction{label: "歌曲成绩单…", run: func() { a.openRanking(tr) }},
	)
	linked := tr.Album.ID != 0
	for _, ar := range tr.Artists {
		linked = linked || ar.ID != 0
	}
	if linked {
		out = append(out, songAction{sep: true})
	}
	for _, ar := range tr.Artists {
		if ar.ID != 0 {
			out = append(out, songAction{label: "查看歌手：" + ar.Name, run: func() { a.openArtist(ar.ID) }})
		}
	}
	if tr.Album.ID != 0 {
		out = append(out, songAction{label: "查看专辑：" + tr.Album.Name, run: func() {
			a.openPage(route{page: pageAlbum, id: tr.Album.ID})
		}})
	}
	if a.profile != nil {
		out = append(out, songAction{label: "添加到歌单", sep: true, sub: a.addToPlaylists(tr, pid)})
		if a.editable(pid) != nil {
			out = append(out, songAction{label: "从歌单中移除", run: func() { a.removeFrom(pid, tr) }})
		}
	}
	return out
}

// addToPlaylists is the choice behind "添加到歌单": a new one, or one of
// the listener's own, except the playlist the song is already in.
func (a *app) addToPlaylists(tr *netease.Track, pid int64) []songAction {
	out := []songAction{{label: "新建歌单…", run: func() { a.newPlaylist(tr) }}}
	for i := range a.mine {
		pl := &a.mine[i]
		if pl.SpecialType == 5 || pl.ID == pid {
			continue
		}
		id := pl.ID
		out = append(out, songAction{label: pl.Name, sep: len(out) == 1, run: func() { a.addTo(id, tr) }})
	}
	return out
}

// songMenuItems fills a desktop menu from actions, keeping their order and
// separators. The phone uses the same actions in mobile.ActionSheet.
func (a *app) songMenuItems(m *ui.Menu, actions []songAction) {
	for _, act := range actions {
		if act.sep {
			m.Separator()
		}
		if act.label == "" {
			continue
		}
		if act.sub != nil {
			m.Submenu(act.label, func(sm *ui.Menu) { a.songMenuItems(sm, act.sub) })
			continue
		}
		if m.Item(act.label).Chosen() {
			act.run()
		}
	}
}
