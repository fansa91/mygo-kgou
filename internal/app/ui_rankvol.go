package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// A chart's past numbers: KuGou publishes most charts a day at a time, and
// keeps every number it published. The page offers them, and shows the
// chart as the one chosen stood.

// The calls the page makes; tests replace them.
var (
	rankVolumes  = netease.RankVols
	rankVolSongs = netease.RankVolSongs
)

// chartVols is how many past numbers the menu offers: a chart has hundreds,
// and the ones wanted are the recent ones.
const chartVols = 15

// initRankVols makes the tables a page needs, for a test that builds the
// app without Run.
func (a *app) initRankVols() {
	if a.rankVols == nil {
		a.rankVols = map[int64][]netease.RankVol{}
	}
	if a.rankVolAt == nil {
		a.rankVolAt = map[int64]int64{}
	}
	if a.rankVolPl == nil {
		a.rankVolPl = map[int64]*netease.Playlist{}
	}
	if a.rankVolBusy == nil {
		a.rankVolBusy = map[int64]bool{}
	}
}

// loadRankVols reads a chart's past numbers once, when its page shows.
func (a *app) loadRankVols(pl *netease.Playlist) {
	a.initRankVols()
	id := pl.ID
	if _, ok := a.rankVols[id]; ok {
		return
	}
	if a.rankVolBusy[id] {
		return
	}
	a.rankVolBusy[id] = true
	go func() {
		list, err := rankVolumes(id)
		a.post(func() {
			delete(a.rankVolBusy, id)
			if err != nil {
				return
			}
			a.rankVols[id] = list
		})
	}()
}

// shownPlaylist is what a chart's page lists: the chart as the number
// chosen stood, or the chart as it stands.
func (a *app) shownPlaylist(pl *netease.Playlist) *netease.Playlist {
	if v := a.rankVolPl[pl.ID]; v != nil {
		return v
	}
	return pl
}

// openRankVol shows the chart as one of its past numbers stood.
func (a *app) openRankVol(pl *netease.Playlist, v netease.RankVol) {
	a.initRankVols()
	if a.rankVolBusy[pl.ID] {
		return
	}
	a.rankVolBusy[pl.ID] = true
	a.rankVolAt[pl.ID] = v.ID
	go func() {
		p, err := rankVolSongs(pl.ID, v.ID, v.Name)
		a.post(func() {
			delete(a.rankVolBusy, pl.ID)
			if err != nil {
				delete(a.rankVolAt, pl.ID)
				a.toast = "读取往期榜单失败，请稍后再试"
				return
			}
			a.rankVolPl[pl.ID] = p
		})
	}()
}

// closeRankVol goes back to the chart as it stands.
func (a *app) closeRankVol(pl *netease.Playlist) {
	a.initRankVols()
	delete(a.rankVolPl, pl.ID)
	delete(a.rankVolAt, pl.ID)
}

// rankVolItems offers a chart's recent numbers, the one shown marked.
func (a *app) rankVolItems(pl *netease.Playlist) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		vs := a.rankVols[pl.ID]
		if len(vs) == 0 {
			m.Item("正在读取往期…").Disabled(true)
			return
		}
		cur := a.rankVolAt[pl.ID]
		for i, v := range vs {
			if i >= chartVols {
				break
			}
			label := v.Name
			if v.Date != "" {
				label += " · " + v.Date
			}
			if v.ID == cur {
				label += "（当前）"
			}
			if m.Item(label).Chosen() {
				a.openRankVol(pl, v)
			}
		}
		m.Separator()
		if cur == 0 {
			m.Item("最新一期").Disabled(true)
			return
		}
		if m.Item("回到最新一期").Chosen() {
			a.closeRankVol(pl)
		}
	}
}
