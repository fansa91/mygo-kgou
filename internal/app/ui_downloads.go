package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// Downloads: the page that lists every song asked for, newest first, with
// how far it has got, where it was saved, and a second try for the ones
// that failed.

func (a *app) downloadsPage(c *ui.Context) {
	entries := a.downloads().entries()
	n := len(entries)
	top, bottom := a.insets(c)
	rows := 1 + max(n, 1)
	ui.List(c.Key("downloads"), &a.dlList, rows, func(i int) {
		if i == 0 {
			ui.Column(c).Children(func() {
				a.startMark(c)
				a.downloadsHeader(c, n)
			})
			return
		}
		if i == rows-1 {
			defer a.endMark(c)
		}
		if n == 0 {
			a.downloadsEmpty(c)
			return
		}
		// The newest first, as a list of what was asked for last reads.
		a.downloadRow(c, &entries[n-i])
	}).Grow(1).Padding(top+pageGap, pagePad-12, bottom+32, pagePad-12).ScrollbarInsets(top, 0, bottom, 0)
}

// downloadsHeader names the page, how many downloads it holds, and what can
// be done with them all: opening the folder, and clearing what ended.
func (a *app) downloadsHeader(c *ui.Context, n int) {
	p := a.pal
	sub := "还没有下载"
	if n > 0 {
		sub = fmt.Sprintf("%d 项", n)
	}
	a.heading(c, "下载管理", sub)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(0, 0, 8, 0).Children(func() {
		if pillButton(c.Key("dl-folder"), iconFolder, "打开文件夹", p.hover.Over(p.bg), p.text).Clicked() {
			a.downloads().openFolder()
		}
		if n > 0 {
			if pillButton(c.Key("dl-clear"), iconTrash, "清空已完成", p.hover.Over(p.bg), p.text).Clicked() {
				a.downloads().clearFinished()
			}
		}
		ui.Box(c).Grow(1)
		ui.Text(c, a.downloadDir()).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0).TextAlign(ui.End)
	})
}

// downloadRow is one download: what it is, how it is coming along, and what
// can be done with it now.
func (a *app) downloadRow(c *ui.Context, t *downloadTask) {
	p := a.pal
	ui.Row(c.Key(t.id)).Padding(11, 12).Gap(14).AlignItems(ui.Center).MinHeight(64).Radius(10).Children(func() {
		a.cover(c, t.track.Album.PicURL, 96, 42, 6)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, t.track.Name).FontSize(13.5).FontWeight(500).SingleLine()
			ui.Text(c, t.track.ArtistNames()).FontSize(12).TextColor(p.muted).SingleLine()
			a.downloadStatus(c, t)
		})
		switch t.state {
		case dlDone:
			if iconButton(c.Key("reveal"), iconFolder, 15, p.muted, "在文件夹中显示").Clicked() {
				a.downloads().reveal(t.name)
			}
		case dlFailed:
			if ui.Button(c.Key("retry"), "重试").Clicked() {
				a.downloads().retry(t.id)
			}
		}
	})
}

// downloadStatus says how a download is coming along: waiting, how far it
// has got, where it was saved, or why it failed.
func (a *app) downloadStatus(c *ui.Context, t *downloadTask) {
	p := a.pal
	switch t.state {
	case dlWaiting:
		ui.Text(c, "等待中").FontSize(11.5).TextColor(p.faint)
	case dlRunning:
		pct := 0
		if t.total > 0 {
			pct = int(float64(t.got) / float64(t.total) * 100)
		}
		// A bar of the row's own, not of the whole width under the title.
		ui.Row(c).Gap(8).AlignItems(ui.Center).Width(280).Children(func() {
			ui.Box(c).Grow(1).Height(3).Radius(2).Background(p.hairline).Draw(func(pt *ui.Painter, r ui.Rect) {
				f := float32(0)
				if t.total > 0 {
					f = min(1, float32(float64(t.got)/float64(t.total)))
				}
				if f > 0 {
					pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * f, H: r.H}, p.accent, 2)
				}
			})
			ui.Textf(c, "%d%%", pct).FontSize(11).FontFeatures("tnum").TextColor(p.muted).Width(36).TextAlign(ui.End)
		})
	case dlDone:
		status := "已保存"
		if q := qualityLabel(t.level); q != "" {
			status += " · " + q
		}
		ui.Text(c, status).FontSize(11.5).TextColor(p.muted).SingleLine()
	case dlFailed:
		ui.Text(c, t.err).FontSize(11.5).TextColor(p.accent).SingleLine()
	}
}

// downloadsEmpty says what the page is for before anything is asked for.
func (a *app) downloadsEmpty(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(0, 0, 80, 0).MinHeight(240).Center().Gap(9).Children(func() {
		ui.Icon(c, iconDownload).Size(36, 36).TextColor(p.faint)
		ui.Text(c, "还没有下载").FontSize(14.5).FontWeight(600)
		ui.Text(c, "在歌曲上右键选「下载」，或在右键菜单里选「多选」后一起下载").FontSize(12.5).TextColor(p.muted)
	})
}
