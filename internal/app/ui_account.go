package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"rsc.io/qr"

	"netease-music-mygo/internal/netease"
)

// debugLog writes what signing in did to login.log in the user data
// directory, once per run. A login that answers 登录成功 and leaves the app
// signed out is otherwise invisible: the window has no console to print to.
var (
	debugOnce sync.Once
	debugFile *os.File
	debugMu   sync.Mutex
)

func debugLog(format string, args ...any) {
	debugOnce.Do(func() {
		dir, err := mygo.App.Path(mygo.PathUserData)
		if err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(dir, "login.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err == nil {
			debugFile = f
		}
	})
	if debugFile == nil {
		return
	}
	debugMu.Lock()
	defer debugMu.Unlock()
	fmt.Fprintf(debugFile, "%s %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}

// loginState is a login under way, by QR code or by texted code.
type loginState struct {
	open     bool
	gen      int // counts logins, to stop the polls of closed ones
	key      string
	code     *qr.Code
	status   int // a QR state, or 0 while the code loads
	polling  bool // whether a poll of the code is running
	scanner  string
	avatar   string
	err      string
	finished bool

	// By text message: the number, the code texted to it, and how long
	// until another may be asked for.
	mode      int // 0: a QR code, 1: a phone number and its code
	phone     string
	sms       string
	sending   bool
	countdown int
	trying    bool
}

// These sign in with a code KuGou texts; tests replace them.
var (
	sendLoginCode = netease.SendLoginCode
	loginWithCode = netease.LoginWithCode
)

// refreshAccount reads who is signed in again, for the badge that changed
// with the player's mode rather than with the account.
func (a *app) refreshAccount() {
	if a.profile == nil {
		return
	}
	go func() {
		prof, err := netease.RefreshAccount()
		if err != nil || prof == nil {
			return
		}
		a.post(func() { a.profile = prof })
	}()
}

// refreshAccountSoon asks again a few seconds later, for what the services
// turned away while the app was starting: the account service answers HTTP 403
// to the burst of requests the first moments are, and the picture is worth one
// more try once that has passed.
func (a *app) refreshAccountSoon() {
	go func() {
		time.Sleep(6 * time.Second)
		prof, err := netease.RefreshAccount()
		if err != nil || prof == nil {
			return
		}
		a.post(func() {
			if a.profile == nil || a.profile.UserID != prof.UserID {
				return
			}
			debugLog("account again %+v", prof)
			a.profile = prof
		})
	}()
}

// boot signs in with the session in the keychain, if any, then loads the
// home page for whoever is listening.
func (a *app) boot() {
	a.homeLoading = true
	// The mode decides how every request is signed, so it is set before the
	// session is restored and before anything is asked of KuGou.
	netease.SetConcept(a.settings.Concept)
	go func() {
		var prof *netease.Profile
		if netease.RestoreSession() {
			// The subscriptions may have been bought or lapsed since the
			// login, and the badge is worthless if it is stale.
			p, err := netease.RefreshAccount()
			switch {
			case err == nil && p == nil:
				netease.ClearSession() // expired
			case err == nil:
				prof = p
			}
		}
		a.post(func() {
			a.signedIn(prof)
		})
	}()
}

// signedIn shows what is personal to prof, or to nobody.
func (a *app) signedIn(prof *netease.Profile) {
	debugLog("signedIn %+v", prof)
	a.profile = prof
	// Playback picks up where it was left, once the session that lets
	// the listener play their songs is back.
	a.restoreSession()
	a.mine, a.saved, a.recPlaylists, a.liked = nil, nil, nil, nil
	a.likedPrograms, a.askedPrograms = nil, nil
	// What the listener may play, and follows, changed: load the pages
	// again.
	a.playlists = map[int64]*netease.Playlist{}
	a.plErr = map[int64]string{}
	a.artists, a.albums, a.users, a.podcasts = map[int64]*artistData{}, map[int64]*albumData{}, map[int64]*userData{}, map[int64]*podcastData{}
	a.podcastTab = &podcastTab{}
	a.searches = map[route]*searchData{}
	a.load(a.route)
	a.loadHome()
	if prof == nil {
		return
	}
	a.loadLiked(prof.UserID)
	a.loadMine(prof.UserID)
	// The picture and the badge are all the account service has, and it
	// refuses a burst: ask again once starting up is over.
	if prof.AvatarURL == "" || prof.Label == "" {
		a.refreshAccountSoon()
	}
}

func (a *app) openLogin() {
	// What was typed for a code is kept, as switching away and back is
	// no reason to ask for it again.
	phone, sms := a.login.phone, a.login.sms
	a.login = loginState{open: true, gen: a.login.gen + 1, phone: phone, sms: sms}
	gen := a.login.gen
	debugLog("open, gen %d, mode %d", gen, a.login.mode)
	go func() {
		key, err := netease.QRKey()
		var code *qr.Code
		if err == nil {
			code, err = qr.Encode(netease.LoginURL(key), qr.M)
		}
		a.post(func() {
			if gen != a.login.gen {
				return
			}
			if err != nil {
				a.login.err = "无法获取二维码，请检查网络"
				return
			}
			a.login.key, a.login.code, a.login.status = key, code, netease.QRWaiting
			a.pollLogin(gen, key)
		})
	}()
}

// pollLogin asks every two seconds whether the code was scanned, until the
// login ends or the dialog closes.
func (a *app) pollLogin(gen int, key string) {
	a.login.polling = true
	go func() {
		defer a.post(func() {
			if gen == a.login.gen {
				a.login.polling = false
			}
		})
		for {
			time.Sleep(2 * time.Second)
			code, msg, name, avatar, err := netease.QRCheck(key)
			debugLog("poll: code=%d msg=%q err=%v", code, msg, err)
			var prof *netease.Profile
			if code == netease.QRConfirmed {
				netease.SaveSession()
				prof, _ = netease.Account()
				debugLog("qr confirmed: prof=%+v", prof)
			}
			done := make(chan bool, 1)
			a.post(func() {
				l := &a.login
				if gen != l.gen || !l.open {
					debugLog("poll dropped: gen %d, now %d, open=%v", gen, l.gen, l.open)
					done <- true
					return
				}
				switch {
				case err != nil:
					// Try again: the network may come back.
				case code == netease.QRConfirmed:
					l.status, l.finished = code, true
					a.signedIn(prof)
					if prof != nil {
						a.pickUp(prof.UserID)
					}
					a.closeLoginSoon(gen)
				case code == netease.QRExpired || code == netease.QRWaiting || code == netease.QRScanned:
					l.status = code
					if name != "" {
						l.scanner, l.avatar = name, avatar
					}
				default:
					l.err = msg
					if l.err == "" {
						l.err = "登录失败，请稍后再试"
					}
				}
				done <- l.status == netease.QRExpired || l.finished || l.err != "" || l.mode != 0
			})
			if <-done {
				return
			}
		}
	}()
}

// closeLoginSoon closes the dialog a moment after it showed the welcome.
func (a *app) closeLoginSoon(gen int) {
	go func() {
		time.Sleep(1200 * time.Millisecond)
		a.post(func() {
			if gen == a.login.gen {
				a.login.open = false
			}
		})
	}()
}

func (a *app) signOut() {
	a.login.gen++
	a.signedIn(nil)
	go netease.Logout()
}

// loginDialog shows the QR code to scan with the KuGou app on a phone, or
// the number and the code KuGou texts to it.
func (a *app) loginDialog(c *ui.Context) {
	p := a.pal
	l := &a.login
	ui.DialogBase(c.Key("login"), &l.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(340).Padding(28, 28, 26, 28).Radius(22).Gap(6).AlignItems(ui.Center).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		title, sub := "扫码登录", "打开酷狗音乐 App，扫一扫登录"
		if netease.Concept() {
			// The mode decides whose token the login hands back, and only
			// that token may play what the 概念版 sells, so the dialog says
			// which one it is asking for.
			title, sub = "概念版登录", "以酷狗概念版的身份登录，会员歌曲可听标准音质"
		}
		if l.mode == 1 {
			sub = "用手机号接收验证码登录"
		}
		switch {
		case l.finished:
			title, sub = "登录成功", "欢迎回来，"+a.profileName()
		case l.err != "":
			sub = l.err
		case l.mode == 0 && l.status == netease.QRScanned:
			title, sub = "扫描成功", "请在手机上确认登录"
		case l.mode == 0 && l.status == netease.QRExpired:
			sub = "二维码已失效"
		}
		ui.Text(c, title).FontSize(20).FontWeight(700)
		ui.Text(c, sub).FontSize(13).TextColor(p.muted).TextAlign(ui.Center).
			LineHeight(1.4).Margin(0, 0, 12, 0)

		if !l.finished {
			ui.Row(c).Gap(4).Margin(0, 0, 12, 0).Children(func() {
				a.loginTab(c, 0, "扫码登录")
				a.loginTab(c, 1, "手机号登录")
			})
		}
		if l.mode == 1 && !l.finished {
			a.phoneLoginBox(c)
			ui.Text(c, "登录后，我喜欢与歌单会同步到你的酷狗账号").FontSize(11.5).TextColor(p.faint).Margin(14, 0, 0, 0)
			return
		}

		ui.Box(c).Size(212, 212).Radius(16).Background(ui.RGB(255, 255, 255)).Border(1, p.hairline).Children(func() {
			if l.code != nil {
				code := l.code
				opacity := float32(1)
				if l.status == netease.QRScanned || l.status == netease.QRExpired || l.finished || l.err != "" {
					opacity = 0.12
				}
				ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Opacity(opacity).
					Draw(func(pt *ui.Painter, r ui.Rect) { drawQR(pt, r, code, 14) })
			} else if l.err == "" {
				// On white whatever the appearance, as the code is.
				s := shimmer{base: ui.RGBA(0, 0, 0, 0.08), sheen: ui.RGBA(0, 0, 0, 0.03)}.in(c)
				qrSkeleton(c, s, 14).Absolute().Top(0).Left(0).Right(0).Bottom(0)
			}
			ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Gap(10).Children(func() {
				switch {
				case l.finished || l.status == netease.QRScanned:
					if bmp := bitmap(a.art.get(netease.Sized(l.avatar, 160), false)); bmp != nil {
						pic := ui.Box(c).Size(72, 72).Radius(36).Shadow(0, 6, 16, -4, ui.RGBA(0, 0, 0, 0.3))
						in := artIn(pic, bmp)
						pic.Children(func() { artImage(c, bmp, 36, in) })
					}
					if l.scanner != "" {
						ui.Text(c, l.scanner).FontSize(14).FontWeight(600).TextColor(ui.Hex("#1d1d1f"))
					}
				case l.status == netease.QRExpired || l.err != "":
					if pillButton(c.Key("refresh"), iconRepeat, "刷新二维码", p.accent, p.onAccent).Clicked() {
						a.openLogin()
					}
				}
			})
		})
		ui.Text(c, "登录后，每日推荐与歌单将根据你的口味生成").FontSize(11.5).TextColor(p.faint).Margin(16, 0, 0, 0)
	})
}

// loginTab switches the dialog between the QR code and the phone number.
func (a *app) loginTab(c *ui.Context, mode int, label string) {
	p := a.pal
	l := &a.login
	bg, fg := p.hover.Over(p.bg), p.text
	if l.mode == mode {
		bg, fg = p.accent, p.onAccent
	}
	if pillButton(c.Key("mode-"+strconv.Itoa(mode)), nil, label, bg, fg).Clicked() && l.mode != mode {
		l.mode, l.err = mode, ""
		if mode == 0 {
			// The poll stops while the phone form shows, so going back to
			// the code starts one again, with a code that is fresh.
			if !l.polling {
				a.openLogin()
			}
		} else {
			l.polling = false
		}
	}
}

// phoneLoginBox is the number, the code, and the way to ask for one.
func (a *app) phoneLoginBox(c *ui.Context) {
	p := a.pal
	l := &a.login
	ui.TextInput(c.Key("phone"), &l.phone).Placeholder("手机号").Label("手机号").FontSize(14).AutoFocus()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		in := ui.TextInput(c.Key("sms"), &l.sms).Placeholder("验证码").Label("验证码").FontSize(14)
		if in.Submitted() {
			a.submitCode()
		}
		label := "获取验证码"
		switch {
		case l.sending:
			label = "发送中…"
		case l.countdown > 0:
			label = strconv.Itoa(l.countdown) + " 秒后重发"
		}
		b := pillButton(c.Key("send"), nil, label, p.hover.Over(p.bg), p.text)
		if l.sending || l.countdown > 0 || !phoneOK(strings.TrimSpace(l.phone)) {
			b.Disabled(true).Opacity(0.5)
		}
		if b.Clicked() {
			a.sendCode()
		}
	})
	ui.Row(c).Gap(10).Justify(ui.End).Margin(6, 0, 0, 0).Children(func() {
		if pillButton(c.Key("cancel"), nil, "取消", p.hover.Over(p.bg), p.text).Clicked() {
			l.open = false
		}
		label := "验证码登录"
		if l.trying {
			label = "正在登录…"
		}
		ok := pillButton(c.Key("ok"), nil, label, p.accent, p.onAccent)
		if l.trying || len(strings.TrimSpace(l.sms)) < 4 {
			ok.Disabled(true).Opacity(0.5)
		}
		if ok.Clicked() {
			a.submitCode()
		}
	})
}

// sendCode asks KuGou to text a code to the number the dialog holds.
func (a *app) sendCode() {
	l := &a.login
	phone := strings.TrimSpace(l.phone)
	if !phoneOK(phone) {
		l.err = "请输入 11 位手机号"
		return
	}
	l.err, l.sending = "", true
	gen := l.gen
	debugLog("asking for a code for %s", phone)
	go func() {
		err := sendLoginCode(phone)
		a.post(func() { a.codeSent(gen, err) })
	}()
}

// codeSent takes KuGou's answer to the request for a code.
func (a *app) codeSent(gen int, err error) {
	l := &a.login
	debugLog("code asked: err=%v (gen %d, now %d)", err, gen, l.gen)
	if gen != l.gen {
		return
	}
	l.sending = false
	if err != nil {
		l.err = loginError(err)
		return
	}
	l.countdown = 60
	a.countDown(gen)
}

// submitCode trades the number and the code for a session.
func (a *app) submitCode() {
	l := &a.login
	phone, sms := strings.TrimSpace(l.phone), strings.TrimSpace(l.sms)
	if !phoneOK(phone) || len(sms) < 4 || l.trying {
		return
	}
	l.err, l.trying = "", true
	gen := l.gen
	debugLog("submitting %s with a %d digit code (gen %d)", phone, len(sms), gen)
	go func() {
		prof, err := loginWithCode(phone, sms)
		a.post(func() { a.codeTried(gen, prof, err) })
	}()
}

// codeTried takes KuGou's answer to the number and the code.
func (a *app) codeTried(gen int, prof *netease.Profile, err error) {
	l := &a.login
	debugLog("answer: prof=%+v err=%v (gen %d, now %d)", prof, err, gen, l.gen)
	if gen != l.gen {
		return
	}
	l.trying = false
	if err != nil {
		l.err = loginError(err)
		return
	}
	l.finished = true
	a.signedIn(prof)
	if prof != nil {
		a.pickUp(prof.UserID)
	}
	a.closeLoginSoon(gen)
}

// countDown ticks the wait before another code may be asked for.
func (a *app) countDown(gen int) {
	go func() {
		for {
			time.Sleep(time.Second)
			done := make(chan bool, 1)
			a.post(func() {
				l := &a.login
				if gen != l.gen || !l.open {
					done <- true
					return
				}
				if l.countdown > 0 {
					l.countdown--
				}
				done <- l.countdown == 0
			})
			if <-done {
				return
			}
		}
	}()
}

// loginError says why a login failed, in words the listener knows.
func loginError(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "kugou: ")
	switch {
	case msg == "":
		return "登录失败，请稍后再试"
	case strings.Contains(msg, "验证码"), strings.Contains(msg, "code"),
		strings.Contains(msg, "账号"), strings.Contains(msg, "手机"):
		return msg
	}
	return "登录失败：" + msg
}

func phoneOK(s string) bool {
	if len(s) != 11 || s[0] != '1' {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// drawQR draws a QR code in r, as crisp squares on white, inside a quiet
// zone of pad.
func drawQR(p *ui.Painter, r ui.Rect, code *qr.Code, pad float32) {
	n := code.Size
	cell := (min(r.W, r.H) - 2*pad) / float32(n)
	x0 := r.X + (r.W-cell*float32(n))/2
	y0 := r.Y + (r.H-cell*float32(n))/2
	ink := ui.Hex("#141416")
	for y := range n {
		for x := range n {
			if code.Black(x, y) {
				// A hair larger, so neighbors join without seams.
				p.Fill(ui.Rect{X: x0 + float32(x)*cell, Y: y0 + float32(y)*cell, W: cell + 0.3, H: cell + 0.3}, ink, 0)
			}
		}
	}
}

func (a *app) profileName() string {
	if a.profile == nil {
		return ""
	}
	return a.profile.Nickname
}

// accountRow sits at the foot of the sidebar: who is signed in, with a
// dropdown to open their page or the settings or sign out, or a way to
// sign in.
func (a *app) accountRow(c *ui.Context) {
	p := a.pal
	b := ui.ButtonBase(c.Key("account")).Height(40).PaddingX(8).Gap(10).Radius(10).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || a.accountOpen && a.profile != nil {
		b.Background(p.hover)
	}
	if a.profile == nil {
		a.accountOpen = false
		b.Label("登录").Children(func() {
			ui.Box(c).Size(26, 26).Radius(13).Background(p.press).Center().Children(func() {
				ui.Icon(c, iconUser).Size(14, 14).TextColor(p.muted)
			})
			ui.Text(c, "登录酷狗音乐").FontSize(13).FontWeight(500).TextColor(p.muted)
		})
		if b.Clicked() {
			a.openLogin()
		}
		return
	}
	prof := a.profile
	b.Label(prof.Nickname).Expanded(a.accountOpen).Children(func() {
		a.avatar(c, prof.AvatarURL, 26, p.press)
		ui.Text(c, prof.Nickname).FontSize(13).FontWeight(600).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		// What the account holds, beside its name: a subscription is worth
		// seeing without opening the listener's page.
		if prof.Label != "" {
			vipTag(c, p.accent, prof.Label)
		}
		ui.Icon(c, iconUpDown).Size(13, 13).TextColor(p.faint)
	})
	if b.Clicked() {
		a.accountOpen = !a.accountOpen
	}
	a.accountMenu(c, b)
}

// accountMenu drops up from the account row, as wide as it, in the colors
// of the volume popover: the listener's own page and the settings, then
// signing out.
func (a *app) accountMenu(c *ui.Context, row ui.Element) {
	pop := popoverColors(a.pal.dark)
	ui.PopoverBase(c.Key("account-menu"), row, &a.accountOpen, func(panel ui.Element) {
		panel.AttachTo(row, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 6, 0).Padding(5).Gap(1).
			Radius(12).Background(pop.fill).Border(0.5, pop.edge).Shadow(0, 8, 24, -4, ui.RGBA(0, 0, 0, 0.2)).
			Role(ui.RoleMenu).FocusGroup(ui.Vertical).Label("账户").TransformOrigin(0.5, 1).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.96, Y: 4}, Exit: &ui.Motion{Scale: 0.98}})
		if w := row.Bounds().W; w > 0 {
			panel.Width(w)
		}
		shortcut := "Ctrl+,"
		if runtime.GOOS == "darwin" {
			shortcut = "⌘,"
		}
		if a.menuItem(c, iconUser, "我的主页", "").AutoFocus().Clicked() {
			a.accountOpen = false
			a.open(route{page: pageUser, id: a.profile.UserID})
		}
		if a.menuItem(c, iconSettings, "设置…", shortcut).Clicked() {
			a.accountOpen = false
			a.openSettings()
		}
		ui.Box(c).Height(0.5).Margin(4, 8).Background(pop.edge)
		if a.menuItem(c, iconSignOut, "退出登录", "").Clicked() {
			a.accountOpen = false
			a.signOut()
		}
	})
}

// menuItem is an item of a dropdown: an icon, its label, and the shortcut
// that does the same, if any.
func (a *app) menuItem(c *ui.Context, icon *ui.SVG, label, shortcut string) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key(label)).Role(ui.RoleMenuItem).Label(label).Height(30).PaddingX(8).Gap(9).Radius(7).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || b.FocusVisible() {
		b.Background(p.press)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(15, 15).TextColor(p.muted)
		ui.Text(c, label).FontSize(13).Grow(1)
		if shortcut != "" {
			ui.Text(c, shortcut).FontSize(12).TextColor(p.faint)
		}
	})
	return b
}

// myPlaylists lists the listener's playlists under a header that opens
// and closes them: those they made, if mine, with a button to make one
// and a menu to rename them, or else with a menu to unsave them.
func (a *app) myPlaylists(c *ui.Context, title string, lists []netease.Playlist, mine bool, collapsed *bool) {
	if len(lists) == 0 {
		return
	}
	p := a.pal
	ui.Row(c.Key(title)).AlignItems(ui.Center).Margin(18, 4, 6, 6).Children(func() {
		b := ui.ButtonBase(c.Key("toggle")).Grow(1).Height(26).PaddingX(4).Gap(6).Radius(6).
			AlignItems(ui.Center).Justify(ui.Start).Role(ui.RoleDisclosure).Label(title)
		if b.Clicked() {
			*collapsed = !*collapsed
		}
		b.Expanded(!*collapsed)
		if b.Hovered() {
			b.Background(p.hover)
		}
		b.Children(func() {
			ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(p.faint).Grow(1)
			arrow := iconDown
			if *collapsed {
				arrow = iconForward
			}
			ui.Icon(c, arrow).Size(12, 12).TextColor(p.faint)
		})
		if mine && iconButton(c.Key("new"), iconPlus, 13, p.faint, "新建歌单").Size(26, 26).Radius(6).Clicked() {
			a.newPlaylist(nil)
		}
	})
	if *collapsed {
		return
	}
	for i := range lists {
		pl := &lists[i]
		selected := a.route.page == pagePlaylist && a.route.id == pl.ID && !a.nowPlaying
		b := ui.ButtonBase(c.Key(pl.ID)).Height(34).PaddingX(8).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(pl.Name)
		switch {
		case selected:
			b.Background(p.press)
		case b.Hovered():
			b.Background(p.hover)
		}
		b.Children(func() {
			a.cover(c, pl.Cover(), 80, 22, 5)
			ui.Text(c, pl.Name).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
		})
		switch {
		case mine && pl.SpecialType != 5:
			b.ContextMenu(func(m *ui.Menu) {
				if m.Item("重命名…").Chosen() {
					a.startRename(pl)
				}
				m.Separator()
				if m.Item("删除歌单…").Chosen() {
					a.confirmDelete(pl)
				}
			})
		case !mine:
			b.ContextMenu(func(m *ui.Menu) {
				if m.Item("取消收藏").Chosen() {
					a.toggleSaved(*pl)
				}
			})
		}
		if b.Clicked() {
			a.open(route{page: pagePlaylist, id: pl.ID})
		}
	}
}
