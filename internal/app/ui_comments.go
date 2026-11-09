package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/kugou"
	"netease-music-mygo/internal/netease"
)

// These load a page of a thread's comments, like a comment or unlike it, and
// write one, a comment of the listener's or the answer to theirs; tests
// replace them.
var (
	threadComments   = netease.ThreadComments
	likeComment      = netease.LikeComment
	commentsLikeable = netease.CommentsLikeable
	sendComment      = netease.SendComment
	replyComment     = netease.ReplyComment
)

// commentTabs are the two pages the comments show: the 精彩 ones KuGou ranks
// first, and the 最新 ones written most recently.
const (
	commentTabHot = "精彩" // most liked first
	commentTabNew = "最新" // most recent first
)

// commentPage is how many comments a page loads, and where it is in them:
// each commentPage loads its own pages and remembers its own scroll.
type commentPage struct {
	list    []netease.Comment
	total   int
	more    bool
	loaded  bool
	loading bool
	err     string
	page    int // the next page to ask for, 1-based
	scroll  ui.ScrollState
}

// commentState is the comments of the song or episode Now Playing shows:
// the 精彩 ones and the 最新 ones, each on its own page with its own place.
type commentState struct {
	track  int64
	thread string      // whose comments they are, as NetEase names it
	hot    commentPage // 精彩: most liked, paginated on its own
	latest commentPage // 最新: most recent, paginated on its own
	active string      // commentTabHot or commentTabNew
	// What the listener is writing, and the comment it answers when it is
	// an answer rather than a comment of their own.
	draft     string
	answer    netease.Comment
	answering bool
	sending   bool
}

// commentPageSize is how many comments a page loads.
const commentPageSize = 30

// activePage is the page the comments are showing now: 精彩 or 最新.
func (cs *commentState) activePage() *commentPage {
	if cs.active == commentTabNew {
		return &cs.latest
	}
	return &cs.hot
}

// commentsFor makes the comments those of tr, loading their first page
// once it changed: Now Playing does it as it shows, for the count on the
// pane's tab.
func (a *app) commentsFor(tr *netease.Track) *commentState {
	cs := &a.comments
	if tr.Local() {
		// A file of the listener's own is on no service, and has nothing
		// to ask for.
		*cs = commentState{}
		return cs
	}
	if cs.track != tr.ID {
		*cs = commentState{track: tr.ID, thread: tr.Thread(), active: commentTabHot}
		a.loadComments()
	}
	return cs
}

// loadComments loads the next page of the comments' tab that is showing.
func (a *app) loadComments() {
	cs := &a.comments
	cp := cs.activePage()
	if cp.loading {
		return
	}
	thread, page := cs.thread, cp.page
	if page == 0 {
		page = 1
	}
	cp.loading, cp.err = true, ""
	go func() {
		order := kugou.CommentsHot
		if cs.active == commentTabNew {
			order = kugou.CommentsNew
		}
		pageRes, err := threadComments(thread, order, page, commentPageSize)
		a.post(func() {
			if cs.thread != thread {
				return
			}
			cp.loading = false
			if err != nil {
				cp.err = "评论加载失败"
				return
			}
			cp.loaded = true
			var list []netease.Comment
			if cs.active == commentTabHot {
				list = pageRes.Hot
			} else {
				list = pageRes.Latest
			}
			// New comments shift the pages: those already shown come again.
			seen := make(map[int64]bool, len(cp.list))
			for _, cm := range cp.list {
				seen[cm.ID] = true
			}
			for _, cm := range list {
				if !seen[cm.ID] {
					cp.list = append(cp.list, cm)
				}
			}
			cp.total = pageRes.Total
			cp.more = pageRes.More
			if cp.page == 0 {
				cp.page = 1
			}
			cp.page++
		})
	}()
}

// commentsTab names the comments' pane, with how many there are.
func (a *app) commentsTab() string {
	if n := a.commentCount(); n > 0 {
		return "评论 " + count(n)
	}
	return "评论"
}

// commentCount is the count the comments' tab shows: the last song's
// while the next one's loads, so that it rolls from one to the other.
func (a *app) commentCount() int {
	cs := &a.comments
	cp := cs.activePage()
	switch {
	case cp.loaded:
		return cp.total
	case cp.err != "":
		return 0
	}
	return a.tabCount.n
}

// rollState is a count as it rolls from one value to another.
type rollState struct {
	n    int
	up   bool       // whether it went up, last it changed
	exit *ui.Motion // where the characters shown go as it changes
}

// rollLine is how far a character rolls, about a line of the tab's text.
const rollLine = 16

// rollCount shows n as text, each character in a slot of its own, as
// the wheels of a counter: one that changes rolls out as the next rolls
// in, upward as n grows and downward as it shrinks, the others staying.
func rollCount(c *ui.Context, st *rollState, n int, text string, fg ui.Color) {
	if n != st.n {
		st.up = n > st.n
		dir := float32(1)
		if !st.up {
			dir = -1
		}
		if st.exit != nil {
			// The characters shown leave from this frame on, which first
			// knows the way.
			st.exit.Y = -dir * rollLine
		}
		st.n, st.exit = n, &ui.Motion{}
	}
	dir := float32(1)
	if !st.up {
		dir = -1
	}
	chars := []rune(text)
	// Clipped to its line, a character leaves out of sight.
	ui.Row(c).ClipY().Children(func() {
		for i, ch := range chars {
			// Slots go by their place from the right, as the wheels of a
			// counter do: the units are always the last.
			k := len(chars) - 1 - i
			roll := ui.Animation{Spring: ui.SpringOptions{Duration: 420 * time.Millisecond, Bounce: 0.15}, Delay: time.Duration(k) * 30 * time.Millisecond}
			slot := ui.Box(c.Key(k)).Transition(ui.ElementTransition{
				Animation: &roll, Size: true,
				Enter: &ui.Motion{Y: dir * rollLine, Collapse: true},
				Exit:  &ui.Motion{Collapse: true},
			})
			slot.Children(func() {
				ui.Text(c.Key(ch), string(ch)).FontSize(13).FontWeight(600).FontFeatures("tnum").TextColor(fg).
					Transition(ui.ElementTransition{Animation: &roll, Colors: true, Enter: &ui.Motion{Y: dir * rollLine}, Exit: st.exit})
			})
		}
	})
}

// count formats a count as people in China read it, to a tenth of a 万.
func count(n int) string {
	if n >= 1e4 && n < 1e6 {
		return fmt.Sprintf("%.1f万", float64(n)/1e4)
	}
	return plays(float64(n))
}

type commentKey struct {
	hot bool
	id  int64
}

// commentsPane lists the comments of tr: a 精彩 / 最新 tab switch, the page
// that is showing, which loads on as it scrolls into view, and below them
// where the listener writes one.
func (a *app) commentsPane(c *ui.Context, tr *netease.Track) {
	cs := a.commentsFor(tr)
	cp := cs.activePage()
	// The two tabs: 精彩 and 最新, each keeping its own page and place.
	ui.Row(c).Padding(12, 12, 6, 12).Gap(10).Children(func() {
		a.commentTab(c, commentTabHot, cp == &cs.hot)
		a.commentTab(c, commentTabNew, cp == &cs.latest)
	})
	a.commentPages(c, cs, cp)
	a.commentComposer(c)
}

// commentPages shows the page of comments the tab holds: a skeleton while
// the first of them loads, what it says when there are none, or the comments
// themselves, the next page loading as the foot comes into view.
func (a *app) commentPages(c *ui.Context, cs *commentState, cp *commentPage) {
	if len(cp.list) == 0 {
		if cp.loading || !cp.loaded && cp.err == "" {
			ui.Column(c).Grow(1).Padding(4, 8, 0, 0).Gap(2).Children(func() {
				s := npShimmer(c)
				s.text(c, 13, 1.3).Width(56).Margin(18, 12, 6, 12)
				commentSkeleton(c, s, 6)
			})
			return
		}
		ui.Column(c).Grow(1).Center().Gap(14).Children(func() {
			switch {
			case cp.err != "":
				ui.Text(c, cp.err).FontSize(15).TextColor(white.Alpha(0.5))
				if pillButton(c.Key("retry"), iconComment, "重试", white.Alpha(0.14), white).Clicked() {
					a.loadComments()
				}
			default:
				ui.Icon(c, iconComment).Size(40, 40).TextColor(white.Alpha(0.3))
				ui.Text(c, "还没有评论，快来抢沙发").FontSize(17).FontWeight(600).TextColor(white.Alpha(0.45))
			}
		})
		return
	}
	now := c.Now()
	scroll := ui.Scroll(c.Key("comments")).Grow(1).TrackScroll(&cp.scroll).Padding(4, 8, 48, 0)
	var end ui.Rect
	scroll.Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			if cs.active == commentTabHot {
				commentHeading(c, commentTabHot+"评论")
			} else {
				commentHeading(c, commentTabNew+"评论 "+count(cp.total))
			}
			for i := range cp.list {
				a.commentRow(c.Key(commentKey{cs.active == commentTabHot, cp.list[i].ID}), &cp.list[i], now)
			}
			if cp.loading {
				commentSkeleton(c.Key("more"), npShimmer(c), 2)
			}
			ui.Row(c.Key("foot")).Height(56).Center().Children(func() {
				switch {
				case cp.loading:
				case cp.err != "":
					retry := ui.ButtonBase(c.Key("retry")).Padding(6, 14).Radius(999).Background(white.Alpha(0.1)).Label("重试")
					retry.Children(func() {
						ui.Text(c, "加载失败，点按重试").FontSize(12.5).TextColor(white.Alpha(0.7))
					})
					if retry.Clicked() {
						a.loadComments()
					}
				case !cp.more:
					ui.Text(c, "没有更多评论了").FontSize(12).TextColor(white.Alpha(0.35))
				}
			})
			end = ui.Box(c.Key("end")).Height(0).Bounds()
		})
	})
	// The next page, once the foot is within a screen of coming into view.
	if box := scroll.Bounds(); cp.more && !cp.loading && cp.err == "" && end.W > 0 && end.Y < box.Y+box.H*2 {
		a.loadComments()
	}
}

// commentTab is one of the two comments pages: 精彩 or 最新.
func (a *app) commentTab(c *ui.Context, name string, chosen bool) {
	bg, fg := white.Alpha(0.06), white.Alpha(0.6)
	if chosen {
		bg, fg = white.Alpha(0.16), white
	}
	b := pillButton(c.Key(name), nil, name, bg, fg)
	if b.Clicked() {
		cs := &a.comments
		if cs.active != name {
			cs.active = name
			a.loadComments()
		}
	}
}

func commentHeading(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(13).FontWeight(700).TextColor(white.Alpha(0.55)).LetterSpacing(0.2).Padding(18, 12, 6, 12)
}

// commentRow is a comment: who wrote it, what, and what it replies to,
// when, from where, and how many liked it, with a thumb to like it too.
func (a *app) commentRow(c *ui.Context, cm *netease.Comment, now time.Time) {
	ui.Row(c).Padding(12).Gap(12).Radius(12).AlignItems(ui.Start).Children(func() {
		a.avatar(c, cm.User.AvatarURL, 34, white.Alpha(0.1))
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(6).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cm.User.Nickname).FontSize(13).FontWeight(600).TextColor(white.Alpha(0.6)).SingleLine().Shrink(1).MinWidth(0)
				ui.Spacer(c)
				a.commentLike(c.Key("like"), cm)
			})
			ui.Text(c, cm.Content).FontSize(15).LineHeight(1.55).TextColor(white.Alpha(0.92))
			if len(cm.BeReplied) > 0 {
				r := cm.BeReplied[0]
				ui.Box(c).Padding(8, 12).Radius(10).Background(white.Alpha(0.07)).Children(func() {
					ui.RichText(c,
						ui.Span{Text: "@" + r.User.Nickname + "：", Color: white.Alpha(0.75), Weight: 600},
						ui.Span{Text: r.Content, Color: white.Alpha(0.6)},
					).FontSize(13).LineHeight(1.5).MaxLines(3)
				})
			}
			meta := commentTime(time.UnixMilli(cm.Time), now)
			if loc := cm.IPLocation.Location; loc != "" {
				meta += " · " + loc
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, meta).FontSize(11.5).TextColor(white.Alpha(0.38)).Shrink(1).MinWidth(0)
				a.commentReply(c, cm)
			})
		})
	})
}

// commentReply is the 回复 under a comment, which takes the composer to
// answering that comment.
func (a *app) commentReply(c *ui.Context, cm *netease.Comment) {
	fg := white.Alpha(0.4)
	b := ui.ButtonBase(c.Key("reply")).Padding(1, 4).Radius(6)
	if b.Hovered() {
		fg = white.Alpha(0.85)
	}
	b.Children(func() { ui.Text(c, "回复").FontSize(11.5).TextColor(fg) })
	if b.Clicked() {
		cs := &a.comments
		cs.answer, cs.answering = *cm, true
	}
}

// commentComposer is where the listener writes a comment, or answers one:
// who it answers when it does, the field, and the button that posts it.
// Signed out, it asks to sign in instead.
func (a *app) commentComposer(c *ui.Context) {
	cs := &a.comments
	ui.Column(c).Padding(6, 12, 10, 12).Gap(6).Children(func() {
		if a.profile == nil {
			if b := pillButton(c.Key("signin"), iconComment, "登录后可以评论", white.Alpha(0.1), white.Alpha(0.75)); b.Clicked() {
				a.openLogin()
			}
			return
		}
		if cs.answering {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "回复 @"+cs.answer.User.Nickname).FontSize(12).TextColor(white.Alpha(0.5)).SingleLine().Shrink(1).MinWidth(0)
				ui.Spacer(c)
				b := ui.ButtonBase(c.Key("cancel-reply")).Padding(2, 6).Radius(6)
				b.Children(func() { ui.Text(c, "取消").FontSize(12).TextColor(white.Alpha(0.5)) })
				if b.Clicked() {
					cs.answering = false
				}
			})
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c.Key("draft"), &cs.draft).Placeholder("说点什么…").Label("评论").
				FontSize(13).Height(36).PaddingX(14).Radius(999).
				Background(white.Alpha(0.08)).TextColor(white).Grow(1).MinWidth(0)
			if in.Submitted() {
				a.submitComment()
			}
			send := pillButton(c.Key("send"), nil, "发送", a.pal.accent, a.pal.onAccent)
			if strings.TrimSpace(cs.draft) == "" || cs.sending {
				send.Disabled(true).Opacity(0.4)
			}
			if send.Clicked() {
				a.submitComment()
			}
		})
	})
}

// submitComment posts what the listener wrote, a comment or the answer to
// one, and then shows the 最新 page from its first, with theirs at the top.
func (a *app) submitComment() {
	cs := &a.comments
	text := strings.TrimSpace(cs.draft)
	if text == "" || cs.sending {
		return
	}
	if a.profile == nil {
		a.openLogin()
		return
	}
	thread, answer, answering := cs.thread, cs.answer, cs.answering
	cs.sending = true
	go func() {
		var err error
		if answering {
			err = replyComment(thread, answer, text)
		} else {
			err = sendComment(thread, text)
		}
		a.post(func() { a.wroteComment(thread, err) })
	}()
}

// wroteComment takes what came of posting what the listener wrote: the field
// empties and the 最新 page comes again with theirs at the top, or the
// comment stays written for them to try again.
func (a *app) wroteComment(thread string, err error) {
	cs := &a.comments
	if cs.thread != thread {
		return
	}
	cs.sending = false
	if err != nil {
		a.toast = "评论发送失败，请稍后再试"
		return
	}
	cs.draft, cs.answering = "", false
	a.toast = "评论已发送"
	a.refreshComments()
}

// refreshComments loads the newest comments from their first page again, so
// the one just written shows at the top of them.
func (a *app) refreshComments() {
	cs := &a.comments
	cs.latest = commentPage{}
	cs.active = commentTabNew
	a.loadComments()
}

// commentLike is a comment's thumb, with how many liked it, which likes it
// or unlikes it: in red once liked. Where the service the app reads takes
// no like, the thumb stays a count.
func (a *app) commentLike(c *ui.Context, cm *netease.Comment) {
	if !commentsLikeable() {
		if cm.LikedCount > 0 {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconThumb).Size(13, 13).TextColor(white.Alpha(0.26))
				ui.Text(c, count(cm.LikedCount)).FontSize(12).FontFeatures("tnum").TextColor(white.Alpha(0.3))
			})
		}
		return
	}
	label, fg := "赞", white.Alpha(0.42)
	if cm.Liked {
		label, fg = "取消赞", a.pal.accent
	}
	b := ui.ButtonBase(c).Gap(4).Padding(3, 6).Margin(-3, -6).Radius(999).AlignItems(ui.Center).FocusRing(false).Label(label)
	if b.Hovered() && !cm.Liked {
		fg = white.Alpha(0.75)
	}
	b.TextColor(b.AnimateColor("fg", fg, ui.Animation{Duration: 120 * time.Millisecond}))
	b.Children(func() {
		icon := iconThumb
		if cm.Liked {
			icon = iconThumbFill
		}
		ui.Icon(c, icon).Size(13, 13)
		if cm.LikedCount > 0 {
			ui.Text(c, count(cm.LikedCount)).FontSize(12).FontFeatures("tnum")
		}
	})
	if b.Clicked() {
		a.toggleCommentLike(cm.ID)
	}
}

// toggleCommentLike likes a comment, or unlikes it, at once in the app and
// then with NetEase, which may refuse: it goes back then. Signed out, it
// asks the listener to sign in.
func (a *app) toggleCommentLike(id int64) {
	if a.profile == nil {
		a.openLogin()
		return
	}
	cs := &a.comments
	thread, like := cs.thread, true
	if cm := cs.find(id); cm != nil {
		like = !cm.Liked
	}
	cs.setLiked(id, like)
	go func() {
		err := likeComment(thread, id, like)
		a.post(func() {
			if err != nil && cs.thread == thread {
				cs.setLiked(id, !like)
				a.toast = "操作失败，请稍后再试"
			}
		})
	}()
}

// find returns the comment id, on either page, or nil.
func (cs *commentState) find(id int64) *netease.Comment {
	for _, list := range [][]netease.Comment{cs.hot.list, cs.latest.list} {
		for i := range list {
			if list[i].ID == id {
				return &list[i]
			}
		}
	}
	return nil
}

// setLiked marks the comment id liked, or not, counting the listener in or
// out, both where it is 精彩 and where it is 最新.
func (cs *commentState) setLiked(id int64, like bool) {
	for _, list := range [][]netease.Comment{cs.hot.list, cs.latest.list} {
		for i := range list {
			if cm := &list[i]; cm.ID == id && cm.Liked != like {
				cm.Liked = like
				if like {
					cm.LikedCount++
				} else {
					cm.LikedCount = max(cm.LikedCount-1, 0)
				}
			}
		}
	}
}

// avatar shows a listener's picture, round, over empty while it loads.
func (a *app) avatar(c *ui.Context, url string, size float32, empty ui.Color) {
	bmp := bitmap(a.art.get(netease.Sized(url, int(size*3)), false))
	box := ui.Box(c).Size(size, size).Radius(size / 2).Shrink(0)
	in := artIn(box, bmp)
	if in < 1 {
		box.Background(empty)
	}
	box.Children(func() { artImage(c, bmp, size/2, in) })
}

// commentTime says when a comment was written, as NetEase does: in
// minutes or hours while recent, then by the day.
func commentTime(t, now time.Time) string {
	d := now.Sub(t)
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d分钟前", int(d/time.Minute))
	case y1 == y2 && m1 == m2 && d1 == d2:
		return t.Format("15:04")
	case now.AddDate(0, 0, -1).Format("20060102") == t.Format("20060102"):
		return "昨天 " + t.Format("15:04")
	case y1 == y2:
		return fmt.Sprintf("%d月%d日", m1, d1)
	default:
		return fmt.Sprintf("%d年%d月%d日", y1, m1, d1)
	}
}
