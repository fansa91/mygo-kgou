package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// userDetail and userPlaylists load a user and their playlists from
// NetEase, and followedArtists and subscribedPodcasts what the listener
// follows; tests replace them.
var (
	userDetail         = netease.UserDetail
	userPlaylists      = netease.UserPlaylists
	followedArtists    = netease.FollowedArtists
	subscribedPodcasts = netease.SubscribedPodcasts
)

// userData is what the app has of a user: who they are, and the playlists
// they made that anyone may see. Of the listener's own page, it has the
// artists they follow and the podcasts they subscribe to too, which
// NetEase shows no one else.
type userData struct {
	user      *netease.User
	playlists []netease.Playlist
	loading   int // how many of the two are on their way
	err       string

	artists       []netease.ArtistProfile
	podcasts      []netease.Podcast
	artistsShown  bool // whether artists has loaded once
	podcastsShown bool
	subsLoading   int
}

func (a *app) loadUser(id int64) {
	d := a.users[id]
	if d == nil {
		d = &userData{}
		a.users[id] = d
	}
	if a.profile != nil && a.profile.UserID == id {
		a.loadSubscriptions(d)
	}
	if d.user != nil || d.loading > 0 {
		return
	}
	d.loading, d.err = 2, ""
	go func() {
		u, err := userDetail(id)
		a.post(func() {
			d.loading--
			if err != nil {
				d.err = "用户加载失败"
				return
			}
			d.user = u
		})
	}()
	go func() {
		made, _, err := userPlaylists(id)
		a.post(func() {
			d.loading--
			if err == nil {
				d.playlists = publicPlaylists(made, id)
			}
		})
	}()
}

// loadSubscriptions loads the artists the listener follows and the
// podcasts they subscribe to, each time their page shows: they follow and
// subscribe on other pages, and these keep up. What they had shows while
// the lists are on their way.
func (a *app) loadSubscriptions(d *userData) {
	if d.subsLoading > 0 {
		return
	}
	d.subsLoading = 2
	go func() {
		ars, err := followedArtists()
		a.post(func() {
			d.subsLoading--
			if err == nil {
				d.artists, d.artistsShown = ars, true
			}
		})
	}()
	go func() {
		pods, err := subscribedPodcasts()
		a.post(func() {
			d.subsLoading--
			if err == nil {
				d.podcasts, d.podcastsShown = pods, true
			}
		})
	}()
}

// publicPlaylists are the playlists of lists a user made that anyone may
// see: their own, as made lists those they saved too, but not their
// private ones.
func publicPlaylists(lists []netease.Playlist, uid int64) []netease.Playlist {
	var out []netease.Playlist
	for _, pl := range lists {
		if pl.Creator.UserID == uid && pl.Privacy != 10 {
			out = append(out, pl)
		}
	}
	return out
}

func (a *app) userPage(c *ui.Context) {
	p := a.pal
	id := a.route.id
	d := a.users[id]
	if d.user == nil {
		if d.loading == 0 {
			a.centered(c, d.err, func() { a.loadUser(id) })
			return
		}
		s := a.pageShimmer(c)
		a.scrollPage(c, func() {
			a.playlistHeaderSkeleton(c, s)
			a.playlistCardsSkeleton(c, s, a.columns(c, 168), 168, false)
		})
		return
	}
	u := d.user
	own := a.profile != nil && a.profile.UserID == id
	size := a.profilePhotoSize()
	a.scrollPage(c, func() {
		ui.Row(c).Gap(a.profileGap()).AlignItems(ui.End).Children(func() {
			ui.Box(c).Size(size, size).Radius(size/2).Shadow(0, 18, 40, -10, p.cardShade).Children(func() {
				a.coverFill(c, u.AvatarURL, 600, size/2)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
				ui.Text(c, "用户").FontSize(12).FontWeight(600).TextColor(p.accent)
				ui.Text(c, u.Nickname).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
				ui.Text(c, fmt.Sprintf("%s 关注 · %s 粉丝", count(u.Follows), count(u.Followeds))).FontSize(13).TextColor(p.muted)
				if own {
					a.memberRow(c)
				}
				if u.Signature != "" {
					ui.Text(c, u.Signature).FontSize(12.5).TextColor(p.muted).MaxLines(2).LineHeight(1.45).Margin(2, 0, 0, 0)
				}
			})
		})
		a.section(c, "创建的歌单")
		switch {
		case d.loading > 0:
			a.playlistCardsSkeleton(c, a.pageShimmer(c), a.columns(c, 168), 168, false)
		case len(d.playlists) == 0:
			a.empty(c, "暂无公开歌单")
		default:
			ui.Grid(c).Columns(a.columns(c, 168)).GapX(20).GapY(28).Children(func() {
				for i := range d.playlists {
					a.playlistCard(c.Key(d.playlists[i].ID), &d.playlists[i])
				}
			})
		}
		if own && d.podcastsShown {
			a.section(c, "订阅的听书")
			if len(d.podcasts) == 0 {
				a.empty(c, "还没有订阅听书")
			}
			ui.Grid(c).Columns(a.columns(c, 340)).GapX(8).GapY(4).Margin(0, -12).Children(func() {
				for i := range d.podcasts {
					a.podcastRow(c.Key(d.podcasts[i].ID), &d.podcasts[i])
				}
			})
		}
		if own && d.artistsShown {
			a.section(c, "关注的歌手")
			if len(d.artists) == 0 {
				a.empty(c, "还没有关注歌手")
			}
			ui.Grid(c).Columns(a.columns(c, 132)).GapX(20).GapY(28).Children(func() {
				for i := range d.artists {
					a.artistCard(c.Key(d.artists[i].ID), &d.artists[i])
				}
			})
		}
	})
}

// memberRow is what the listener's own account pays for, and the day each
// subscription runs out. The 概念版 sells two of them — 超级会员 and
// 豪华会员 — and an account may hold both at once, each ending on its own
// day, so each is said apart rather than as one date.
func (a *app) memberRow(c *ui.Context) {
	prof := a.profile
	if prof == nil {
		return
	}
	p := a.pal
	var parts []netease.VIPProduct
	for _, prod := range prof.Products {
		if prod.Name != "" {
			parts = append(parts, prod)
		}
	}
	if len(parts) == 0 {
		if prof.Label == "" {
			return
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			vipTag(c, p.accent, prof.Label)
			ui.Text(c, "暂无到期时间").FontSize(12.5).TextColor(p.faint)
		})
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Margin(2, 0, 0, 0).Children(func() {
		for _, prod := range parts {
			label := prod.Name
			if day := vipDay(prod.End); day != "" {
				label += " · " + day + " 到期"
			}
			ui.Text(c, label).FontSize(11.5).FontWeight(600).TextColor(p.accent).
				Padding(3, 9).Radius(999).Background(p.accent.Alpha(0.1)).SingleLine()
		}
	})
}

// minePage is the phone's account home: the listener's profile, entries to
// the parts of their library, and the way into settings. It is the root of
// the 我的 tab; settings is one page pushed on top of it.
func (a *app) minePage(c *ui.Context) {
	p := a.pal
	if a.profile == nil {
		a.scrollPage(c, func() {
			ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(18).Padding(0, 32).Children(func() {
				ui.Box(c).Size(84, 84).Radius(42).Background(p.hover).Center().Children(func() {
					ui.Icon(c, iconUser).Size(38, 38).TextColor(p.faint)
				})
				ui.Text(c, "登录后查看你的音乐").FontSize(16).FontWeight(600)
				ui.Text(c, "同步歌单、收藏与评论").FontSize(13).TextColor(p.muted)
				if pillButton(c, nil, "登录", p.accent, p.onAccent).Clicked() {
					a.openLogin()
				}
			})
		})
		return
	}

	id := a.profile.UserID
	d := a.users[id]
	size := a.profilePhotoSize()
	a.scrollPage(c, func() {
		// Profile card: avatar, name and the account's standing.
		ui.Row(c).Gap(a.profileGap()).AlignItems(ui.Center).Margin(0, 0, 8, 0).Children(func() {
			ui.Box(c).Size(size, size).Radius(size/2).Shadow(0, 18, 40, -10, p.cardShade).Children(func() {
				a.coverFill(c, a.profile.AvatarURL, 600, size/2)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
				ui.Text(c, a.profile.Nickname).FontSize(24).FontWeight(800).LetterSpacing(-0.5).MaxLines(2).LineHeight(1.2)
				if d != nil && d.user != nil {
					ui.Text(c, fmt.Sprintf("%s 关注 · %s 粉丝", count(d.user.Follows), count(d.user.Followeds))).FontSize(13).TextColor(p.muted)
				} else if a.profile.Label != "" {
					ui.Text(c, a.profile.Label).FontSize(12.5).TextColor(p.accent)
				}
				if a.profile.VIP || a.profile.Lite {
					a.memberRow(c)
				}
			})
		})

		// Entries to the library and to settings, as a grid of tiles.
		entry := func(key string, icon *ui.SVG, label string, act func()) {
			tile := ui.ButtonBase(c.Key(key)).Column().Gap(10).AlignItems(ui.Center).Justify(ui.Center).
				Padding(16, 4).Radius(14).Background(p.hover).FocusRing(false).Label(label)
			if tile.Hovered() {
				tile.Background(p.press)
			}
			tile.Children(func() {
				ui.Icon(c, icon).Size(24, 24).TextColor(p.accent)
				ui.Text(c, label).FontSize(12.5).FontWeight(600).SingleLine()
			})
			if tile.Clicked() {
				act()
			}
		}
		a.section(c, "我的音乐")
		ui.Grid(c).Columns(4).Gap(10).Children(func() {
			entry("mine-daily", iconDaily, "每日推荐", func() { a.open(route{page: pageDaily}) })
			entry("mine-charts", iconCharts, "排行榜", func() { a.open(route{page: pageCharts}) })
			entry("mine-liked", iconHeart, "我喜欢的", func() {
				if len(a.liked) == 0 {
					a.toast = "还没有喜欢的歌曲"
					return
				}
				a.open(route{page: pageUser, id: id})
			})
			entry("mine-download", iconDownload, "下载", func() { a.open(route{page: pageDownloads}) })
			entry("mine-local", iconFolder, "本地音乐", func() { a.open(route{page: pageLocal}) })
			entry("mine-cloud", iconCloud, "音乐云盘", func() { a.open(route{page: pageCloud}) })
			entry("mine-podcast", iconPodcast, "听书", func() { a.open(route{page: pagePodcasts}) })
			entry("mine-home", iconUser, "我的主页", func() { a.open(route{page: pageUser, id: id}) })
			entry("mine-fm", iconRadio, "私人 FM", func() { a.open(route{page: pageFM}) })
			entry("mine-settings", iconSettings, "设置", func() { a.open(route{page: pageSettings}) })
		})

		a.section(c, "创建的歌单")
		switch {
		case d == nil || d.loading > 0:
			a.playlistCardsSkeleton(c, a.pageShimmer(c), a.columns(c, 168), 168, false)
		case len(d.playlists) == 0:
			a.empty(c, "还没有创建歌单")
		default:
			ui.Grid(c).Columns(a.columns(c, 168)).GapX(20).GapY(28).Children(func() {
				for i := range d.playlists {
					a.playlistCard(c.Key(fmt.Sprintf("mine-%d", d.playlists[i].ID)), &d.playlists[i])
				}
			})
		}
		if len(a.saved) > 0 {
			a.section(c, "收藏的歌单")
			ui.Grid(c).Columns(a.columns(c, 168)).GapX(20).GapY(28).Children(func() {
				for i := range a.saved {
					a.playlistCard(c.Key(fmt.Sprintf("saved-%d", a.saved[i].ID)), &a.saved[i])
				}
			})
		}
	})
}

// vipDay is the day a subscription ends, as its page says it: the services
// write "2006-01-02 15:04:05", and the hour is no use there.
func vipDay(stamp string) string {
	if stamp == "" {
		return ""
	}
	if len(stamp) > 10 {
		return stamp[:10]
	}
	return stamp
}

// userRow is a user in a list, opening their page.
func (a *app) userRow(c *ui.Context, u *netease.User) {
	p := a.pal
	row := ui.ButtonBase(c).Padding(10, 12).Gap(14).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(u.Nickname)
	if row.Hovered() {
		row.Background(p.hover)
	}
	row.Children(func() {
		a.cover(c, u.AvatarURL, 120, 48, 24)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, u.Nickname).FontSize(14).FontWeight(600).SingleLine()
			if u.Signature != "" {
				ui.Text(c, u.Signature).FontSize(12.5).TextColor(p.muted).SingleLine()
			}
		})
	})
	if row.Clicked() {
		a.open(route{page: pageUser, id: u.UserID})
	}
}
