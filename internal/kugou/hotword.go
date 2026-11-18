package kugou

// HotWords asks for the words on KuGou's search box's board.
//
// The board answers in tabs — 热搜榜, 飙升榜, DJ榜, 外语热搜 and the rest —
// each a list of keywords, and 热搜榜 is the one the search box shows. KuGou
// renames them from time to time, so a tab that is not in the answer falls
// back to the first of them rather than to nothing.
func HotWords(c *Client, tab string) ([]string, error) {
	var res struct {
		Data struct {
			List []struct {
				Name     string `json:"name"`
				Keywords []struct {
					Keyword string `json:"keyword"`
				} `json:"keywords"`
			} `json:"list"`
		} `json:"data"`
	}
	err := c.send("GET", request{
		Router: "msearch.kugou.com",
		Path:   "/api/v3/search/hot_tab",
		Params: map[string]string{"navid": "1", "plat": "2"},
	}, &res)
	if err != nil {
		return nil, err
	}
	if tab == "" {
		tab = "热搜榜"
	}
	var first []string
	for _, g := range res.Data.List {
		seen := map[string]bool{}
		var words []string
		for _, k := range g.Keywords {
			if w := clean(k.Keyword); w != "" && !seen[w] {
				seen[w] = true
				words = append(words, w)
			}
		}
		if first == nil {
			first = words
		}
		if g.Name == tab {
			return words, nil
		}
	}
	return first, nil
}
