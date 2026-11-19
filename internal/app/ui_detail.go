package app

import (
	"github.com/egoist/mygo/ui"
)

// The heads of the pages of a playlist, an album and an artist share a
// shape: a cover beside the name, what it says about itself, and what it
// says about the music in it. On a phone they are laid out differently,
// as the head of a page is the same on every one of them.

// detailCoverSize is how wide the cover of such a page is. A phone keeps
// the name room beside it: the desktop's 212px leaves too little of a
// narrow screen, and folds the name into a strip.
func (a *app) detailCoverSize() float32 {
	if a.phone {
		return 120
	}
	return 212
}

// detailGap is the space between that cover and the text beside it.
func (a *app) detailGap() float32 {
	if a.phone {
		return 16
	}
	return 28
}

// detailNameSize is the size of the name on such a page.
func (a *app) detailNameSize() float32 {
	if a.phone {
		return 21
	}
	return 30
}

// phoneDetailHead is the head of a playlist's, an album's or an artist's
// page on a phone: a smaller cover beside the name, and the description
// behind a tap, as there is no room for it under the name. An artist's
// cover is round, the others' are square.
func (a *app) phoneDetailHead(c *ui.Context, cover, kind, name, meta, desc string, round bool) {
	p := a.pal
	size := a.detailCoverSize()
	radius := float32(12)
	if round {
		radius = size / 2
	}
	ui.Row(c).Gap(a.detailGap()).AlignItems(ui.End).Padding(0, 0, 12, 0).Children(func() {
		ui.Box(c).Size(size, size).Radius(radius).
			Shadow(0, 12, 30, -10, p.cardShade).Children(func() {
			a.coverFill(c, cover, 600, radius)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(5).Children(func() {
			ui.Text(c, kind).FontSize(11.5).FontWeight(600).TextColor(p.accent)
			ui.Text(c, name).FontSize(a.detailNameSize()).FontWeight(800).
				LetterSpacing(-0.4).MaxLines(2).LineHeight(1.25)
			if meta != "" {
				ui.Text(c, meta).FontSize(12).TextColor(p.muted)
			}
		})
	})
	a.phoneDetailDesc(c, name, desc)
}

// phoneDetailDesc is a description on a phone: a few lines of it, and a
// tap on them opens all of it, as the few lines under the name are cut
// off where they end.
func (a *app) phoneDetailDesc(c *ui.Context, title, desc string) {
	if desc == "" {
		return
	}
	p := a.pal
	b := ui.ButtonBase(c.Key("desc")).Column().AlignItems(ui.Start).FocusRing(false).Label("简介")
	b.Children(func() {
		ui.Text(c, desc).FontSize(12).TextColor(p.muted).MaxLines(2).LineHeight(1.45)
	})
	if b.Clicked() {
		a.showDesc(title, desc)
	}
}
