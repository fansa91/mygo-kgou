package app

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/localfolder"
	"netease-music-mygo/internal/netease"
)

// The local library: the songs in the listener's own folders — the ones the
// app downloaded, beside the ones it never did — played from their files,
// with the tags, the pictures and the words the files carry.

// What the local page lists: every song in a row, the artists they are by, or
// the albums they are on.
const (
	localTabSongs = iota
	localTabArtists
	localTabAlbums
)

// The cards of the page's artists and albums: how wide one may be, and the
// room between them.
const (
	localArtistWidth = 140
	localAlbumWidth  = 156
	localGap         = 20
)

// What a group of songs picked from the page's other views is: one artist's
// songs, or one album's.
const (
	localPickNone = iota
	localPickArtist
	localPickAlbum
)

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
		Format:   t.Format,
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
	// What the file's own tags said, which tells ALAC from the AAC of the
	// same container.
	if tr.Format != "" {
		return tr.Format
	}
	switch strings.ToLower(filepath.Ext(tr.Path)) {
	case ".mp3":
		return local.FormatMP3
	case ".flac":
		return local.FormatFLAC
	case ".ogg", ".oga":
		return local.FormatOgg
	case ".opus":
		return local.FormatOpus
	case ".m4a", ".mp4", ".m4b":
		return local.FormatM4A
	case ".wav":
		return local.FormatWAV
	}
	return ""
}

// localDir is where the local library is: the folder chosen, or the one the
// downloads go to, which is where the app's own songs are. A phone whose
// folder the system took back has none until the listener picks it again.
func (a *app) localDir() string {
	if a.localLost {
		return ""
	}
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
	if dir == "" {
		// A phone's folder is gone until the listener picks it again.
		a.localTracks, a.localSkipped, a.localErr = nil, 0, ""
		return
	}
	a.localLoading, a.localErr = true, ""
	go func() {
		tracks, skipped, err := a.readLocal(dir)
		a.post(func() {
			a.localLoading = false
			if err != nil {
				a.localErr = "无法读取这个文件夹，请重新选择"
				return
			}
			a.setLocal(tracks, skipped)
			a.localScannedDir = dir
		})
	}()
}

// readLocal reads the songs of a folder, off the UI thread. Android 11 and
// later hide shared folders from a Go directory walk: there the native plugin
// lists the document tree the listener picked, and Go reads the files it
// names where they are. The other platforms, and the app's own folder, are
// walked as always.
func (a *app) readLocal(dir string) ([]local.Track, int, error) {
	if scansDocumentTree && a.settings.LocalPicked {
		paths, err := listLocalFolder(context.Background())
		if err != nil {
			return nil, 0, err
		}
		return local.ScanFiles(paths)
	}
	return local.Scan(dir)
}

// setLocal makes tracks the local library, as a scan of it found them.
func (a *app) setLocal(tracks []local.Track, skipped int) {
	a.localTracks = make([]netease.Track, 0, len(tracks))
	for _, t := range tracks {
		a.localTracks = append(a.localTracks, localTrack(t))
	}
	a.localSkipped, a.localScanned = skipped, time.Now()
	a.localArtistN, a.localAlbumN = localCounts(a.localTracks)
}

// localCounts returns how many artists and albums a library of songs holds.
func localCounts(tracks []netease.Track) (artists, albums int) {
	who, what := map[string]bool{}, map[string]bool{}
	for _, tr := range tracks {
		who[strings.ToLower(localArtistName(tr))] = true
		what[strings.ToLower(localAlbumName(tr))+"\x1f"+strings.ToLower(tr.ArtistNames())] = true
	}
	return len(who), len(what)
}

// localShown returns the songs the page lists: the library's, or those of
// them the filter keeps, and of a group picked, if one is.
func (a *app) localShown() []netease.Track {
	q := strings.ToLower(strings.TrimSpace(a.localFilter))
	kind, name, artist, picked := a.localPicked()
	if q == "" && !picked {
		return a.localTracks
	}
	out := make([]netease.Track, 0, 16)
	for _, tr := range a.localTracks {
		if picked && !localInGroup(tr, kind, name, artist) {
			continue
		}
		if q != "" && !localMatches(tr, q) {
			continue
		}
		out = append(out, tr)
	}
	return out
}

// localMatches reports whether a song answers a filter, as its name, its
// artist or its album name.
func localMatches(tr netease.Track, q string) bool {
	return strings.Contains(strings.ToLower(tr.Name), q) ||
		strings.Contains(strings.ToLower(tr.ArtistNames()), q) ||
		strings.Contains(strings.ToLower(tr.Album.Name), q)
}

// localPicked returns the group the page was sent to: an artist's songs, an
// album's, or none.
func (a *app) localPicked() (kind int, name, artist string, ok bool) {
	ref := a.localPickRef
	switch {
	case strings.HasPrefix(ref, "artist:"):
		return localPickArtist, strings.TrimPrefix(ref, "artist:"), "", true
	case strings.HasPrefix(ref, "album:"):
		album, who, _ := strings.Cut(strings.TrimPrefix(ref, "album:"), "\x1f")
		return localPickAlbum, album, who, true
	}
	return localPickNone, "", "", false
}

// localPick sends the page to a group of songs: an artist's, or an album's.
func (a *app) localPick(ref string) {
	a.localPickRef = ref
	a.localResetList = true
}

// localInGroup reports whether a song is of the group picked.
func localInGroup(tr netease.Track, kind int, name, artist string) bool {
	if kind == localPickAlbum {
		return strings.EqualFold(localAlbumName(tr), name) && strings.EqualFold(tr.ArtistNames(), artist)
	}
	return strings.EqualFold(localArtistName(tr), name)
}

// localArtistName is the artist a song is filed under, and localAlbumName the
// album it is on: the library's own names for the files that say nothing.
func localArtistName(tr netease.Track) string {
	if n := strings.TrimSpace(tr.ArtistNames()); n != "" {
		return n
	}
	return "未知歌手"
}

func localAlbumName(tr netease.Track) string {
	if n := strings.TrimSpace(tr.Album.Name); n != "" {
		return n
	}
	return "未知专辑"
}

// localGroup is a set of the library's songs: one artist's, or one album's.
type localGroup struct {
	name   string // the artist's, or the album's
	artist string // the artist of an album
	year   string
	cover  string // a local cover of one of its songs, "" for none
	count  int    // how many of the library's songs it holds
	lower  string // its name and artist, which it is grouped and sorted by
}

// localGroupList gathers the songs the page lists into the artists they are
// by, or the albums they are on, in the order a library keeps them: by name.
//
// A library of thousands is not gathered anew for each frame: the groups are
// kept until what they were gathered from changes, which the key says.
func (a *app) localGroupList(kind int) []localGroup {
	key := strings.Join([]string{
		strconv.Itoa(kind), strings.ToLower(a.localFilter), a.localPickRef,
		strconv.Itoa(len(a.localTracks)), strconv.FormatInt(a.localScanned.UnixNano(), 10),
	}, "\x1f")
	if key == a.localGroupKey {
		return a.localGroupCache
	}
	a.localGroupKey, a.localGroupCache = key, a.localGroupsOf(kind)
	return a.localGroupCache
}

// localGroupsOf works out the groups of the songs the page lists.
func (a *app) localGroupsOf(kind int) []localGroup {
	shown := a.localShown()
	out := make([]localGroup, 0, len(shown))
	index := make(map[string]int, len(shown))
	for _, tr := range shown {
		name, artist := localArtistName(tr), ""
		if kind == localTabAlbums {
			name, artist = localAlbumName(tr), tr.ArtistNames()
		}
		lower := strings.ToLower(name) + "\x1f" + strings.ToLower(artist)
		at, seen := index[lower]
		if !seen {
			at = len(out)
			index[lower] = at
			out = append(out, localGroup{name: name, artist: artist, year: tr.Year, lower: lower})
		}
		g := &out[at]
		g.count++
		if g.cover == "" {
			g.cover = tr.Album.PicURL
		}
		if g.year == "" {
			g.year = tr.Year
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].lower < out[j].lower })
	return out
}

// openLocalDialog picks the files or folder of the local library; tests
// replace it.
var openLocalDialog = mygo.Dialog.Open

// The folder a phone reads, which lives in the plugin's native code: tests
// replace them.
var (
	pickLocalFolder    = localfolder.Pick
	restoreLocalFolder = localfolder.Restore
	forgetLocalFolder  = localfolder.Forget
	listLocalFolder    = localfolder.List
	// scansDocumentTree says the folder is read through the system's document
	// tree rather than by walking its path, as on Android. Tests set it.
	scansDocumentTree = runtime.GOOS == "android"
	// folderInPlace reports whether a phone reads a folder where it is, its
	// subfolders and all, rather than copying the songs into the app.
	folderInPlace = localfolder.Available
)

// chooseLocalDir asks for the folder of the local library, and scans it. A
// phone reads a folder of its own storage where it is: the songs stay where
// the listener keeps them, and the folders under it come with them.
func (a *app) chooseLocalDir() {
	if a.phone {
		if folderInPlace() {
			a.pickLocalFolder()
			return
		}
		a.addLocalSongs()
		return
	}
	go func() {
		paths, err := openLocalDialog(mygo.OpenDialogOptions{
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
		a.post(func() { a.readLocalFolder(dir) })
	}()
}

// pickLocalFolder has the listener choose a folder of the phone, which the
// library then reads where it is: nothing is copied, and the folders under it
// are read too.
func (a *app) pickLocalFolder() {
	go func() {
		path, err := pickLocalFolder(context.Background())
		if err != nil {
			a.post(func() { a.toast = "无法读取这个文件夹，请换一个" })
			return
		}
		if path == "" {
			return // no folder picked
		}
		a.post(func() { a.readLocalFolder(path) })
	}()
}

// readLocalFolder makes path the folder of the local library, and reads it.
func (a *app) readLocalFolder(path string) {
	a.settings.LocalDir = path
	a.settings.LocalPicked = a.phone
	a.localLost = false
	a.changed()
	a.scanLocal()
}

// reopenLocalFolder takes back the permission to the folder a phone read in an
// earlier run, which the system keeps only for a while: without it the songs
// are there, but unreadable.
func (a *app) reopenLocalFolder() {
	if !a.phone || !folderInPlace() || !a.settings.LocalPicked {
		return
	}
	go func() {
		got, err := restoreLocalFolder(context.Background())
		if err != nil {
			return
		}
		a.post(func() { a.reopenedLocalFolder(got) })
	}()
}

// reopenedLocalFolder takes a phone's library from what the folder picked
// before is now.
func (a *app) reopenedLocalFolder(got localfolder.Restored) {
	next := localReopened(got)
	if next.forget {
		go forgetLocalFolder(context.Background())
	}
	switch {
	case next.path != "":
		a.settings.LocalDir = next.path
		a.localLost = false
		a.changed()
		a.scanLocal()
	case next.lost:
		// The folder is gone, or the system took the permission to it back:
		// the page says so, and offers picking it again.
		a.settings.LocalDir, a.settings.LocalPicked = "", false
		a.localLost = true
		a.changed()
	}
}

// localReopened is what a phone's library becomes of the folder the app took
// back: the path to read, whether the app drops what it kept of the folder,
// and whether the page has to ask for one.
type localReopen struct {
	path   string // the folder to read, "" for none
	forget bool   // the folder is gone: drop what the app kept of it
	lost   bool   // the page asks for a folder again
}

func localReopened(got localfolder.Restored) localReopen {
	switch {
	case got.Path != "":
		return localReopen{path: got.Path}
	case got.Saved:
		return localReopen{forget: true, lost: true}
	}
	return localReopen{}
}

// localMusicDir is where songs added on a phone are kept: the app's own
// folder, which outlives the copies the system's picker hands over.
func localMusicDir() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Music"), nil
}

// localMusicDirPath is localMusicDir; tests replace it.
var localMusicDirPath = localMusicDir

// addLocalSongs picks songs on a phone and copies them into the app's
// folder, which the local library then reads. A phone gives an app no folder
// of another app's: the copies a picker hands over are temporary, and
// Android's folder picker answers with a tree the dialog cannot read, so
// with a folder chosen nothing happens. Songs copied here stay.
func (a *app) addLocalSongs() {
	go func() {
		paths, err := openLocalDialog(mygo.OpenDialogOptions{
			Title:       "添加歌曲",
			ButtonLabel: "添加",
			Multiple:    true,
			Filters: []mygo.FileFilter{{
				Name:       "音频",
				Extensions: []string{"mp3", "flac", "ogg", "oga", "opus", "m4a", "mp4", "m4b", "wav"},
			}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		dir, err := localMusicDirPath()
		if err != nil {
			a.post(func() { a.toast = "添加失败，请稍后再试" })
			return
		}
		n, err := copyLocalSongs(paths, dir)
		a.post(func() { a.localSongsAdded(dir, n, err) })
	}()
}

// localSongsAdded makes the folder the songs were copied into the local
// library's, and says how many came.
func (a *app) localSongsAdded(dir string, n int, err error) {
	if err != nil || n == 0 {
		a.toast = "添加失败，请稍后再试"
		return
	}
	a.settings.LocalDir = dir
	a.settings.LocalPicked = false // the app's own folder, which needs no permission
	a.localLost = false
	a.changed()
	a.scanLocal()
	a.toast = fmt.Sprintf("已添加 %d 首", n)
}

// copyLocalSongs copies the songs picked into dir, each under its own name.
// A song copied before is left as it is; two songs that share a name are
// kept apart by a number. Songs that cannot be copied are left out, and the
// rest still come.
func copyLocalSongs(paths []string, dir string) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	n := 0
	for _, src := range paths {
		if copyLocalSongTo(src, dir) == nil {
			n++
		}
	}
	return n, nil
}

// copyLocalSongTo copies src into dir under its own name. A copy the same
// size is taken for the same song, so picking it again adds nothing; a name
// already taken by a song of another size gets a number.
func copyLocalSongTo(src, dir string) error {
	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	name := filepath.Base(src)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; i <= 99; i++ {
		file := name
		if i > 1 {
			file = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		dst := filepath.Join(dir, file)
		di, err := os.Stat(dst)
		if err != nil {
			return copyLocalSong(src, dst)
		}
		if os.SameFile(si, di) || di.Size() == si.Size() {
			return nil
		}
	}
	return fmt.Errorf("没有空余的文件名给 %s", name)
}

// copyLocalSong writes a copy of src at dst: through a file beside it, so a
// copy cut short leaves no half song in the library.
func copyLocalSong(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func (a *app) localPage(c *ui.Context) {
	if a.localResetList {
		// The view changed as the list built, so where it was scrolled is
		// forgotten now, before it lists other songs.
		a.localResetList = false
		a.trackList, a.pageScroll = ui.ListState{}, ui.ScrollState{}
	}
	if kind, name, artist, ok := a.localPicked(); ok {
		a.localGroupPage(c, kind, name, artist)
		return
	}
	switch a.localTab {
	case localTabArtists, localTabAlbums:
		a.localGroupsPage(c, a.localTab)
	default:
		songs := a.localShown()
		a.trackRows(c, songs, true, func(c *ui.Context) {
			a.localHeader(c)
			if len(songs) == 0 {
				a.localEmpty(c, len(a.localTracks) > 0)
			}
		})
	}
}

// localGroupPage lists the songs of the group the page was sent to.
func (a *app) localGroupPage(c *ui.Context, kind int, name, artist string) {
	songs := a.localShown()
	a.trackRows(c, songs, true, func(c *ui.Context) {
		a.localGroupHeader(c, kind, name, artist, songs)
		if len(songs) == 0 {
			a.empty(c, "这里没有歌")
		}
	})
}

// localGroupHeader names the group, with what can be done with its songs and
// the way back to the library.
func (a *app) localGroupHeader(c *ui.Context, kind int, name, artist string, songs []netease.Track) {
	p := a.pal
	sub := "歌手"
	if kind == localPickAlbum {
		sub = "专辑"
		if artist != "" {
			sub += " · " + artist
		}
		for _, tr := range songs {
			if tr.Year != "" {
				sub += " · " + tr.Year
				break
			}
		}
	}
	sub += fmt.Sprintf(" · %d 首", len(songs))
	cover := ""
	for _, tr := range songs {
		if tr.Album.PicURL != "" {
			cover = tr.Album.PicURL
			break
		}
	}
	radius := float32(999)
	if kind == localPickAlbum {
		radius = 14
	}
	ui.Row(c).Gap(24).AlignItems(ui.End).Padding(0, 0, 24, 0).Children(func() {
		ui.Box(c).Size(148, 148).Radius(radius).Shadow(0, 14, 32, -10, p.cardShade).Children(func() {
			a.coverFill(c, cover, 400, radius)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, sub).FontSize(12).FontWeight(600).TextColor(p.accent)
			ui.Text(c, name).FontSize(28).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
			ui.Row(c).Gap(10).Margin(4, 0, 0, 0).Children(func() {
				if pillButton(c.Key("play"), iconPlay, "播放全部", p.accent, p.onAccent).Clicked() {
					a.pl.PlayQueue(songs, firstPlayable(songs))
				}
				if pillButton(c.Key("back"), iconBack, "返回", p.hover.Over(p.bg), p.text).Clicked() {
					a.localPick("")
				}
			})
		})
	})
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
		if a.localArtistN > 0 {
			sub += fmt.Sprintf(" · %d 位歌手 · %d 张专辑", a.localArtistN, a.localAlbumN)
		}
		if a.localSkipped > 0 {
			sub += fmt.Sprintf(" · %d 首暂不支持", a.localSkipped)
		}
	}
	a.heading(c, "本地音乐", sub)
	pill := pillButton
	if a.phone {
		// Three full-size pills do not fit a phone's content width.
		pill = func(c *ui.Context, icon *ui.SVG, label string, bg, fg ui.Color) ui.Element {
			return pillButtonSized(c, icon, label, bg, fg, true)
		}
	}
	pickLabel := "选择文件夹"
	addSongs := a.phone && folderInPlace()
	if a.phone && !addSongs {
		// A phone without the plugin can only be handed songs, which the app
		// copies into its own folder.
		pickLabel = "添加歌曲"
	}
	if a.localLost {
		pickLabel = "重新选择"
	}
	buttons := func() {
		if pill(c.Key("local-dir"), iconFolder, pickLabel, p.hover.Over(p.bg), p.text).Clicked() {
			a.chooseLocalDir()
		}
		if addSongs {
			// Reading a folder where it is does not stop adding a song from
			// elsewhere, which the app keeps a copy of: an icon, as the row
			// has no room for another name.
			if iconButton(c.Key("local-add"), iconPlus, 18, p.text, "添加歌曲").Clicked() {
				a.addLocalSongs()
			}
		}
		if len(a.localTracks) > 0 {
			if pill(c.Key("local-play"), iconPlay, "播放全部", p.accent, p.onAccent).Clicked() {
				songs := a.localShown()
				a.pl.PlayQueue(songs, firstPlayable(songs))
			}
			if a.phone {
				if iconButton(c.Key("local-rescan"), iconReload, 18, p.text, "重新扫描").Clicked() {
					a.scanLocal()
				}
			} else if pill(c.Key("local-rescan"), iconReload, "重新扫描", p.hover.Over(p.bg), p.text).Clicked() {
				a.scanLocal()
			}
		}
	}
	path := func(align ui.Align) {
		ui.Text(c, a.localDir()).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0).TextAlign(align)
	}
	// A phone's buttons fill its row: the folder's path would run off the
	// edge, and an empty library offers the same actions below, so the
	// header would only say it twice.
	if a.phone {
		if len(a.localTracks) > 0 {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(0, 0, 10, 0).Children(buttons)
		}
	} else {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(0, 0, 10, 0).Children(func() {
			buttons()
			ui.Box(c).Grow(1)
			path(ui.End)
		})
	}
	filter := func(w float32) {
		in := ui.TextInput(c.Key("local-filter"), &a.localFilter).
			Placeholder("筛选歌名、歌手或专辑").FontSize(13)
		if w > 0 {
			in.Width(w)
		} else {
			in.FillWidth()
		}
	}
	if a.phone {
		// A 260px field beside the three tabs runs off a phone's screen.
		// Let the tabs have their row, and give the field one of its own.
		ui.Column(c).Gap(10).AlignItems(ui.Stretch).Margin(0, 0, 8, 0).Children(func() {
			if segmented(c.Key("local-tab"), &a.localTab, "歌曲", "歌手", "专辑") {
				a.localResetList = true
			}
			if len(a.localTracks) > 0 {
				filter(0)
			}
		})
	} else {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Margin(0, 0, 8, 0).Children(func() {
			if segmented(c.Key("local-tab"), &a.localTab, "歌曲", "歌手", "专辑") {
				a.localResetList = true
			}
			if len(a.localTracks) > 0 {
				ui.Box(c).Grow(1)
				filter(260)
			}
		})
	}
}

// localGroupsPage lists the artists of the library, or the albums its songs
// are on, a row of cards at a time: a library of many builds only the rows
// the page shows, as its songs do.
func (a *app) localGroupsPage(c *ui.Context, kind int) {
	groups := a.localGroupList(kind)
	width := float32(localArtistWidth)
	if kind == localTabAlbums {
		width = localAlbumWidth
	}
	top, bottom := a.insets(c)
	cols := a.columns(c, width)
	rows := 0
	if len(groups) > 0 {
		rows = (len(groups) + cols - 1) / cols
	}
	a.underTop, a.underBottom = true, true
	ui.List(c.Key(a.route), &a.trackList, rows+1, func(i int) {
		if i == 0 {
			ui.Column(c).Children(func() {
				a.startMark(c)
				a.localHeader(c)
				if len(groups) == 0 {
					a.localEmpty(c, len(a.localTracks) > 0)
				}
			})
			return
		}
		if i == rows {
			defer a.endMark(c)
		}
		ui.Row(c).Gap(localGap).AlignItems(ui.Start).Children(func() {
			for k := 0; k < cols; k++ {
				at := (i-1)*cols + k
				if at >= len(groups) {
					// A row short of cards keeps the width of the others.
					ui.Box(c).Grow(1)
					continue
				}
				g := groups[at]
				if kind == localTabAlbums {
					a.localAlbumCard(c.Key(g.name+"\x1f"+g.artist), g)
				} else {
					a.localArtistCard(c.Key(g.name), g)
				}
			}
		})
	}).Grow(1).Padding(top+pageGap, pagePad-12, bottom+32, pagePad-12).ScrollbarInsets(top, 0, bottom, 0)
}

// localArtistCard is an artist's picture of one of their songs, round, over
// their name and how many of their songs the library holds.
func (a *app) localArtistCard(c *ui.Context, g localGroup) {
	p := a.pal
	ref := g
	card := ui.ButtonBase(c).Column().Grow(1).Gap(10).AlignItems(ui.Stretch).FocusRing(false).Label(g.name)
	hovered := card.Hovered()
	card.Children(func() {
		art := ui.Box(c).AspectRatio(1).Radius(999)
		if hovered {
			art.Shadow(0, 12, 28, -6, p.cardShade)
		} else {
			art.Shadow(0, 2, 8, -2, p.cardShade)
		}
		art.Children(func() { a.coverFill(c, g.cover, 400, 999) })
		ui.Text(c, g.name).FontSize(13).FontWeight(500).SingleLine().AlignSelf(ui.Center)
		ui.Text(c, fmt.Sprintf("%d 首", g.count)).FontSize(12).TextColor(p.muted).
			SingleLine().AlignSelf(ui.Center).Margin(-6, 0, 0, 0)
	})
	if card.Clicked() {
		a.localPick("artist:" + ref.name)
	}
}

// localAlbumCard is an album's cover over its name, its artist and the year
// it came out.
func (a *app) localAlbumCard(c *ui.Context, g localGroup) {
	p := a.pal
	card := ui.ButtonBase(c).Column().Grow(1).Gap(10).AlignItems(ui.Stretch).FocusRing(false).Label(g.name)
	hovered := card.Hovered()
	card.Children(func() {
		art := ui.Box(c).AspectRatio(1).Radius(10)
		if hovered {
			art.Shadow(0, 12, 28, -6, p.cardShade)
		} else {
			art.Shadow(0, 2, 8, -2, p.cardShade)
		}
		art.Children(func() { a.coverFill(c, g.cover, 400, 10) })
		ui.Text(c, g.name).FontSize(13).FontWeight(500).MaxLines(2).LineHeight(1.35)
		meta := g.artist
		if g.year != "" {
			if meta != "" {
				meta += " · "
			}
			meta += g.year
		}
		ui.Text(c, meta).FontSize(12).TextColor(p.muted).SingleLine().Margin(-6, 0, 0, 0)
	})
	if card.Clicked() {
		a.localPick("album:" + g.name + "\x1f" + g.artist)
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
		case a.localLost:
			ui.Icon(c, iconFolder).Size(32, 32).TextColor(p.faint)
			ui.Text(c, "文件夹需要重新选择").FontSize(14.5).FontWeight(600)
			ui.Text(c, "系统收回了这个文件夹的访问权限").FontSize(12.5).TextColor(p.muted)
			if pillButton(c.Key("local-repick"), iconFolder, "重新选择", p.hover.Over(p.bg), p.text).Clicked() {
				a.chooseLocalDir()
			}
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
			hint, pick := "把歌曲放进这个文件夹，或者选择一个装着音乐的文件夹", "选择文件夹"
			switch {
			case a.localLost:
				hint, pick = "系统收回了这个文件夹的访问权限，请重新选择一次", "重新选择"
			case a.phone && folderInPlace():
				hint = "选一个装着音乐的文件夹，子文件夹也会一起读进来"
			case a.phone:
				// A phone without the plugin can only be handed songs, which
				// the app copies into its own folder.
				hint, pick = "点「添加歌曲」，把手机里的歌加进来", "添加歌曲"
			}
			ui.Icon(c, iconNote).Size(34, 34).TextColor(p.faint)
			ui.Text(c, "这里还没有歌").FontSize(14.5).FontWeight(600)
			ui.Text(c, hint).FontSize(12.5).TextColor(p.muted).MaxWidth(330).TextAlign(ui.Center)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if pillButton(c.Key("local-pick"), iconFolder, pick, p.hover.Over(p.bg), p.text).Clicked() {
					a.chooseLocalDir()
				}
				if a.phone && folderInPlace() {
					if pillButton(c.Key("local-pick-songs"), iconPlus, "添加歌曲", p.hover.Over(p.bg), p.text).Clicked() {
						a.addLocalSongs()
					}
				}
			})
		}
	})
}
