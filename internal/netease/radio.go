package netease

import (
	"strconv"
	"strings"

	"netease-music-mygo/internal/kugou"
)

// The listener's radio: songs picked as they listen, and what listeners say
// about them.

// FM modes: what the personal FM picks songs by.
const (
	FMDefault  = "DEFAULT"  // the listener's taste
	FMFamiliar = "FAMILIAR" // songs like those they know
	FMExplore  = "EXPLORE"  // songs new to them
)

// PersonalFM returns the next few songs of the listener's personal FM, in
// mode. KuGou picks them from a pool: its own taste model, a style, or the
// third it keeps for exploration, which is what the modes map to.
func PersonalFM(mode string) ([]Track, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	pool := 0
	switch mode {
	case FMFamiliar:
		pool = 1
	case FMExplore:
		pool = 2
	}
	songs, err := kugou.PersonalFM(c, pool, nil)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(songs))
	for _, t := range songs {
		out = append(out, toTrack(t))
	}
	return out, nil
}

// HeartMode returns songs like one the listener is playing: KuGou's personal
// FM continues from a song when it is given one, which is what this asks.
// start and pid are NetEase's idea of where the mode began, and are unused.
func HeartMode(song, start, pid int64) ([]Track, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	seed, ok := app.index.Track(song)
	if !ok {
		return nil, ErrUnavailable
	}
	songs, err := kugou.PersonalFM(c, 1, &seed)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(songs))
	for _, t := range songs {
		out = append(out, toTrack(t))
	}
	return out, nil
}

// FMTrash tells the personal FM the listener does not like a song, which it
// plays no more. KuGou's radio keeps no such list: the player skips it, and
// the next songs come as they would have.
func FMTrash(id int64) error { return ErrUnsupported }

// ThreadComments returns a page of what listeners wrote about the song or
// episode a thread names, ordered by order: the 精彩 tab asks for the most
// liked, the 最新 tab for the most recent. Each page loads on its own, so
// the two tabs keep their own place.
func ThreadComments(thread string, order kugou.CommentOrder, page, limit int) (*Comments, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	id := threadSong(thread)
	if id == 0 {
		return nil, ErrUnsupported
	}
	k, ok := app.index.Lookup(id)
	if !ok || k.Hash == "" {
		return nil, ErrUnsupported
	}
	if limit <= 0 {
		limit = 30
	}
	list, total, err := kugou.Comments(c, k.Hash, order, page, limit)
	if err != nil {
		return nil, err
	}
	out := &Comments{Total: total, More: (page-1)*limit+len(list) < total}
	for _, m := range list {
		if order == kugou.CommentsHot {
			out.Hot = append(out.Hot, toComment(m))
		} else {
			out.Latest = append(out.Latest, toComment(m))
		}
	}
	return out, nil
}

// toComment reads what KuGou serves of a comment as the app shows it.
func toComment(m kugou.Comment) Comment {
	cm := Comment{
		ID:         m.ID,
		Content:    m.Content,
		Time:       m.At.UnixMilli(),
		LikedCount: m.Likes,
	}
	cm.User.Nickname, cm.User.AvatarURL = m.UserName, m.UserPic
	cm.IPLocation.Location = m.Location
	if m.Reply != nil {
		var r struct {
			User struct {
				Nickname string `json:"nickname"`
			} `json:"user"`
			Content string `json:"content"`
		}
		r.User.Nickname, r.Content = m.Reply.UserName, m.Reply.Content
		cm.BeReplied = append(cm.BeReplied, r)
	}
	return cm
}

// CommentsLikeable reports whether the app may like a comment. KuGou keeps
// no such endpoint the player's client may call, so the rows show how many
// liked a comment without offering to add one.
func CommentsLikeable() bool { return false }

// LikeComment likes what a listener wrote, or unlikes it.
func LikeComment(thread string, id int64, like bool) error { return ErrUnsupported }

// threadSong reads the song's number out of a comment thread, which the app
// names "R_SO_4_<id>" for a song and "A_DJ_1_<id>" for an episode.
func threadSong(thread string) int64 {
	i := strings.LastIndex(thread, "_")
	if i < 0 {
		return 0
	}
	n, _ := strconv.ParseInt(thread[i+1:], 10, 64)
	return n
}
