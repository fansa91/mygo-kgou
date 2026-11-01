package netease

import (
	"errors"
	"strconv"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/kugou"
)

// Search, and the pages it leads to: an album, a singer, someone's account.

// SearchPage returns limit of what matches a query, of a type, from offset.
func SearchPage(query string, kind SearchType, limit, offset int) (SearchResult, error) {
	c, err := kuGou()
	if err != nil {
		return SearchResult{}, err
	}
	if limit <= 0 {
		limit = 30
	}
	page := offset/limit + 1
	var out SearchResult

	switch kind {
	case SearchSongs:
		res, err := kugou.Search(c, query, page, limit)
		if err != nil {
			return out, err
		}
		out.Total = res.Total
		for _, t := range res.Songs {
			out.Songs = append(out.Songs, toTrack(t))
		}

	case SearchAlbums:
		list, total, err := kugou.SearchRaw(c, "album", query, page, limit)
		if err != nil {
			return out, err
		}
		out.Total = total
		for _, l := range list {
			out.Albums = append(out.Albums, albumFrom(l))
		}

	case SearchArtists:
		list, total, err := kugou.SearchRaw(c, "author", query, page, limit)
		if err != nil {
			return out, err
		}
		out.Total = total
		for _, l := range list {
			out.Artists = append(out.Artists, artistFrom(l))
		}

	case SearchPlaylists:
		list, total, err := kugou.SearchRaw(c, "special", query, page, limit)
		if err != nil {
			return out, err
		}
		out.Total = total
		for _, l := range list {
			out.Playlists = append(out.Playlists, playlistFrom(l))
		}

	case SearchUsers:
		// KuGou searches no accounts: its search service knows songs,
		// albums, singers, playlists and videos, and nothing else. The
		// page says so with nothing found rather than failing.
		return out, nil

	case SearchPodcasts:
		albums, _, err := kugou.PodcastLibrary(c, 906, page, limit, true)
		if err != nil {
			return out, err
		}
		for _, a := range albums {
			out.Podcasts = append(out.Podcasts, toPodcast(a))
		}
		out.Total = len(out.Podcasts)

	default:
		return out, errors.New("netease: no such search")
	}
	return out, nil
}

// Search returns the first limit songs matching a query.
func Search(query string, limit int) ([]Track, error) {
	res, err := SearchPage(query, SearchSongs, limit, 0)
	return res.Songs, err
}

// SearchSuggest returns what the search box offers while typing.
func SearchSuggest(query string) (Suggestions, error) {
	c, err := kuGou()
	if err != nil {
		return Suggestions{}, err
	}
	songs, _, err := kugou.Suggestions(c, query)
	if err != nil {
		return Suggestions{}, err
	}
	var s Suggestions
	for _, t := range songs {
		s.Songs = append(s.Songs, toTrack(t))
	}
	if len(s.Songs) > 0 {
		s.Order = append(s.Order, "songs")
	}
	return s, nil
}

// AlbumDetail returns an album with its songs.
func AlbumDetail(id int64) (*AlbumProfile, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	al, songs, err := kugou.AlbumDetail(c, id)
	if err != nil {
		return nil, err
	}
	p := &AlbumProfile{
		Album:       Album{ID: al.ID, Name: al.Name, PicURL: al.Pic},
		Size:        al.Songs,
		Description: al.Intro,
	}
	if al.Artist != "" {
		p.Artists = []Artist{{Name: al.Artist}}
	}
	if t, err := parseDate(al.Publish); err == nil {
		p.PublishTime = t
	}
	for _, t := range songs {
		p.Songs = append(p.Songs, toTrack(t))
	}
	// The album names its singer by name alone, which opens nothing; the
	// songs in it name them by number too.
	for _, t := range songs {
		if who := artistsOf(t); len(who) > 0 && who[0].ID != 0 {
			p.Artists = who
			break
		}
	}
	if p.Size == 0 {
		p.Size = len(p.Songs)
	}
	return p, nil
}

// ArtistDetail returns who a singer is, with their newest songs.
func ArtistDetail(id int64) (*ArtistProfile, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	a, err := kugou.ArtistDetail(c, id)
	if err != nil {
		return nil, err
	}
	p := &ArtistProfile{
		Artist:    Artist{ID: a.ID, Name: a.Name},
		Avatar:    a.Avatar,
		PicURL:    a.Pic,
		BriefDesc: a.Intro,
		MusicSize: a.Songs,
		AlbumSize: a.Albums,
	}
	songs, err := kugou.ArtistSongs(c, id, 1, 50)
	if err == nil {
		for _, t := range songs {
			p.Songs = append(p.Songs, toTrack(t))
		}
	}
	return p, nil
}

// ArtistLatestSongs returns limit of a singer's newest songs.
func ArtistLatestSongs(id int64, limit int) ([]Track, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	songs, err := kugou.ArtistSongs(c, id, 1, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(songs))
	for _, t := range songs {
		out = append(out, toTrack(t))
	}
	return out, nil
}

// ArtistAlbums returns limit of a singer's albums from offset, and whether
// more follow.
func ArtistAlbums(id int64, limit, offset int) ([]AlbumProfile, bool, error) {
	c, err := kuGou()
	if err != nil {
		return nil, false, err
	}
	if limit <= 0 {
		limit = 30
	}
	albums, more, err := kugou.ArtistAlbums(c, id, offset/limit+1, limit)
	if err != nil {
		return nil, false, err
	}
	out := make([]AlbumProfile, 0, len(albums))
	for _, a := range albums {
		p := AlbumProfile{
			Album: Album{ID: a.ID, Name: a.Name, PicURL: a.Pic},
			Size:  a.Songs,
		}
		if a.Artist != "" {
			p.Artists = []Artist{{Name: a.Artist}}
		}
		if t, err := parseDate(a.Publish); err == nil {
			p.PublishTime = t
		}
		out = append(out, p)
	}
	return out, more, nil
}

// UserDetail returns who a user is. KuGou's account services answer only
// about the device asking, so this is the listener when signed in, and there
// is no one else to name.
func UserDetail(id int64) (*User, error) {
	if loaded == nil || loaded.UserID != id {
		return nil, ErrUnsupported
	}
	p, err := Account()
	if err != nil {
		return nil, err
	}
	return &User{UserID: p.UserID, Nickname: p.Nickname, AvatarURL: p.AvatarURL}, nil
}

// albumFrom reads an album out of a search. Its picture arrives as "img",
// which is the name this service uses and the one that was missing: the
// search's albumns showed without covers.
func albumFrom(l map[string]any) AlbumProfile {
	p := AlbumProfile{
		Album: Album{
			ID:     pickInt(l, "albumid", "album_id", "AlbumID"),
			Name:   clean(pickString(l, "albumname", "album_name")),
			PicURL: sized(pickString(l, "img", "imgurl", "image", "albumcover")),
		},
		Size: int(pickInt(l, "songcount", "song_count", "audio_total")),
	}
	if who := clean(pickString(l, "singer", "singername", "author_name")); who != "" {
		p.Artists = []Artist{{Name: who}}
	}
	if t, err := parseDate(pickString(l, "publish_time", "publishtime", "publish_date", "publishTime")); err == nil {
		p.PublishTime = t
	}
	return p
}

// artistFrom reads a singer out of a search. Their portrait is "Avatar" on a
// host of its own, which the search spells with a capital.
func artistFrom(l map[string]any) ArtistProfile {
	p := ArtistProfile{
		Artist: Artist{
			ID:   pickInt(l, "authorid", "author_id", "AuthorId", "singerid"),
			Name: clean(pickString(l, "authorname", "author_name", "AuthorName", "singername")),
		},
		Avatar: sized(pickString(l, "Avatar", "avatar", "sizable_avatar", "FirstFrameImage")),
	}
	p.PicURL = p.Avatar
	return p
}

// playlistFrom reads a playlist out of a search. Its collection id arrives
// as "gid", and the number is what the app keys the playlist by: without it
// every playlist found had the same number, so all of them opened the last.
func playlistFrom(l map[string]any) Playlist {
	sp := kugou.Special{
		CollectionID: pickString(l, "gid", "global_collection_id", "collection_id"),
		Name:         clean(pickString(l, "specialname", "name")),
		Pic:          sized(pickString(l, "img", "pic", "imgurl", "flexible_cover")),
		Intro:        clean(pickString(l, "intro")),
		Count:        int(pickInt(l, "song_count", "songcount", "count", "percount")),
		PlayCount:    pickInt(l, "play_count", "total_play_count", "playcount"),
		Creator:      clean(pickString(l, "nickname", "username")),
	}
	return toPlaylist(sp)
}

var _ = mygo.IsDev
var _ = strconv.Itoa
