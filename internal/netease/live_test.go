package netease

import (
	"fmt"
	"os"
	"testing"

	"netease-music-mygo/internal/kugou"
)

// TestLive walks what the app's pages walk, against the real service: the
// home page, a chart, a playlist, a search, then playing a song and reading
// its words. It runs only when asked, as it needs the network.
//
//	KG_LIVE=1 go test ./internal/netease -run TestLive -v
func TestLive(t *testing.T) {
	if os.Getenv("KG_LIVE") == "" {
		t.Skip("set KG_LIVE=1 to ask the real service")
	}

	featured, err := FeaturedPlaylists(6)
	if err != nil {
		t.Fatalf("featured: %v", err)
	}
	fmt.Printf("\n推荐歌单 %d 个\n", len(featured))
	for _, p := range featured[:min(3, len(featured))] {
		fmt.Printf("  %-28s id=%-20d tracks=%d %s\n", cut(p.Name, 26), p.ID, p.TrackCount, cut(p.Cover(), 40))
	}

	charts, err := Charts()
	if err != nil {
		t.Fatalf("charts: %v", err)
	}
	fmt.Printf("\n排行榜 %d 个\n", len(charts))
	for _, c := range charts[:min(3, len(charts))] {
		fmt.Printf("  %-12s id=%-20d 前三：%v\n", c.Name, c.ID, c.Preview[:min(2, len(c.Preview))])
	}

	daily, err := DailySongs()
	if err != nil {
		t.Fatalf("daily: %v", err)
	}
	fmt.Printf("\n每日推荐 %d 首\n", len(daily))
	for _, s := range daily[:min(3, len(daily))] {
		fmt.Printf("  %-20s %-22s %d:%02d playable=%v\n",
			cut(s.Name, 18), cut(s.ArtistNames(), 20), s.Duration/60000, s.Duration/1000%60, s.Playable())
	}

	// 榜单详情：点进去要能看到歌
	if len(charts) > 0 {
		p, err := PlaylistDetail(charts[0].ID)
		if err != nil {
			t.Fatalf("chart detail: %v", err)
		}
		fmt.Printf("\n榜单《%s》%d 首，前三：\n", p.Name, len(p.Tracks))
		for _, s := range p.Tracks[:min(3, len(p.Tracks))] {
			fmt.Printf("  %-20s %-22s pay=%d\n", cut(s.Name, 18), cut(s.ArtistNames(), 20), s.Fee)
		}
	}

	// 歌单详情
	if len(featured) > 0 {
		p, err := PlaylistDetail(featured[0].ID)
		if err != nil {
			t.Fatalf("playlist detail: %v", err)
		}
		fmt.Printf("\n歌单《%s》%d 首（服务端 %d）\n", p.Name, len(p.Tracks), p.TrackCount)
		for _, s := range p.Tracks[:min(3, len(p.Tracks))] {
			fmt.Printf("  %-20s %-22s pay=%d\n", cut(s.Name, 18), cut(s.ArtistNames(), 20), s.Fee)
		}
	}

	// 搜索 -> 播放 -> 歌词
	res, err := SearchPage("富士山下", SearchSongs, 5, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	fmt.Printf("\n搜索 富士山下：%d 首（共 %d）\n", len(res.Songs), res.Total)
	for _, s := range res.Songs {
		fmt.Printf("  %-20s %-16s pay=%d\n", cut(s.Name, 18), cut(s.ArtistNames(), 14), s.Fee)
		u, err := SongURL(s.ID, "standard")
		if err != nil {
			fmt.Printf("     url: %v\n", err)
			continue
		}
		fmt.Printf("     url: %d KB %s %.60s\n", u.Size/1024, u.Type, u.URL)
		lrc, _, yrc, _, err := Lyrics(s.ID)
		if err != nil {
			fmt.Printf("     lyric: %v\n", err)
			break
		}
		fmt.Printf("     lrc: %d 行  yrc(逐字): %d 字节\n", lines(lrc), len(yrc))

		// 评论：精彩的前几条，和最新的前几条
		for _, tab := range []struct {
			name  string
			order kugou.CommentOrder
		}{{"精彩", kugou.CommentsHot}, {"最新", kugou.CommentsNew}} {
			cs, err := ThreadComments(s.Thread(), tab.order, 1, 10)
			if err != nil {
				fmt.Printf("     comments(%s): %v\n", tab.name, err)
				continue
			}
			list := cs.Latest
			if tab.order == kugou.CommentsHot {
				list = cs.Hot
			}
			fmt.Printf("     [%s] 评论 %d 条，取 %d 条：\n", tab.name, cs.Total, len(list))
			for _, cm := range list {
				fmt.Printf("       %-14s %-5s 赞%d  %s\n", cut(cm.User.Nickname, 12), cm.IPLocation.Location, cm.LikedCount, cut(cm.Content, 34))
			}
		}
		break
	}

	// 榜单里一首能放的
	if len(charts) > 1 {
		p, err := PlaylistDetail(charts[1].ID)
		if err == nil {
			for _, s := range p.Tracks {
				if u, err := SongURL(s.ID, "standard"); err == nil {
					fmt.Printf("\n榜单可播：%s - %s  %d KB %s\n", cut(s.ArtistNames(), 16), cut(s.Name, 20), u.Size/1024, u.Type)
					break
				}
			}
		}
	}
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func lines(s string) int {
	n := 0
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}

// TestLiveAudiobooks walks the audiobook page and plays an episode.
func TestLiveAudiobooks(t *testing.T) {
	if os.Getenv("KG_LIVE") == "" {
		t.Skip("set KG_LIVE=1 to ask the real service")
	}
	pods, err := RecommendedPodcasts()
	if err != nil {
		t.Fatalf("recommended: %v", err)
	}
	fmt.Printf("\n听书推荐 %d 个\n", len(pods))
	for _, p := range pods[:min(3, len(pods))] {
		fmt.Printf("  %-26s id=%-12d 期数=%d 分类=%s\n", cut(p.Name, 24), p.ID, p.ProgramCount, p.Category)
	}
	if len(pods) == 0 {
		return
	}
	pod := pods[0]
	eps, more, err := PodcastPrograms(pod.ID, 5, 0)
	if err != nil {
		t.Fatalf("programs: %v", err)
	}
	fmt.Printf("《%s》前 %d 期（更多=%v）\n", cut(pod.Name, 20), len(eps), more)
	for _, e := range eps[:min(3, len(eps))] {
		t := e.Track(&pod)
		fmt.Printf("  %-24s %d:%02d id=%d\n", cut(t.Name, 22), t.Duration/60000, t.Duration/1000%60, t.ID)
		if u, err := SongURL(t.ID, "standard"); err != nil {
			fmt.Printf("     url: %v\n", err)
		} else {
			fmt.Printf("     url: %d KB %s\n", u.Size/1024, u.Type)
			break
		}
	}

	fm, err := PersonalFM(FMDefault)
	if err != nil {
		t.Fatalf("fm: %v", err)
	}
	fmt.Printf("\n私人FM %d 首\n", len(fm))
	for _, s := range fm[:min(3, len(fm))] {
		fmt.Printf("  %-20s %-16s playable=%v\n", cut(s.Name, 18), cut(s.ArtistNames(), 14), s.Playable())
	}
	if len(fm) > 0 {
		heart, err := HeartMode(fm[0].ID, fm[0].ID, 0)
		if err != nil {
			fmt.Printf("心动模式: %v\n", err)
		} else {
			fmt.Printf("心动模式（以《%s》为种子）%d 首\n", cut(fm[0].Name, 16), len(heart))
			for _, s := range heart[:min(3, len(heart))] {
				fmt.Printf("  %-20s %-16s\n", cut(s.Name, 18), cut(s.ArtistNames(), 14))
			}
		}
	}
}
