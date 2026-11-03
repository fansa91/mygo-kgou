package netease

import (
	"fmt"
	"strconv"

	"netease-music-mygo/internal/kugou"
)

// What the home page is made of: the playlists it offers, the songs picked
// for the day, and the charts.

// FeaturedPlaylists returns the playlists the home page recommends.
func FeaturedPlaylists(limit int) ([]Playlist, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	list, err := kugou.RecommendPlaylists(c, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Playlist, 0, len(list))
	for _, sp := range list {
		out = append(out, toPlaylist(sp))
	}
	return out, nil
}

// RecommendedPlaylists returns the day's playlists picked for the listener.
// KuGou picks them for the device rather than for the account, and offers
// them in the same list the home page's cards come from.
func RecommendedPlaylists() ([]Playlist, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	list, err := kugou.RecommendPlaylists(c, 12)
	if err != nil {
		return nil, err
	}
	out := make([]Playlist, 0, len(list))
	for _, sp := range list {
		out = append(out, toPlaylist(sp))
	}
	return out, nil
}

// DailySongs returns the day's recommended songs.
func DailySongs() ([]Track, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	songs, err := kugou.DailySongs(c)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(songs))
	for _, t := range songs {
		out = append(out, toTrack(t))
	}
	if len(out) == 0 {
		// A signed-out listener gets nothing picked for them; the styles
		// the service does have are what the day sounds like.
		style, err := kugou.StyleSongs(c, "")
		if err == nil {
			for _, t := range style {
				out = append(out, toTrack(t))
			}
		}
	}
	return out, nil
}

// Charts returns the charts, each with its top three songs.
func Charts() ([]Playlist, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	ranks, err := kugou.Ranks(c)
	if err != nil {
		return nil, err
	}
	out := make([]Playlist, 0, len(ranks))
	for _, r := range ranks {
		p := Playlist{
			Name:        r.Name,
			CoverURL:    r.Pic,
			PicURL:      r.Pic,
			Description: r.Intro,
			UpdateFreq:  r.Update,
			Preview:     preview(r.Top),
		}
		p.ID = app.lists.remember("rank", strconv.FormatInt(r.ID, 10), r.Name, r.Pic)
		out = append(out, p)
	}
	return out, nil
}

// PlaylistDetail returns a playlist with up to 1000 of its tracks, or a
// chart with the songs it holds.
func PlaylistDetail(id int64) (*Playlist, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	kind, ref, ok := fromKind(id)
	if !ok {
		return nil, fmt.Errorf("netease: no playlist %d", id)
	}
	switch kind {
	case "rank":
		rankID, _ := strconv.ParseInt(ref, 10, 64)
		p := Playlist{ID: id}
		if r, ok := app.lists.lookup(id); ok {
			p.Name, p.CoverURL, p.PicURL = r.Name, r.Pic, r.Pic
		}
		for page := 1; page <= 10; page++ {
			songs, total, err := kugou.RankSongs(c, rankID, page, 100)
			if err != nil {
				if page == 1 {
					return nil, err
				}
				break
			}
			p.TrackCount = total
			for _, t := range songs {
				p.Tracks = append(p.Tracks, toTrack(t))
			}
			if len(songs) < 100 || len(p.Tracks) >= total {
				break
			}
		}
		return &p, nil

	case "list":
		sp, err := kugou.Playlist(c, ref)
		if err != nil {
			return nil, err
		}
		p := toPlaylist(*sp)
		p.ID = id
		for page := 1; page <= 10; page++ {
			songs, total, err := kugou.PlaylistSongs(c, ref, page, 100)
			if err != nil {
				if page == 1 {
					return nil, err
				}
				break
			}
			if total > 0 {
				p.TrackCount = total
			}
			for _, t := range songs {
				p.Tracks = append(p.Tracks, toTrack(t))
			}
			if len(songs) < 100 {
				break
			}
		}
		return &p, nil

	case "cloud", "liked":
		listid, _ := strconv.ParseInt(ref, 10, 64)
		r, _ := app.lists.lookup(id)
		// A list remembered before its collection id was kept has none, and
		// without it the songs arrive with no qualities. Asking the account
		// for its lists fills it in, once.
		if r.GID == "" {
			r = learnCollectionID(c, id, r, kind, listid)
		}
		p := Playlist{ID: id, Name: r.Name, CoverURL: r.Pic, PicURL: r.Pic}
		if kind == "liked" {
			p.SpecialType = 5
			p.Name, p.Description = "我喜欢的音乐", "我喜欢的音乐"
		}
		p.Creator.UserID, p.Creator.Nickname = c.UserID, myName()
		// The cloud list says what the list holds but not what its songs
		// are, so their qualities come from the playlist read by the name
		// the public service knows it by — once, for all the pages.
		qualities := kugou.PlaylistQualities(c, r.GID)
		for page := 1; page <= 20; page++ {
			songs, total, err := kugou.ListSongs(c, listid, r.Type, page, 200)
			if err != nil {
				if page == 1 {
					return nil, err
				}
				break
			}
			qualities.Fill(songs)
			if total > 0 {
				p.TrackCount = total
			}
			for _, t := range songs {
				tr := toTrack(t)
				app.files.set(listid, tr.ID, t.FileID)
				p.Tracks = append(p.Tracks, tr)
			}
			if len(songs) < 200 || (total > 0 && len(p.Tracks) >= total) {
				break
			}
		}
		app.files.save()
		return &p, nil
	}
	return nil, fmt.Errorf("netease: no playlist %d", id)
}

// learnCollectionID learns the collection id the public playlist service calls one of
// the listener's lists by, and keeps it beside the list. The account's lists
// are one request; a failure leaves the list as it was, read without its
// songs' qualities.
func learnCollectionID(c *kugou.Client, id int64, r listRef, kind string, listid int64) listRef {
	lists, err := kugou.MyLists(c)
	if err != nil {
		return r
	}
	for _, l := range lists {
		if l.ID != listid || l.CollectionID == "" {
			continue
		}
		app.lists.rememberCloud(kind, listid, r.Type, r.Name, r.Pic, l.CollectionID)
		r.GID = l.CollectionID
		return r
	}
	return r
}
