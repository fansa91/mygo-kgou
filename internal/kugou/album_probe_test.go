package kugou

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
	"sort"
	"testing"
)

// TestProbeAlbumKeys dumps the keys of an album's song list and an artist's
// song list, which the services send in shapes of their own.
func TestProbeAlbumKeys(t *testing.T) {
	if os.Getenv("KG_PROBE") == "" {
		t.Skip("set KG_PROBE=1 to ask the real service")
	}
	c, err := New(os.Getenv("KG_CACHE"))
	if err != nil {
		t.Fatal(err)
	}

	dump := func(name string, l map[string]any) {
		keys := make([]string, 0, len(l))
		for k := range l {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Printf("\n--- %s ---\n", name)
		for _, k := range keys {
			b, _ := json.Marshal(l[k])
			v := string(b)
			if len(v) > 90 {
				v = v[:90] + "…"
			}
			fmt.Printf("  %-18s %s\n", k, v)
		}
	}

	for _, size := range []int{50, 60, 80, 100} {
		time.Sleep(1500 * time.Millisecond)
		var probe struct {
			Data []map[string]any `json:"data"`
		}
		err := c.send("POST", request{
			Base: albumHost, Path: "/kmr/v1/audio_group/author", Hdr: map[string]string{"kg-tid": "220"},
			Body: map[string]any{"author_id": 420, "page": 1, "pagesize": size, "area_code": 1},
		}, &probe)
		fmt.Printf("artist songs pagesize=%-5d err=%v got=%d\n", size, err, len(probe.Data))
	}

	var album struct {
		Data struct {
			Total int              `json:"total"`
			Songs []map[string]any `json:"songs"`
		} `json:"data"`
	}
	if err := c.send("POST", request{
		Base: albumHost, Path: "/v1/album_audio/lite", Hdr: map[string]string{"kg-tid": "255"},
		Body: map[string]any{"album_id": 8714931, "area_code": 1, "page": 1, "pagesize": 3},
	}, &album); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("album total=%d songs=%d\n", album.Data.Total, len(album.Data.Songs))
	if len(album.Data.Songs) > 0 {
		dump("album song", album.Data.Songs[0])
	}

	var artist struct {
		Data []map[string]any `json:"data"`
	}
	if err := c.send("POST", request{
		Base: albumHost, Path: "/kmr/v1/audio_group/author", Hdr: map[string]string{"kg-tid": "220"},
		Body: map[string]any{"author_id": 420, "page": 1, "pagesize": 2, "area_code": 1},
	}, &artist); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("\nartist songs=%d\n", len(artist.Data))
	if len(artist.Data) > 0 {
		dump("artist song", artist.Data[0])
	}
}
