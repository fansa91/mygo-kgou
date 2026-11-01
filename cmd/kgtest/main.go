// Command kgtest talks to KuGou the way the player does, to check that what
// internal/kugou signs is what KuGou accepts.
//
//	go run ./cmd/kgtest register
//	go run ./cmd/kgtest search 晴天
//	go run ./cmd/kgtest play 晴天 lossless
//	go run ./cmd/kgtest lyric 晴天
//	go run ./cmd/kgtest comments 晴天
//	go run ./cmd/kgtest get complexsearch.kugou.com /v2/search/song keyword=晴天
//	go run ./cmd/kgtest post everydayrec.service.kugou.com /everyday_song_recommend '{"platform":"android","userid":0}'
//	go run ./cmd/kgtest login 13800000000
//	go run ./cmd/kgtest verify 13800000000 123456
//	go run ./cmd/kgtest lists
//	go run ./cmd/kgtest liked
//	go run ./cmd/kgtest like 富士山下
//	go run ./cmd/kgtest qr
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"netease-music-mygo/internal/kugou"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	dir := os.Getenv("KG_CACHE")
	if dir == "" {
		home, _ := os.UserCacheDir()
		dir = home + "/mygo-kugou"
	}
	client, err := kugou.New(dir)
	if err != nil {
		fail(err)
	}
	if os.Getenv("KG_LITE") != "" {
		client.Lite = true
	}
	fmt.Println("device:", client.String())
	if s := loadSession(dir); s != nil {
		client.SetUser(s.UserID, s.Token)
	}

	switch os.Args[1] {
	case "register":
		if err := client.Register(); err != nil {
			fail(err)
		}
		fmt.Println("dfid:", client.DFID())

	case "qr":
		// The sign-in by QR code, as the app asks it: a key, then the
		// state of that key. Without a phone to scan with, what this can
		// show is that the key is issued and that the poll answers.
		var key struct {
			Data struct {
				QRCode string `json:"qrcode"`
			} `json:"data"`
		}
		err := client.SendWeb("GET", "https://login-user.kugou.com", "/v2/qrcode", map[string]string{
			"appid": "1001", "type": "1", "plat": "4",
			"qrcode_txt": "https://h5.kugou.com/apps/loginQRCode/html/index.html?appid=1005&",
			"srcappid":   "2919",
		}, nil, &key)
		if err != nil {
			fail(err)
		}
		fmt.Println("qrcode:", key.Data.QRCode)
		fmt.Println("url:", "https://h5.kugou.com/apps/loginQRCode/html/index.html?qrcode="+key.Data.QRCode)
		for i := 0; i < 3; i++ {
			var res struct {
				Data struct {
					Status int    `json:"status"`
					Token  string `json:"token"`
					Msg    string `json:"msg"`
				} `json:"data"`
				Status  int `json:"status"`
				ErrCode int `json:"error_code"`
			}
			err := client.SendWeb("GET", "https://login-user.kugou.com", "/v2/get_userinfo_qrcode", map[string]string{
				"plat": "4", "appid": "1005", "srcappid": "2919",
				"qrcode": key.Data.QRCode, "dev": client.DFID(),
			}, nil, &res)
			if err != nil {
				fail(err)
			}
			fmt.Printf("poll %d: outer=%d errcode=%d state=%d msg=%q token=%v\n",
				i+1, res.Status, res.ErrCode, res.Data.Status, res.Data.Msg, res.Data.Token != "")
			time.Sleep(2 * time.Second)
		}

	case "get":
		if len(os.Args) < 4 {
			usage()
		}
		router := os.Args[2]
		if router == "-" {
			router = ""
		}
		params := map[string]string{}
		for _, kv := range os.Args[4:] {
			for i := 0; i < len(kv); i++ {
				if kv[i] == '=' {
					params[kv[:i]] = kv[i+1:]
					break
				}
			}
		}
		body, err := kugou.RawGet(client, router, os.Args[3], params)
		if err != nil {
			fail(err)
		}
		fmt.Println(indent(body))

	case "post":
		if len(os.Args) < 5 {
			usage()
		}
		router := os.Args[2]
		if router == "-" {
			router = ""
		}
		var body any
		if err := json.Unmarshal([]byte(os.Args[4]), &body); err != nil {
			fail(err)
		}
		raw, err := kugou.RawPost(client, router, os.Args[3], body)
		if err != nil {
			fail(err)
		}
		fmt.Println(indent(raw))

	case "search":
		if len(os.Args) < 3 {
			usage()
		}
		page, err := kugou.Search(client, os.Args[2], 1, 10)
		if err != nil {
			fail(err)
		}
		fmt.Printf("%d songs in all\n", page.Total)
		for _, t := range page.Songs {
			// The singers' numbers are what make their names links.
			fmt.Printf("%-30s %-16s %-14s %d:%02d pay=%d id=%d singers=%v\n",
				t.Name, t.Singer, t.Album, t.Duration/60000, t.Duration/1000%60, t.PayType, t.AlbumAudioID, t.Singers)
		}

	case "vip":
		// A subscription song, streamed as whatever client this is.
		if len(os.Args) < 3 {
			usage()
		}
		res, err := kugou.Search(client, os.Args[2], 1, 30)
		if err != nil {
			fail(err)
		}
		var song *kugou.Track
		for i := range res.Songs {
			if res.Songs[i].Privilege >= 9 {
				song = &res.Songs[i]
				break
			}
		}
		if song == nil {
			fmt.Println("no subscription song in the first page")
			return
		}
		fmt.Printf("%s - %s  privilege=%d payType=%d\n", song.Singer, song.Name, song.Privilege, song.PayType)
		st, err := kugou.URL(client, song, "standard")
		if err != nil {
			fmt.Println("refused:", err)
			return
		}
		fmt.Printf("playing %s %dkbps %d bytes\n%s\n", st.Type, st.BitRate, st.Size, st.URL)

	case "play":
		if len(os.Args) < 3 {
			usage()
		}
		level := "standard"
		if len(os.Args) > 3 {
			level = os.Args[3]
		}
		track, err := firstPlayable(client, os.Args[2], level)
		if err != nil {
			fail(err)
		}
		fmt.Printf("%s - %s\n", track.Singer, track.Name)
		st, err := kugou.URL(client, track, level)
		if err != nil {
			fail(err)
		}
		fmt.Printf("level=%s type=%s bitrate=%dkbps size=%d\n%s\n", st.Level, st.Type, st.BitRate, st.Size, st.URL)

	case "comments":
		if len(os.Args) < 3 {
			usage()
		}
		page, err := kugou.Search(client, os.Args[2], 1, 1)
		if err != nil {
			fail(err)
		}
		if len(page.Songs) == 0 {
			fail(errors.New("no songs found"))
		}
		t := page.Songs[0]
		fmt.Printf("%s - %s albumAudioID=%d hash=%s\n", t.Singer, t.Name, t.AlbumAudioID, t.Hash)
		want := 1
		if len(os.Args) > 3 {
			want = int(mustInt(os.Args[3]))
		}
		for _, tab := range []struct {
			name  string
			order kugou.CommentOrder
		}{{"精彩", kugou.CommentsHot}, {"最新", kugou.CommentsNew}} {
			list, total, err := kugou.Comments(client, t.Hash, tab.order, want, 5)
			if err != nil {
				fmt.Printf("\n[%s] err: %v\n", tab.name, err)
				continue
			}
			fmt.Printf("\n[%s] 第%d页 total=%d got=%d\n", tab.name, want, total, len(list))
			for _, m := range list {
				fmt.Printf("%-16s %-6s %v 赞%d\n  %s\n", m.UserName, m.Location, m.At.Format("2006-01-02 15:04"), m.Likes, m.Content)
				if m.Reply != nil {
					fmt.Printf("  ↳ %s: %s\n", m.Reply.UserName, m.Reply.Content)
				}
			}
		}

	case "album":
		if len(os.Args) < 3 {
			usage()
		}
		al, songs, err := kugou.AlbumDetail(client, mustInt(os.Args[2]))
		if err != nil {
			fail(err)
		}
		fmt.Printf("id=%d name=%q artist=%q songs=%d publish=%q kind=%q pic=%s\n",
			al.ID, al.Name, al.Artist, al.Songs, al.Publish, al.Kind, al.Pic)
		fmt.Printf("intro=%q\n", short(al.Intro, 160))
		fmt.Printf("tracks=%d\n", len(songs))
		for i, t := range songs {
			if i >= 5 {
				break
			}
			// The singers' numbers are what make their names links.
			fmt.Printf("  %-30s %-16s dur=%d playable=%v singers=%v\n", t.Name, t.Singer, t.Duration, t.AlbumAudioID != 0, t.Singers)
		}

	case "artist":
		if len(os.Args) < 3 {
			usage()
		}
		a, err := kugou.ArtistDetail(client, mustInt(os.Args[2]))
		if err != nil {
			fail(err)
		}
		fmt.Printf("id=%d name=%q songs=%d albums=%d avatar=%s pic=%s\n", a.ID, a.Name, a.Songs, a.Albums, a.Avatar, a.Pic)
		fmt.Printf("intro=%q\n", short(a.Intro, 160))
		songs, err := kugou.ArtistSongs(client, a.ID, 1, 5)
		if err != nil {
			fmt.Printf("songs error: %v\n", err)
		}
		for _, t := range songs {
			fmt.Printf("  %-30s %-16s dur=%d\n", t.Name, t.Singer, t.Duration)
		}
		als, more, err := kugou.ArtistAlbums(client, a.ID, 1, 5)
		if err != nil {
			fmt.Printf("albums error: %v\n", err)
		}
		for _, al := range als {
			fmt.Printf("  album %d %q %q more=%v\n", al.ID, al.Name, al.Publish, more)
		}

	case "albumfind":
		if len(os.Args) < 3 {
			usage()
		}
		lists, total, err := kugou.SearchRaw(client, "album", os.Args[2], 1, 5)
		if err != nil {
			fail(err)
		}
		fmt.Printf("total=%d\n", total)
		for _, l := range lists {
			b, _ := json.Marshal(l)
			fmt.Printf("%s\n", short(string(b), 700))
		}

	case "artistfind":
		if len(os.Args) < 3 {
			usage()
		}
		lists, total, err := kugou.SearchRaw(client, "author", os.Args[2], 1, 5)
		if err != nil {
			fail(err)
		}
		fmt.Printf("total=%d\n", total)
		for _, l := range lists {
			b, _ := json.Marshal(l)
			fmt.Printf("%s\n", short(string(b), 700))
		}

	case "lyric":
		if len(os.Args) < 3 {
			usage()
		}
		page, err := kugou.Search(client, os.Args[2], 1, 5)
		if err != nil {
			fail(err)
		}
		for i := range page.Songs {
			t := &page.Songs[i]
			l, err := kugou.Lyrics(client, t.Hash, t.AlbumAudioID)
			if err != nil {
				fmt.Printf("%s - %s: %v\n", t.Singer, t.Name, err)
				continue
			}
			fmt.Printf("%s - %s\n--- lrc ---\n%s\n", t.Singer, t.Name, head(l.LRC, 12))
			if l.KRC != "" {
				fmt.Printf("--- word timing ---\n%s\n", head(l.KRC, 8))
			}
			return
		}
		fail(errors.New("no lyrics found"))

	case "login":
		if len(os.Args) < 3 {
			usage()
		}
		if err := kugou.SendSMSCode(client, os.Args[2]); err != nil {
			fail(err)
		}
		fmt.Println("a code is on its way; check the phone")

	case "verify":
		if len(os.Args) < 4 {
			usage()
		}
		s, err := kugou.LoginByCode(client, os.Args[2], os.Args[3])
		if err != nil {
			fail(err)
		}
		if err := saveSession(dir, s); err != nil {
			fail(err)
		}
		fmt.Printf("signed in as %s (%d), token %s…\n", s.Nickname, s.UserID, head(s.Token, 8))

	case "me":
		acc, err := kugou.Profile(client)
		if err != nil {
			fail(err)
		}
		vip, err := kugou.UnionVIP(client)
		fmt.Printf("%s\n%s\nvip_type=%d vip_end=%q concept=%v concept_end=%q\n",
			acc.Nickname, acc.Avatar, acc.VIPType, acc.VIPEnd, acc.Concept, acc.ConceptEnd)
		if err != nil {
			fmt.Println("union vip:", err)
		} else {
			fmt.Printf("union vip: type=%d concept=%v end=%q\n", vip.VIPType, vip.Concept, vip.ConceptEnd)
		}

	case "lists":
		lists, err := kugou.MyLists(client)
		if err != nil {
			fail(err)
		}
		for _, l := range lists {
			kind := "made"
			if l.Type == 1 {
				kind = "saved"
			}
			mark := ""
			if l.Default || strings.Contains(l.Name, "我喜欢") {
				mark = "  <- the liked songs"
			}
			fmt.Printf("%-12d %-6s %-24s %4d songs%s\n", l.ID, kind, l.Name, l.Count, mark)
		}

	case "liked":
		l, err := kugou.Liked(client)
		if err != nil {
			fail(err)
		}
		fmt.Printf("liked list %d %q\n", l.ID, l.Name)
		songs, total, err := kugou.ListSongs(client, l.ID, l.Type, 1, 50)
		if err != nil {
			fail(err)
		}
		fmt.Printf("%d songs in all\n", total)
		for _, t := range songs {
			fmt.Printf("  %-24s %-16s fileid=%d\n", t.Name, t.Singer, t.FileID)
		}

	case "like", "unlike":
		if len(os.Args) < 3 {
			usage()
		}
		track, err := firstPlayable(client, os.Args[2], "standard")
		if err != nil {
			fail(err)
		}
		l, err := kugou.Liked(client)
		if err != nil {
			fail(err)
		}
		if os.Args[1] == "like" {
			err = kugou.AddToList(client, l.ID, []kugou.Track{*track}, l.Type)
		} else {
			songs, _, listErr := kugou.ListSongs(client, l.ID, l.Type, 1, 300)
			if listErr != nil {
				fail(listErr)
			}
			var file int64
			for _, s := range songs {
				if s.Hash == track.Hash {
					file = s.FileID
				}
			}
			if file == 0 {
				fail(errors.New("that song is not in the liked list"))
			}
			err = kugou.DelFromList(client, l.ID, []int64{file}, l.Type)
		}
		if err != nil {
			fail(err)
		}
		fmt.Printf("%s: %s - %s\n", os.Args[1], track.Singer, track.Name)

	default:
		usage()
	}
}

// session is the login kgtest keeps between runs, so the account commands do
// not need the phone every time.
type session struct {
	Token    string `json:"token"`
	UserID   int64  `json:"userid"`
	Nickname string `json:"nickname,omitempty"`
}

func loadSession(dir string) *session {
	b, err := os.ReadFile(dir + "/cli-session.json")
	if err != nil {
		return nil
	}
	var s session
	if json.Unmarshal(b, &s) != nil || s.Token == "" {
		return nil
	}
	return &s
}

func saveSession(dir string, s *kugou.Session) error {
	b, err := json.Marshal(session{Token: s.Token, UserID: s.UserID, Nickname: s.Nickname})
	if err != nil {
		return err
	}
	return os.WriteFile(dir+"/cli-session.json", b, 0o600)
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && strings.Contains(s, sub))
}

// firstPlayable walks a search and returns the first song KuGou will stream.
func firstPlayable(c *kugou.Client, query, level string) (*kugou.Track, error) {
	page, err := kugou.Search(c, query, 1, 10)
	if err != nil {
		return nil, err
	}
	for i := range page.Songs {
		if _, err := kugou.URL(c, &page.Songs[i], level); err == nil {
			return &page.Songs[i], nil
		}
	}
	return nil, errors.New("nothing in that search plays without a subscription")
}

func head(s string, n int) string {
	lines := 0
	for i := range s {
		if s[i] == '\n' {
			lines++
			if lines >= n {
				return s[:i]
			}
		}
	}
	return s
}

func indent(b []byte) string {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return string(b)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(out)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: kgtest register | search <keyword> | play <keyword> [level] | lyric <keyword> |\n"+
		"       get <router|-> <path> k=v... | login <mobile> | verify <mobile> <code> | me |\n"+
		"       lists | liked | like <keyword> | unlike <keyword>")
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func mustInt(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		fail(err)
	}
	return v
}

func short(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
