package lyrics

import (
	"encoding/base64"
	"testing"
	"time"
)

// krcLanguageTag builds the tag a KuGou KRC file carries, the way the
// service writes it: base64 of JSON, one entry per line of the song.
func krcLanguageTag(t *testing.T, content string) string {
	t.Helper()
	return "[language:" + base64.StdEncoding.EncodeToString([]byte(content)) + "]"
}

// TestParseKRCTranslation covers the translation a KuGou KRC carries in its
// [language:] tag: the type 1 entries hold the translated line, as one
// string, aligned with the lines of the body.
func TestParseKRCTranslation(t *testing.T) {
	lang := krcLanguageTag(t, `{"content":[{"type":1,"language":0,"lyricContent":[["俱乐部不是找另一半的绝佳场所"],["所以酒吧很适合我"],["我和朋友们百无聊赖的喝着酒"]]}],"version":1}`)
	krc := lang + "\r\n" +
		"[0,3000]<0,1000,0>The <1000,1000,0>club <2000,1000,0>isn't <3000,1000,0>the best place to find a lover\r\n" +
		"[3000,3000]<3000,1000,0>So <4000,1000,0>the bar <5000,1000,0>is where I go\r\n" +
		"[6000,3000]<6000,1000,0>Me <7000,1000,0>and my friends at the table doing shots\r\n"
	lines := Build("", "", "", krc)
	if len(lines) != 3 {
		t.Fatalf("got %d lines: %+v", len(lines), lines)
	}
	want := []string{
		"俱乐部不是找另一半的绝佳场所",
		"所以酒吧很适合我",
		"我和朋友们百无聊赖的喝着酒",
	}
	for i, w := range want {
		if lines[i].Translation != w {
			t.Errorf("line %d: translation %q, want %q", i, lines[i].Translation, w)
		}
	}
	if lines[0].Text != "The club isn't the best place to find a lover" {
		t.Errorf("line 0 text: %q", lines[0].Text)
	}
}

// TestParseKRCRomanized covers the type 0 entries, which hold the line read
// out in Latin letters, split into the words it is sung in.
func TestParseKRCRomanized(t *testing.T) {
	lang := krcLanguageTag(t, `{"content":[{"type":0,"language":0,"lyricContent":[["lan ","lou ","yu ","pin ","ci ","su ","fa "],["yan ","ya ","pau ","dik ","nei ","dong ","ma "]]}],"version":1}`)
	krc := lang + "\r\n" +
		"[0,3000]<0,1000,0>拦路雨偏似雪花\r\n" +
		"[3000,3000]<3000,1000,0>饮泣的你冻吗\r\n"
	lines := Build("", "", "", krc)
	if len(lines) != 2 {
		t.Fatalf("got %d lines: %+v", len(lines), lines)
	}
	if lines[0].Romanized != "lan lou yu pin ci su fa" {
		t.Errorf("line 0: romanized %q", lines[0].Romanized)
	}
	if lines[1].Romanized != "yan ya pau dik nei dong ma" {
		t.Errorf("line 1: romanized %q", lines[1].Romanized)
	}
}

// TestParseKRCNoTranslation covers a song the service gives no translation
// for: the tag holds an empty list, and the lines carry nothing beside them.
func TestParseKRCNoTranslation(t *testing.T) {
	lang := krcLanguageTag(t, `{"content":[],"version":1}`)
	krc := lang + "\r\n" +
		"[0,3000]<0,1000,0>徘徊过多少橱窗\r\n"
	lines := Build("", "", "", krc)
	if len(lines) != 1 {
		t.Fatalf("got %d lines: %+v", len(lines), lines)
	}
	if lines[0].Translation != "" || lines[0].Romanized != "" {
		t.Errorf("line 0: %+v", lines[0])
	}
}

func TestParseLRC(t *testing.T) {
	lines := mergeLyrics("[00:01.000] 作词 : 某人\n[00:30.54]故事的小黄花\n[00:34.165][01:10.1]从出生那年就飘着\n[00:40.00]\n",
		"[00:30.54]The little yellow flower\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines: %+v", len(lines), lines)
	}
	if lines[1].At != 30540*time.Millisecond || lines[1].Translation != "The little yellow flower" {
		t.Errorf("line 1: %+v", lines[1])
	}
	if lines[3].At != 70100*time.Millisecond || lines[3].Text != "从出生那年就飘着" {
		t.Errorf("line 3: %+v", lines[3])
	}
	if i := Index(lines, 33*time.Second); i != 1 {
		t.Errorf("index at 33s = %d", i)
	}
	if i := Index(lines, 0); i != -1 {
		t.Errorf("index at 0 = %d", i)
	}
}

func TestParseYRC(t *testing.T) {
	yrc := `{"t":0,"c":[{"tx":"作词: "},{"tx":"某人"}]}
[13010,4780](13010,660,0)从(13670,670,0)不(14340,30,0) (14370,480,0)主
[17790,1770](17790,170,0)I(17960,450,0) (18410,290,0)love`
	lines := Build("", "[00:13.00]Never\n[00:17.80]我爱", yrc, "")
	if len(lines) != 2 || !lines[0].Timed {
		t.Fatalf("lines %+v", lines)
	}
	l := lines[0]
	if l.Text != "从不 主" || l.At != 13010*time.Millisecond || l.End != 17790*time.Millisecond {
		t.Errorf("line %q %v-%v", l.Text, l.At, l.End)
	}
	if len(l.Words) != 3 || l.Words[1].Text != "不 " || l.Words[2].Start != 14370*time.Millisecond {
		t.Errorf("words %+v", l.Words)
	}
	if l.Translation != "Never" || lines[1].Translation != "我爱" {
		t.Errorf("translations %q %q", l.Translation, lines[1].Translation)
	}
}

func TestSpreadWords(t *testing.T) {
	w := SpreadWords("我爱 you all", 0, 10*time.Second)
	got := []string{}
	for _, x := range w {
		got = append(got, x.Text)
	}
	if len(w) != 4 || got[0] != "我" || got[1] != "爱" || got[2] != "you " || got[3] != "all" {
		t.Fatalf("words %q", got)
	}
	if w[0].Start != 0 || w[3].End > 10*time.Second || w[1].Start != w[0].End {
		t.Errorf("times %+v", w)
	}
}

func TestQuiets(t *testing.T) {
	s := time.Second
	lines := []Line{
		{At: 0, End: 2 * s, Text: "作词 : 林夕"},
		{At: 2 * s, End: 4 * s, Text: "作曲：陈小霞"},
		{At: 20 * s, End: 25 * s, Text: "一"},
		{At: 26 * s, End: 30 * s, Text: "二"},
		{At: 45 * s, End: 50 * s, Text: "三"},
	}
	got := Quiets(lines, 60*s, 8*s)
	want := []Quiet{{0, 20 * s}, {30 * s, 45 * s}, {50 * s, 60 * s}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("quiet %d: %v, want %v", i, got[i], want[i])
		}
	}
	if q := Quiets(nil, 60*s, 8*s); len(q) != 0 {
		t.Errorf("a song without lyrics: %v", q)
	}
}

func TestIsCredit(t *testing.T) {
	for text, want := range map[string]bool{
		"作词 : 林夕":            true,
		"作曲：陈小霞":             true,
		"编曲: 藤田淳平(Elements)": true,
		"Producer: 周杰伦":      true,
		"我说：你好":              false,
		"时间：是一把刀":            false,
		"悴んだ心 ふるえる眼差し":       false,
		"词：方文山":              true,
		"曲终人散：空余叹":           false,
	} {
		if IsCredit(text) != want {
			t.Errorf("%q: %v", text, !want)
		}
	}
}

// LRC writes the lines a file is tagged with, each translation on the line
// after the one it translates, under the same time.
func TestLRC(t *testing.T) {
	lines := []Line{
		{At: 0, Text: "作词 : 林夕"},
		{At: 12*time.Second + 340*time.Millisecond, Text: "晴天", Translation: "A sunny day"},
		// A translation that repeats the line, as one in the same language
		// would, is not written twice.
		{At: 20 * time.Second, Text: "好", Translation: "好"},
		// Neither a word nor its translation: nothing to show.
		{At: 25 * time.Second},
	}
	want := "[00:00.00]作词 : 林夕\n" +
		"[00:12.34]晴天\n" +
		"[00:12.34]A sunny day\n" +
		"[00:20.00]好\n"
	if got := LRC(lines); got != want {
		t.Errorf("LRC:/n%q/nwant:/n%q", got, want)
	}
}
