package kugou

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestLiteProbe asks the services what the 概念版 client may play: whether the
// privilege endpoint marks a song as needing a subscription, and whether the
// player's URL endpoint hands a 概念版 request the standard quality anyway.
// Run with KG_LITE=1.
func TestLiteProbe(t *testing.T) {
	if os.Getenv("KG_LITE") == "" {
		t.Skip("set KG_LITE=1 to ask the real services")
	}
	c, err := New(os.Getenv("KG_CACHE"))
	if err != nil {
		t.Fatal(err)
	}
	c.Lite = true
	combos := map[[2]int]int{}
	var vip *Track
	for _, kw := range []string{"晴天", "富士山下", "孤勇者", "起风了", "周杰伦", "林俊杰"} {
		res, err := Search(c, kw, 1, 30)
		if err != nil {
			continue
		}
		for i := range res.Songs {
			x := &res.Songs[i]
			combos[[2]int{x.Privilege, x.PayType}]++
			if vip == nil && x.Privilege >= 9 && x.PayType == 2 {
				cp := *x
				vip = &cp
			}
		}
	}
	for k, n := range combos {
		fmt.Printf("privilege=%d payType=%d: %d songs\n", k[0], k[1], n)
	}
	if vip == nil {
		t.Fatal("no VIP-only song was found to ask about")
	}
	fmt.Printf("\n### %s / %s  privilege=%d payType=%d hash=%s albumID=%d\n",
		vip.Name, vip.Singer, vip.Privilege, vip.PayType, vip.Hash, vip.AlbumID)
	if st, err := URL(c, vip, "standard"); err != nil {
		fmt.Printf("VIP song as the 概念版: %v\n", err)
	} else {
		fmt.Printf("VIP song as the 概念版: %s %dkbps %.60s\n", st.Type, st.BitRate, st.URL)
	}
	probeLite(t, c, *vip)
}

// probeLite asks the two endpoints, lite as the 概念版 client sends them.
func probeLite(t *testing.T, c *Client, s Track) {
	t.Helper()

	// 1. The 概念版 privilege, which says per quality whether it needs VIP and
	// which ogg file the client actually plays.
	body := map[string]any{
		"appid": "1005", "area_code": 1, "behavior": "play", "clientver": 20489,
		"need_hash_offset": 1, "relate": 1, "support_verify": 1,
		"resource": []map[string]any{{"type": "audio", "page_id": 0, "hash": s.Hash, "album_id": s.AlbumID}},
		"qualities": []string{"128", "320", "flac", "high", "viper_atmos", "viper_tape", "viper_clear", "super", "multitrack"},
	}
	b, _ := json.Marshal(body)
	raw, err := c.do("POST", call{
		Path:   "/v2/get_res_privilege/lite",
		Router: "media.store.kugou.com",
		Body:   string(b),
		Type:   "application/json",
		Hdr:    map[string]string{"x-router": "media.store.kugou.com"},
	})
	if err != nil {
		t.Fatalf("privilege lite: %v", err)
	}
	fmt.Printf("privilege: %.600s\n", raw)

	// A song nobody sells, to see the endpoint answer at all.
	if songs, err := Search(c, "晴天", 1, 20); err == nil {
		for _, x := range songs.Songs {
			if x.Privilege >= 8 && x.PayType == 0 {
				if st, err := stream(c, &x, "standard"); err == nil {
					fmt.Printf("free song plays: %s %s\n", st.Type, st.Level)
				} else {
					fmt.Printf("free song refused: %v\n", err)
				}
				break
			}
		}
	}

	// 2. What the ogg 128 file is called.
	ogg128 := digOgg(raw)
	fmt.Printf("ogg_128_hash: %q\n", ogg128)

	// 3. The player's URL endpoint, lite as the 概念版 sends it.
	ask := func(name string, params map[string]string) {
		r := call{Path: "/v5/url", Router: "trackercdn.kugou.com", Params: params, Key: true, DFID: c.Dev.DFID}
		out, err := c.do("GET", r)
		if err != nil {
			fmt.Printf("%s: err %v\n", name, err)
			return
		}
		fmt.Printf("%s: %s\n", name, out)
	}

	standard := func(hash string) map[string]string {
		return map[string]string{
			"clienttime": fmt.Sprint(os.Getenv("KG_NOW")),
			"album_id": fmt.Sprint(s.AlbumID), "area_code": "1", "hash": hash,
			"ssa_flag": "is_fromtrack", "version": "11430", "page_id": "151369488",
			"quality": "128", "album_audio_id": fmt.Sprint(s.AlbumAudioID),
			"behavior": "play", "pid": "2", "cmd": "26", "pidversion": "3001",
			"IsFreePart": "0", "ppage_id": "463467626,350369493,788954147",
			"cdnBackup": "1", "module": "", "clientver": "11430",
			"appid": "1005", "mid": c.Dev.MID, "dfid": c.Dev.DFID, "uuid": "-",
		}
	}
	lite := func(hash string) map[string]string {
		p := standard(hash)
		p["appid"] = "3116"
		p["clientver"] = "11440"
		p["pid"] = "411"
		p["page_id"] = "967177915"
		p["ppage_id"] = "356753938,823673182,967485191"
		p["need_ogg"] = "1"
		return p
	}

	ask("standard 128", standard(s.Hash))
	if ogg128 != "" {
		ask("lite 128 ogg", lite(ogg128))
	}
	ask("lite 128 mp3", lite(s.Hash))
}

// digOgg digs the ogg 128 hash out of the privilege answer.
func digOgg(raw []byte) string {
	var res struct {
		Data []struct {
			TransParam map[string]any `json:"trans_param"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &res) != nil || len(res.Data) == 0 {
		return ""
	}
	if v, ok := res.Data[0].TransParam["ogg_128_hash"].(string); ok {
		return v
	}
	return ""
}
