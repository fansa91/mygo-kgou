package kugou

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// formatProbeSong is one song to ask the URL service about: what it is, the
// album it is on, and every hash the album service holds for it.
type formatProbeSong struct {
	Track
	albumID int64
	hashes  map[string]string // the field it was found under, and its hash
}

// probeAnswer is what the URL service answers one question.
type probeAnswer struct {
	URL      []string `json:"url"`
	ExtName  string   `json:"extName"`
	BitRate  int      `json:"bitRate"`
	FileSize int64    `json:"fileSize"`
	Msg      string   `json:"msg"`
	ErrCode  int      `json:"errcode"`
	Offset   *struct {
		EndMS int64 `json:"end_ms"`
	} `json:"hash_offset"`
}

// probeResult is what a question fetched: the file itself, looked at.
type probeResult struct {
	ext   string
	magic string // the file's own first bytes: fLaC, OggS, ID3…
	sound string // a FLAC's own rate, channels and depth: "44100Hz 2ch 16bit"
	kbps  int
	size  int64
	note  string
}

// TestFormatProbe asks the player's URL service what a song's hashes and its
// qualities each fetch: whether the hash names the file or the quality does,
// and which formats the catalogue really holds. Run with KG_FORMAT=1.
func TestFormatProbe(t *testing.T) {
	if os.Getenv("KG_FORMAT") == "" {
		t.Skip("set KG_FORMAT=1 to ask the real service")
	}
	c, err := New(os.Getenv("KG_CACHE"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ensureDFID(); err != nil {
		t.Fatal(err)
	}

	// Songs the catalogue gives away: a sold one is served a minute of
	// preview at any quality, which says nothing about formats.
	queries := []string{"加州旅馆", "梁祝 小提琴", "渡口 蔡琴", "平凡之路", "Yesterday Once More"}
	want := 2
	if v, err := strconv.Atoi(os.Getenv("KG_FORMAT_SONGS")); err == nil && v > 0 {
		want = v
	}
	seen := map[int64]bool{}
	songs := 0
	for _, kw := range queries {
		res, err := Search(c, kw, 1, 20)
		if err != nil {
			fmt.Printf("%s: %v\n", kw, err)
			continue
		}
		for i := range res.Songs {
			if songs >= want {
				break
			}
			x := res.Songs[i]
			if x.AlbumID == 0 || seen[x.AlbumID] || x.Hash == "" || x.PayType != 0 {
				continue
			}
			seen[x.AlbumID] = true
			s := probeSongOf(c, x)
			if s == nil {
				continue
			}
			songs++
			probeFormats(t, c, *s)
		}
	}
	if songs == 0 {
		t.Fatal("no free song was found to ask about")
	}
}

// probeSongOf reads a song as the album service names it: its audio_info
// carries every upload the catalogue holds, which the flat search rows do
// not.
func probeSongOf(c *Client, x Track) *formatProbeSong {
	var res struct {
		Data struct {
			Songs []map[string]any `json:"songs"`
		} `json:"data"`
	}
	err := c.send("POST", request{
		Base: albumHost,
		Path: "/v1/album_audio/lite",
		Hdr:  map[string]string{"kg-tid": "255"},
		Body: map[string]any{
			"album_id": x.AlbumID, "area_code": 1, "page": 1, "pagesize": 50,
		},
	}, &res)
	if err != nil {
		fmt.Printf("  album %d: %v\n", x.AlbumID, err)
		return nil
	}
	var l map[string]any
	for _, row := range res.Data.Songs {
		base, _ := row["base"].(map[string]any)
		if pickInt(base, "album_audio_id") == x.AlbumAudioID {
			l = row
			break
		}
	}
	if l == nil {
		if len(res.Data.Songs) == 0 {
			return nil
		}
		l = res.Data.Songs[0]
	}
	t := trackFromParts(l)
	info, _ := l["audio_info"].(map[string]any)
	out := &formatProbeSong{Track: t, albumID: t.AlbumID, hashes: map[string]string{}}
	if out.AlbumID == 0 {
		out.AlbumID = x.AlbumID
		out.albumID = x.AlbumID
	}
	for k, v := range info {
		s, ok := v.(string)
		if !ok || s == "" {
			continue
		}
		lk := strings.ToLower(k)
		if !strings.Contains(lk, "hash") {
			continue
		}
		out.hashes[lk] = strings.ToLower(s)
	}
	return out
}

// probeFormats prints what one song's qualities and hashes each fetch.
func probeFormats(t *testing.T, c *Client, s formatProbeSong) {
	t.Helper()
	var names []string
	for k := range s.hashes {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Printf("\n### %s / %s  album %d  privilege=%d pay=%d  %dms\n",
		s.Singer, s.Name, s.albumID, s.Privilege, s.PayType, s.Duration)
	fmt.Printf("audio_info hashes:")
	for _, k := range names {
		fmt.Printf(" %s=%s", k, short(s.hashes[k]))
	}
	fmt.Println()

	// 1. What each quality fetches, asked with the standard hash.
	fmt.Printf("\n-- quality sweep, hash=%s --\n", short(s.hashes["hash_128"]))
	fmt.Printf("%-13s %-6s %-6s %-20s %-5s %-10s %s\n", "quality", "ext", "magic", "file says", "kbps", "bytes", "note")
	for _, q := range []string{"64", "96", "128", "160", "192", "256", "320", "flac", "high",
		"super", "viper_tape", "viper_clear", "viper_atmos", "hires", "ape", "ogg", "m4a", "atmos"} {
		r := probeURL(c, s, s.hashes["hash_128"], q, nil)
		fmt.Printf("%-13s %-6s %-6s %-20s %-5d %-10d %s\n", q, r.ext, r.magic, r.sound, r.kbps, r.size, r.note)
	}

	// 2. Whether the hash names anything at all: each of the song's own, and
	// one that never existed.
	fmt.Printf("\n-- hash sweep --\n")
	fmt.Printf("%-13s %-10s %-6s %-6s %-6s %-5s %-10s %s\n", "hash field", "asked", "quality", "ext", "magic", "kbps", "bytes", "note")
	for _, q := range []string{"320", "flac"} {
		for _, k := range names {
			r := probeURL(c, s, s.hashes[k], q, nil)
			fmt.Printf("%-13s %-10s %-6s %-6s %-6s %-5d %-10d %s\n", k, short(s.hashes[k]), q, r.ext, r.magic, r.kbps, r.size, r.note)
		}
		r := probeURL(c, s, "0123456789abcdef0123456789abcdef", q, nil)
		fmt.Printf("%-13s %-10s %-6s %-6s %-6s %-5d %-10d %s\n", "bogus", "01234567", q, r.ext, r.magic, r.kbps, r.size, r.note)
	}

	// 3. With no album audio id, the hash is the song's only name.
	fmt.Printf("\n-- hash alone (album_audio_id=0) --\n")
	fmt.Printf("%-13s %-10s %-6s %-6s %-6s %-20s %s\n", "hash field", "asked", "quality", "ext", "magic", "file says", "note")
	alone := formatProbeSong{Track: s.Track, albumID: s.albumID, hashes: s.hashes}
	alone.AlbumAudioID = 0
	for _, k := range append([]string{"hash_128", "hash_flac"}, "bogus") {
		h := alone.hashes[k]
		if k == "bogus" {
			h = "0123456789abcdef0123456789abcdef"
		}
		r := probeURL(c, alone, h, "flac", nil)
		fmt.Printf("%-13s %-10s %-6s %-6s %-6s %-20s %s\n", k, short(h), "flac", r.ext, r.magic, r.sound, r.note)
	}

	// 4. The 概念版 asks as itself for the Ogg file it streams. It signs as
	// its own app, which is why a standard client's key is refused.
	lc := *c
	lc.Lite = true
	fmt.Printf("\n-- 概念版 (pid 411, need_ogg=1) --\n")
	fmt.Printf("%-13s %-6s %-6s %-20s %-5s %-10s %s\n", "quality", "ext", "magic", "file says", "kbps", "bytes", "note")
	lite := map[string]string{"pid": "411", "page_id": "967177915",
		"ppage_id": "356753938,823673182,967485191", "need_ogg": "1"}
	for _, q := range []string{"128", "320", "flac", "high"} {
		r := probeURL(&lc, s, s.hashes["hash_128"], q, lite)
		fmt.Printf("%-13s %-6s %-6s %-20s %-5d %-10d %s\n", q, r.ext, r.magic, r.sound, r.kbps, r.size, r.note)
	}
}

// probePause is the room left between two questions: the player's URL service
// answers "操作太频繁" to a burst, which would pass for a format it never had.
const probePause = 400 * time.Millisecond

// probeURL puts one question to the player's URL service, reads what it
// served, and looks at the file itself to name the container.
func probeURL(c *Client, s formatProbeSong, hash, quality string, extra map[string]string) probeResult {
	if hash == "" {
		return probeResult{ext: "-", magic: "-", note: "the song has no such hash"}
	}
	params := map[string]string{
		"album_id":       fmt.Sprint(s.albumID),
		"area_code":      "1",
		"hash":           hash,
		"ssa_flag":       "is_fromtrack",
		"version":        "11430",
		"page_id":        "151369488",
		"quality":        quality,
		"album_audio_id": fmt.Sprint(s.AlbumAudioID),
		"behavior":       "play",
		"pid":            "2",
		"cmd":            "26",
		"pidversion":     "3001",
		"IsFreePart":     "0",
		"ppage_id":       "463467626,350369493,788954147",
		"cdnBackup":      "1",
		"module":         "",
		"clientver":      "11430",
	}
	for k, v := range extra {
		params[k] = v
	}
	dfid := c.Dev.DFID
	if dfid == "" {
		dfid = randomKey(24)
	}
	// The service refuses a burst, so a refusal is waited out and asked
	// again: a format it serves must not pass for one it does not.
	var res probeAnswer
	for try := 0; try < 4; try++ {
		time.Sleep(probePause)
		raw, err := c.do("GET", call{
			Router: "trackercdn.kugou.com",
			Path:   "/v5/url",
			Params: params,
			Key:    true,
			DFID:   dfid,
		})
		if err != nil {
			return probeResult{ext: "-", magic: "-", note: err.Error()}
		}
		res = probeAnswer{}
		if err := json.Unmarshal(raw, &res); err != nil {
			return probeResult{ext: "-", magic: "-", note: "unreadable answer: " + snippet(raw)}
		}
		if strings.Contains(res.Msg, "频繁") {
			time.Sleep(time.Duration(try+1) * 2 * time.Second)
			continue
		}
		break
	}
	if len(res.URL) == 0 || res.URL[0] == "" {
		note := res.Msg
		if note == "" {
			note = "no url"
			if res.ErrCode != 0 {
				note = "errcode"
			}
		}
		if res.ErrCode != 0 {
			note = fmt.Sprintf("%s %d", note, res.ErrCode)
		}
		return probeResult{ext: "-", magic: "-", note: note}
	}
	out := probeResult{
		ext:  strings.TrimPrefix(strings.ToLower(res.ExtName), "."),
		kbps: res.BitRate / 1000,
		size: res.FileSize,
	}
	url := strings.Replace(res.URL[0], "http://", "https://", 1)
	out.magic, out.sound = probeHead(url)
	if res.Offset != nil && res.Offset.EndMS > 0 && s.Duration > 0 && res.Offset.EndMS < s.Duration-2000 {
		out.note = fmt.Sprintf("preview, ends at %dms of %dms", res.Offset.EndMS, s.Duration)
	}
	return out
}

// probeHead reads the first bytes of a file and names what it holds: the
// container by its magic, and for a FLAC what its STREAMINFO says about the
// sound, which is what tells a 16-bit lossless file from a 24-bit master.
func probeHead(url string) (string, string) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "?", ""
	}
	req.Header.Set("Range", "bytes=0-63")
	cl := &http.Client{Timeout: 20 * time.Second}
	res, err := cl.Do(req)
	if err != nil {
		return "?", ""
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 64))
	if err != nil || len(b) < 4 {
		return "?", ""
	}
	switch {
	case string(b[:4]) == "fLaC":
		// "fLaC", a 4-byte block header, then STREAMINFO: 16+16+24+24 bits of
		// block and frame sizes, then 20 of rate, 3 of channels, 5 of
		// (depth-1).
		if len(b) < 22 {
			return "fLaC", ""
		}
		rate := int(b[18])<<12 | int(b[19])<<4 | int(b[20])>>4
		ch := (int(b[20])>>1)&0x07 + 1
		depth := ((int(b[20])&0x01)<<4 | int(b[21])>>4) + 1
		return "fLaC", fmt.Sprintf("%dHz %dch %dbit", rate, ch, depth)
	case string(b[:4]) == "OggS":
		return "OggS", ""
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		return "ftyp", ""
	case string(b[:3]) == "ID3":
		return "ID3", ""
	case len(b) >= 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0:
		return "mp3", ""
	}
	return fmt.Sprintf("%x", b[:4]), ""
}

// short keeps a hash column narrow while staying recognisable.
func short(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// TestFormatFallback asks about a song the catalogue never uploaded lossless,
// which is what tells whether a quality the song does not have is refused or
// quietly answered with a smaller file. Run with KG_FORMAT=1.
func TestFormatFallback(t *testing.T) {
	if os.Getenv("KG_FORMAT") == "" {
		t.Skip("set KG_FORMAT=1 to ask the real service")
	}
	c, err := New(os.Getenv("KG_CACHE"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ensureDFID(); err != nil {
		t.Fatal(err)
	}
	var found *Track
	for _, kw := range []string{"白噪音", "纯音乐 钢琴", "儿歌", "翻唱", "DJ版", "伴奏", "手机铃声"} {
		res, err := Search(c, kw, 1, 30)
		if err != nil {
			continue
		}
		for i := range res.Songs {
			x := res.Songs[i]
			if x.PayType == 0 && x.Hash != "" && x.HashLossl == "" && x.AlbumID != 0 {
				found = &x
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		t.Fatal("no song without a lossless upload was found")
	}
	s := probeSongOf(c, *found)
	if s == nil {
		t.Fatal("the song's album did not name it")
	}
	fmt.Printf("\n### %s / %s  album %d  privilege=%d pay=%d  %dms\n",
		s.Singer, s.Name, s.albumID, s.Privilege, s.PayType, s.Duration)
	fmt.Printf("hashes: hash=%s hash_320=%s hash_flac=%s hash_high=%s\n",
		short(s.hashes["hash"]), short(s.hashes["hash_320"]), short(s.hashes["hash_flac"]), short(s.hashes["hash_high"]))
	fmt.Printf("%-13s %-6s %-6s %-20s %-5s %-10s %s\n", "quality", "ext", "magic", "file says", "kbps", "bytes", "note")
	for _, q := range []string{"128", "320", "flac", "high"} {
		r := probeURL(c, *s, s.hashes["hash"], q, nil)
		fmt.Printf("%-13s %-6s %-6s %-20s %-5d %-10d %s\n", q, r.ext, r.magic, r.sound, r.kbps, r.size, r.note)
	}
}

// TestFormatCoverage counts, across what a search hands back, how many songs
// hold each upload: the formats exist only where the catalogue has them.
// Run with KG_FORMAT=1.
func TestFormatCoverage(t *testing.T) {
	if os.Getenv("KG_FORMAT") == "" {
		t.Skip("set KG_FORMAT=1 to ask the real service")
	}
	c, err := New(os.Getenv("KG_CACHE"))
	if err != nil {
		t.Fatal(err)
	}
	queries := []string{"周杰伦", "邓丽君", "五月天", "古典 钢琴", "爵士", "摇滚", "说唱", "民谣"}
	var all, free, high, lossl, only128 int
	for _, kw := range queries {
		res, err := Search(c, kw, 1, 30)
		if err != nil {
			t.Logf("%s: %v", kw, err)
			continue
		}
		for _, x := range res.Songs {
			all++
			if x.PayType == 0 {
				free++
			}
			if x.HashHires != "" {
				high++
			}
			if x.HashLossl != "" {
				lossl++
			}
			if x.HashHigh == "" && x.HashLossl == "" && x.HashHires == "" {
				only128++
			}
		}
	}
	fmt.Printf("\n%d songs from %d searches: %d free, %d with FLAC, %d with Hi-Res, %d with nothing but MP3\n",
		all, len(queries), free, lossl, high, only128)
}
