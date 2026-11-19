package app

import (
	"github.com/egoist/mygo/ui"
)

// descSheet is a description a phone cannot show in full where it lies: the
// few lines under a playlist's, an album's or an artist's name are cut off,
// so the phone asks for it in a dialog of its own, with the whole of it to
// scroll through.
type descSheet struct {
	open  bool
	title string
	body  string
}

// showDesc asks to see a description in full.
func (a *app) showDesc(title, body string) {
	a.desc = descSheet{open: true, title: title, body: body}
}

// descDialog is the dialog a phone shows a description in.
func (a *app) descDialog(c *ui.Context) {
	p := a.pal
	d := &a.desc
	ui.DialogBase(c.Key("description"), &d.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.4))
		// A phone's screen is narrow: the panel takes what it can, and the
		// text inside it scrolls, so a long description is all there.
		w, h := c.Size()
		width := min(float32(420), w-48)
		panel.Width(width).MaxHeight(h-120).Padding(20, 20, 16, 20).Radius(18).Gap(10).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.45)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		ui.Text(c, d.title).FontSize(17).FontWeight(700).MaxLines(2).LineHeight(1.3)
		// The room the text has: what it needs, up to what the screen
		// leaves once the title and the button have theirs, so that a long
		// description scrolls and a short one leaves the panel small.
		lines := float32(len([]rune(d.body))/22 + 1)
		body := min(max(lines*21, 21), h-260)
		ui.Scroll(c).Height(body).ScrollbarInsets(0, 0, 4, 0).Children(func() {
			ui.Text(c, d.body).FontSize(13).TextColor(p.muted).LineHeight(1.6)
		})
		if pillButton(c.Key("close"), nil, "关闭", p.hover.Over(p.bg), p.text).Clicked() {
			d.open = false
		}
	})
}
