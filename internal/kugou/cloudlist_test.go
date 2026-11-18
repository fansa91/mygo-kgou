package kugou

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// The body of a list write: the fields as they are, the token sealed beside
// them under a key of the call's own, and that key the service's to open.
func TestCloudBodySealsTheToken(t *testing.T) {
	c := &Client{UserID: 746430436, Token: "tk"}
	body, err := cloudBodySeed(c, map[string]any{"listid": 23, "sort": 1}, "ab12CD")
	if err != nil {
		t.Fatal(err)
	}
	if body["userid"] != int64(746430436) || body["listid"] != 23 || body["sort"] != 1 {
		t.Errorf("the body is %v", body)
	}
	key, _ := body["enckey"].(string)
	if len(key) != 256 || strings.ToUpper(key) != key {
		t.Errorf("enckey is %d characters of %q", len(key), key)
	}
	if strings.Trim(strings.ReplaceAll(strings.ReplaceAll(key, "0", ""), "1", ""), "23456789ABCDEF") != "" {
		t.Errorf("enckey is not hex: %q", key)
	}
	encstr, _ := body["encstr"].(string)
	raw, err := base64.StdEncoding.DecodeString(encstr)
	if err != nil {
		t.Fatalf("encstr is not base64: %v", err)
	}
	sum := md5Hex("ab12CD")
	plain, err := aesCBCDecrypt(raw, []byte(sum[:16]), []byte(sum[16:32]))
	if err != nil {
		t.Fatalf("encstr does not open: %v", err)
	}
	var held struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(plain, &held); err != nil {
		t.Fatalf("encstr holds %q", plain)
	}
	if held.Token != "tk" {
		t.Errorf("encstr holds the token %q", held.Token)
	}
	if strings.Contains(string(mustJSON(t, body)), "tk") {
		t.Error("the token is in the body as it is")
	}
}

// The order is sent one list to a call, each with the place it takes, and
// the cover and the name go the same way.
func TestListWritesGoUpAsTheServiceReadsThem(t *testing.T) {
	var mu sync.Mutex
	var asked []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v7/get_all_list" {
			// The lists as the service holds them, which a write reads
			// before it names the place one is in.
			json.NewEncoder(w).Encode(map[string]any{"status": 1, "data": map[string]any{
				"total_ver": 640,
				"info": []any{
					map[string]any{"listid": 23, "listname": "一", "type": 0, "sort": 2},
					map[string]any{"listid": 25, "listname": "二", "type": 0, "sort": 1},
				},
			}})
			return
		}
		if got := r.URL.Path; got != "/cloudlist.service/v4/modify_list" {
			t.Errorf("asked %s", got)
		}
		if got := r.URL.Query().Get("last_area"); got != "gztx" {
			t.Errorf("last_area %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("the body does not read: %v", err)
		}
		mu.Lock()
		asked = append(asked, body)
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{
			"status": 1,
			"data": map[string]any{"info": map[string]any{
				"pic": "http://c1.kgimg.com/stdmusic/{size}/20261118/pic.jpg",
			}},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), Base: srv.URL, UserID: 1, Token: "tk",
		Dev: device{DFID: "d", MID: "m"}}
	lists := []UserList{{ID: 23, Name: "一", Sort: 2}, {ID: 25, Name: "二", Sort: 1}}
	if err := SortLists(c, 640, lists); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 2 {
		t.Fatalf("%d calls, want 2", len(asked))
	}
	for i, body := range asked {
		if body["sort"] != float64(len(lists)-i) || body["total_ver"] != float64(640) {
			t.Errorf("call %d sends %v", i, body)
		}
		if _, ok := body["enckey"]; !ok {
			t.Errorf("call %d has no enckey", i)
		}
	}

	if err := RenameList(c, 23, 0, "通勤"); err != nil {
		t.Fatal(err)
	}
	last := asked[len(asked)-1]
	if last["name"] != "通勤" || last["listid"] != float64(23) {
		t.Errorf("the rename sends %v", last)
	}
	// The place the list is in goes with the name, or the name moves it.
	if last["sort"] != float64(2) {
		t.Errorf("the rename sends sort %v", last["sort"])
	}

	cover, err := SetListCover(c, 640, UserList{ID: 23, Type: 0, Sort: 2}, "stdmusic/20261118/pic.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if got := asked[len(asked)-1]["pic"]; got != "stdmusic/20261118/pic.jpg" {
		t.Errorf("the cover sends %q", got)
	}
	if want := "https://c1.kgimg.com/stdmusic/480/20261118/pic.jpg"; cover != want {
		t.Errorf("the cover reads back %q, want %q", cover, want)
	}
}

// A list's cover names the picture it was given, whichever way the upload
// named it.
func TestCoverNamesTheStoredPicture(t *testing.T) {
	c := &Client{HTTP: http.DefaultClient, Base: "http://127.0.0.1:1", UserID: 1, Token: "tk"}
	for _, pic := range []string{"20261118/pic.jpg", "stdmusic/20261118/pic.jpg"} {
		body, err := cloudBody(c, map[string]any{"pic": pic})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := body["pic"]; !ok {
			t.Errorf("%q did not reach the body", pic)
		}
	}
	if _, err := SetListCover(c, 0, UserList{ID: 1}, " "); err == nil {
		t.Error("a cover with no picture was accepted")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The lists are shown in the order the listener gave them: the numbers from
// the highest down, and the ones the service never numbered as they came.
func TestOrderedListsKeepsTheListenerOrder(t *testing.T) {
	lists := []UserList{
		{ID: 1, Name: "默认收藏", Sort: 1},
		{ID: 2, Name: "我喜欢", Sort: 0},
		{ID: 12, Name: "ost", Sort: 0},
		{ID: 15, Name: "my days", Sort: 3},
		{ID: 16, Name: "虾米时光", Sort: 5},
	}
	var got []int64
	for _, l := range OrderedLists(lists) {
		got = append(got, l.ID)
	}
	want := []int64{16, 15, 1, 2, 12}
	if !slices.Equal(got, want) {
		t.Errorf("shown as %v, want %v", got, want)
	}
	if lists[0].ID != 1 {
		t.Error("the lists read were changed under the caller")
	}
}
