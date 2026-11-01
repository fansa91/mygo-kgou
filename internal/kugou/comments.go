package kugou

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A Comment is what a listener wrote about a song, and the reply the singer
// gave it when there is one.
type Comment struct {
	ID       int64
	Content  string
	UserName string
	UserPic  string
	Location string
	At       time.Time
	Likes    int
	ReplyNum int
	Reply    *Comment
}

// CommentOrder is how a song's comments are ordered.
type CommentOrder int

const (
	// CommentsHot is the 精彩 tab: the comments most listeners liked, first.
	CommentsHot CommentOrder = iota
	// CommentsNew is the 最新 tab: the comments written most recently, first.
	CommentsNew
)

func (o CommentOrder) rankPath() string {
	switch o {
	case CommentsNew:
		return "/r/v1/rank/newest"
	default:
		return "/r/v1/rank/topliked"
	}
}

// commentHost serves a song's comments to the phone's song page. It knows
// nothing of the gateway: no device parameters, no signature, only the
// number that page itself sends as its code.
const commentHost = "https://mcomment.kugou.com/index.php"

// commentCode is that number, fixed in the phone's comment page.
const commentCode = "fc4be23b4e972707f36b8a828a93ba8a"

// commentsZone is where the service writes its times: Beijing time, with no
// offset to say so.
var commentsZone = time.FixedZone("CST", 8*60*60)

// Comments asks what listeners wrote about a song, one page at a time, and
// how many there are in all, ordered by order. The rank routes live behind
// the gateway under the m.comment.service.kugou.com service, keyed by the
// song's hash as extdata.
func Comments(c *Client, hash string, order CommentOrder, page, size int) ([]Comment, int, error) {
	if hash == "" {
		return nil, 0, fmt.Errorf("kugou: the song has no hash to find its comments by")
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 30
	}
	params := map[string]string{
		"extdata":  hash,
		"p":        strconv.Itoa(page),
		"pagesize": strconv.Itoa(size),
		"code":     commentCode,
	}
	var out struct {
		Count int              `json:"count"`
		List  []map[string]any `json:"list"`
	}
	err := c.send("GET", request{
		Router: "m.comment.service.kugou.com",
		Path:   order.rankPath(),
		Params: params,
	}, &out)
	if err != nil {
		return nil, 0, err
	}
	list := make([]Comment, 0, len(out.List))
	for _, m := range out.List {
		list = append(list, commentFrom(m))
	}
	return list, out.Count, nil
}

// commentFrom reads one comment, and the first reply under it.
func commentFrom(m map[string]any) Comment {
	cm := Comment{
		ID:       pickInt(m, "id", "commentid"),
		Content:  strings.TrimSpace(pickString(m, "content")),
		UserName: pickString(m, "user_name", "nickname", "username"),
		UserPic:  sized(pickString(m, "user_pic", "userpic", "avatar")),
		Location: pickString(m, "location"),
		ReplyNum: int(pickInt(m, "reply_num", "comments_num")),
		At:       commentTime(pickString(m, "addtime"), pickInt(m, "addtimestamp")),
	}
	if like, ok := m["like"].(map[string]any); ok {
		cm.Likes = int(pickInt(like, "count", "likenum"))
	} else {
		cm.Likes = int(pickInt(m, "like", "likenum", "praise_num"))
	}
	// The reply under it: the singer's when they made one, else the first.
	if replys, ok := m["replys"].([]any); ok {
		for _, r := range replys {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			reply := commentFrom(rm)
			if reply.Content == "" {
				continue
			}
			// A singer's reply names them apart from the account that wrote it.
			if name := pickString(rm, "author_name"); name != "" {
				reply.UserName = name
			}
			cm.Reply = &reply
			break
		}
	}
	return cm
}

// commentTime reads when a comment was written: the service writes it as
// Beijing time, and sometimes also as a Unix second.
func commentTime(stamp string, unix int64) time.Time {
	if stamp != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", stamp, commentsZone); err == nil {
			return t
		}
	}
	if unix > 0 {
		return time.Unix(unix, 0)
	}
	return time.Time{}
}
