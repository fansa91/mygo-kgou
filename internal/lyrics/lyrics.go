package lyrics

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// A Line is a line of timed lyrics, with its translation if any, and
// when each of its words is sung.
type Line struct {
	At   time.Duration
	End  time.Duration
	Text string
	// Translation is the line in another language, as KuGou's KRC files
	// carry it: the original in the language sung, and this beside it.
	Translation string
	// Romanized is the line written in Latin letters, as KuGou's KRC
	// files give it for songs sung in Chinese or Korean.
	Romanized string
	Words     []Word
	// Timed says the words carry their own times, as NetEase's word-timed
	// lyrics give them; else they share the line's time evenly.
	Timed bool
}

// A Word is a word of a line, or a character of Chinese or Japanese,
// with when it is sung.
type Word struct {
	Text       string
	Start, End time.Duration
}

var lrcTime = regexp.MustCompile(`\[(\d+):(\d+)(?:[.:](\d+))?\]`)

// parseLRC reads lyrics in the LRC format, as NetEase serves them: a line
// may carry several times, and credits come as lines of their own.
func parseLRC(lrc string) []Line {
	var out []Line
	for _, raw := range strings.Split(lrc, "\n") {
		times := lrcTime.FindAllStringSubmatchIndex(raw, -1)
		if len(times) == 0 {
			continue
		}
		text := strings.TrimSpace(raw[times[len(times)-1][1]:])
		for _, m := range times {
			min, _ := strconv.Atoi(raw[m[2]:m[3]])
			sec, _ := strconv.Atoi(raw[m[4]:m[5]])
			var frac time.Duration
			if m[6] >= 0 {
				f := raw[m[6]:m[7]]
				n, _ := strconv.Atoi(f)
				frac = time.Duration(n) * time.Second
				for range len(f) {
					frac /= 10
				}
			}
			at := time.Duration(min)*time.Minute + time.Duration(sec)*time.Second + frac
			out = append(out, Line{At: at, Text: text})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

var (
	yrcLine = regexp.MustCompile(`^\[(\d+),(\d+)\]`)
	yrcWord = regexp.MustCompile(`\((\d+),(\d+),\d+\)([^(]*)`)

	// KRC format uses angle brackets for word timing: <start,length,0>word
	krcLine = regexp.MustCompile(`^\[(\d+),(\d+)\]`)
	krcWord = regexp.MustCompile(`<(\d+),(\d+),\d+>([^<]*)`)
	// [language:base64json] tag contains translation data
	krcLanguage = regexp.MustCompile(`\[language:([A-Za-z0-9+/=]+)\]`)
)

// parseYRC reads NetEase's word-timed lyrics: a line is
// "[start,length](start,length,0)word(start,length,0)word…", in
// milliseconds. Credits come as lines of JSON, which it leaves out.
func parseYRC(yrc string) []Line {
	ms := func(s string) time.Duration {
		n, _ := strconv.Atoi(s)
		return time.Duration(n) * time.Millisecond
	}
	var out []Line
	for _, raw := range strings.Split(yrc, "\n") {
		m := yrcLine.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		l := Line{At: ms(m[1]), Timed: true}
		l.End = l.At + ms(m[2])
		for _, w := range yrcWord.FindAllStringSubmatch(raw[len(m[0]):], -1) {
			start := ms(w[1])
			word := Word{Text: w[3], Start: start, End: start + ms(w[2])}
			if strings.TrimSpace(word.Text) == "" {
				// A space between words: it belongs to the word before.
				if n := len(l.Words); n > 0 {
					l.Words[n-1].Text += " "
				}
				continue
			}
			l.Words = append(l.Words, word)
		}
		for _, w := range l.Words {
			l.Text += w.Text
		}
		l.Text = strings.TrimSpace(l.Text)
		if l.Text != "" {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// parseKRC reads KuGou's word-timed lyrics (KRC): a line is
// "[start,length]<start,length,0>word<start,length,0>word..." in milliseconds.
//
// A KRC file may also carry a [language:base64json] tag, whose JSON holds one
// entry per timed line of the body, aligned with it:
//
//	content: [{type: 1, language: 0, lyricContent: [["译好的这一行"], ...]}]
//
// An entry of type 1 holds the line's translation, as one string; an entry
// of type 0 holds the line read out in Latin letters, split into the words
// it is sung in. Both are wanted: the translation is the line in another
// language, the reading is how the line sounds.
func parseKRC(krc string) ([]Line, []string) {
	ms := func(s string) time.Duration {
		n, _ := strconv.Atoi(s)
		return time.Duration(n) * time.Millisecond
	}
	var lines []Line
	var translations []string
	var readings []string
	krcLines := strings.Split(krc, "\n")

	// First pass: collect the [language:] tag.
	var langData string
	for _, raw := range krcLines {
		if m := krcLanguage.FindStringSubmatch(raw); m != nil {
			langData = m[1]
			break
		}
	}

	// Decode it: what each line of the song says in another tongue, and
	// how it reads out loud.
	if langData != "" {
		decoded, err := base64.StdEncoding.DecodeString(langData)
		if err == nil {
			var lang struct {
				Content []struct {
					Type         int        `json:"type"`
					LyricContent [][]string `json:"lyricContent"`
				} `json:"content"`
				Version int `json:"version"`
			}
			if json.Unmarshal(decoded, &lang) == nil {
				for _, c := range lang.Content {
					switch c.Type {
					case 1:
						// The translated line, one string; some songs
						// split it into several, which are joined.
						for _, entry := range c.LyricContent {
							translations = append(translations, strings.TrimSpace(strings.Join(entry, "")))
						}
					case 0:
						// The line read out, one string per word.
						for _, entry := range c.LyricContent {
							readings = append(readings, strings.TrimSpace(strings.Join(entry, "")))
						}
					}
				}
			}
		}
	}

	// Second pass: parse KRC lines.
	//
	// What the tag holds is aligned with the timed lines of the body by
	// position, so the position of each line in the body is kept as it is
	// read: a line that carries no words is left out of the result, and
	// counting only the lines kept would shift everything after it.
	body := 0
	for _, raw := range krcLines {
		m := krcLine.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		l := Line{At: ms(m[1]), Timed: true}
		l.End = l.At + ms(m[2])
		if body < len(translations) {
			l.Translation = translations[body]
		}
		if body < len(readings) {
			l.Romanized = readings[body]
		}
		body++
		for _, w := range krcWord.FindAllStringSubmatch(raw[len(m[0]):], -1) {
			start := ms(w[1])
			word := Word{Text: w[3], Start: start, End: start + ms(w[2])}
			if strings.TrimSpace(word.Text) == "" {
				if n := len(l.Words); n > 0 {
					l.Words[n-1].Text += " "
				}
				continue
			}
			l.Words = append(l.Words, word)
		}
		for _, w := range l.Words {
			l.Text += w.Text
		}
		l.Text = strings.TrimSpace(l.Text)
		if l.Text != "" {
			lines = append(lines, l)
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].At < lines[j].At })
	return lines, translations
}

// Build makes the lines to show from what NetEase serves: the
// word-timed lyrics where there are some, else the timed lines, with the
// translation of each.
// If krc is provided, it takes precedence over yrc and lrc for word-timed display.
func Build(lrc, translation, yrc, krc string) []Line {
	var lines []Line
	if krc != "" {
		lines, _ = parseKRC(krc)
	}
	if len(lines) == 0 && yrc != "" {
		lines = parseYRC(yrc)
	}
	if len(lines) == 0 {
		for _, l := range parseLRC(lrc) {
			if l.Text != "" {
				lines = append(lines, l)
			}
		}
		for i := range lines {
			end := lines[i].At + 6*time.Second
			if i+1 < len(lines) {
				end = min(end, lines[i+1].At)
			}
			lines[i].End = end
			lines[i].Words = SpreadWords(lines[i].Text, lines[i].At, end)
		}
	}
	// Fallback: a translation that came apart from the lyrics, matched to
	// the line sung at the same time. A line that already has one keeps
	// it, which is what the KRC's own translation is for.
	for _, t := range parseLRC(translation) {
		if t.Text == "" {
			continue
		}
		best, bestGap := -1, 1500*time.Millisecond
		for i := range lines {
			gap := lines[i].At - t.At
			if gap < 0 {
				gap = -gap
			}
			if gap < bestGap {
				best, bestGap = i, gap
			}
		}
		if best >= 0 && lines[best].Translation == "" {
			lines[best].Translation = t.Text
		}
	}
	return lines
}

// LRC formats lines as LRC, for a song's file: each line timed, and, where
// the song has a translation, the translated line right after it under the
// same time, so a player that reads the tag shows the song in both
// languages, one line after the other.
func LRC(lines []Line) string {
	var b strings.Builder
	for _, l := range lines {
		text := strings.TrimSpace(l.Text)
		translation := strings.TrimSpace(l.Translation)
		if text == "" && translation == "" {
			continue
		}
		stamp := lrcStamp(l.At)
		if text != "" {
			b.WriteString(stamp)
			b.WriteString(text)
			b.WriteString("\n")
		}
		// A translation that repeats the line, as one in the same language
		// does, would show it twice.
		if translation != "" && translation != text {
			b.WriteString(stamp)
			b.WriteString(translation)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// lrcStamp formats a time as LRC keeps it: [mm:ss.xx].
func lrcStamp(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	cs := int64(d / (10 * time.Millisecond))
	return fmt.Sprintf("[%02d:%02d.%02d]", cs/6000, cs/100%60, cs%100)
}

// mergeLyrics pairs timed lines with their translations, without words'
// own times.
func mergeLyrics(lrc, translation string) []Line {
	return Build(lrc, translation, "", "")
}

// SpreadWords splits text into words, each Chinese, Japanese or Korean
// character one of them, and shares the time from start to end among them
// by their lengths, for lines whose words have no times of their own.
func SpreadWords(text string, start, end time.Duration) []Word {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			if cur.Len() > 0 {
				cur.WriteRune(' ')
				flush()
			}
		case isCJK(r):
			flush()
			words = append(words, string(r))
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	if len(words) == 0 {
		return nil
	}
	total := 0
	for _, w := range words {
		total += utf8.RuneCountInString(strings.TrimSpace(w))
	}
	// Singing takes most of the line, not the pause before the next.
	span := time.Duration(float64(end-start) * 0.85)
	out := make([]Word, len(words))
	at := start
	for i, w := range words {
		d := span * time.Duration(utf8.RuneCountInString(strings.TrimSpace(w))) / time.Duration(max(total, 1))
		out[i] = Word{Text: w, Start: at, End: at + d}
		at += d
	}
	return out
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// Index returns the line sung at pos, or -1 before the first.
func Index(lines []Line, pos time.Duration) int {
	i := sort.Search(len(lines), func(i int) bool { return lines[i].At > pos })
	return i - 1
}

// A Quiet is a stretch of a song with nothing sung: its intro, an
// interlude, or its outro.
type Quiet struct {
	From, To time.Duration
}

// Quiets returns the stretches of a song length long, of at least least,
// in which none of lines is sung. Credits, as 作词 and 作曲, are not sung.
func Quiets(lines []Line, length, least time.Duration) []Quiet {
	var out []Quiet
	add := func(from, to time.Duration) {
		if to-from >= least {
			out = append(out, Quiet{from, to})
		}
	}
	at := time.Duration(0)
	sung := false
	for _, l := range lines {
		if IsCredit(l.Text) {
			continue
		}
		add(at, l.At)
		at, sung = max(at, l.End), true
	}
	if sung && length > 0 {
		add(at, length)
	}
	return out
}

// creditNames are what credit lines begin with.
var creditNames = []string{"作词", "作曲", "编曲", "制作", "监制", "混音", "录音", "母带", "和声", "吉他", "贝斯", "鼓", "键盘", "弦乐", "出品", "发行", "OP", "SP", "词", "曲", "Lyrics", "Music", "Composer", "Arranger", "Producer"}

// IsCredit reports whether a line names who made the song, as "作词 :
// 林夕", rather than being sung.
func IsCredit(text string) bool {
	i := strings.IndexAny(text, ":：")
	if i <= 0 || utf8.RuneCountInString(text[:i]) > 12 {
		return false
	}
	head := strings.TrimSpace(text[:i])
	for _, n := range creditNames {
		// A name of one character, as 词, is the whole of the head.
		if head == n || utf8.RuneCountInString(n) > 1 && strings.HasPrefix(head, n) {
			return true
		}
	}
	return false
}
