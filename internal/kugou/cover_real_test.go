package kugou

import (
	"os"
	"testing"
)

// TestCovers asks the real services whether the songs they send come with a
// picture the app can show. Every page that lists songs reads them through a
// parser of its own, and one that forgets the picture leaves the app drawing
// placeholders, so each is checked where it is read.
func TestCovers(t *testing.T) {
	if os.Getenv("KG_COVER") == "" {
		t.Skip("set KG_COVER=1 to ask the real services")
	}
	c, err := New(os.Getenv("KG_CACHE"))
	if err != nil {
		t.Fatal(err)
	}

	// A recommended playlist: the parser for a list's own file list.
	lists, err := RecommendPlaylists(c, 8)
	if err != nil {
		t.Fatalf("recommended lists: %v", err)
	}
	if len(lists) == 0 {
		t.Fatal("no playlists were recommended")
	}
	var songs []Track
	for _, l := range lists {
		songs, _, err = PlaylistSongs(c, l.CollectionID, 1, 5)
		if err == nil && len(songs) > 0 {
			break
		}
	}
	if len(songs) == 0 {
		t.Fatalf("no playlist gave songs: %v", err)
	}
	check(t, "playlist", songs)

	// A chart, whose songs keep their hashes under audio_info.
	ranks, err := Ranks(c)
	if err != nil {
		t.Fatalf("ranks: %v", err)
	}
	if len(ranks) > 0 {
		songs, _, err := RankSongs(c, ranks[0].ID, 1, 5)
		if err != nil {
			t.Fatalf("rank songs: %v", err)
		}
		check(t, "rank", songs)
	}

	// The daily recommendation, which is what the home page opens on.
	if songs, err := DailySongs(c); err == nil && len(songs) > 0 {
		check(t, "daily", songs)
	}

	// A search, the first page anyone sees.
	if out, err := Search(c, "晴天", 1, 5); err == nil {
		check(t, "search", out.Songs)
	}
}

// check reports how many of songs carry a picture, and fails when none does:
// one service may omit it, all of them omitting it is a parser that does not
// look.
func check(t *testing.T, what string, songs []Track) {
	t.Helper()
	if len(songs) == 0 {
		t.Logf("%s: no songs", what)
		return
	}
	got := 0
	for _, s := range songs {
		if s.Image != "" {
			got++
		}
	}
	t.Logf("%s: %d of %d songs have a picture, first %q", what, got, len(songs), songs[0].Image)
	if got == 0 {
		t.Fatalf("%s: none of %d songs has a picture", what, len(songs))
	}
}
