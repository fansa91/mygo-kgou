package netease

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"netease-music-mygo/internal/kugou"
)

// What the app offers beyond the songs it was asked for: the songs beside a
// song, the scenes KuGou's radio is built from, a song's record across the
// charts, a chart's past numbers, the words listeners keep using in a song's
// comments, and the covers and the order of the listener's own playlists.

// SimilarSongs returns songs like id, the first handful of them.
func SimilarSongs(id int64) ([]Track, error) { return SimilarRadio(id) }

// SimilarRadio returns a few songs like id. KuGou has no list of similar
// songs to ask for: what it has is the personal FM, which takes a song as
// the pool it recommends from — a song the listener likes starts 心动模式,
// and any song starts this. The pool asked for is the one the service
// describes as recommending by style, which 心动模式 uses too, so the radio
// goes on from the song rather than from the listener's taste at large.
func SimilarRadio(id int64) ([]Track, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	seed, ok := app.index.Track(id)
	if !ok || seed.Hash == "" {
		return nil, ErrUnavailable
	}
	songs, err := kugou.PersonalFM(c, 1, &seed)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(songs))
	for _, t := range songs {
		// The song it goes on from is already playing.
		if t.Hash == seed.Hash {
			continue
		}
		out = append(out, toTrack(t))
	}
	return out, nil
}

// Scene is one of KuGou's scenes: 助眠, 车载, 工作学习 and the rest, each a
// radio of its own.
type Scene struct {
	ID    int64
	Title string
	Popup string
	Label string
	Pic   string
	BG    string
	Color string
	Songs int64
	Plays int64
}

// Scenes returns the scenes the radio is built from.
func Scenes() ([]Scene, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	list, err := kugou.Scenes(c)
	if err != nil {
		return nil, err
	}
	out := make([]Scene, 0, len(list))
	for _, s := range list {
		out = append(out, Scene{
			ID: s.ID, Title: s.Title, Popup: s.Popup, Label: s.Label,
			Pic: s.Pic, BG: s.BG, Color: s.Color, Songs: int64(s.Songs), Plays: s.Plays,
		})
	}
	return out, nil
}

// SceneRadio returns a page of a scene's radio.
func SceneRadio(sceneID int64, page int) ([]Track, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	songs, err := kugou.SceneSongs(c, sceneID, page, 20)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(songs))
	for _, t := range songs {
		out = append(out, toTrack(t))
	}
	return out, nil
}

// RankingPlatform is one platform's record of a song.
type RankingPlatform struct {
	Name  string
	Logo  string
	Rank  int
	Times int
	Last  string
}

// Ranking is a song's record across the charts.
type Ranking struct {
	Song      Track // what it is, for the page to show
	On, Total int
	Title     string
	Note      string
	Platforms []RankingPlatform
}

// SongRanking returns the record id has across the platforms KuGou watches.
func SongRanking(id int64) (*Ranking, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	k, ok := app.index.Lookup(id)
	if !ok || k.AlbumAudioID == 0 {
		return nil, ErrUnavailable
	}
	rec, err := kugou.SongRanking(c, k.AlbumAudioID)
	if err != nil {
		return nil, err
	}
	out := &Ranking{On: rec.On, Total: rec.Total, Title: rec.Title, Note: rec.Note}
	out.Song = Track{
		ID:      id,
		Name:    k.Name,
		Album:   Album{ID: k.AlbumID, PicURL: ""},
		Artists: []Artist{{Name: k.Singer}},
	}
	for _, p := range rec.Platforms {
		out.Platforms = append(out.Platforms, RankingPlatform{
			Name: p.Name, Logo: p.Logo, Rank: p.Rank, Times: p.Times, Last: p.Last,
		})
	}
	return out, nil
}

// RankVol is one of a chart's past numbers.
type RankVol struct {
	ID   int64
	Name string
	Date string
}

// RankVols returns the numbers a chart published, newest first.
func RankVols(playlist int64) ([]RankVol, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	kind, ref, ok := fromKind(playlist)
	if !ok || kind != "rank" {
		return nil, ErrUnsupported
	}
	rankID, _ := strconv.ParseInt(ref, 10, 64)
	list, err := kugou.RankVols(c, rankID)
	if err != nil {
		return nil, err
	}
	out := make([]RankVol, 0, len(list))
	for _, v := range list {
		out = append(out, RankVol{ID: v.ID, Name: v.Name, Date: v.Date})
	}
	return out, nil
}

// RankVolSongs returns a chart as one of its past numbers stood, named for
// the page that shows it: the chart's name, then the number's.
func RankVolSongs(playlist, vol int64, volName string) (*Playlist, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	kind, ref, ok := fromKind(playlist)
	if !ok || kind != "rank" {
		return nil, ErrUnsupported
	}
	rankID, _ := strconv.ParseInt(ref, 10, 64)
	p := Playlist{ID: playlist}
	if r, ok := app.lists.lookup(playlist); ok {
		p.Name, p.CoverURL, p.PicURL = r.Name, r.Pic, r.Pic
	}
	for page := 1; page <= 10; page++ {
		songs, total, err := kugou.RankSongsAt(c, rankID, vol, page, 100)
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
	if volName != "" {
		p.Description = volName
	}
	return &p, nil
}

// CommentHotWords returns the words listeners keep using in a song's
// comments, most used first.
func CommentHotWords(id int64) ([]string, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	k, ok := app.index.Lookup(id)
	if !ok || k.AlbumAudioID == 0 {
		return nil, ErrUnsupported
	}
	words, err := kugou.CommentHotWords(c, k.AlbumAudioID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, w.Word)
	}
	return out, nil
}

// CommentsByWord returns a page of the comments a hot word is in.
func CommentsByWord(id int64, word string, page, limit int) (*Comments, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	k, ok := app.index.Lookup(id)
	if !ok || k.AlbumAudioID == 0 {
		return nil, ErrUnsupported
	}
	if limit <= 0 {
		limit = 30
	}
	list, err := kugou.CommentsByWord(c, k.AlbumAudioID, word, page, limit)
	if err != nil {
		return nil, err
	}
	out := &Comments{Total: list.Total, More: (page-1)*limit+len(list.List) < list.Total}
	for _, m := range list.List {
		out.Hot = append(out.Hot, toComment(m))
	}
	return out, nil
}

// MovePlaylist moves one of the listener's own playlists up or down among
// those of its kind, and answers the order it left them in, by listid.
func MovePlaylist(id int64, up bool) ([]int64, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	kind, ref, ok := fromKind(id)
	if !ok || (kind != "cloud" && kind != "liked") {
		return nil, ErrUnsupported
	}
	listid, _ := strconv.ParseInt(ref, 10, 64)
	r, _ := app.lists.lookup(id)
	lists, ver, err := kugou.MyListsVersion(c)
	if err != nil {
		return nil, err
	}
	// The lists are ordered among those of their kind: the ones made here
	// and those saved from others are numbered apart. They are taken in the
	// order the app shows them, which is the one being changed.
	var same []kugou.UserList
	for _, l := range kugou.OrderedLists(lists) {
		if l.Type != r.Type || isLikedList(l) {
			continue
		}
		same = append(same, l)
	}
	at := -1
	for i, l := range same {
		if l.ID == listid {
			at = i
		}
	}
	if at < 0 {
		return nil, ErrUnsupported
	}
	to := at - 1
	if !up {
		to = at + 1
	}
	if to < 0 || to >= len(same) {
		return nil, nil // already at the end it was moved to
	}
	same[at], same[to] = same[to], same[at]
	if err := kugou.SortLists(c, ver, same); err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(same))
	for _, l := range same {
		out = append(out, l.ID)
	}
	return out, nil
}

// SetPlaylistCover gives one of the listener's playlists the picture in the
// file at path, which is uploaded first, and answers the cover the service
// now serves.
func SetPlaylistCover(id int64, path string) (string, error) {
	c, err := kuGou()
	if err != nil {
		return "", err
	}
	kind, ref, ok := fromKind(id)
	if !ok || (kind != "cloud" && kind != "liked") {
		return "", ErrUnsupported
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	name, err := kugou.UploadCover(c, data, strings.TrimPrefix(filepath.Ext(path), "."))
	if err != nil {
		return "", err
	}
	listid, _ := strconv.ParseInt(ref, 10, 64)
	r, _ := app.lists.lookup(id)
	lists, ver, err := kugou.MyListsVersion(c)
	if err != nil {
		return "", err
	}
	list := kugou.UserList{ID: listid, Type: r.Type}
	for _, l := range lists {
		if l.ID == listid {
			list = l
		}
	}
	cover, err := kugou.SetListCover(c, ver, list, name)
	if err != nil {
		return "", err
	}
	app.lists.rememberCloud(kind, listid, r.Type, r.Name, cover, r.GID)
	return cover, nil
}
