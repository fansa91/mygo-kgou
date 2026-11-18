package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The scenes KuGou's radio is built from: each a stream of songs picked for
// a place rather than for the listener, played by a radio that never ends.
// They are offered under the FM, which is the other radio the app has.

// scenes is what the page asks for; tests replace it.
var scenes = netease.Scenes

// loadScenes reads the scenes once, the first time the FM shows.
func (a *app) loadScenes() {
	if a.scenesLoading || len(a.scenes) > 0 {
		return
	}
	a.scenesLoading, a.scenesErr = true, ""
	go func() {
		list, err := scenes()
		a.post(func() {
			a.scenesLoading = false
			if err != nil {
				a.scenesErr = "场景电台暂时无法读取"
				return
			}
			a.scenes = list
		})
	}()
}

// sceneCards draws the scenes as cards of their own pictures, the one play
// saying so.
func (a *app) sceneCards(c *ui.Context) {
	p := a.pal
	if len(a.scenes) == 0 {
		switch {
		case a.scenesErr != "":
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.scenesErr).FontSize(13).TextColor(p.muted)
				if pillButton(c.Key("scenes-retry"), iconReload, "重试", p.hover.Over(p.bg), p.text).Clicked() {
					a.loadScenes()
				}
			})
		case a.scenesLoading:
			ui.Grid(c).Columns(min(4, max(2, a.columns(c, 190)))).Gap(16).Children(func() {
				for i := 0; i < 4; i++ {
					ui.Box(c.Key(i)).Height(124).Radius(14).Background(p.hover)
				}
			})
		}
		return
	}
	ui.Grid(c).Columns(min(4, max(2, a.columns(c, 190)))).Gap(16).Children(func() {
		for i := range a.scenes {
			a.sceneCard(c.Key(a.scenes[i].ID), &a.scenes[i])
		}
	})
}

// sceneCard is one scene: its picture, its name and what it holds.
func (a *app) sceneCard(c *ui.Context, s *netease.Scene) {
	on := a.pl.Radio() == sceneRadio(s.ID)
	starting := a.radioStarting == sceneRadio(s.ID)
	card := ui.ButtonBase(c).Height(124).Radius(14).FocusRing(false).Label(s.Title)
	pose := ui.Pose{}
	if card.Hovered() {
		pose = ui.Pose{Scale: 1.015}
	}
	card.AnimatePose(pose, ui.Snappy)
	card.Children(func() {
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(14).Background(a.pal.hover).Children(func() {
			a.coverFill(c, s.BG, 400, 14)
		})
		// Dark enough at the foot for the words over it, whatever the
		// picture is.
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(14).
			LinearGradient(ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
				{Color: ui.RGBA(0, 0, 0, 0.05), At: 0},
				{Color: ui.RGBA(0, 0, 0, 0.55), At: 1},
			}})
		ui.Column(c).Absolute().Left(14).Right(14).Bottom(12).Gap(3).Children(func() {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Text(c, s.Title).FontSize(15).FontWeight(700).TextColor(white).SingleLine().Shrink(1).MinWidth(0)
				switch {
				case on:
					ui.Box(c).Padding(1, 6).Radius(999).Background(white.Alpha(0.22)).Children(func() {
						ui.Text(c, "正在播放").FontSize(10).FontWeight(600).TextColor(white)
					})
				case starting:
					ui.Box(c).Padding(1, 6).Radius(999).Background(white.Alpha(0.16)).Children(func() {
						ui.Text(c, "正在准备").FontSize(10).FontWeight(600).TextColor(white)
					})
				}
			})
			line := s.Label
			if line == "" {
				line = s.Popup
			}
			if s.Songs > 0 {
				line = fmt.Sprintf("%s · %s 首", line, count(int(s.Songs)))
			}
			ui.Text(c, line).FontSize(11.5).TextColor(white.Alpha(0.75)).SingleLine()
		})
	})
	if card.Clicked() {
		a.playScene(*s)
	}
}
