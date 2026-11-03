package kugou

import (
	"sort"
	"strconv"
	"strings"
)

// The services behind the app's pages: what it shows before anyone searches.
// Each one answers in its own shape, with its own capitalisation, so each has
// its own reading here and hands back a Track or a Special.

// Special is a playlist. KuGou names one by a global collection id, a string
// like "collection_3_400251166_22_0", which the app cannot key on; Special
// carries the number the adapter gives it as well.
type Special struct {
	CollectionID string
	Name         string
	Pic          string
	Intro        string
	Count        int
	PlayCount    int64
	Creator      string
	Tags         []string
	Songs        []Track // however many the service sent along
}

// RecommendPlaylists returns the playlists the home page offers, from the
// service the phone reads its recommendation cards from.
func RecommendPlaylists(c *Client, limit int) ([]Special, error) {
	if limit <= 0 {
		limit = 30
	}
	sec := strconv.FormatInt(now(), 10)
	var res struct {
		Data struct {
			SpecialList []map[string]any `json:"special_list"`
			HasNext     int              `json:"has_next"`
		} `json:"data"`
	}
	err := c.send("POST", request{
		Router: "specialrec.service.kugou.com",
		Path:   "/v2/special_recommend",
		Body: map[string]any{
			"appid": c.appID(), "mid": c.Dev.MID, "clientver": c.clientVer(), "platform": "android",
			"clienttime": sec, "userid": c.UserID, "module_id": 1, "page": 1,
			"pagesize": limit,
			// The player's key: the time it asked, salted.
			"key": keyForWith(sec, c.Dev.MID, strconv.FormatInt(c.UserID, 10), c.appID(), c.keySalt()),
			"special_recommend": map[string]any{
				"withtag": 1, "withsong": 1, "sort": 1, "ugc": 1,
				"is_selected": 0, "withrecommend": 1, "area_code": 1, "categoryid": 0,
			},
			"req_multi": 1, "retrun_min": 5, "return_special_falg": 1,
		},
	}, &res)
	if err != nil {
		return nil, err
	}
	out := make([]Special, 0, len(res.Data.SpecialList))
	for _, l := range res.Data.SpecialList {
		sp := Special{
			CollectionID: pickString(l, "global_collection_id", "gid"),
			Name:         clean(pickString(l, "specialname", "name")),
			Pic:          listCover(pickString(l, "img", "imgurl", "flexible_cover", "pic")),
			Intro:        clean(pickString(l, "intro")),
			Count:        int(pickInt(l, "count", "percount", "song_count", "songcount")),
			PlayCount:    pickInt(l, "play_count", "playcount"),
			Creator:      pickString(l, "nickname", "username", "singername"),
		}
		for _, t := range pickTags(l["tags"]) {
			sp.Tags = append(sp.Tags, t)
		}
		out = append(out, sp)
	}
	return out, nil
}

// Playlist returns a playlist by the id the app knows it by: the global
// collection id it carries.
func Playlist(c *Client, collectionID string) (*Special, error) {
	var res struct {
		Data []map[string]any `json:"data"`
	}
	err := c.send("POST", request{
		Router: "pubsongs.kugou.com",
		Path:   "/v3/get_list_info",
		Body: map[string]any{
			"data":   []map[string]string{{"global_collection_id": collectionID}},
			"userid": c.UserID,
			"token":  c.Token,
		},
	}, &res)
	if err != nil {
		return nil, err
	}
	if len(res.Data) == 0 {
		return nil, ErrUnavailable
	}
	l := res.Data[0]
	sp := &Special{
		CollectionID: pickString(l, "global_collection_id", "list_create_gid"),
		Name:         clean(pickString(l, "name")),
		Pic:          sized(pickString(l, "pic")),
		Intro:        clean(pickString(l, "intro")),
		Count:        int(pickInt(l, "count")),
		PlayCount:    pickInt(l, "heat", "play_count"),
		Creator:      pickString(l, "list_create_username"),
	}
	for _, t := range pickTags(l["musiclib_tags"]) {
		sp.Tags = append(sp.Tags, t)
	}
	return sp, nil
}

// PlaylistSongs returns a page of a playlist's songs, and how many it has.
func PlaylistSongs(c *Client, collectionID string, page, size int) ([]Track, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 100
	}
	var res struct {
		Data struct {
			Count int              `json:"count"`
			Songs []map[string]any `json:"songs"`
		} `json:"data"`
	}
	err := c.send("GET", request{
		Path: "/pubsongs/v2/get_other_list_file_nofilt",
		Params: map[string]string{
			"area_code": "1", "begin_idx": strconv.Itoa((page - 1) * size),
			"plat": "1", "type": "1", "mode": "1", "personal_switch": "1",
			"extend_fields": "abtags,hot_cmt,popularization", "pagesize": strconv.Itoa(size),
			"global_collection_id": collectionID,
		},
	}, &res)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Track, 0, len(res.Data.Songs))
	for _, l := range res.Data.Songs {
		out = append(out, trackFromFile(l))
	}
	return out, res.Data.Count, nil
}

// Rank is a chart: KuGou's name, its cover, and the songs its card shows.
type Rank struct {
	ID     int64
	Name   string
	Pic    string
	Intro  string
	Update string
	Top    []RankLine // the three the card lists
}

// RankLine is a song a chart's card shows: its name, and who sings it.
type RankLine struct {
	Name   string
	Artist string
}

// Ranks returns the charts, each with its top three songs.
func Ranks(c *Client) ([]Rank, error) {
	var res struct {
		Data struct {
			Info []map[string]any `json:"info"`
		} `json:"data"`
	}
	err := c.send("GET", request{
		Path:   "/ocean/v6/rank/list",
		Params: map[string]string{"plat": "2", "withsong": "1", "parentid": "0"},
	}, &res)
	if err != nil {
		return nil, err
	}
	out := make([]Rank, 0, len(res.Data.Info))
	for _, l := range res.Data.Info {
		id := pickInt(l, "rankid", "rank_id")
		if id == 0 {
			continue
		}
		r := Rank{
			ID:     id,
			Name:   clean(pickString(l, "rankname", "name")),
			Pic:    sized(pickString(l, "img_9", "img_cover", "imgurl", "bannerurl")),
			Intro:  clean(pickString(l, "intro")),
			Update: pickString(l, "update_frequency"),
		}
		for _, s := range maps(l["songinfo"]) {
			name := clean(pickString(s, "name", "songname"))
			if i := strings.Index(name, " - "); i > 0 {
				name = clean(name[i+3:])
			}
			if name != "" {
				r.Top = append(r.Top, RankLine{Name: name, Artist: clean(pickString(s, "author", "author_name"))})
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// RankSongs returns a page of a chart's songs, and how many it holds.
func RankSongs(c *Client, rankID int64, page, size int) ([]Track, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 100
	}
	var res struct {
		Total int `json:"total"`
		Data  struct {
			Total    int              `json:"total"`
			SongList []map[string]any `json:"songlist"`
		} `json:"data"`
	}
	err := c.send("POST", request{
		Path: "/openapi/kmr/v2/rank/audio",
		Hdr:  map[string]string{"kg-tid": "369"},
		Body: map[string]any{
			"area_code": 1, "pagesize": size, "rank_cid": 0, "type": 1,
			"page": page, "rank_id": rankID,
			"show_portrait_mv": 1, "show_type_total": 1, "filter_original_remarks": 1,
		},
	}, &res)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Track, 0, len(res.Data.SongList))
	for _, l := range res.Data.SongList {
		out = append(out, trackFromRank(l))
	}
	total := res.Data.Total
	if total == 0 {
		total = res.Total
	}
	return out, total, nil
}

// DailySongs returns the songs picked for the listener today.
func DailySongs(c *Client) ([]Track, error) {
	var res struct {
		Data struct {
			SongList []map[string]any `json:"song_list"`
		} `json:"data"`
	}
	err := c.send("POST", request{
		Router: "everydayrec.service.kugou.com",
		Path:   "/everyday_song_recommend",
		Body:   map[string]any{"platform": "android", "userid": c.UserID},
	}, &res)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(res.Data.SongList))
	for _, l := range res.Data.SongList {
		out = append(out, trackFrom(l))
	}
	return out, nil
}

// StyleSongs returns songs of one style, tagIDs being a comma separated list
// such as "S14" for Mandopop, or "" for whatever the service picks.
func StyleSongs(c *Client, tagIDs string) ([]Track, error) {
	var res struct {
		Data struct {
			SongList []map[string]any `json:"song_list"`
		} `json:"data"`
	}
	err := c.send("POST", request{
		Path:   "/everydayrec.service/everyday_style_recommend",
		Params: map[string]string{"tagids": tagIDs, "platform": "ios"},
		Body:   map[string]any{},
	}, &res)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(res.Data.SongList))
	for _, l := range res.Data.SongList {
		out = append(out, trackFrom(l))
	}
	return out, nil
}

// Album is an album, as its page shows it.
type Album struct {
	ID      int64
	Name    string
	Pic     string
	Artist  string
	Intro   string
	Publish string
	Kind    string // 录音室专辑, 单曲…
	Songs   int
}

// albumHost is where the album services live. They answer through it alone:
// asked of the gateway they fail, which is why no album page would load.
const albumHost = "https://openapi.kugou.com"

// AlbumDetail returns an album and, when the service hands them over, its
// songs.
func AlbumDetail(c *Client, id int64) (*Album, []Track, error) {
	var res struct {
		Data []map[string]any `json:"data"`
	}
	err := c.send("POST", request{
		Base: albumHost,
		Path: "/kmr/v2/albums",
		Hdr:  map[string]string{"kg-tid": "255"},
		Body: map[string]any{"data": []map[string]string{{"album_id": strconv.FormatInt(id, 10)}}},
	}, &res)
	if err != nil {
		return nil, nil, err
	}
	if len(res.Data) == 0 {
		return nil, nil, ErrUnavailable
	}
	l := res.Data[0]
	al := &Album{
		ID:      pickInt(l, "album_id", "id"),
		Name:    clean(pickString(l, "album_name", "name")),
		Pic:     sized(pickString(l, "sizable_cover", "cover")),
		Artist:  clean(pickString(l, "author_name")),
		Intro:   clean(pickString(l, "intro")),
		Publish: pickString(l, "publish_date"),
		Kind:    pickString(l, "type"),
		// The album itself does not say how many songs it holds: its song
		// list does, below.
	}
	if al.ID == 0 {
		al.ID = id
	}

	out, total, err := albumSongs(c, id)
	if err != nil {
		// The album itself is still worth showing.
		return al, nil, nil
	}
	al.Songs = total
	if al.Songs == 0 {
		al.Songs = len(out)
	}
	return al, out, nil
}

// albumSongPage is how many of an album's songs the service hands over at
// once: it refuses a larger page with "too many requests", which is how the
// album's songs came to be missing.
const albumSongPage = 50

// albumSongs reads an album's songs and how many there are in all. The song
// list names a song in three parts — the song, its audio and its album — so
// each is read apart, unlike the flat rows a search hands back.
func albumSongs(c *Client, id int64) ([]Track, int, error) {
	var out []Track
	total := 0
	for page := 1; ; page++ {
		var res struct {
			Data struct {
				Total int              `json:"total"`
				Songs []map[string]any `json:"songs"`
			} `json:"data"`
		}
		err := c.send("POST", request{
			Base: albumHost,
			Path: "/v1/album_audio/lite",
			Hdr:  map[string]string{"kg-tid": "255"},
			Body: map[string]any{
				"album_id": id, "area_code": 1, "page": page, "pagesize": albumSongPage,
			},
		}, &res)
		if err != nil {
			if page == 1 {
				return nil, 0, err
			}
			break
		}
		total = res.Data.Total
		for _, l := range res.Data.Songs {
			out = append(out, trackFromParts(l))
		}
		if len(res.Data.Songs) < albumSongPage || len(out) >= total {
			break
		}
	}
	return out, total, nil
}

// trackFromParts reads a song as the album and artist services name it: the
// song under "base", its qualities under "audio_info", its singers under
// "authors" and its album under "album_info".
func trackFromParts(l map[string]any) Track {
	base, _ := l["base"].(map[string]any)
	info, _ := l["audio_info"].(map[string]any)
	album, _ := l["album_info"].(map[string]any)
	rights, _ := l["copyright"].(map[string]any)
	sale, _ := l["deprecated"].(map[string]any)
	extend, _ := l["extend"].(map[string]any)

	t := Track{
		AlbumAudioID: pickInt(base, "album_audio_id"),
		AlbumID:      pickInt(base, "album_id"),
		Name:         clean(pickString(base, "audio_name")),
		Singer:       clean(pickString(base, "author_name")),
		Album:        clean(pickString(album, "album_name")),
		Image:        sized(pickString(album, "cover")),
		// The album's date, where the row carries one; the download asks
		// the album itself for the rest.
		Year: pickString(album, "publish_date"),
		// The song's place in its album: the album service keeps the
		// number in "extend" as "sort", and the disc as "disc".
		TrackNo:   int(pickInt(extend, "sort")),
		DiscNo:    int(pickInt(extend, "disc")),
		Hash:      strings.ToLower(pickString(info, "hash", "hash_128")),
		HashHigh:  strings.ToLower(pickString(info, "hash_320")),
		HashLossl: strings.ToLower(pickString(info, "hash_flac")),
		HashHires: strings.ToLower(pickString(info, "hash_high", "hash_super")),
		Duration:  pickInt(info, "duration_128", "duration"),
		Size:      pickInt(info, "filesize_128", "filesize"),
		// What the song costs is what its rights say, as it is everywhere
		// else the service lists songs.
		PayType:   int(pickInt(sale, "pay_type")),
		Privilege: int(pickInt(rights, "privilege")),
		Singers:   singersOf(l["authors"]),
	}
	if who := singers(l["authors"]); who != "" {
		t.Singer = who
	}
	if i := strings.Index(t.Name, " - "); i > 0 && t.Singer != "" && strings.Contains(t.Name[:i], t.Singer) {
		t.Name = clean(t.Name[i+3:])
	}
	return t
}

// Artist is a singer, as their page shows them.
type Artist struct {
	ID       int64
	Name     string
	Avatar   string // square
	Pic      string // wide
	Intro    string
	Songs    int
	Albums   int
	Followed int
}

// ArtistDetail returns who a singer is. The service names one singer under
// "data" itself rather than in a list, which is why no artist page loaded.
func ArtistDetail(c *Client, id int64) (*Artist, error) {
	var res struct {
		Data map[string]any `json:"data"`
	}
	err := c.send("POST", request{
		Base: albumHost,
		Path: "/kmr/v3/author",
		Hdr:  map[string]string{"kg-tid": "36"},
		Body: map[string]any{"author_id": id},
	}, &res)
	if err != nil {
		return nil, err
	}
	if len(res.Data) == 0 {
		return nil, ErrUnavailable
	}
	l := res.Data
	a := &Artist{
		ID:     pickInt(l, "author_id", "id"),
		Name:   clean(pickString(l, "author_name", "name")),
		Avatar: sized(pickString(l, "sizable_avatar", "avatar")),
		Pic:    sized(pickString(l, "sizable_cover", "cover")),
		Intro:  clean(pickString(l, "intro", "brief_desc")),
		Songs:  int(pickInt(l, "song_count", "audio_total", "music_total")),
		Albums: int(pickInt(l, "album_count", "album_total")),
	}
	// A singer's picture is a square one; the wide one only comes with it.
	if a.Pic == "" {
		a.Pic = a.Avatar
	}
	if a.ID == 0 {
		a.ID = id
	}
	return a, nil
}

// ArtistSongs returns a page of a singer's songs, newest first.
func ArtistSongs(c *Client, id int64, page, size int) ([]Track, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 100
	}
	var res struct {
		Data []map[string]any `json:"data"`
	}
	err := c.send("POST", request{
		Base: albumHost,
		Path: "/kmr/v1/audio_group/author",
		Hdr:  map[string]string{"kg-tid": "220"},
		Body: map[string]any{"author_id": id, "page": page, "pagesize": size, "area_code": 1},
	}, &res)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(res.Data))
	for _, l := range res.Data {
		out = append(out, trackFrom(l))
	}
	return out, nil
}

// ArtistAlbums returns a page of a singer's albums, and whether more follow.
func ArtistAlbums(c *Client, id int64, page, size int) ([]Album, bool, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 30
	}
	var res struct {
		Data []map[string]any `json:"data"`
	}
	err := c.send("POST", request{
		Base: albumHost,
		Path: "/kmr/v1/author/albums",
		Hdr:  map[string]string{"kg-tid": "36"},
		Body: map[string]any{"author_id": id, "page": page, "pagesize": size, "area_code": 1},
	}, &res)
	if err != nil {
		return nil, false, err
	}
	out := make([]Album, 0, len(res.Data))
	for _, l := range res.Data {
		out = append(out, Album{
			ID:      pickInt(l, "album_id", "id"),
			Name:    clean(pickString(l, "album_name", "name")),
			Pic:     sized(pickString(l, "sizable_cover", "cover")),
			Artist:  clean(pickString(l, "author_name")),
			Publish: pickString(l, "publish_date"),
			Songs:   int(pickInt(l, "audio_total", "songcount")),
		})
	}
	return out, len(out) == size, nil
}

// PersonalFM returns songs picked for the listener: from their taste, from a
// style, or — when seed is given — continuing after that song.
func PersonalFM(c *Client, pool int, seed *Track) ([]Track, error) {
	ms := nowMS()
	body := map[string]any{
		"appid": c.appID(), "clienttime": ms, "mid": c.Dev.MID, "action": "play",
		"recommend_source_locked": 0, "song_pool_id": pool, "callerid": 0, "m_type": 1,
		"platform": "ios", "area_code": 1, "remain_songcnt": 0, "clientver": c.clientVer(),
		"is_overplay": 0, "mode": "normal",
		// The player's own fake media id, which the service expects.
		"fakem": "ca981cfc583a4c37f28d2d49000013c16a0a",
		"key":   keyForWith(strconv.FormatInt(ms, 10), c.Dev.MID, strconv.FormatInt(c.UserID, 10), c.appID(), c.keySalt()),
	}
	if c.UserID != 0 {
		body["userid"], body["kguid"] = c.UserID, c.UserID
	}
	if c.Token != "" {
		body["token"] = c.Token
	}
	if seed != nil {
		body["hash"], body["songid"] = seed.Hash, seed.AlbumAudioID
	}
	var res struct {
		Data struct {
			SongList []map[string]any `json:"song_list"`
		} `json:"data"`
	}
	err := c.send("POST", request{
		Router: "persnfm.service.kugou.com",
		Path:   "/v2/personal_recommend",
		Body:   body,
	}, &res)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(res.Data.SongList))
	for _, l := range res.Data.SongList {
		out = append(out, trackFrom(l))
	}
	return out, nil
}

// Suggestions are the songs a search box offers while typing.
func Suggestions(c *Client, query string) ([]Track, []string, error) {
	var res struct {
		Data struct {
			Lists []map[string]any `json:"lists"`
		} `json:"data"`
	}
	err := c.send("GET", request{
		Router: "searchtip.kugou.com",
		Path:   "/v2/getSearchTip",
		Params: map[string]string{
			"keyword": query, "AlbumTipCount": "10", "CorrectTipCount": "10",
			"MVTipCount": "10", "MusicTipCount": "10",
		},
	}, &res)
	if err != nil {
		return nil, nil, err
	}
	var songs []Track
	var words []string
	for _, l := range res.Data.Lists {
		if t := trackFrom(l); t.Name != "" {
			songs = append(songs, t)
		}
		if w := clean(pickString(l, "keyword", "KeyWord")); w != "" {
			words = append(words, w)
		}
	}
	return songs, words, nil
}

// trackFromFile reads a song out of a playlist's file list, which names the
// same song differently from the search service: its qualities arrive as a
// list of goods, and its name arrives as "artist - song".
func trackFromFile(l map[string]any) Track {
	t := Track{
		AlbumAudioID: pickInt(l, "mixsongid", "add_mixsongid", "album_audio_id", "audio_id"),
		AlbumID:      pickInt(l, "album_id"),
		Name:         clean(pickString(l, "filename", "songname", "name")),
		Singer:       clean(pickString(l, "singername", "author_name")),
		Album:        clean(pickString(l, "remark", "album_name")),
		Duration:     duration(l),
		Size:         pickInt(l, "size", "filesize"),
		Image:        coverOf(l),
		Year:         pickString(l, "publish_date"),
		Hash:         strings.ToLower(pickString(l, "hash", "filehash")),
		PayType:      int(pickInt(l, "feetype", "pay_type")),
		Privilege:    int(pickInt(l, "privilege")),
		FileID:       pickInt(l, "fileid", "file_id"),
		Singers:      singersOf(fieldOf(l, "singerinfo", "singers", "Singers", "authors")),
	}
	// A file's qualities are its goods: one per bit rate it was uploaded at.
	for _, g := range maps(l["relate_goods"]) {
		t.put(strings.ToLower(pickString(g, "hash")), pickInt(g, "level"), pickInt(g, "bitrate"))
	}
	// The name comes as the credits read; the singers are given apart.
	if who := singers(l["singerinfo"]); who != "" {
		t.Singer = who
	}
	if i := strings.Index(t.Name, " - "); i > 0 && t.Singer != "" && strings.Contains(t.Name[:i], t.Singer) {
		t.Name = clean(t.Name[i+3:])
	}
	if info, ok := l["albuminfo"].(map[string]any); ok {
		if t.Album == "" {
			t.Album = clean(pickString(info, "name"))
		}
		if t.Year == "" {
			t.Year = pickString(info, "publish_date")
		}
	}
	return t
}

// trackFromRank reads a chart's song, which keeps its hashes in audio_info
// rather than at the top: the chart needs no hash of its own, so it sends
// none.
func trackFromRank(l map[string]any) Track {
	t := trackFrom(l)
	if info, ok := l["audio_info"].(map[string]any); ok {
		if t.Hash == "" {
			t.Hash = strings.ToLower(pickString(info, "hash_128", "hash"))
		}
		if t.HashHigh == "" {
			t.HashHigh = strings.ToLower(pickString(info, "hash_320"))
		}
		if t.HashLossl == "" {
			t.HashLossl = strings.ToLower(pickString(info, "hash_flac"))
		}
		if t.HashHires == "" {
			t.HashHires = strings.ToLower(pickString(info, "hash_high", "hash_super"))
		}
		if t.Duration == 0 {
			t.Duration = pickInt(info, "duration_128", "duration")
		}
		if t.Size == 0 {
			t.Size = pickInt(info, "filesize_128", "filesize")
		}
	}
	if who := singers(l["authors"]); who != "" {
		t.Singer = who
	}
	if i := strings.Index(t.Name, " - "); i > 0 {
		t.Name = clean(t.Name[i+3:])
	}
	return t
}

// duration reads a millisecond length from the names the services give it,
// some of which count seconds. A playlist's file list calls it "timelen",
// which is why the songs of a playlist showed no length at all.
func duration(l map[string]any) int64 {
	for _, k := range []string{"timelength", "time_length", "timelen", "duration", "duration_128", "Duration"} {
		if d := pickInt(l, k); d > 0 {
			if d < 10000 {
				return d * 1000
			}
			return d
		}
	}
	return 0
}

func singers(v any) string {
	var names []string
	for _, s := range maps(v) {
		if n := clean(pickString(s, "name", "author_name")); n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, "、")
}

// singersOf reads the singers of a song with the number each one's page
// answers to, whichever of the names the services give them by.
func singersOf(v any) []Singer {
	var out []Singer
	for _, s := range maps(v) {
		name := clean(pickString(s, "name", "author_name", "nickname"))
		if name == "" {
			continue
		}
		out = append(out, Singer{ID: pickInt(s, "id", "author_id", "singerid"), Name: name})
	}
	return out
}

// fieldOf returns the first of keys the map holds something under, which is
// how a song names its singers in one service and its authors in another.
func fieldOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

// maps reads an array of objects, whatever the field is called and whether
// the service sent an array at all.
func maps(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, x := range arr {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func pickTags(v any) []string {
	var out []string
	for _, t := range maps(v) {
		if n := pickString(t, "tag_name", "name"); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// sized asks the image CDN for a picture big enough to show, in place of the
// {size} the service leaves in its URLs.
func sized(pic string) string {
	if pic == "" {
		return ""
	}
	pic = strings.ReplaceAll(pic, "{size}", "480")
	return strings.Replace(pic, "http://", "https://", 1)
}

// Tags returns the playlist categories, most used first, for a browse page.
func Tags(c *Client) ([]string, error) {
	var res struct {
		Data []map[string]any `json:"data"`
	}
	err := c.send("GET", request{
		Path:   "/v1/tag/list",
		Params: map[string]string{"plat": "1"},
	}, &res)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range res.Data {
		if n := pickString(l, "tag_name", "name"); n != "" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}
