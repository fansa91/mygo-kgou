package netease

import (
	"errors"
	"strconv"
	"sync"
	"time"

	"netease-music-mygo/internal/kugou"
)

// Signing in. KuGou logs a listener in by a QR code their phone scans: a key
// is made here, the phone authorises it, and the answer carries the token
// every other call then carries.

const (
	QRExpired   = 800
	QRWaiting   = 801
	QRScanned   = 802
	QRConfirmed = 803
)

// The service that hands out QR codes, and the app it believes is asking.
const (
	loginHost = "https://login-user.kugou.com"
	srcAppID  = "2919"
)

// QRKey returns a key to draw a login QR code for.
func QRKey() (string, error) {
	c, err := kuGou()
	if err != nil {
		return "", err
	}
	var res struct {
		Data struct {
			QRCode string `json:"qrcode"`
		} `json:"data"`
	}
	err = c.SendWeb("GET", loginHost, "/v2/qrcode", map[string]string{
		"appid": "1001", "type": "1", "plat": "4",
		// The appid the code is drawn for decides whose token the scan
		// hands back: a code drawn for the standard client logs in as
		// it, and the 概念版 refuses that token as another build's. The
		// srcappid stays the login service's own either way.
		"qrcode_txt": "https://h5.kugou.com/apps/loginQRCode/html/index.html?appid=" + c.AppID() + "&",
		"srcappid":   srcAppID,
	}, nil, &res)
	if err != nil {
		return "", err
	}
	if res.Data.QRCode == "" {
		return "", errors.New("netease: no QR code")
	}
	return res.Data.QRCode, nil
}

// LoginURL is what the QR code holds, for a phone to open.
func LoginURL(key string) string {
	return "https://h5.kugou.com/apps/loginQRCode/html/index.html?qrcode=" + key
}

// QRCheck reports where the login of key is: a QR state, or another code with
// the service's message when it refuses. Once confirmed, the session is kept
// and every later call carries it.
func QRCheck(key string) (code int, msg, nickname, avatar string, err error) {
	c, err := kuGou()
	if err != nil {
		return 0, "", "", "", err
	}
	var res struct {
		Data struct {
			Status   int    `json:"status"`
			Token    string `json:"token"`
			UserID   int64  `json:"userid"`
			Nickname string `json:"nickname"`
			Pic      string `json:"user_pic"`
			Msg      string `json:"msg"`
		} `json:"data"`
		Status  int `json:"status"`
		ErrCode int `json:"error_code"`
	}
	err = c.SendWeb("GET", loginHost, "/v2/get_userinfo_qrcode", map[string]string{
		"plat": "4", "appid": c.AppID(), "srcappid": srcAppID,
		"qrcode": key, "dev": c.DFID(),
	}, nil, &res)
	if err != nil {
		return 0, "", "", "", err
	}
	switch res.Data.Status {
	case 0:
		return QRExpired, "the QR code expired", "", "", nil
	case 1:
		return QRWaiting, "", "", "", nil
	case 2:
		return QRScanned, "", res.Data.Nickname, res.Data.Pic, nil
	case 4:
		s := &session{
			Token:    res.Data.Token,
			UserID:   res.Data.UserID,
			Nickname: res.Data.Nickname,
			Avatar:   res.Data.Pic,
		}
		if s.Token == "" {
			return 0, "no token", "", "", errors.New("netease: login answered without a token")
		}
		s.Lite = c.Lite
		// The QR answer carries the token, not the name; ask the account
		// service for the rest so the sidebar has something to show.
		readAccount(c, s)
		loaded = s
		c.SetUser(s.UserID, s.Token)
		c.SetVIP(s.VIPType, s.VIPToken)
		if err := writeSession(s); err != nil {
			return QRConfirmed, err.Error(), s.Nickname, s.Avatar, nil
		}
		return QRConfirmed, "", s.Nickname, s.Avatar, nil
	}
	if res.Data.Msg != "" {
		return 0, res.Data.Msg, "", "", nil
	}
	return 0, "code " + strconv.Itoa(res.ErrCode), "", "", nil
}

// Account returns the profile of who is signed in, or nil.
func Account() (*Profile, error) {
	if _, err := kuGou(); err != nil {
		return nil, err
	}
	if loaded == nil {
		return nil, nil
	}
	return loaded.Account(), nil
}

// RefreshAccount asks the services what the account is now. The name and the
// subscriptions change without the app — a subscription is bought on a phone,
// and it should show here without signing out and in again.
func RefreshAccount() (*Profile, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	if loaded == nil {
		return nil, nil
	}
	readAccount(c, loaded)
	writeSession(loaded)
	return loaded.Account(), nil
}

// readAccount fills in what the login's own answer left out — the name, the
// picture and the subscription — from the account and VIP services. A service
// that does not answer costs the session nothing.
func readAccount(c *kugou.Client, s *session) {
	acc, err := profile(c)
	if err == nil {
		if s.Nickname == "" {
			s.Nickname = acc.Nickname
		}
		if s.Avatar == "" {
			s.Avatar = acc.Avatar
		}
		if acc.VIPType != 0 {
			s.VIPType = acc.VIPType
		}
		if acc.Concept {
			s.Concept = true
		}
	}
	// The 概念版's subscription is not in the account service's answer at
	// all: it is sold on a business line of its own, and only the
	// subscription service knows it — nor does that service collapse the
	// two products it sells into one, as each ends on a day of its own.
	if vip, err := kugou.UnionVIP(c); err == nil {
		s.Concept = vip.Concept
		s.Products = vip.ConceptProducts
		if vip.VIPType != 0 && s.VIPType == 0 {
			s.VIPType = vip.VIPType
		}
	}
}

// profile is the account service asked again when it turns the app away. It
// answers HTTP 403 to a burst of requests from one device, which is what
// starting up is: the name and the picture arrive a moment later rather than
// not at all, and the badge is not worth losing to a race.
func profile(c *kugou.Client) (kugou.Account, error) {
	var (
		acc kugou.Account
		err error
	)
	for i := range 3 {
		if acc, err = kugou.Profile(c); err == nil {
			return acc, nil
		}
		time.Sleep(time.Duration(400*(i+1)) * time.Millisecond)
	}
	return acc, err
}

// SaveSession keeps the session. It is written when the login completes, so
// there is nothing left to do here, and the call stays for the app's sake.
func SaveSession() error {
	if loaded == nil {
		return errors.New("no session")
	}
	return writeSession(loaded)
}

// RestoreSession reports whether a session was kept.
func RestoreSession() bool {
	if _, err := kuGou(); err != nil {
		return false
	}
	return loaded != nil
}

// myName is the nickname of who is signed in, as the session kept it.
func myName() string {
	if loaded == nil {
		return ""
	}
	return loaded.Nickname
}

// ClearSession forgets the session, here and in the keychain.
func ClearSession() {
	clearSession()
	forgetLiked()
	if app.client != nil {
		app.client.SetUser(0, "")
	}
}

// Logout ends the session.
func Logout() { ClearSession() }

// SetConcept makes the player speak as the 概念版, or as the standard client
// again. Only the client changes: which client a login was made as is part of
// that login, and a token is refused by the other one, so signing in again is
// what settles it rather than this rewording the session.
func SetConcept(on bool) {
	c, err := kuGou()
	if err != nil {
		return
	}
	c.Lite = on
}

// Concept reports whether the player speaks as the 概念版.
func Concept() bool {
	c, err := kuGou()
	return err == nil && c.Lite
}

// SendLoginCode asks KuGou to text a sign-in code to mobile.
func SendLoginCode(mobile string) error {
	c, err := kuGou()
	if err != nil {
		return err
	}
	return kugou.SendSMSCode(c, mobile)
}

// LoginWithCode signs the listener in with the code KuGou texted to mobile,
// and keeps the session the login handed back.
func LoginWithCode(mobile, code string) (*Profile, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	s, err := kugou.LoginByCode(c, mobile, code)
	if err != nil {
		return nil, err
	}
	ses := &session{Token: s.Token, UserID: s.UserID, Nickname: s.Nickname, Avatar: s.Avatar,
		VIPType: s.VIPType, VIPToken: s.VIPToken, Lite: s.Lite}
	loaded = ses
	c.SetUser(s.UserID, s.Token)
	c.SetVIP(s.VIPType, s.VIPToken)
	readAccount(c, ses)
	if err := writeSession(ses); err != nil {
		return nil, err
	}
	forgetLiked()
	return ses.Account(), nil
}

// liked is the listener's liked list, once looked up: KuGou names it by a
// listid that is theirs alone, so it is asked for rather than known.
var (
	likedOnce sync.Mutex
	likedList *kugou.UserList
)

func liked() (kugou.UserList, error) {
	c, err := kuGou()
	if err != nil {
		return kugou.UserList{}, err
	}
	likedOnce.Lock()
	defer likedOnce.Unlock()
	if likedList != nil {
		return *likedList, nil
	}
	l, err := kugou.Liked(c)
	if err != nil {
		return kugou.UserList{}, err
	}
	likedList = &l
	return l, nil
}

// forgetLiked drops what was known of the liked list, which belongs to the
// session that has just ended or begun.
func forgetLiked() {
	likedOnce.Lock()
	likedList = nil
	likedOnce.Unlock()
}

// LikedIDs returns the songs the listener liked, which KuGou keeps in a
// playlist of their own.
func LikedIDs(uid int64) ([]int64, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	if !c.SignedIn() {
		return nil, kugou.ErrNoSession
	}
	ll, err := liked()
	if err != nil {
		return nil, err
	}
	var ids []int64
	for page := 1; page <= 20; page++ {
		songs, total, err := kugou.ListSongs(c, ll.ID, ll.Type, page, 200)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		for _, t := range songs {
			tr := toTrack(t)
			ids = append(ids, tr.ID)
			app.files.set(ll.ID, tr.ID, t.FileID)
		}
		if len(songs) < 200 || (total > 0 && len(ids) >= total) {
			break
		}
	}
	app.files.save()
	return ids, nil
}

// Like likes a song for the listener, or unlikes it: KuGou puts it in the
// list their hearts go in, and takes it out again by the number that list
// gave it.
func Like(id int64, like bool) error {
	c, err := kuGou()
	if err != nil {
		return err
	}
	if !c.SignedIn() {
		return kugou.ErrNoSession
	}
	ll, err := liked()
	if err != nil {
		return err
	}
	if like {
		t, ok := app.index.Track(id)
		if !ok {
			return ErrUnavailable
		}
		return kugou.AddToList(c, ll.ID, []kugou.Track{t}, ll.Type)
	}
	file, ok := app.files.get(ll.ID, id)
	if !ok {
		// The list was never read on this run: read it, which is what
		// says where the song sits.
		if _, err := LikedIDs(c.UserID); err != nil {
			return err
		}
		file, ok = app.files.get(ll.ID, id)
	}
	if !ok {
		return nil // it is not in the list; nothing to take out
	}
	if err := kugou.DelFromList(c, ll.ID, []int64{file}, ll.Type); err != nil {
		return err
	}
	app.files.forget(ll.ID, id)
	app.files.save()
	return nil
}

// UserPlaylists returns the playlists a listener made, the liked songs
// first, and those they saved.
func UserPlaylists(uid int64) (made, saved []Playlist, err error) {
	c, err := kuGou()
	if err != nil {
		return nil, nil, err
	}
	// KuGou answers this for the listener alone.
	if !c.SignedIn() || uid != c.UserID {
		return nil, nil, nil
	}
	lists, err := kugou.MyLists(c)
	if err != nil {
		return nil, nil, err
	}
	for _, l := range lists {
		if isLikedList(l) {
			p := Playlist{
				Name: "我喜欢的音乐", CoverURL: l.Pic, PicURL: l.Pic,
				Description: "我喜欢的音乐", SpecialType: 5, TrackCount: l.Count,
			}
			p.ID = app.lists.rememberCloud("liked", l.ID, l.Type, "我喜欢的音乐", l.Pic)
			p.Creator.UserID, p.Creator.Nickname = c.UserID, myName()
			made = append([]Playlist{p}, made...)
			continue
		}
		p := Playlist{Name: l.Name, CoverURL: l.Pic, PicURL: l.Pic, TrackCount: l.Count}
		p.Creator.UserID, p.Creator.Nickname = c.UserID, l.Creator
		if p.Creator.Nickname == "" {
			p.Creator.Nickname = myName()
		}
		p.ID = app.lists.rememberCloud("cloud", l.ID, l.Type, l.Name, l.Pic)
		if l.Type == 1 {
			p.Subscribed = true
			saved = append(saved, p)
			continue
		}
		made = append(made, p)
	}
	return made, saved, nil
}

// isLikedList reports whether l is the list KuGou keeps the liked songs in,
// by the same reading the lookup uses.
func isLikedList(l kugou.UserList) bool { return kugou.LooksLiked(l) }

// CreatePlaylist makes a new playlist for the listener.
func CreatePlaylist(name string) (*Playlist, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	id, err := kugou.CreateList(c, name)
	if err != nil {
		return nil, err
	}
	p := &Playlist{Name: name}
	p.Creator.UserID, p.Creator.Nickname = c.UserID, myName()
	p.ID = app.lists.rememberCloud("cloud", id, 0, name, "")
	return p, nil
}

// RenamePlaylist renames one of the listener's playlists.
func RenamePlaylist(id int64, name string) error {
	c, err := kuGou()
	if err != nil {
		return err
	}
	kind, ref, ok := fromKind(id)
	if !ok || (kind != "cloud" && kind != "liked") {
		return ErrUnsupported
	}
	listid, _ := strconv.ParseInt(ref, 10, 64)
	if err := kugou.RenameList(c, listid, name); err != nil {
		return err
	}
	if r, ok := app.lists.lookup(id); ok {
		app.lists.rememberCloud(kind, listid, r.Type, name, r.Pic)
	}
	return nil
}

// DeletePlaylist deletes one of the listener's playlists; a playlist they
// saved is unsaved instead, which is the same listid going away.
func DeletePlaylist(id int64) error {
	c, err := kuGou()
	if err != nil {
		return err
	}
	kind, ref, ok := fromKind(id)
	if !ok || (kind != "cloud" && kind != "liked") {
		return ErrUnsupported
	}
	listid, _ := strconv.ParseInt(ref, 10, 64)
	r, _ := app.lists.lookup(id)
	return kugou.DelList(c, listid, r.Type)
}

// AddToPlaylist adds songs to one of the listener's playlists.
func AddToPlaylist(pid int64, songs ...int64) error {
	c, err := kuGou()
	if err != nil {
		return err
	}
	kind, ref, ok := fromKind(pid)
	if !ok || (kind != "cloud" && kind != "liked") {
		return ErrUnsupported
	}
	listid, _ := strconv.ParseInt(ref, 10, 64)
	r, _ := app.lists.lookup(pid)
	tracks := make([]kugou.Track, 0, len(songs))
	for _, id := range songs {
		t, ok := app.index.Track(id)
		if !ok {
			continue
		}
		tracks = append(tracks, t)
	}
	if len(tracks) == 0 {
		return ErrUnavailable
	}
	return kugou.AddToList(c, listid, tracks, r.Type)
}

// RemoveFromPlaylist takes songs out of one of the listener's playlists.
func RemoveFromPlaylist(pid int64, songs ...int64) error {
	c, err := kuGou()
	if err != nil {
		return err
	}
	kind, ref, ok := fromKind(pid)
	if !ok || (kind != "cloud" && kind != "liked") {
		return ErrUnsupported
	}
	listid, _ := strconv.ParseInt(ref, 10, 64)
	r, _ := app.lists.lookup(pid)
	var files []int64
	for _, id := range songs {
		if f, ok := app.files.get(listid, id); ok {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		// Where the songs sit in the list is not known: read it, which
		// is what says.
		if _, _, err := kugou.ListSongs(c, listid, r.Type, 1, 300); err != nil {
			return err
		}
		for _, id := range songs {
			if f, ok := app.files.get(listid, id); ok {
				files = append(files, f)
			}
		}
	}
	if len(files) == 0 {
		return nil
	}
	if err := kugou.DelFromList(c, listid, files, r.Type); err != nil {
		return err
	}
	for _, id := range songs {
		app.files.forget(listid, id)
	}
	app.files.save()
	return nil
}

// SubscribePlaylist saves a playlist for the listener, or unsaves it.
func SubscribePlaylist(id int64, subscribe bool) error {
	c, err := kuGou()
	if err != nil {
		return err
	}
	kind, ref, ok := fromKind(id)
	if !ok || kind != "list" {
		return ErrUnsupported
	}
	if subscribe {
		r, _ := app.lists.lookup(id)
		return kugou.CollectList(c, r.Name, ref)
	}
	// The saved copy is a list of the listener's own; it is what goes.
	r, _ := app.lists.lookup(id)
	lists, err := kugou.MyLists(c)
	if err != nil {
		return err
	}
	for _, l := range lists {
		if l.CollectionID == ref || (r.Name != "" && l.Name == r.Name && l.Type == 1) {
			return kugou.DelList(c, l.ID, 1)
		}
	}
	return nil
}

// RecentPlays returns the songs the listener played lately.
func RecentPlays(limit int) ([]Play, error) { return nil, ErrUnsupported }

// Events returns what the listener's friends posted, newest first.
func Events(uid int64, limit int, before int64) (events []Event, last int64, more bool, err error) {
	return nil, 0, false, ErrUnsupported
}

// FollowArtist follows a singer, or unfollows them.
func FollowArtist(id int64, follow bool) error { return ErrUnsupported }

// FollowedArtists returns the singers the listener follows.
func FollowedArtists() ([]ArtistProfile, error) { return nil, ErrUnsupported }
