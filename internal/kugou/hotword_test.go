package kugou

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// The search box's board answers in tabs, and the words the field lists
// are 热搜榜's.
func TestHotWordsTakeTheBoard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/search/hot_tab" || r.URL.Query().Get("navid") != "1" {
			t.Errorf("asked %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if got := r.Header.Get("x-router"); got != "msearch.kugou.com" {
			t.Errorf("router %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"list": []any{
			map[string]any{"name": "飙升榜", "keywords": []any{
				map[string]any{"keyword": "哪里都是你"},
			}},
			map[string]any{"name": "热搜榜", "keywords": []any{
				map[string]any{"keyword": "晴天"},
				map[string]any{"keyword": "晴天"},
				map[string]any{"keyword": " 富士山下.mp3 "},
				map[string]any{"keyword": ""},
			}},
		}}})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), Base: srv.URL, Dev: device{DFID: "d", MID: "m"}}
	got, err := HotWords(c, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"晴天", "富士山下"}; !slices.Equal(got, want) {
		t.Fatalf("hot words %q, want %q", got, want)
	}
}

// A tab KuGou no longer names is the first of them rather than nothing.
func TestHotWordsFallBackToTheFirstTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"list": []any{
			map[string]any{"name": "今日热搜", "keywords": []any{map[string]any{"keyword": "春泥"}}},
			map[string]any{"name": "飙升榜", "keywords": []any{map[string]any{"keyword": "哪里都是你"}}},
		}}})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), Base: srv.URL, Dev: device{DFID: "d", MID: "m"}}
	got, err := HotWords(c, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"春泥"}; !slices.Equal(got, want) {
		t.Fatalf("hot words %q, want %q", got, want)
	}
}
