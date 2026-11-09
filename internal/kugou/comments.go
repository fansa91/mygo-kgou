package kugou

import (
	"encoding/json"
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
func Comments(c *Client, hash string, order CommentOrder, page, size int) (CommentsPage, error) {
	if hash == "" {
		return CommentsPage{}, fmt.Errorf("kugou: the song has no hash to find its comments by")
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
	var out map[string]any
	err := c.send("GET", request{
		Router: "m.comment.service.kugou.com",
		Path:   order.rankPath(),
		Params: params,
	}, &out)
	if err != nil {
		return CommentsPage{}, err
	}
	return commentsPageFrom(out), nil
}

// commentsPageFrom reads a page as the service sends it: the comments, how
// many there are in all, and the pool they are filed under.
func commentsPageFrom(out map[string]any) CommentsPage {
	pg := CommentsPage{Total: int(pickInt(out, "count")), Pool: pickString(out, "childrenid")}
	for _, m := range maps(out["list"]) {
		pg.List = append(pg.List, commentFrom(m))
		// The name the service files them under, "周杰伦 - 晴天", comes with
		// the comments rather than with the page.
		if pg.Name == "" {
			pg.Name = strings.TrimSpace(pickString(m, "song_show_text"))
		}
	}
	return pg
}

// CommentsPage is a page of what listeners wrote, and the song's own comment
// pool: the number the service files a new comment or a reply under, and the
// name it keeps beside them ("周杰伦 - 晴天").
type CommentsPage struct {
	List  []Comment
	Total int
	Pool  string // childrenid: the pool a comment is written into
	Name  string // childrenname: the song as the service names it there
}

// commentPool reads what a song's comments are filed under: every page of
// them carries it, so the first one is enough.
func commentPool(c *Client, hash string) (pool, name string, err error) {
	pg, err := Comments(c, hash, CommentsNew, 1, 1)
	if err != nil {
		return "", "", err
	}
	if pg.Pool == "" || pg.Pool == "0" {
		return "", "", fmt.Errorf("kugou: the song has no comment pool to write into")
	}
	return pg.Pool, pg.Name, nil
}

// SendComment writes what the listener says about a song, filed under the
// song's comment pool, and returns the number the service gave it.
func SendComment(c *Client, hash string, audioID int64, content string) (int64, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, fmt.Errorf("kugou: there is nothing to write")
	}
	if c.Token == "" || c.UserID == 0 {
		return 0, fmt.Errorf("kugou: comments need a listener signed in")
	}
	pool, name, err := commentPool(c, hash)
	if err != nil {
		return 0, err
	}
	data := map[string]any{"content": content, "images": []any{}}
	// The album audio is the song the comment hangs on: the service numbers
	// a song by it, and drops a comment written without one.
	if audioID != 0 {
		data["album_audio_id"] = audioID
	}
	body, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		return 0, err
	}
	return c.postComment(map[string]string{
		"r":            "commentsv3/add",
		"code":         commentCode,
		"childrenid":   pool,
		"childrenname": name,
	}, string(body), true)
}

// ReplyComment answers a comment under the song's pool, quoting what it
// answers as the phone's page does.
func ReplyComment(c *Client, hash string, audioID int64, target Comment, content string) (int64, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, fmt.Errorf("kugou: there is nothing to write")
	}
	if c.Token == "" || c.UserID == 0 {
		return 0, fmt.Errorf("kugou: comments need a listener signed in")
	}
	if target.ID == 0 {
		return 0, fmt.Errorf("kugou: there is no comment to answer")
	}
	pool, name, err := commentPool(c, hash)
	if err != nil {
		return 0, err
	}
	// A reply quotes the comment it answers, appended to its own words: the
	// service shows the reply as written, and the quote is what the phone's
	// page writes for it.
	return c.postComment(map[string]string{
		"r":            "commentsv2/reply",
		"code":         commentCode,
		"childrenid":   pool,
		"childrenname": name,
		"content":      replyContent(content, target),
		"tid":          strconv.FormatInt(target.ID, 10),
		"pid":          "0",
		"is_t":         "1",
	}, "", false)
}

// replyContent is what a reply says: the listener's words, then the comment
// they answer as "//@nickname:what it said".
func replyContent(content string, target Comment) string {
	content = strings.TrimSpace(content)
	said := strings.TrimSpace(target.Content)
	if said == "" || target.UserName == "" || strings.Contains(content, "//@") {
		return content
	}
	return content + "//@" + target.UserName + ":" + said
}

// DeleteComment takes back a comment the listener wrote, by the number the
// service gave it.
func DeleteComment(c *Client, hash string, id int64) error {
	if id == 0 {
		return fmt.Errorf("kugou: there is no comment to take back")
	}
	if c.Token == "" || c.UserID == 0 {
		return fmt.Errorf("kugou: comments need a listener signed in")
	}
	pool, _, err := commentPool(c, hash)
	if err != nil {
		return err
	}
	_, err = c.postComment(map[string]string{
		"r":          "commentsv2/delcomment",
		"code":       commentCode,
		"childrenid": pool,
		"cid":        strconv.FormatInt(id, 10),
		"tid":        strconv.FormatInt(id, 10),
	}, "", false)
	return err
}

// postComment writes to the phone's comment page: /index.php under the
// comment router, whose "key" covers the moment, the device, and — for a new
// comment — the body it posts.
func (c *Client) postComment(params map[string]string, body string, withBody bool) (int64, error) {
	sec := strconv.FormatInt(time.Now().Unix(), 10)
	mid := c.Dev.MID
	params["mid"], params["clienttime"] = mid, sec
	params["uuid"] = "-"
	params["dfid"] = c.Dev.DFID
	params["appid"], params["clientver"] = c.appID(), c.clientVer()
	params["kugouid"] = strconv.FormatInt(c.UserID, 10)
	params["clienttoken"] = c.Token
	params["ver"] = "6"
	key := sec + mid
	if withBody {
		key += body
	}
	params["key"] = c.signKey(key)
	// The comment page sends only what it lists, and signs nothing but the
	// "key" above: the gateway's own parameters and signature are not its.
	q := call{Router: "m.comment.service.kugou.com", Path: "/index.php", Params: params, Bare: true, NoSign: true}
	if body != "" {
		q.Body, q.Type = body, "application/json"
	}
	var out map[string]any
	if err := c.decode("POST", q, &out); err != nil {
		return 0, err
	}
	return pickInt(out, "id", "commentid", "cid"), nil
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
