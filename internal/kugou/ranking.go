package kugou

import (
	"strconv"
	"time"
)

// A song's record across the charts: the places it reached, on the platforms
// KuGou watches, and how long it stayed. The phone's song page shows it as
// 歌曲成绩单, and the app shows the same.

// Ranking is what the service says of one song.
type Ranking struct {
	On, Total int    // the platforms it charted on, of those watched
	Title     string // the service's own line: "已上榜 6/17"
	Note      string // and what that means, in its words
	Platforms []RankingPlatform
}

// RankingPlatform is one platform's record of the song.
type RankingPlatform struct {
	Name  string
	Logo  string
	Rank  int    // the best place it reached there
	Times int    // how long it stayed on that chart
	Last  string // the day it was last seen there
}

// SongRanking returns a song's record across the platforms. A song no chart
// has seen answers with nothing, which is not a failure.
func SongRanking(c *Client, albumAudioID int64) (*Ranking, error) {
	if albumAudioID == 0 {
		return nil, ErrUnavailable
	}
	var res map[string]any
	err := c.get(call{
		Path:   "/grow/v1/song_ranking/play_page/ranking_info",
		Params: map[string]string{"album_audio_id": strconv.FormatInt(albumAudioID, 10)},
	}, &res)
	if err != nil {
		return nil, err
	}
	return rankingFrom(res), nil
}

// rankingFrom reads the record out of the service's answer.
func rankingFrom(res map[string]any) *Ranking {
	data, _ := res["data"].(map[string]any)
	r := &Ranking{
		On:    int(pickInt(data, "platform_num")),
		Total: int(pickInt(data, "platform_total")),
		Title: clean(pickString(data, "title1")),
		Note:  clean(pickString(data, "title2")),
	}
	for _, l := range maps(firstField(data, "info")) {
		name := clean(pickString(l, "platform_name"))
		if name == "" {
			continue
		}
		r.Platforms = append(r.Platforms, RankingPlatform{
			Name:  name,
			Logo:  sized(pickString(l, "platform_logo")),
			Rank:  int(pickInt(l, "ranking_num")),
			Times: int(pickInt(l, "ranking_times")),
			Last:  pickString(l, "last_time"),
		})
	}
	if r.Total == 0 {
		r.Total = len(r.Platforms)
	}
	if r.On == 0 {
		r.On = len(r.Platforms)
	}
	if r.Title == "" && r.On > 0 {
		r.Title = "已上榜 " + strconv.Itoa(r.On) + "/" + strconv.Itoa(r.Total)
	}
	return r
}

// A chart's past numbers. KuGou publishes most charts a day at a time, and
// keeps every number it published: the page offers them to go back through.

// RankVol is one of those numbers: the chart as it stood that day.
type RankVol struct {
	ID   int64  // what the chart's songs are asked for by
	Name string // "322期"
	Date string // "2026-11-18"
	At   time.Time
}

// RankVols returns the numbers a chart published, newest first.
func RankVols(c *Client, rankID int64) ([]RankVol, error) {
	if rankID == 0 {
		return nil, ErrUnavailable
	}
	var res map[string]any
	err := c.get(call{
		Path: "/ocean/v6/rank/vol",
		Params: map[string]string{
			"rank_cid": "0", "rankid": strconv.FormatInt(rankID, 10),
			"ranktype": "1", "type": "0", "plat": "2",
		},
	}, &res)
	if err != nil {
		return nil, err
	}
	return rankVolsFrom(res), nil
}

// rankVolsFrom reads a chart's numbers out of the service's answer, newest
// first and each of them once.
func rankVolsFrom(res map[string]any) []RankVol {
	data, _ := res["data"].(map[string]any)
	var out []RankVol
	seen := map[int64]bool{}
	for _, group := range maps(firstField(data, "info")) {
		for _, l := range maps(group["vols"]) {
			id := pickInt(l, "volid", "vol_id")
			if id == 0 || seen[id] {
				continue
			}
			seen[id] = true
			v := RankVol{
				ID:   id,
				Name: clean(pickString(l, "volname", "voltitle", "outer_text")),
				At:   rankVolTime(pickString(l, "voltime")),
			}
			if v.Name == "" {
				v.Name = v.At.Format("2006-01-02")
			}
			if !v.At.IsZero() {
				v.Date = v.At.Format("2006-01-02")
			}
			out = append(out, v)
		}
	}
	return out
}

// rankVolTime reads when a number came out, which the service writes as
// Beijing time.
func rankVolTime(s string) time.Time {
	if len(s) < 10 {
		return time.Time{}
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s[:min(len(s), 19)], commentsZone)
	if err != nil {
		t, err = time.ParseInLocation("2006-01-02", s[:10], commentsZone)
	}
	if err != nil {
		return time.Time{}
	}
	return t
}
