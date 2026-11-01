package kugou

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestProbe asks, one by one, the services that could stand in for the parts
// of NetEase mygo uses and KuGou's documented API does not cover directly:
// the home page, the charts, long audio and the listener's own radio.
func TestProbe(t *testing.T) {
	if os.Getenv("KG_PROBE") == "" {
		t.Skip("set KG_PROBE=1 to ask the real service")
	}
	c, err := New(os.Getenv("KG_CACHE"))
	if err != nil {
		t.Fatal(err)
	}
	mid := c.Dev.MID
	sec := strconv.FormatInt(time.Now().Unix(), 10)

	// ask sends one probe: body nil means GET, otherwise JSON POST.
	ask := func(name, method, router, path string, params map[string]any, body map[string]any, hdr map[string]string) {
		p := map[string]string{}
		for k, v := range params {
			p[k] = fmt.Sprint(v)
		}
		r := call{Path: path, Router: router, Params: p, Hdr: hdr, Raw: true, Bare: true}
		// Signed over its own parameters only, as these services want: the
		// gateway's device parameters are not theirs to know.
		for _, k := range []string{"dfid", "mid", "uuid", "appid", "clientver", "clienttime"} {
			if _, ok := p[k]; !ok {
				r.Params[k] = map[string]string{
					"dfid": c.Dev.DFID, "mid": mid, "uuid": "-",
					"appid": appID, "clientver": clientVer, "clienttime": sec,
				}[k]
			}
		}
		if body != nil {
			b, _ := json.Marshal(body)
			r.Body, r.Type = string(b), "application/json"
		}
		raw, err := c.do(method, r)
		report(name, raw, err)
	}

	// 1. 推荐歌单：首页「为你推荐」的卡片
	ask("special_recommend 推荐歌单", "POST", "specialrec.service.kugou.com", "/v2/special_recommend", nil,
		map[string]any{
			"appid": appID, "mid": mid, "clientver": clientVer, "platform": "android",
			"clienttime": sec, "userid": 0, "module_id": 1, "page": 1, "pagesize": 30,
			"key": keyFor(sec, mid, "0", appID),
			"special_recommend": map[string]any{
				"withtag": 1, "withsong": 1, "sort": 1, "ugc": 1,
				"is_selected": 0, "withrecommend": 1, "area_code": 1, "categoryid": 0,
			},
			"req_multi": 1, "retrun_min": 5, "return_special_falg": 1,
		}, nil)

	// 2. 每日推荐
	ask("everyday_song_recommend 每日推荐", "POST", "everydayrec.service.kugou.com", "/everyday_song_recommend", nil,
		map[string]any{"platform": "android", "userid": 0}, nil)

	// 3. 首页刷歌（沉浸式推荐流）
	ask("home_discover_rec 首页刷歌", "POST", "", "/homediscoverrec/v1/client/home_discover_rec",
		map[string]any{"module_key": "home_discover_rec", "module_id": 1, "area_code": 1,
			"platform": "android", "userid": 0, "clientver": 20809},
		map[string]any{"support": "only_song", "userid": 0, "recall_type": "song",
			"today_play_num": 0, "pagesize": 4,
			"go_ky_extra": []map[string]string{
				{"key": "network", "val": "2"}, {"key": "play_mode", "val": "2"}, {"key": "no_mv_ret", "val": "1"},
			}}, nil)

	// 4. 排行榜列表
	ask("rank/list 排行榜列表", "GET", "", "/ocean/v6/rank/list",
		map[string]any{"plat": 2, "withsong": 1, "parentid": 0}, nil, nil)

	// 5. 私人 FM
	ms := time.Now().UnixMilli()
	ask("personal_recommend 私人FM", "POST", "persnfm.service.kugou.com", "/v2/personal_recommend", nil,
		map[string]any{
			"appid": appID, "clienttime": ms, "mid": mid,
			"action": "play", "recommend_source_locked": 0, "song_pool_id": 0,
			"callerid": 0, "m_type": 1, "platform": "android", "area_code": 1,
			"remain_songcnt": 0, "clientver": clientVer, "is_overplay": 0,
			"mode": "normal", "fakem": "ca981cfc583a4c37f28d2d49000013c16a0a",
			"key": keyFor(strconv.FormatInt(ms, 10), mid, "0", appID),
		}, nil)

	// 6. 长音频（播客/有声书）每日推荐
	ask("longaudio daily_recommend 播客推荐", "POST", "", "/longaudio/v1/home_new/daily_recommend",
		map[string]any{"module_id": 1, "size": 10, "page": 1}, nil, nil)

	// 7. 长音频专辑列表
	ask("longaudio album/list 播客分类", "GET", "", "/longaudio/v1/album/list",
		map[string]any{"api_ver": 2, "gender": 0, "sort": 0, "tag_id": 906,
			"free": 1, "status": 0, "page": 1, "page_size": 10, "clientver": 20789}, nil, nil)

	// 8. 综合搜索（看它到底给了哪些类目）
	ask("search/complex 综合搜索", "GET", "", "/v6/search/complex",
		map[string]any{"platform": "AndroidFilter", "keyword": "晴", "page": 1,
			"pagesize": 5, "cursor": 0}, nil, nil)
}

func report(name string, raw []byte, err error) {
	fmt.Printf("\n===== %s\n", name)
	if err != nil {
		fmt.Printf("  err: %v\n", err)
		return
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		fmt.Printf("  body: %.200s\n", raw)
		return
	}
	fmt.Printf("  bytes=%d  %s\n", len(raw), shape(v, 0))
}

// shape describes a JSON answer by its keys rather than its values: what the
// probe wants to know is which fields a service offers.
func shape(v any, depth int) string {
	if depth > 3 {
		return "..."
	}
	switch x := v.(type) {
	case map[string]any:
		s := "{"
		n := 0
		for k, val := range x {
			if n > 0 {
				s += " "
			}
			n++
			if n > 14 {
				s += "..."
				break
			}
			s += k + ":" + shape(val, depth+1)
		}
		return s + "}"
	case []any:
		if len(x) == 0 {
			return "[]"
		}
		return fmt.Sprintf("[%d of %s]", len(x), shape(x[0], depth+1))
	case string:
		if len(x) > 28 {
			x = x[:28] + "…"
		}
		return strconv.Quote(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return "null"
	}
	return "?"
}
