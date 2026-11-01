package netease

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/kugou"
)

// The app names things by numbers: a song by one int64, a playlist by
// another. KuGou names them by hashes and by collection ids, which are
// strings. This package holds the numbers, and the table that turns them
// back: a song's hash and album audio, and a playlist's collection id.

var (
	ErrUnavailable = errors.New("this song isn't available to stream")
	ErrInPlaylist  = errors.New("the song is in the playlist already")

	// ErrUnsupported is what a call answers when KuGou has nothing behind
	// it: signing in is NetEase's, and so are the comments and the rest of
	// an account.
	ErrUnsupported = errors.New("KuGou has no such call")
)

// Levels are the stream levels, from the lowest.
var Levels = []string{"standard", "higher", "exhigh", "lossless", "hires"}

// Lossless reports whether level streams FLAC.
func Lossless(level string) bool { return level == "lossless" || level == "hires" }

var (
	once    sync.Once
	loaded  *session
	loadErr error
)

// state is everything this package keeps between calls: who is signed in,
// the client that talks to KuGou, and the table of the numbers it handed out.
type state struct {
	client *kugou.Client
	index  *kugou.Index
	lists  *listIndex
	files  *fileTable
}

var app state

// session is the login KuGou gave the listener.
type session struct {
	Token    string `json:"token"`
	UserID   int64  `json:"userid"`
	Nickname string `json:"nickname,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	// What the account holds, and whether it signed in as the 概念版: both
	// are what the badge beside the name says, and the second is what
	// decides how songs are streamed.
	VIPType  int    `json:"vip_type,omitempty"`
	VIPToken string `json:"vip_token,omitempty"`
	Lite     bool   `json:"lite,omitempty"`
	Concept  bool   `json:"concept,omitempty"`
	// What the subscriptions are, and when each ends: the 概念版 sells two,
	// and the account's page says how long it has them for.
	Products []kugou.Product `json:"products,omitempty"`
}

// Account is who is signed in, as the pages and the player see them.
func (s *session) Account() *Profile {
	if s == nil {
		return nil
	}
	p := &Profile{
		UserID:    s.UserID,
		Nickname:  s.Nickname,
		AvatarURL: s.Avatar,
		VIP:       s.VIPType != 0 || s.Concept,
		Lite:      s.Lite,
		Label:     vipLabel(s),
	}
	for _, prod := range s.Products {
		p.Products = append(p.Products, VIPProduct{Name: prod.Name(), End: prod.End})
	}
	return p
}

// vipLabel is what the badge says, in the order that fits the most in: the
// 概念版's own subscription, then the build the account signed in as, then
// the standard subscription.
func vipLabel(s *session) string {
	switch {
	case s.Concept:
		return "概念版VIP"
	case s.Lite:
		return "概念版"
	case s.VIPType != 0:
		return "VIP"
	}
	return ""
}

// dataDir is where the app keeps what it must not lose: the device it
// registered, the songs it has named, the login.
func dataDir() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// kuGou returns the client, registering a device on first use.
func kuGou() (*kugou.Client, error) {
	once.Do(func() {
		dir, err := dataDir()
		if err != nil {
			loadErr = err
			return
		}
		c, err := kugou.New(dir)
		if err != nil {
			loadErr = err
			return
		}
		idx, err := kugou.OpenIndex(dir)
		if err != nil {
			loadErr = err
			return
		}
		lists, err := openListIndex(dir)
		if err != nil {
			loadErr = err
			return
		}
		files, err := openFileTable(dir)
		if err != nil {
			loadErr = err
			return
		}
		// A device KuGou has never seen is answered "status 0" by the
		// search service, and refused a stream; registering it is what
		// makes this install a player rather than an anonymous caller.
		if err := c.EnsureDevice(); err != nil && c.DFID() == "" {
			loadErr = err
			return
		}
		app = state{client: c, index: idx, lists: lists, files: files}
		if s := readSession(); s != nil {
			loaded = s
			c.Lite = s.Lite
			c.SetUser(s.UserID, s.Token)
			c.SetVIP(s.VIPType, s.VIPToken)
		}
	})
	if loadErr != nil {
		return nil, loadErr
	}
	return app.client, nil
}

// listIndex is the table that turns the numbers this package gives playlists
// and charts back into what KuGou calls them.
type listIndex struct {
	mu   sync.Mutex
	path string
	byID map[int64]listRef
}

type listRef struct {
	// Kind is what the number names: "list" for a public playlist, "rank"
	// for a chart, "cloud" for one of the listener's own, and "liked" for
	// the one their hearts go in.
	Kind string `json:"kind"`
	Ref  string `json:"ref"` // its collection id, rank id or listid
	Type int    `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
	Pic  string `json:"pic,omitempty"`
}

func openListIndex(dir string) (*listIndex, error) {
	x := &listIndex{path: filepath.Join(dir, "kugou-lists.json"), byID: map[int64]listRef{}}
	if b, err := os.ReadFile(x.path); err == nil {
		json.Unmarshal(b, &x.byID)
	}
	return x, nil
}

// remember names a playlist, and answers the number the app knows it by.
func (x *listIndex) remember(kind, ref, name, pic string) int64 {
	id := kugou.SongID(kind + ":" + ref)
	x.mu.Lock()
	defer x.mu.Unlock()
	known := x.byID[id]
	if known.Name == name && known.Pic == pic {
		return id
	}
	x.byID[id] = listRef{Kind: kind, Ref: ref, Name: name, Pic: pic}
	x.save()
	return id
}

// rememberCloud names one of the listener's own playlists, which KuGou knows
// by a listid and whether it was made here or saved.
func (x *listIndex) rememberCloud(kind string, listid int64, typ int, name, pic string) int64 {
	id := kugou.SongID(kind + ":" + strconv.FormatInt(listid, 10))
	x.mu.Lock()
	defer x.mu.Unlock()
	known := x.byID[id]
	ref := listRef{Kind: kind, Ref: strconv.FormatInt(listid, 10), Type: typ, Name: name, Pic: pic}
	if known == ref {
		return id
	}
	x.byID[id] = ref
	x.save()
	return id
}

func (x *listIndex) lookup(id int64) (listRef, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.byID[id]
	return r, ok
}

func (x *listIndex) save() {
	b, err := json.Marshal(x.byID)
	if err != nil {
		return
	}
	os.WriteFile(x.path, b, 0o600)
}

func sessionFile() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kugou-session.json"), nil
}

const sessionSecret = "kugou-session"

// The login is kept in the user data directory, beside the device and the
// tables, and in the keychain when that takes it. The Credential Manager
// refused the write on this machine, and a login that is lost every time the
// app closes reads as a login that did not happen; the file is what is read
// first, so a session is one that survives a restart.
func readSession() *session {
	if path, err := sessionFile(); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			if s := parseSession(b); s != nil {
				return s
			}
		}
	}
	if b, err := mygo.Secrets.Get(sessionSecret); err == nil {
		if s := parseSession(b); s != nil {
			return s
		}
	}
	return nil
}

func parseSession(b []byte) *session {
	if len(b) == 0 {
		return nil
	}
	var s session
	if json.Unmarshal(b, &s) != nil || s.Token == "" {
		return nil
	}
	return &s
}

func writeSession(s *session) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	// The keychain is the better place for a token; failing to reach it
	// must not cost the listener their login.
	keychain := mygo.Secrets.Set(sessionSecret, b)
	path, err := sessionFile()
	if err != nil {
		return err
	}
	file := os.WriteFile(path, b, 0o600)
	if file != nil && keychain != nil {
		return file
	}
	return nil
}

func clearSession() {
	loaded = nil
	mygo.Secrets.Delete(sessionSecret)
	if path, err := sessionFile(); err == nil {
		os.Remove(path)
	}
}

// artistsOf names the singers a song is credited to, each with the number
// their page answers to, which is what makes their name a link. KuGou
// counts some of them once and names them again among the others, so a
// singer already listed is not listed twice.
func artistsOf(t kugou.Track) []Artist {
	var out []Artist
	for _, s := range t.Singers {
		if s.ID == 0 {
			continue
		}
		if slices.ContainsFunc(out, func(a Artist) bool { return a.ID == s.ID }) {
			continue
		}
		out = append(out, Artist{ID: s.ID, Name: s.Name})
	}
	if len(out) == 0 {
		return []Artist{{Name: t.Singer}}
	}
	return out
}

// toTrack turns a KuGou song into the one the app knows, naming it by the
// number derived from its hash.
func toTrack(t kugou.Track) Track {
	out := Track{
		ID:       app.index.Remember(t),
		Name:     t.Name,
		Duration: t.Duration,
		Album:    Album{ID: t.AlbumID, Name: t.Album, PicURL: t.Image},
		Hash:     t.Hash,
	}
	if t.Singer != "" {
		out.Artists = artistsOf(t)
	}
	// Whether a song streams is what KuGou's privilege says, not its price:
	// a sold song carries privilege 10 and is refused, while one sold but
	// still free to hear carries 8 and plays. Asking for it was how this
	// rule was found.
	//
	// The badge is a separate question, and the row answers it whether the
	// song plays here or not: a subscription song is a subscription song.
	out.VIP = t.PayType == 1 || t.PayType == 2 || t.Privilege >= 9
	switch {
	case t.Privilege >= 8 || (t.Privilege == 0 && t.PayType == 0):
		out.Privilege = &Privilege{ID: out.ID, Pl: 320}
	case lite() && out.VIP && t.PayType != 3:
		// The 概念版 hears a subscription song at standard quality, which
		// is what its own client plays; a song sold outright is still the
		// listener's to buy.
		out.Privilege = &Privilege{ID: out.ID, Pl: 128}
	default:
		out.Fee = 1
		out.Privilege = &Privilege{ID: out.ID, Pl: 0}
	}
	return out
}

// lite reports whether the player speaks as the 概念版, which changes what it
// may hear.
func lite() bool { return app.client != nil && app.client.Lite }

// toPlaylist turns a KuGou playlist into the one the app knows.
func toPlaylist(sp kugou.Special) Playlist {
	p := Playlist{
		Name:        sp.Name,
		CoverURL:    sp.Pic,
		PicURL:      sp.Pic,
		Description: sp.Intro,
		PlayCount:   float64(sp.PlayCount),
		TrackCount:  sp.Count,
	}
	p.Creator.Nickname = sp.Creator
	p.ID = app.lists.remember("list", sp.CollectionID, sp.Name, sp.Pic)
	for _, t := range sp.Songs {
		p.Tracks = append(p.Tracks, toTrack(t))
	}
	return p
}

// fromKind reads the number back into what KuGou calls the thing.
func fromKind(id int64) (string, string, bool) {
	r, ok := app.lists.lookup(id)
	if !ok {
		return "", "", false
	}
	return r.Kind, r.Ref, true
}

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// preview is a chart's card, as the app draws it: the song, then who sings it.
func preview(lines []kugou.RankLine) []ChartLine {
	out := make([]ChartLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, ChartLine{First: l.Name, Second: l.Artist})
	}
	return out
}
