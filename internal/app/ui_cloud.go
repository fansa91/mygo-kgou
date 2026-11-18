package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/netease"
)

// The music cloud: the files the listener uploaded themselves, which KuGou
// keeps for the account and streams back like any other song. What is put
// there from here is in the phone's 我的 - 云盘, and the other way round.
//
// The page lists them, plays them, takes them out and puts files in: the
// same three the cloud service offers.

// The calls the page makes; tests replace them.
var (
	cloudSongs  = netease.CloudSongs
	cloudUpload = netease.CloudUploadFile
	cloudDelete = netease.CloudDelete
)

// cloudPageSize is how many songs are asked for at a time. The service
// answers pages, and the page shows them all: a cloud of a few hundred
// files is a handful of calls.
const cloudPageSize = 100

// cloudPageLimit is the most songs the page reads before it says so.
const cloudPageLimit = 1000

// loadCloud shows the cloud's songs, reading them the first time the page
// shows or once they were emptied.
func (a *app) loadCloud() {
	if a.cloudLoading || len(a.cloudTracks) > 0 {
		return
	}
	a.refreshCloud()
}

// refreshCloud reads the cloud again, all of it, and replaces what the page
// shows.
func (a *app) refreshCloud() {
	if a.cloudLoading {
		return
	}
	if a.profile == nil {
		a.cloudTracks, a.cloudTotal, a.cloudErr = nil, 0, ""
		return
	}
	a.cloudLoading, a.cloudErr = true, ""
	go func() {
		tracks, total, err := fetchCloud()
		a.post(func() { a.setCloud(tracks, total, err) })
	}()
}

// fetchCloud reads the whole cloud, a page at a time.
func fetchCloud() ([]netease.Track, int, error) {
	var all []netease.Track
	total := 0
	for page := 1; ; page++ {
		songs, n, err := cloudSongs(page, cloudPageSize)
		if err != nil {
			return nil, 0, err
		}
		total = n
		all = append(all, songs...)
		if len(songs) < cloudPageSize || len(all) >= total || len(all) >= cloudPageLimit {
			break
		}
	}
	return all, max(total, len(all)), nil
}

// setCloud makes tracks the cloud's songs, as reading it found them.
func (a *app) setCloud(tracks []netease.Track, total int, err error) {
	a.cloudLoading = false
	if err != nil {
		a.cloudErr = "无法读取音乐云盘"
		return
	}
	a.cloudTracks, a.cloudTotal, a.cloudErr = tracks, total, ""
}

// cloudShown returns the songs the page lists: the cloud's, or those of them
// the filter keeps.
func (a *app) cloudShown() []netease.Track {
	q := strings.ToLower(strings.TrimSpace(a.cloudFilter))
	if q == "" {
		return a.cloudTracks
	}
	out := make([]netease.Track, 0, 16)
	for _, tr := range a.cloudTracks {
		if strings.Contains(strings.ToLower(tr.Name), q) ||
			strings.Contains(strings.ToLower(tr.ArtistNames()), q) ||
			strings.Contains(strings.ToLower(tr.CloudName), q) {
			out = append(out, tr)
		}
	}
	return out
}

func (a *app) cloudPage(c *ui.Context) {
	a.trackRows(c, a.cloudShown(), true, a.cloudHeader)
}

// cloudHeader names the page, how much is in the cloud and how big it is,
// with what can be done with it all and a field to find one.
func (a *app) cloudHeader(c *ui.Context) {
	p := a.pal
	sub := "还没有读取"
	switch {
	case a.profile == nil:
		sub = "登录后可用"
	case a.cloudLoading:
		sub = "正在读取…"
	case a.cloudErr != "":
		sub = a.cloudErr
	case len(a.cloudTracks) > 0:
		sub = fmt.Sprintf("%d 首 · %s", len(a.cloudTracks), cloudSizeText(cloudSize(a.cloudTracks)))
		if a.cloudTotal > len(a.cloudTracks) {
			// A cloud longer than the page reads says how much of it shows.
			sub = fmt.Sprintf("已读 %d/%d 首 · %s", len(a.cloudTracks), a.cloudTotal, cloudSizeText(cloudSize(a.cloudTracks)))
		}
	}
	a.heading(c, "音乐云盘", sub)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(0, 0, 10, 0).Children(func() {
		if a.profile != nil {
			if pillButton(c.Key("cloud-upload"), iconPlus, "上传", p.accent, p.onAccent).Clicked() {
				a.uploadCloud()
			}
		}
		if len(a.cloudTracks) > 0 {
			if pillButton(c.Key("cloud-play"), iconPlay, "播放全部", p.hover.Over(p.bg), p.text).Clicked() {
				a.pl.PlayQueue(a.cloudShown(), 0)
			}
		}
		if a.profile != nil && !a.cloudLoading {
			if pillButton(c.Key("cloud-refresh"), iconReload, "刷新", p.hover.Over(p.bg), p.text).Clicked() {
				a.refreshCloud()
			}
		}
		ui.Box(c).Grow(1)
		if a.cloudBusy != "" {
			ui.Text(c, a.cloudBusy).FontSize(12).TextColor(p.muted)
		}
	})
	if len(a.cloudTracks) > 0 {
		ui.TextInput(c.Key("cloud-filter"), &a.cloudFilter).
			Placeholder("筛选歌名、歌手或文件名").FontSize(13).Width(280).Margin(0, 0, 8, 0)
	}
	if len(a.cloudShown()) == 0 {
		a.cloudEmpty(c, len(a.cloudTracks) > 0)
	}
}

// cloudEmpty says what the page is for before it has songs, or why the
// filter keeps them all.
func (a *app) cloudEmpty(c *ui.Context, filtered bool) {
	p := a.pal
	ui.Column(c).Padding(0, 0, 60, 0).MinHeight(220).Center().Gap(10).Children(func() {
		switch {
		case a.profile == nil:
			ui.Icon(c, iconCloud).Size(34, 34).TextColor(p.faint)
			ui.Text(c, "登录后可以听云盘里的歌").FontSize(14.5).FontWeight(600)
			ui.Text(c, "手机上「我的 - 云盘」里的音乐，这里都能看到").FontSize(12.5).TextColor(p.muted)
			if pillButton(c.Key("cloud-login"), iconUser, "登录酷狗", p.accent, p.onAccent).Clicked() {
				a.openLogin()
			}
		case a.cloudLoading:
			ui.Icon(c, iconReload).Size(32, 32).TextColor(p.faint)
			ui.Text(c, "正在读取音乐云盘…").FontSize(14.5).FontWeight(600)
		case a.cloudErr != "":
			ui.Icon(c, iconCloud).Size(32, 32).TextColor(p.faint)
			ui.Text(c, a.cloudErr).FontSize(14.5).FontWeight(600)
			if pillButton(c.Key("cloud-retry"), iconReload, "重试", p.hover.Over(p.bg), p.text).Clicked() {
				a.refreshCloud()
			}
		case filtered:
			ui.Icon(c, iconSearch).Size(32, 32).TextColor(p.faint)
			ui.Text(c, "没有符合的歌").FontSize(14.5).FontWeight(600)
			if pillButton(c.Key("cloud-clear"), iconX, "清除筛选", p.hover.Over(p.bg), p.text).Clicked() {
				a.cloudFilter = ""
			}
		default:
			ui.Icon(c, iconCloud).Size(34, 34).TextColor(p.faint)
			ui.Text(c, "云盘还是空的").FontSize(14.5).FontWeight(600)
			ui.Text(c, "把自己的音乐传上去，或者下载到本地后再听").FontSize(12.5).TextColor(p.muted)
			if pillButton(c.Key("cloud-first"), iconPlus, "上传歌曲", p.hover.Over(p.bg), p.text).Clicked() {
				a.uploadCloud()
			}
		}
	})
}

// uploadCloud asks for the files to put in the cloud, and puts them there
// one after another, saying which of how many it is on.
func (a *app) uploadCloud() {
	if a.profile == nil {
		a.openLogin()
		return
	}
	if a.cloudBusy != "" {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:       "上传到音乐云盘",
			ButtonLabel: "上传",
			Multiple:    true,
			Filters: []mygo.FileFilter{{
				Name:       "音频文件",
				Extensions: []string{"mp3", "flac", "ogg", "oga", "wav", "m4a", "aac"},
			}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		done, failed := 0, 0
		for i, path := range paths {
			a.post(func() {
				a.cloudBusy = fmt.Sprintf("正在上传 %d/%d", i+1, len(paths))
			})
			if _, err := cloudUpload(cloudFileInfo(path)); err != nil {
				failed++
				continue
			}
			done++
		}
		a.post(func() {
			a.cloudBusy = ""
			switch {
			case done > 0 && failed > 0:
				a.toast = fmt.Sprintf("已上传 %d 首，%d 首失败", done, failed)
			case done > 0:
				a.toast = fmt.Sprintf("已上传 %d 首到云盘", done)
			default:
				a.toast = "上传失败，请稍后再试"
			}
			a.refreshCloud()
		})
	}()
}

// cloudFileInfo reads what a file says it is, for the entry the cloud keeps:
// its tags when it has them, and its name when it does not.
func cloudFileInfo(path string) netease.CloudUpload {
	f := netease.CloudUpload{Path: path}
	if t, err := local.Read(path); err == nil {
		f.Name = t.Name
		f.Singer = t.Artist
		f.Duration = t.Length.Milliseconds()
	}
	if f.Name == "" {
		f.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return f
}

// deleteCloud takes a song out of the cloud, once the listener says so.
func (a *app) deleteCloud(tr *netease.Track) {
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Type:    mygo.MessageWarning,
			Title:   "从云盘删除",
			Message: "《" + tr.Name + "》将从音乐云盘里删除，手机和其他设备上也听不到了。",
			Buttons: []string{"取消", "删除"},
		})
		if err != nil || res.Button != 1 {
			return
		}
		err = cloudDelete([]int64{tr.ID})
		a.post(func() {
			if err != nil {
				a.toast = "删除失败，请稍后再试"
				return
			}
			a.toast = "已从云盘删除"
			a.cloudTracks = removeTrack(a.cloudTracks, tr.ID)
			a.cloudTotal = max(a.cloudTotal-1, len(a.cloudTracks))
		})
	}()
}

// removeTrack returns tracks without the one of id.
func removeTrack(tracks []netease.Track, id int64) []netease.Track {
	out := tracks[:0]
	for _, tr := range tracks {
		if tr.ID != id {
			out = append(out, tr)
		}
	}
	return out
}

// cloudItems builds the menu of a song of the cloud: what can be done with
// it here, which is copying its name, playing it, and taking it out of the
// cloud.
func (a *app) cloudItems(m *ui.Menu, tr *netease.Track) {
	if m.Item("复制歌名").Chosen() {
		copyText(tr.Name)
	}
	if m.Item("复制歌名和歌手").Chosen() {
		copyText(tr.Name + " - " + tr.ArtistNames())
	}
	m.Separator()
	if m.Item("下载").Chosen() {
		a.downloads().add([]netease.Track{*tr})
	}
	m.Separator()
	if m.Item("从云盘删除").Chosen() {
		a.deleteCloud(tr)
	}
}

// cloudSize is how many bytes the cloud's songs take, as it listed them.
func cloudSize(tracks []netease.Track) int64 {
	var n int64
	for _, tr := range tracks {
		n += tr.Size
	}
	return n
}

// cloudSizeText says a size as people read it.
func cloudSizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
