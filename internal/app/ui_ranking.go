package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// A song's record across the charts: the platforms it reached, the best
// place it took on each, and how long it stayed. The phone's song page shows
// it as 歌曲成绩单; it has a page of its own here.

// songRanking is what the page asks for; tests replace it.
var songRanking = netease.SongRanking

// openRanking opens the record of tr, reading it as the page shows.
func (a *app) openRanking(tr *netease.Track) {
	a.rankingTrack = *tr
	a.ranking, a.rankingErr = nil, ""
	a.open(route{page: pageRanking, id: tr.ID})
}

// loadRanking reads the record of the song id, once.
func (a *app) loadRanking(id int64) {
	if a.rankingLoading || id == 0 {
		return
	}
	if a.ranking != nil && a.ranking.Song.ID == id {
		return
	}
	a.rankingLoading, a.rankingErr = true, ""
	go func() {
		rec, err := songRanking(id)
		a.post(func() {
			a.rankingLoading = false
			if err != nil {
				a.rankingErr = "成绩单暂时无法读取"
				return
			}
			a.ranking = rec
		})
	}()
}

// rankingPage shows the song, its record's headline, and each platform's.
func (a *app) rankingPage(c *ui.Context) {
	p := a.pal
	tr := a.rankingTrack
	a.loadRanking(tr.ID)
	top, bottom := a.insets(c)
	ui.Scroll(c.Key("ranking")).TrackScroll(&a.pageScroll).Grow(1).
		Padding(top+pageGap, pagePad, bottom+48, pagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		a.startMark(c)
		defer a.endMark(c)
		ui.Row(c).Gap(26).AlignItems(ui.End).Padding(0, 0, 28, 0).Children(func() {
			ui.Box(c).Size(172, 172).Radius(12).Shadow(0, 16, 36, -10, p.cardShade).Children(func() {
				a.coverFill(c, tr.Album.PicURL, 500, 12)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(7).Children(func() {
				ui.Text(c, "歌曲成绩单").FontSize(12).FontWeight(600).TextColor(p.accent)
				ui.Text(c, tr.Name).FontSize(28).FontWeight(800).LetterSpacing(-0.5).MaxLines(2).LineHeight(1.2)
				ui.Text(c, tr.ArtistNames()).FontSize(14).TextColor(p.muted).SingleLine()
				if r := a.ranking; r != nil {
					if r.Title != "" {
						ui.Text(c, r.Title).FontSize(20).FontWeight(700).Margin(6, 0, 0, 0)
					}
					if r.Note != "" {
						ui.Text(c, r.Note).FontSize(12.5).TextColor(p.muted).MaxLines(2).LineHeight(1.45)
					}
				}
			})
		})
		switch {
		case a.rankingErr != "":
			ui.Column(c).Padding(0, 0, 40, 0).Center().Gap(12).Children(func() {
				ui.Text(c, a.rankingErr).FontSize(13).TextColor(p.muted)
				if pillButton(c.Key("ranking-retry"), iconReload, "重试", p.hover.Over(p.bg), p.text).Clicked() {
					a.ranking, a.rankingErr = nil, ""
					a.loadRanking(tr.ID)
				}
			})
		case a.ranking == nil || a.rankingLoading && a.ranking.Song.ID != tr.ID:
			s := a.pageShimmer(c)
			for i := 0; i < 4; i++ {
				s.bone(c.Key(i), 12).Height(56).WidthPercent(100).Margin(0, 0, 10, 0)
			}
		case len(a.ranking.Platforms) == 0:
			ui.Column(c).Padding(0, 0, 60, 0).Center().Gap(8).Children(func() {
				ui.Icon(c, iconCharts).Size(32, 32).TextColor(p.faint)
				ui.Text(c, "这首还没有上榜记录").FontSize(14).FontWeight(600)
				ui.Text(c, "上榜之后，这里会列出它到过的地方").FontSize(12.5).TextColor(p.muted)
			})
		default:
			a.section(c, "上榜明细")
			ui.Column(c).Gap(5).Children(func() {
				for i := range a.ranking.Platforms {
					a.rankingRow(c.Key(i), &a.ranking.Platforms[i])
				}
			})
		}
	})
}

// rankingRow is one platform's record: which platform, the best place the
// song took there, how long it stayed, and when it was last seen.
func (a *app) rankingRow(c *ui.Context, pl *netease.RankingPlatform) {
	p := a.pal
	ui.Row(c).Padding(12, 14).Gap(14).Radius(12).AlignItems(ui.Center).Background(p.hover).Children(func() {
		a.avatar(c, pl.Logo, 26, p.press)
		ui.Text(c, pl.Name).FontSize(14).FontWeight(600).Width(110).SingleLine()
		rank := "—"
		if pl.Rank > 0 {
			rank = fmt.Sprintf("最高第 %d 名", pl.Rank)
		}
		ui.Text(c, rank).FontSize(13).Width(110).TextColor(p.text)
		ui.Text(c, fmt.Sprintf("在榜 %s 次", count(pl.Times))).FontSize(12.5).TextColor(p.muted).Width(110)
		ui.Box(c).Grow(1)
		if pl.Last != "" {
			ui.Text(c, strings.ReplaceAll(pl.Last, "-", "/")).FontSize(12).TextColor(p.faint).SingleLine()
		}
	})
}
