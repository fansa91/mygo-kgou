package app

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/tag"
)

// Downloading songs: each is fetched at the level the listener chose, named
// as the format in the settings says, tagged with what it is, and written
// to the download folder. A few download at a time; the others wait.

// defaultDownloadFormat names a downloaded song by its artist and its
// name, which every player reads.
const defaultDownloadFormat = "{歌手} - {歌名}"

// defaultDownloadQuality is the level a download asks for: the best the
// service has, unless the listener chooses another.
const defaultDownloadQuality = "lossless"

// downloadWorkers is how many songs download at once.
const downloadWorkers = 3

// downloadClient fetches the songs: KuGou's CDN serves a certificate the
// system does not trust, as it does for playback.
var downloadClient = &http.Client{
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
}

// downloadState is how far a song on its way to the disk has got.
type downloadState int

const (
	dlWaiting downloadState = iota
	dlRunning
	dlDone
	dlFailed
)

// downloadTask is one song being downloaded.
type downloadTask struct {
	// id names the task in the log the downloads page shows, and keeps its
	// row the same one as it goes from waiting to done.
	id    int64
	track netease.Track
	state downloadState
	got   int64  // bytes fetched
	total int64  // bytes expected, 0 until known
	level string // the level asked for
	name  string // where it was saved, once done
	err   string // why it failed
}

// downloader runs the downloads of the app, a few at a time.
type downloader struct {
	a *app

	mu      sync.Mutex
	seq     int64
	waiting []*downloadTask
	tasks   []*downloadTask // the batch the bar shows
	log     []*downloadTask // everything asked for, for the downloads page
	active  int
	ticking time.Time // when the view was last told of the progress
}

// downloadLogMax is how many downloads the page keeps: the oldest that
// have ended make room for the new ones.
const downloadLogMax = 200

// downloads returns the app's downloader, making it the first time.
func (a *app) downloads() *downloader {
	if a.dl == nil {
		a.dl = &downloader{a: a}
	}
	return a.dl
}

// downloadOptions is what a download was asked with, taken when it was
// queued so that the goroutine reads no state the views change.
type downloadOptions struct {
	dir    string
	format string
	level  string
}

// downloadDir is where downloads go: the folder chosen, or the system's
// music folder.
func (a *app) downloadDir() string {
	if dir := strings.TrimSpace(a.settings.DownloadDir); dir != "" {
		return dir
	}
	if dir, err := mygo.App.Path(mygo.PathMusic); err == nil && dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return home
}

// add puts tracks on the queue, and starts them.
func (d *downloader) add(tracks []netease.Track) {
	if len(tracks) == 0 {
		return
	}
	opts := downloadOptions{dir: d.a.downloadDir(), format: d.a.settings.DownloadFormat, level: d.a.settings.DownloadQuality}
	if opts.level == "" {
		opts.level = defaultDownloadQuality
	}
	d.mu.Lock()
	for i := range tracks {
		d.seq++
		t := &downloadTask{id: d.seq, track: tracks[i], level: opts.level}
		d.waiting = append(d.waiting, t)
		d.tasks = append(d.tasks, t)
		d.log = append(d.log, t)
	}
	d.trim()
	d.pump(opts)
	d.mu.Unlock()
	// The bar shows only while there is something on its way, and the page
	// the downloads, so the listener knows the ask was heard.
	n := len(tracks)
	d.a.post(func() {
		if n == 1 {
			d.a.toast = "已加入下载"
		} else {
			d.a.toast = fmt.Sprintf("已加入下载 %d 首", n)
		}
	})
}

// trim keeps the log to the last downloadLogMax downloads, letting go of
// the oldest that have ended rather than one still on its way. The caller
// holds the lock.
func (d *downloader) trim() {
	if len(d.log) <= downloadLogMax {
		return
	}
	drop := len(d.log) - downloadLogMax
	kept := d.log[:0]
	for _, t := range d.log {
		if drop > 0 && t.state != dlRunning && t.state != dlWaiting {
			drop--
			continue
		}
		kept = append(kept, t)
	}
	d.log = kept
}

// pump starts the songs that fit, each in its goroutine. The caller holds
// the lock.
func (d *downloader) pump(opts downloadOptions) {
	for d.active < downloadWorkers && len(d.waiting) > 0 {
		t := d.waiting[0]
		d.waiting = d.waiting[1:]
		t.state = dlRunning
		d.active++
		go func() {
			d.run(t, opts)
			d.mu.Lock()
			d.active--
			d.pump(opts)
			d.mu.Unlock()
			d.report()
		}()
	}
}

// run downloads one song: its stream, its cover, the tags, and the file
// under its name in the download folder.
func (d *downloader) run(t *downloadTask, opts downloadOptions) {
	st, err := netease.SongURL(t.track.ID, opts.level)
	if err != nil {
		d.fail(t, "这首歌暂时不能下载")
		return
	}
	ext := st.Type
	if ext == "" {
		ext = "mp3"
	}
	rel := downloadName(opts.format, &t.track, st.Level, ext)
	target := uniquePath(filepath.Join(opts.dir, rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		d.fail(t, "无法创建下载文件夹")
		return
	}
	// The song is written under a name of its format, so that the tags go
	// in by the container they are for, and takes its name once whole.
	tmp, err := os.CreateTemp(filepath.Dir(target), ".mygo-download-*."+ext)
	if err != nil {
		d.fail(t, "无法写入下载文件夹")
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	d.setTotal(t, st.Size)
	if err := d.fetch(t, st.URL, tmp); err != nil {
		tmp.Close()
		d.fail(t, "下载中断，请稍后再试")
		return
	}
	if err := tmp.Close(); err != nil {
		d.fail(t, "无法写入下载文件")
		return
	}
	cover, mime := d.cover(&t.track)
	meta := tag.Meta{
		Title:       t.track.Name,
		Artist:      t.track.ArtistNames(),
		Album:       t.track.Album.Name,
		AlbumArtist: albumArtist(&t.track),
		Year:        "",
		Lyrics:      d.lyricsOf(&t.track),
		Cover:       cover,
		MIME:        mime,
	}
	// A tag that cannot be written leaves the song as it downloaded, which
	// is still a song.
	tag.Write(tmpName, meta)
	if err := os.Rename(tmpName, target); err != nil {
		d.fail(t, "无法保存下载文件")
		return
	}
	d.mu.Lock()
	t.name = target
	t.state = dlDone
	t.got = max(t.got, st.Size)
	d.mu.Unlock()
}

// lyricsOf returns the song's words for its file's tag, as LRC under the
// song's name, artists and album, with each line's translation after it.
// An episode, or a song whose words cannot be had, leaves the tag without
// them.
func (d *downloader) lyricsOf(tr *netease.Track) string {
	if tr.ProgramID != 0 {
		return ""
	}
	lrc, translation, yrc, krc, err := netease.Lyrics(tr.ID)
	if err != nil {
		return ""
	}
	body := lyrics.LRC(lyrics.Build(lrc, translation, yrc, krc))
	if body == "" {
		return ""
	}
	var b strings.Builder
	for _, h := range []struct{ tag, value string }{
		{"ti", tr.Name},
		{"ar", tr.ArtistNames()},
		{"al", tr.Album.Name},
	} {
		if v := strings.TrimSpace(h.value); v != "" {
			// A bracket in a name would end the header early.
			v = strings.NewReplacer("[", "", "]", "").Replace(v)
			b.WriteString("[" + h.tag + ":" + v + "]\n")
		}
	}
	b.WriteString(body)
	return b.String()
}

// fetch copies what is at url into out, reporting how far it has got.
func (d *downloader) fetch(t *downloadTask, url string, out io.Writer) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", netease.UserAgent)
	res, err := downloadClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading: %s", res.Status)
	}
	if t.total == 0 && res.ContentLength > 0 {
		d.setTotal(t, res.ContentLength)
	}
	buf := make([]byte, 64<<10)
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			d.progress(t, int64(n))
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// cover returns the album's picture for the file's tags, and its type.
func (d *downloader) cover(tr *netease.Track) ([]byte, string) {
	pic := netease.Sized(tr.Album.PicURL, 600)
	if pic == "" {
		return nil, ""
	}
	b, err := netease.Fetch(pic)
	if err != nil || len(b) == 0 {
		return nil, ""
	}
	return b, http.DetectContentType(b)
}

func (d *downloader) setTotal(t *downloadTask, n int64) {
	d.mu.Lock()
	t.total = n
	d.mu.Unlock()
}

// progress adds n bytes to what fetched, and tells the view now and then
// rather than of every chunk.
func (d *downloader) progress(t *downloadTask, n int64) {
	d.mu.Lock()
	t.got += n
	tick := time.Since(d.ticking) > 150*time.Millisecond
	if tick {
		d.ticking = time.Now()
	}
	d.mu.Unlock()
	if tick {
		d.a.post(func() {})
	}
}

func (d *downloader) fail(t *downloadTask, msg string) {
	d.mu.Lock()
	t.state = dlFailed
	t.err = msg
	d.mu.Unlock()
}

// report tells the listener of a download batch once it has all ended:
// what got to the folder, and what failed. Nothing is on its way any more.
func (d *downloader) report() {
	d.mu.Lock()
	if len(d.waiting) > 0 || d.active > 0 {
		d.mu.Unlock()
		return
	}
	done, failed := 0, 0
	var firstErr string
	for _, t := range d.tasks {
		switch t.state {
		case dlDone:
			done++
		case dlFailed:
			failed++
			if firstErr == "" {
				firstErr = t.err
			}
		}
	}
	// Forget the finished batch: the bar shows only what is on its way.
	d.tasks = nil
	d.mu.Unlock()
	if done+failed == 0 {
		return
	}
	msg := ""
	switch {
	case failed == 0:
		msg = fmt.Sprintf("已下载 %d 首", done)
	case done == 0:
		msg = "下载失败：" + firstErr
	default:
		msg = fmt.Sprintf("已下载 %d 首，%d 首失败", done, failed)
	}
	d.a.post(func() {
		d.a.toast = msg
	})
}

// entries returns what the downloads page shows: every song asked for
// since the page last cleared them, oldest first, each copied so that the
// page reads none of them as another download changes it.
func (d *downloader) entries() []downloadTask {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]downloadTask, len(d.log))
	for i, t := range d.log {
		out[i] = *t
	}
	return out
}

// retry asks for a download that failed once more.
func (d *downloader) retry(id int64) {
	d.mu.Lock()
	var track netease.Track
	found := false
	for _, t := range d.log {
		if t.id == id {
			track, found = t.track, true
			break
		}
	}
	d.mu.Unlock()
	if found {
		d.add([]netease.Track{track})
	}
}

// clearFinished forgets the downloads that ended, keeping those on their
// way.
func (d *downloader) clearFinished() {
	d.mu.Lock()
	kept := d.log[:0]
	for _, t := range d.log {
		if t.state == dlRunning || t.state == dlWaiting {
			kept = append(kept, t)
		}
	}
	d.log = kept
	d.mu.Unlock()
	d.a.post(func() {})
}

// openPath and revealPath reach the desktop; tests replace them.
var (
	openPath   = mygo.Shell.OpenPath
	revealPath = mygo.Shell.ShowItemInFolder
)

// openFolder opens the folder downloads go to.
func (d *downloader) openFolder() {
	_ = openPath(d.a.downloadDir())
}

// reveal shows a downloaded file in the file manager.
func (d *downloader) reveal(path string) {
	if path != "" {
		revealPath(path)
	}
}

// downloadName names a file by format, a template of the tokens below,
// which may hold folders.
func downloadName(format string, tr *netease.Track, level, ext string) string {
	if strings.TrimSpace(format) == "" {
		format = defaultDownloadFormat
	}
	duration := time.Duration(tr.Duration) * time.Millisecond
	values := map[string]string{
		"{歌名}": tr.Name,
		"{歌手}": tr.ArtistNames(),
		"{专辑}": tr.Album.Name,
		"{音质}": qualityLabel(level),
		"{时长}": fmt.Sprintf("%dm%02ds", int(duration.Minutes()), int(duration.Seconds())%60),
	}
	name := format
	for token, value := range values {
		name = strings.ReplaceAll(name, token, cleanName(value))
	}
	// A template may put a song in folders; its values may not.
	var parts []string
	for _, part := range strings.Split(name, "/") {
		part = strings.Trim(cleanName(part), " .")
		if part == "" || part == "." || part == ".." {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		parts = []string{"未知歌曲"}
	}
	return strings.Join(parts, string(filepath.Separator)) + "." + ext
}

// cleanName replaces what a file name may not hold on Windows, and the
// characters that would make a path go elsewhere.
func cleanName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			b.WriteRune('_')
		default:
			if r < 0x20 {
				continue
			}
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// uniquePath returns path, or the first name beside it that is free:
// "song (2).mp3" when "song.mp3" is there.
func uniquePath(path string) string {
	if _, err := os.Stat(path); err != nil {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; ; i++ {
		p := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(p); err != nil {
			return p
		}
	}
}

// qualityLabel names a level shortly, for a file's name.
func qualityLabel(level string) string {
	switch level {
	case "standard":
		return "标准"
	case "higher", "exhigh":
		return "高品"
	case "lossless":
		return "无损"
	case "hires":
		return "Hi-Res"
	}
	return cleanName(level)
}

// albumArtist is the artist an album is filed under: the first credit.
func albumArtist(tr *netease.Track) string {
	if len(tr.Artists) > 0 {
		return tr.Artists[0].Name
	}
	return tr.ArtistNames()
}
