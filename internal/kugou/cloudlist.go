package kugou

import (
	"encoding/base64"
	"errors"
	"path"
	"slices"
	"strconv"
	"strings"
)

// The listener's own playlists. They live in the cloud list service, which
// names one by a number ("listid") rather than by the global collection id
// the public playlist service uses. A list is either made here (type 0) or
// saved from someone else (type 1); the liked songs are a list of the first
// kind that KuGou keeps for every account.

// UserList is one of the listener's own playlists.
type UserList struct {
	ID           int64 // its listid
	Name         string
	Type         int  // 0: made here, 1: saved from another
	Default      bool // the list KuGou keeps the liked songs in
	Pic          string
	Count        int
	Sort         int // its place among the lists of its kind
	Creator      string
	CollectionID string // what the public playlist service calls it
}

// MyLists returns the playlists the listener made and saved.
func MyLists(c *Client) ([]UserList, error) {
	lists, _, err := MyListsVersion(c)
	return lists, err
}

// MyListsVersion returns the listener's playlists and the version the
// service holds them at: changing their order or their covers names that
// version, so that a change made elsewhere is not written over.
func MyListsVersion(c *Client) ([]UserList, int64, error) {
	if !c.SignedIn() {
		return nil, 0, ErrNoSession
	}
	var res map[string]any
	err := c.send("POST", request{
		Router: "cloudlist.service.kugou.com",
		Path:   "/v7/get_all_list",
		Params: map[string]string{
			"plat": "1", "userid": strconv.FormatInt(c.UserID, 10), "token": c.Token,
		},
		Body: map[string]any{
			"userid": c.UserID, "token": c.Token, "total_ver": 0,
			"type": 2, "page": 1, "pagesize": 100,
		},
	}, &res)
	if err != nil {
		return nil, 0, err
	}
	var out []UserList
	for _, l := range firstArray(res["data"], "info", "lists", "list") {
		out = append(out, userListFrom(l))
	}
	data, _ := res["data"].(map[string]any)
	return out, pickInt(data, "total_ver"), nil
}

// OrderedLists answers the listener's lists in the order they gave them: the
// service keeps the order as a number on each list and answers them as they
// were made, and the app shows the numbers from the highest down, the ones
// it never numbered last.
func OrderedLists(lists []UserList) []UserList {
	out := slices.Clone(lists)
	slices.SortStableFunc(out, func(a, b UserList) int {
		switch {
		case a.Sort > b.Sort:
			return -1
		case a.Sort < b.Sort:
			return 1
		}
		return 0
	})
	return out
}

// SortLists gives the listener's lists the order they are given in, first
// shown first: each one's place among them, from one. KuGou numbers the
// lists it made and those it saved apart, so a caller ordering one kind
// sends that kind, one list to a call, as the service takes them.
func SortLists(c *Client, totalVer int64, lists []UserList) error {
	for i, l := range lists {
		l.Sort = len(lists) - i
		if err := modifyList(c, totalVer, l, map[string]any{"sort": l.Sort}, &empty{}); err != nil {
			return err
		}
	}
	return nil
}

// SetListCover gives one of the listener's lists a picture of its own, named
// as the cover upload names what it stored, and answers the picture the
// service now serves for it.
func SetListCover(c *Client, totalVer int64, l UserList, pic string) (string, error) {
	pic = strings.TrimSpace(pic)
	if pic == "" {
		return "", errors.New("kugou: there is no picture to set")
	}
	// The service keeps covers as paths under its image CDN.
	if !strings.HasPrefix(pic, "stdmusic/") {
		pic = "stdmusic/" + path.Base(pic)
	}
	var res struct {
		Data struct {
			Info struct {
				Pic string `json:"pic"`
			} `json:"info"`
		} `json:"data"`
	}
	if err := modifyList(c, totalVer, l, map[string]any{"pic": pic}, &res); err != nil {
		return "", err
	}
	// The answer names the picture with the size in it, which is the form
	// the app shows a list's cover in.
	cover := strings.ReplaceAll(res.Data.Info.Pic, "{size}", "480")
	cover = strings.ReplaceAll(cover, "http://", "https://")
	if cover == "" {
		cover = "https://imge.kugou.com/stdmusic/480/" + strings.TrimPrefix(pic, "stdmusic/")
	}
	return cover, nil
}

// modifyList changes one of the listener's lists: its name, its picture, or
// its place among the lists of its kind. The place it is already in goes
// with every change, as a place it is not in moves it.
func modifyList(c *Client, totalVer int64, l UserList, fields map[string]any, out any) error {
	body := map[string]any{
		"listid": l.ID, "type": l.Type, "total_ver": totalVer,
		"sort": l.Sort, "tags": "", "intro": "", "is_pri": 0,
	}
	for k, v := range fields {
		body[k] = v
	}
	body, err := cloudBody(c, body)
	if err != nil {
		return err
	}
	return c.send("POST", request{
		Path:   "/cloudlist.service/v4/modify_list",
		Params: cloudParams(),
		Body:   body,
	}, out)
}

// cloudParams are what the cloud list's own services want beside a body: the
// two the app sends with every write it makes.
func cloudParams() map[string]string {
	return map[string]string{"last_area": "gztx", "last_time": strconv.FormatInt(now(), 10)}
}

// cloudBody is what the cloud list's write services read: the fields as
// plain JSON, with the token sealed beside them as "encstr" under a key of
// the call's own, which travels RSA-encrypted in "enckey". The whole-body
// cipher these services once took is answered with 20010 now.
func cloudBody(c *Client, fields map[string]any) (map[string]any, error) {
	return cloudBodySeed(c, fields, cloudSeed())
}

// cloudBodySeed is cloudBody under a key the caller names, which a test
// knows and the service never does.
func cloudBodySeed(c *Client, fields map[string]any, seed string) (map[string]any, error) {
	sum := md5Hex(seed)
	cipher, err := aesCBCEncrypt([]byte("{"+quote("token")+":"+quote(c.Token)+"}"),
		[]byte(sum[:16]), []byte(sum[16:32]))
	if err != nil {
		return nil, err
	}
	key, err := c.rsaSealPKCS1Key([]byte(seed))
	if err != nil {
		return nil, err
	}
	fields["userid"] = c.UserID
	fields["enckey"] = strings.ToUpper(key)
	fields["encstr"] = base64.StdEncoding.EncodeToString(cipher)
	return fields, nil
}

func userListFrom(l map[string]any) UserList {
	return UserList{
		ID:           pickInt(l, "listid", "list_id", "id"),
		Name:         clean(pickString(l, "listname", "name", "specialname")),
		Type:         int(pickInt(l, "type", "list_type")),
		Default:      pickInt(l, "is_default", "is_def", "def", "is_my_like", "like") == 1,
		Pic:          listCover(pickString(l, "sizable_cover", "pic", "imgurl", "create_user_pic")),
		Count:        int(pickInt(l, "songcount", "count", "list_count", "total")),
		Sort:         int(pickInt(l, "sort")),
		Creator:      pickString(l, "list_create_username", "nickname", "username"),
		CollectionID: pickString(l, "global_collection_id", "gid", "list_create_gid"),
	}
}

// listCover is a playlist's own picture. A list with none is answered with
// the picture of whoever made it, and an account's own icon — the one beside
// its name everywhere — is no cover for a playlist: the app draws a plain
// square instead, which says "no picture" rather than "this one's maker".
func listCover(pic string) string {
	if pic == "" || strings.Contains(pic, "/kugouicon/") {
		return ""
	}
	return sized(pic)
}

// Liked returns the list the listener's liked songs go in. KuGou keeps one
// per account, named "我喜欢"; an account whose own name for it differs is
// found by the flag the service sets, or by it being the first one made.
func Liked(c *Client) (UserList, error) {
	lists, err := MyLists(c)
	if err != nil {
		return UserList{}, err
	}
	// The name is the surest sign, the listid the last resort.
	for _, l := range lists {
		if strings.Contains(l.Name, "我喜欢") {
			return l, nil
		}
	}
	for _, l := range lists {
		if l.Default {
			return l, nil
		}
	}
	for _, l := range lists {
		if LooksLiked(l) {
			return l, nil
		}
	}
	return UserList{}, errors.New("kugou: this account has no list for the liked songs")
}

// LooksLiked reports whether l is the list KuGou keeps the liked songs in,
// as an account's own name for it may differ.
func LooksLiked(l UserList) bool {
	if strings.Contains(l.Name, "我喜欢") || l.Default {
		return true
	}
	return l.Type == 0 && l.ID == 1 && l.Count == 0
}

// Qualities are the files each song of a playlist was uploaded as. The cloud
// list says what the listener's playlist holds, but not what its songs are:
// its rows carry no relate_goods, and read alone every song of one is a
// 128kbps file, which made a list of FLACs show as all standard. The public
// playlist service answers the same songs by the playlist's
// global_collection_id with their qualities, so this reads that once and the
// caller fills what it read from the cloud.
type Qualities struct {
	byAudio map[int64]qualities
	byHash  map[string]qualities
}

// qualities are the files a song was uploaded as, by quality.
type qualities struct{ high, lossl, hires string }

// PlaylistQualities reads a playlist through the public service, by the
// collection id the cloud list calls it by. A list whose collection id is not
// known has none, and its songs keep the qualities the cloud sent.
func PlaylistQualities(c *Client, gid string) Qualities {
	out := Qualities{byAudio: map[int64]qualities{}, byHash: map[string]qualities{}}
	if gid == "" {
		return out
	}
	for page := 1; page <= 10; page++ {
		var res struct {
			Data struct {
				Songs []map[string]any `json:"songs"`
			} `json:"data"`
		}
		err := c.send("GET", request{
			Path: "/pubsongs/v2/get_other_list_file_nofilt",
			Params: map[string]string{
				"area_code": "1", "begin_idx": strconv.Itoa((page - 1) * 100),
				"plat": "1", "type": "1", "mode": "1", "personal_switch": "1",
				"extend_fields": "relate_goods", "pagesize": "100",
				"global_collection_id": gid,
			},
		}, &res)
		if err != nil {
			return out
		}
		for _, l := range res.Data.Songs {
			var t Track
			for _, g := range maps(l["relate_goods"]) {
				t.put(strings.ToLower(pickString(g, "hash")), pickInt(g, "level"), pickInt(g, "bitrate"))
			}
			if t.HashHigh == "" && t.HashLossl == "" && t.HashHires == "" {
				continue
			}
			q := qualities{high: t.HashHigh, lossl: t.HashLossl, hires: t.HashHires}
			if id := pickInt(l, "mixsongid", "album_audio_id"); id != 0 {
				out.byAudio[id] = q
			}
			if own := strings.ToLower(pickString(l, "hash", "filehash")); own != "" {
				out.byHash[own] = q
			}
		}
		if len(res.Data.Songs) < 100 {
			break
		}
	}
	return out
}

// Fill gives each of songs the qualities it was uploaded as, where the
// playlist knew them.
func (q Qualities) Fill(songs []Track) {
	for i := range songs {
		// A song is the album audio it is: the two services number that the
		// same, while the hashes they hold for it often differ, as a list
		// remembers the file it was added by and the public list serves the
		// one it plays now.
		qual, ok := q.byAudio[songs[i].AlbumAudioID]
		if !ok || songs[i].AlbumAudioID == 0 {
			qual, ok = q.byHash[songs[i].Hash]
		}
		if ok {
			songs[i].HashHigh, songs[i].HashLossl, songs[i].HashHires = qual.high, qual.lossl, qual.hires
		}
	}
}

// ListSongs returns a page of one of the listener's own playlists, and how
// many songs it holds.
func ListSongs(c *Client, listid int64, typ, page, size int) ([]Track, int, error) {
	if !c.SignedIn() {
		return nil, 0, ErrNoSession
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 300
	}
	var res map[string]any
	err := c.send("POST", request{
		Router: "cloudlist.service.kugou.com",
		Path:   "/v4/get_list_all_file_v3",
		Body: map[string]any{
			"listid": listid, "userid": c.UserID, "token": c.Token, "type": typ,
			"page": page, "pagesize": size, "area_code": 1, "allplatform": 1, "show_cover": 1,
		},
	}, &res)
	if err != nil {
		return nil, 0, err
	}
	data, _ := res["data"].(map[string]any)
	rows := firstArray(data, "songs", "info", "list", "files", "audios")
	out := make([]Track, 0, len(rows))
	for _, l := range rows {
		out = append(out, trackFromFile(l))
	}
	return out, int(pickInt(data, "count", "total", "songcount")), nil
}

// AddToList puts songs in one of the listener's playlists.
func AddToList(c *Client, listid int64, songs []Track, typ int) error {
	if !c.SignedIn() {
		return ErrNoSession
	}
	if len(songs) == 0 {
		return nil
	}
	data := make([]map[string]any, 0, len(songs))
	for _, t := range songs {
		data = append(data, map[string]any{
			"number": 1, "name": t.Name, "hash": t.HashFor("standard"), "size": 0,
			"sort": 0, "timelen": 0, "bitrate": 0,
			"album_id": t.AlbumID, "mixsongid": t.AlbumAudioID,
		})
	}
	ct := strconv.FormatInt(now(), 10)
	return c.send("POST", request{
		Path: "/cloudlist.service/v6/add_song",
		Params: map[string]string{
			"last_time": ct, "last_area": "gztx",
			"userid": strconv.FormatInt(c.UserID, 10), "token": c.Token,
		},
		Body: map[string]any{
			"userid": c.UserID, "token": c.Token, "listid": listid, "list_ver": 0,
			"type": typ, "slow_upload": 1, "scene": "false;null", "data": data,
		},
	}, &empty{})
}

// DelFromList takes songs out of one of the listener's playlists. KuGou
// knows a song in a list by the "fileid" it gave it there, not by its hash,
// so a caller keeps that number from when it read the list.
func DelFromList(c *Client, listid int64, fileIDs []int64, typ int) error {
	if !c.SignedIn() {
		return ErrNoSession
	}
	if len(fileIDs) == 0 {
		return nil
	}
	data := make([]map[string]any, 0, len(fileIDs))
	for _, id := range fileIDs {
		data = append(data, map[string]any{"fileid": id})
	}
	return c.send("POST", request{
		Router: "cloudlist.service.kugou.com",
		Path:   "/v4/delete_songs",
		Body: map[string]any{
			"listid": listid, "userid": c.UserID, "data": data,
			"type": typ, "token": c.Token, "list_ver": 0,
		},
	}, &empty{})
}

// CreateList makes a playlist for the listener and answers its listid.
func CreateList(c *Client, name string) (int64, error) {
	if !c.SignedIn() {
		return 0, ErrNoSession
	}
	ct := strconv.FormatInt(now(), 10)
	var res map[string]any
	err := c.send("POST", request{
		Path: "/cloudlist.service/v5/add_list",
		Params: map[string]string{
			"last_time": ct, "last_area": "gztx",
			"userid": strconv.FormatInt(c.UserID, 10), "token": c.Token,
		},
		Body: map[string]any{
			"userid": c.UserID, "token": c.Token, "total_ver": 0, "name": name,
			"type": 0, "source": 1, "is_pri": 0, "from_shupinmv": 0,
			"list_create_userid": c.UserID, "list_create_listid": 0, "list_create_gid": "",
		},
	}, &res)
	if err != nil {
		return 0, err
	}
	data, _ := res["data"].(map[string]any)
	if id := pickInt(data, "listid", "list_id"); id != 0 {
		return id, nil
	}
	// Some answers name the list only by its name and the time it was made.
	if lists, err := MyLists(c); err == nil {
		for _, l := range lists {
			if l.Name == name {
				return l.ID, nil
			}
		}
	}
	return 0, errors.New("kugou: the playlist was made without a number")
}

// RenameList gives one of the listener's playlists a new name.
func RenameList(c *Client, listid int64, typ int, name string) error {
	return modifyList(c, 0, heldList(c, listid, typ), map[string]any{"name": name}, &empty{})
}

// heldList reads one of the listener's lists as the service holds it: a
// write names the place a list is in, so it has to know that place.
func heldList(c *Client, listid int64, typ int) UserList {
	if lists, _, err := MyListsVersion(c); err == nil {
		for _, l := range lists {
			if l.ID == listid {
				return l
			}
		}
	}
	return UserList{ID: listid, Type: typ}
}

// DelList deletes one of the listener's playlists, or unsaves a playlist
// they saved: both are a listid going away. The delete service reads the
// token as it is, unlike the ones that change a list.
func DelList(c *Client, listid int64, typ int) error {
	if !c.SignedIn() {
		return ErrNoSession
	}
	return c.send("POST", request{
		Path:   "/cloudlist.service/v3/delete_list",
		Params: cloudParams(),
		Body: map[string]any{
			"userid": c.UserID, "token": c.Token, "listid": listid,
			"total_ver": 0, "type": typ,
		},
	}, &empty{})
}

// CollectList saves someone else's playlist for the listener.
func CollectList(c *Client, name, collectionID string) error {
	if !c.SignedIn() {
		return ErrNoSession
	}
	return c.send("POST", request{
		Path: "/cloudlist.service/v5/add_list",
		Body: map[string]any{
			"userid": c.UserID, "token": c.Token, "total_ver": 0, "name": name,
			"type": 1, "source": 1, "is_pri": 0, "from_shupinmv": 0,
			"list_create_userid": 0, "list_create_listid": 0, "list_create_gid": collectionID,
		},
	}, &empty{})
}

// empty is what a call whose answer nobody reads decodes into.
type empty struct{}

func (e *empty) UnmarshalJSON(b []byte) error { return nil }

// firstArray finds the first list of things in v, under one of the names the
// service uses for it.
func firstArray(v any, names ...string) []map[string]any {
	switch x := v.(type) {
	case []any:
		return mapsOf(x)
	case map[string]any:
		for _, n := range names {
			if arr, ok := x[n].([]any); ok {
				return mapsOf(arr)
			}
		}
		// Nothing under the names hoped for: take the first list there is.
		for _, item := range x {
			if arr, ok := item.([]any); ok {
				return mapsOf(arr)
			}
		}
	}
	return nil
}

func mapsOf(arr []any) []map[string]any {
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
