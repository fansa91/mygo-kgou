package app

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// An album's page shows its songs once they load, and the singer's name in
// its header opens their page: the album names them by number too, not by
// name alone.
func TestAlbumPageShowsItsSongs(t *testing.T) {
	asked := make(chan int64, 4)
	albumDetail = func(id int64) (*netease.AlbumProfile, error) {
		asked <- id
		return &netease.AlbumProfile{
			Album:   netease.Album{ID: 7, Name: "演示专辑"},
			Artists: []netease.Artist{{ID: 12, Name: "陈奕迅"}},
			Songs: []netease.Track{
				{ID: 1, Name: "富士山下", Artists: []netease.Artist{{ID: 12, Name: "陈奕迅"}}, Duration: 200000},
				{ID: 2, Name: "浮夸", Artists: []netease.Artist{{ID: 12, Name: "陈奕迅"}}, Duration: 200000},
			},
		}, nil
	}
	defer func() { albumDetail = netease.AlbumDetail }()
	a := newTestApp()
	a.settings = defaultSettings()
	a.open(route{page: pageAlbum, id: 7})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	select {
	case id := <-asked:
		if id != 7 {
			t.Fatalf("asked for the album %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("the album wasn't asked for")
	}
	// As the answer lands, which post drops without a window.
	a.albums[7].album = &netease.AlbumProfile{
		Album:   netease.Album{ID: 7, Name: "演示专辑"},
		Artists: []netease.Artist{{ID: 12, Name: "陈奕迅"}},
		Songs: []netease.Track{
			{ID: 1, Name: "富士山下", Artists: []netease.Artist{{ID: 12, Name: "陈奕迅"}}, Duration: 200000},
			{ID: 2, Name: "浮夸", Artists: []netease.Artist{{ID: 12, Name: "陈奕迅"}}, Duration: 200000},
		},
	}
	tt.Frame()
	for _, want := range []string{"演示专辑", "富士山下", "浮夸"} {
		if !tt.HasText(want) {
			t.Errorf("the album page does not show %q", want)
		}
	}

	// The singer's name opens their page.
	if err := tt.Click("陈奕迅"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route != (route{page: pageArtist, id: 12}) {
		t.Errorf("the singer's name opened %+v", a.route)
	}
}

// An album that fails to load says so, and a retry asks again.
func TestAlbumPageAsksAgainWhenItFails(t *testing.T) {
	asked := make(chan int64, 4)
	albumDetail = func(id int64) (*netease.AlbumProfile, error) {
		asked <- id
		return nil, errors.New("offline")
	}
	defer func() { albumDetail = netease.AlbumDetail }()
	a := newTestApp()
	a.settings = defaultSettings()
	a.open(route{page: pageAlbum, id: 9})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	select {
	case <-asked:
	case <-time.After(time.Second):
		t.Fatal("the album wasn't asked for")
	}
	// As the refusal lands, which post drops without a window.
	a.albums[9].loading, a.albums[9].err = false, "专辑加载失败"
	tt.Frame()
	if !tt.HasText("专辑加载失败") {
		t.Error("the page does not say the album failed to load")
	}
	if err := tt.Click("重试"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	select {
	case <-asked:
	case <-time.After(time.Second):
		t.Error("the retry did not ask again")
	}
}
