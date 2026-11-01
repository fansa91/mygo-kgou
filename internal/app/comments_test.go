package app

import (
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/kugou"
	"netease-music-mygo/internal/netease"
)

// The count on the comments' tab holds the last song's while the next
// one's loads, for it to roll from one to the other, and goes once the
// next song turns out to have none.
func TestCommentCountHoldsWhileLoading(t *testing.T) {
	a, tt, _ := coverApp(t)
	a.comments = commentState{track: 2, hot: commentPage{loaded: true, total: 128}}
	tt.Frame()
	a.pl.Next()
	a.comments = commentState{track: 3, hot: commentPage{loading: true}}
	tt.Frame()
	if _, ok := tt.Find("评论 128"); !ok {
		t.Fatal("the count went as the next song's loads")
	}
	a.comments.hot.loading, a.comments.hot.loaded, a.comments.hot.total = false, true, 96
	tt.Frame()
	if _, ok := tt.Find("评论 96"); !ok {
		t.Error("the count did not change once loaded")
	}
	a.pl.Next()
	a.comments = commentState{track: 1, hot: commentPage{loaded: true}}
	tt.Advance(time.Second)
	tt.Frame()
	if _, ok := tt.Find("评论"); !ok {
		t.Error("a song without comments shows a count")
	}
}

// Now Playing shows an episode's comments, the episode's and not its
// audio's, under a tab of their own beside what it is about, which stays
// chosen as the frames go.
func TestEpisodeComments(t *testing.T) {
	asked := make(chan string, 4)
	threadComments = func(thread string, order kugou.CommentOrder, page, limit int) (*netease.Comments, error) {
		asked <- thread
		return &netease.Comments{}, nil
	}
	defer func() { threadComments = netease.ThreadComments }()
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 13, Name: "第一期", ProgramID: 3, PodcastID: 5, Duration: 600000}})
	a.pl.SetLyrics(nil)
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Advance(time.Second)
	tt.Frame()
	if thread := wait(t, asked, "the comments"); thread != "A_DJ_1_3" {
		t.Fatalf("asked for the comments of %s", thread)
	}
	if a.npPane != paneProgram {
		t.Fatalf("opened on pane %d", a.npPane)
	}
	if err := tt.Click("评论"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if a.npPane != paneComments {
		t.Errorf("on pane %d", a.npPane)
	}
}

// 赞 on a comment likes it at once, counting the listener in on both the
// pages the comment is on, and asks NetEase to; again, it unlikes it.
func TestLikeComment(t *testing.T) {
	type call struct {
		thread string
		id     int64
		like   bool
	}
	calls := make(chan call, 4)
	likeComment = func(thread string, id int64, like bool) error { calls <- call{thread, id, like}; return nil }
	defer func() { likeComment = netease.LikeComment }()
	// KuGou takes no like: a service that does is what this test covers.
	commentsLikeable = func() bool { return true }
	defer func() { commentsLikeable = netease.CommentsLikeable }()
	a, tt, _ := coverApp(t)
	a.profile = &netease.Profile{UserID: 1}
	cm := netease.Comment{ID: 9, Content: "好听", LikedCount: 41}
	a.comments = commentState{
		track: 2, thread: "R_SO_4_2",
		hot:    commentPage{loaded: true, total: 1, list: []netease.Comment{cm}},
		latest: commentPage{list: []netease.Comment{cm}},
	}
	a.npPane = paneComments
	tt.Frame()
	if err := tt.Click("赞"); err != nil {
		t.Fatal(err)
	}
	if c := wait(t, calls, "the like"); c != (call{"R_SO_4_2", 9, true}) {
		t.Errorf("asked NetEase for %+v", c)
	}
	for _, cm := range []netease.Comment{a.comments.hot.list[0], a.comments.latest.list[0]} {
		if !cm.Liked || cm.LikedCount != 42 {
			t.Fatalf("liked %v by %d", cm.Liked, cm.LikedCount)
		}
	}
	tt.Frame()
	if err := tt.Click("取消赞"); err != nil {
		t.Fatal(err)
	}
	if c := wait(t, calls, "the unlike"); c.like {
		t.Error("liked again")
	}
	for _, cm := range []netease.Comment{a.comments.hot.list[0], a.comments.latest.list[0]} {
		if cm.Liked || cm.LikedCount != 41 {
			t.Errorf("unliked: liked %v by %d", cm.Liked, cm.LikedCount)
		}
	}
}

// KuGou's comments come in two ranks: the 精彩 ones it thinks best, and the
// 最新 ones written last. The pane shows one page at a time, switching
// between the two, and takes no like of its own: the rows show how many
// thumbs a comment got in place of a thumb to add one.
func TestCommentsShowWhatKuGouServes(t *testing.T) {
	commentsLikeable = func() bool { return false }
	defer func() { commentsLikeable = netease.CommentsLikeable }()
	// The service is asked for one page or the other, and each answers with
	// its own comments.
	var mu sync.Mutex
	var orders []kugou.CommentOrder
	threadComments = func(thread string, order kugou.CommentOrder, page, limit int) (*netease.Comments, error) {
		mu.Lock()
		orders = append(orders, order)
		mu.Unlock()
		if order == kugou.CommentsNew {
			return &netease.Comments{Total: 2, Latest: []netease.Comment{{ID: 8, Content: "小白是只猫"}}}, nil
		}
		return &netease.Comments{Total: 2}, nil
	}
	defer func() { threadComments = netease.ThreadComments }()
	a, tt, _ := coverApp(t)
	liked := netease.Comment{ID: 7, Content: "单曲循环一整晚", LikedCount: 5028}
	liked.User.Nickname = "不懂事"
	liked.IPLocation.Location = "广东"
	liked.BeReplied = append(liked.BeReplied, struct {
		User struct {
			Nickname string `json:"nickname"`
		} `json:"user"`
		Content string `json:"content"`
	}{})
	liked.BeReplied[0].User.Nickname, liked.BeReplied[0].Content = "周柏豪", "握不住的东西连伸手都是多余的"
	other := netease.Comment{ID: 8, Content: "小白是只猫"}
	other.User.Nickname = "冰美式"
	a.comments = commentState{
		track: a.pl.Current().ID, thread: a.pl.Current().Thread(), active: commentTabHot,
		hot:    commentPage{loaded: true, total: 2, list: []netease.Comment{liked}},
		latest: commentPage{loaded: true, total: 2, list: []netease.Comment{other}},
	}
	a.npPane = paneComments
	tt.Advance(time.Second)
	tt.Frame()

	// The 精彩 page shows its own comments, and only those.
	for _, want := range []string{"精彩", "最新", "精彩评论", "单曲循环一整晚", "不懂事", "· 广东", "@周柏豪：", "5028"} {
		if !tt.HasText(want) {
			t.Errorf("the pane does not show %q", want)
		}
	}
	if tt.HasText("小白是只猫") {
		t.Error("the pane shows the 最新 comments under the 精彩 ones")
	}
	if tt.HasText("赞") {
		t.Error("offers a like the service cannot take")
	}

	// Switching to 最新 asks for that page's comments, and shows them
	// instead of the 精彩 ones.
	if err := tt.Click("最新"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	mu.Lock()
	asked := append([]kugou.CommentOrder(nil), orders...)
	mu.Unlock()
	newest := false
	for _, o := range asked {
		if o == kugou.CommentsNew {
			newest = true
		}
	}
	if !newest {
		t.Errorf("asked for %v, want the newest among them", asked)
	}
	if !tt.HasText("最新评论 2") || !tt.HasText("小白是只猫") {
		t.Error("the 最新 page does not show its own comments")
	}
	if tt.HasText("单曲循环一整晚") {
		t.Error("the 最新 page shows the 精彩 comments too")
	}
}
