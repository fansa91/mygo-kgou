package kugou

import (
	"encoding/base64"
	"encoding/json"
	"errors"
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
	Creator      string
	CollectionID string // what the public playlist service calls it
}

// MyLists returns the playlists the listener made and saved.
func MyLists(c *Client) ([]UserList, error) {
	if !c.SignedIn() {
		return nil, ErrNoSession
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
		return nil, err
	}
	var out []UserList
	for _, l := range firstArray(res["data"], "info", "lists", "list") {
		out = append(out, userListFrom(l))
	}
	return out, nil
}

func userListFrom(l map[string]any) UserList {
	return UserList{
		ID:           pickInt(l, "listid", "list_id", "id"),
		Name:         clean(pickString(l, "listname", "name", "specialname")),
		Type:         int(pickInt(l, "type", "list_type")),
		Default:      pickInt(l, "is_default", "is_def", "def", "is_my_like", "like") == 1,
		Pic:          listCover(pickString(l, "sizable_cover", "pic", "imgurl", "create_user_pic")),
		Count:        int(pickInt(l, "songcount", "count", "list_count", "total")),
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
func RenameList(c *Client, listid int64, name string) error {
	return cloudSend(c, "/v1/modify_list", map[string]any{
		"total_ver": 0, "listid": listid, "type": 0, "sort": 0,
		"tags": "", "intro": "", "name": name,
	}, false, &empty{})
}

// DelList deletes one of the listener's playlists, or unsaves a playlist
// they saved: both are a listid going away.
func DelList(c *Client, listid int64, typ int) error {
	return cloudSend(c, "/v2/delete_list", map[string]any{
		"listid": listid, "total_ver": 0, "type": typ,
	}, true, &empty{})
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

// cloudSend is one call to the cloud list service that is sealed on the wire
// as well as signed: the body goes AES-encrypted under a key of this call's
// own, and the key travels RSA-encrypted in "p". The answer comes back the
// same way, base64 or raw depending on the endpoint.
func cloudSend(c *Client, path string, body any, b64 bool, out any) error {
	if !c.SignedIn() {
		return ErrNoSession
	}
	aes := strings.ToLower(randomKey(6))
	sum := md5Hex(aes)
	plain, err := json.Marshal(body)
	if err != nil {
		return err
	}
	cipher, err := aesCBCEncrypt(plain, []byte(sum[:16]), []byte(sum[16:32]))
	if err != nil {
		return err
	}
	payload := string(cipher)
	if b64 {
		payload = base64.StdEncoding.EncodeToString(cipher)
	}
	p, err := rsaSealPKCS1([]byte("{" + quote("aes") + ":" + quote(aes) +
		"," + quote("uid") + ":" + quote(strconv.FormatInt(c.UserID, 10)) +
		"," + quote("token") + ":" + quote(c.Token) + "}"))
	if err != nil {
		return err
	}
	ct := strconv.FormatInt(now(), 10)
	params := map[string]string{
		"appid": c.appID(), "clientver": c.clientVer(), "mid": c.Dev.MID, "dfid": c.Dev.DFID,
		"clienttime": ct, "key": c.signKey(ct), "p": p,
	}
	if b64 {
		params["last_area"], params["last_time"] = "gztx", ct
	}
	raw, err := c.do("POST", call{
		Path:   path,
		Router: "cloudlist.service.kugou.com",
		Params: params,
		Body:   payload,
		Type:   "application/json;charset=utf-8",
		Raw:    true,
	})
	if err != nil {
		return err
	}
	return json.Unmarshal(unsealAnswer(raw, sum), out)
}

// unsealAnswer opens the cloud service's answer, which arrives as the AES
// ciphertext in hex, as raw bytes, or as JSON when the service chose not to
// bother.
func unsealAnswer(raw []byte, sum string) []byte {
	key, iv := []byte(sum[:16]), []byte(sum[16:32])
	if plain, err := aesCBCDecrypt(raw, key, iv); err == nil && json.Valid(plain) {
		return plain
	}
	if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); err == nil {
		if plain, err := aesCBCDecrypt(decoded, key, iv); err == nil && json.Valid(plain) {
			return plain
		}
	}
	return raw
}

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
