package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// A profile's round photo shares the desktop's 212px on an artist's page
// and the listener's own. On a phone that leaves under 100px for the name,
// which folds into a strip; both pages use one size to keep the name and
// its buttons readable.
func TestPhoneProfilePhotoSize(t *testing.T) {
	if got := (&app{phone: true}).profilePhotoSize(); got >= artistPhotoSize {
		t.Errorf("phone photo %v is as big as the desktop's %v", got, artistPhotoSize)
	}
	if got := (&app{}).profilePhotoSize(); got != artistPhotoSize {
		t.Errorf("desktop photo = %v, want %v", got, artistPhotoSize)
	}
}

// The listener's own page on a phone shows their name on one line, their
// followers and bio readable, and their playlists and podcasts — none of
// it folded into a narrow column by an oversized photo.
func TestPhoneUserPageReadable(t *testing.T) {
	a := ownPageApp()
	a.phone = true
	a.pageStates = map[route]pageState{}
	a.route = route{page: pageUser, id: 9}
	tt := ui.NewTester(a.view, 390, 844)
	tt.SetInsets(44, 0, 34, 0)
	tt.Frame()
	if !tt.HasText("听歌的人") || !tt.HasText("12 关注 · 24 粉丝") {
		t.Fatalf("the header is missing: %v", tt.Texts())
	}
	if !tt.HasText("创建的歌单") || !tt.HasText("订阅的听书") || !tt.HasText("关注的歌手") {
		t.Fatalf("the sections are missing: %v", tt.Texts())
	}
	// A name folded into a thin column is much taller than one on a line.
	// At 30px type, "听歌的人" on one line is under 48px tall.
	r, ok := tt.Find("听歌的人")
	if !ok {
		t.Fatalf("no name rectangle: %v", tt.Texts())
	}
	if r.H > 52 {
		t.Errorf("the name is %.0fpx tall — the photo is squeezing it", r.H)
	}
}

// The settings page's account group, signed in, fits on a phone: the
// nickname, status and sign-out button are all present and within screen.
func TestPhoneSettingsAccountFits(t *testing.T) {
	a := phoneApp(t)
	a.profile = &netease.Profile{UserID: 9, Nickname: "听歌的人"}
	a.phoneSelectTab(3)
	phoneFits(t, a, 390, 844)
	tt := ui.NewTester(a.view, 390, 2400) // tall enough to build every group
	tt.Frame()
	for _, s := range []string{"账号", "听歌的人", "已登录酷狗音乐", "退出登录", "外观"} {
		if !tt.HasText(s) {
			t.Errorf("settings has no %q: %v", s, tt.Texts())
		}
	}
	// The download-name description must not fold into a thin strip beside
	// the field: on a phone the field sits under it, leaving it wide.
	desc := "可用 {歌名}、{歌手}、{专辑}、{音质}、{时长}，用 / 分子文件夹；留空则用 {歌手} - {歌名}"
	r, ok := tt.Find(desc)
	if !ok {
		t.Fatalf("no download-name description: %v", tt.Texts())
	}
	if r.W < 200 {
		t.Errorf("download-name description is only %.0fpx wide — folded beside the field", r.W)
	}
}
