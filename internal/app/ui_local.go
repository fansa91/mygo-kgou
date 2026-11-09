package app

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/netease"
)

// The local library: the songs in the listener's own folders — the ones the
// app downloaded, beside the ones it never did — played from their files,
// with the tags, the pictures and the words the files carry.

// localTrack makes a song the app knows of one found in a folder: its file
// is where it plays from, and its picture, if it has one, loads from there.
func localTrack(t local.Track) netease.Track {
	tr := netease.Track{
		ID:       localID(t.Path),
		Name:     t.Name,
		Duration: t.Length.Milliseconds(),
		Path:     t.Path,
		Year:     t.Year,
		TrackNo:  t.TrackNo,
		DiscNo:   t.DiscNo,
	}
	tr.Artists = []netease.Artist{{Name: t.Artist}}
	tr.Album = netease.Album{Name: t.Album}
	if t.Covered {
		tr.Album.PicURL = localCoverURL(t.Path)
	}
	return tr
}

// localID names a song of the local library by where its file is: negative,
// as the numbers KuGou gives are not, and the same from one run to the next,
// so that a session picks up where it was left.
func localID(path string) int64 {
	h := fnv.New64a()
	h.Write([]byte(strings.ToLower(path)))
	return -int64(h.Sum64()&0x7fff_ffff_ffff_ffff) - 1
}

// localCoverURL names a song's picture for the covers' cache: the file it is
// in, which loads it when the cover shows.
func localCoverURL(path string) string { return "local:" + path }

// localCoverPath returns the file a cover names, when it names one of the
// local library's rather than a picture on the CDN.
func localCoverPath(url string) (string, bool) {
	path, ok := strings.CutPrefix(url, "local:")
	return path, ok
}

// localFormat returns the format of a song of the local library, as its row
// shows it, and "" for a song of KuGou's.
func localFormat(tr *netease.Track) string {
	if !tr.Local() {
		return ""
	}
	switch strings.ToLower(filepath.Ext(tr.Path)) {
	case ".mp3":
		return local.FormatMP3
	case ".flac":
		return local.FormatFLAC
	case ".ogg", ".oga":
		return local.FormatOgg
	case ".wav":
		return local.FormatWAV
	}
	return ""
}

// localDir is where the local library is: the folder chosen, or the one the
// downloads go to, which is where the app's own songs are.
func (a *app) localDir() string {
	if dir := strings.TrimSpace(a.settings.LocalDir); dir != "" {
		return dir
	}
	return a.downloadDir()
}

// loadLocal shows the songs of the local library, scanning its folder the
// first time the page shows, or once the folder has changed.
func (a *app) loadLocal() {
	if a.localLoading || !a.localScanned.IsZero() && a.localScannedDir == a.localDir() {
		return
	}
	a.scanLocal()
}

// scanLocal reads the folder of the local library again. A scan already
// under way is not started again.
func (a *app) scanLocal() {
	if a.localLoading {
		return
	}
	dir := a.localDir()
	a.localLoading, a.localErr = true, ""
	go func() {
		tracks, skipped, err := local.Scan(dir)
		a.post(func() {
			a.localLoading = false
			if err != nil {
				a.localErr = "无法读取这个文件夹"
				return
			}
			a.setLocal(tracks, skipped)
			a.localScannedDir = dir
		})
	}()
}

// setLocal makes tracks the local library, as a scan of it found them.
func (a *app) setLocal(tracks []local.Track, skipped int) {
	a.localTracks = make([]netease.Track, 0, len(tracks))
	for _, t := range tracks {
		a.localTracks = append(a.localTracks, localTrack(t))
	}
	a.localSkipped, a.localScanned = skipped, time.Now()
}

// localShown returns the songs the page lists: the library's, or those of
// them the filter keeps.
func (a *app) localShown() []netease.Track {
	q := strings.ToLower(strings.TrimSpace(a.localFilter))
	if q == "" {
		return a.localTracks
	}
	out := make([]netease.Track, 0, 16)
	for _, tr := range a.localTracks {
		if strings.Contains(strings.ToLower(tr.Name), q) ||
			strings.Contains(strings.ToLower(tr.ArtistNames()), q) ||
			strings.Contains(strings.ToLower(tr.Album.Name), q) {
			out = append(out, tr)
		}
	}
	return out
}

// chooseLocalDir asks for the folder of the local library, and scans it.
func (a *app) chooseLocalDir() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:             "选择音乐文件夹",
			DefaultPath:       a.localDir(),
			ButtonLabel:       "选择",
			Directory:         true,
			CreateDirectories: false,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		dir := paths[0]
		a.post(func() {
			a.settings.LocalDir = dir
			a.changed()
			a.scanLocal()
		})
	}()
}

func (a *app) localPage(c *ui.Context) {
	tracks := a.localShown()
	a.trackRows(c, tracks, true, a.localHeader)
}

// localHeader names the page, the folder it reads and how many songs are in
// it, with what can be done with them all and a field to find one.
func (a *app) localHeader(c *ui.Context) {
	p := a.pal
	sub := "还没有扫描"
	switch {
	case a.localLoading:
		sub = "正在扫描…"
	case len(a.localTracks) > 0:
		sub = fmt.Sprintf("%d 首", len(a.localTracks))
		if a.localSkipped > 0 {
			sub += fmt.Sprintf(" · %d 首暂不支持", a.localSkipped)
		}
	}
	a.heading(c, "本地音乐", sub)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(0, 0, 10, 0).Children(func() {
		if pillButton(c.Key("local-dir"), iconFolder, "选择文件夹", p.hover.Over(p.bg), p.text).Clicked() {
			a.chooseLocalDir()
		}
		if len(a.localTracks) > 0 {
			if pillButton(c.Key("local-play"), iconPlay, "播放全部", p.accent, p.onAccent).Clicked() {
				a.pl.PlayQueue(a.localShown(), 0)
			}
			if pillButton(c.Key("local-rescan"), iconReload, "重新扫描", p.hover.Over(p.bg), p.text).Clicked() {
				a.scanLocal()
			}
		}
		ui.Box(c).Grow(1)
		ui.Text(c, a.localDir()).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0).TextAlign(ui.End)
	})
	if len(a.localTracks) > 0 {
		ui.TextInput(c.Key("local-filter"), &a.localFilter).
			Placeholder("筛选歌名、歌手或专辑").FontSize(13).Width(260).Margin(0, 0, 8, 0)
	}
	if len(a.localTracks) == 0 || len(a.localShown()) == 0 {
		a.localEmpty(c, len(a.localTracks) > 0)
	}
}

// localEmpty says what the page is for before it has songs, or why the
// filter keeps them all.
func (a *app) localEmpty(c *ui.Context, filtered bool) {
	p := a.pal
	ui.Column(c).Padding(0, 0, 60, 0).MinHeight(220).Center().Gap(10).Children(func() {
		switch {
		case a.localLoading:
			ui.Icon(c, iconReload).Size(32, 32).TextColor(p.faint)
			ui.Text(c, "正在扫描文件夹…").FontSize(14.5).FontWeight(600)
			ui.Text(c, a.localDir()).FontSize(12.5).TextColor(p.muted).SingleLine().MaxWidth(420)
		case a.localErr != "":
			ui.Icon(c, iconFolder).Size(32, 32).TextColor(p.faint)
			ui.Text(c, a.localErr).FontSize(14.5).FontWeight(600)
			if pillButton(c.Key("local-retry"), iconReload, "重试", p.hover.Over(p.bg), p.text).Clicked() {
				a.scanLocal()
			}
		case filtered:
			ui.Icon(c, iconSearch).Size(32, 32).TextColor(p.faint)
			ui.Text(c, "没有符合的歌").FontSize(14.5).FontWeight(600)
			if pillButton(c.Key("local-clear"), iconX, "清除筛选", p.hover.Over(p.bg), p.text).Clicked() {
				a.localFilter = ""
			}
		default:
			ui.Icon(c, iconNote).Size(34, 34).TextColor(p.faint)
			ui.Text(c, "这里还没有歌").FontSize(14.5).FontWeight(600)
			ui.Text(c, "把歌曲放进这个文件夹，或者选择一个装着音乐的文件夹").FontSize(12.5).TextColor(p.muted)
			if pillButton(c.Key("local-pick"), iconFolder, "选择文件夹", p.hover.Over(p.bg), p.text).Clicked() {
				a.chooseLocalDir()
			}
		}
	})
}
