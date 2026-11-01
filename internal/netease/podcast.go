package netease

import (
	"fmt"

	"netease-music-mygo/internal/kugou"
)

// Podcasts. KuGou has no podcasts; it has long audio, whose albums are
// shows and whose audios are episodes, so that is what these read.

// PodcastDetail returns a podcast.
func PodcastDetail(id int64) (*Podcast, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	a, err := kugou.PodcastAlbum(c, id)
	if err != nil {
		return nil, err
	}
	p := toPodcast(*a)
	if p.ID != id {
		p.ID = id
	}
	return &p, nil
}

// PodcastPrograms returns limit of a podcast's episodes from offset, the
// newest first, and whether it has more.
func PodcastPrograms(id int64, limit, offset int) ([]Program, bool, error) {
	c, err := kuGou()
	if err != nil {
		return nil, false, err
	}
	if limit <= 0 {
		limit = 30
	}
	episodes, more, err := kugou.PodcastEpisodes(c, id, offset/limit+1, limit)
	if err != nil {
		return nil, false, err
	}
	out := make([]Program, 0, len(episodes))
	for _, e := range episodes {
		t := toTrack(e)
		t.ProgramID = t.ID
		t.PodcastID = id
		out = append(out, toProgram(t, id, ""))
	}
	return out, more, nil
}

// RecommendedPodcasts returns shows to listen to. KuGou's own daily pick is
// of radio programmes, whose episodes live on a service of their own; the
// free shelf of audio books is what this app can list and play, so that is
// what it offers, the most played first.
func RecommendedPodcasts() ([]Podcast, error) {
	c, err := kuGou()
	if err != nil {
		return nil, err
	}
	albums, _, err := kugou.PodcastLibrary(c, 906, 1, 20, true)
	if err != nil {
		return nil, err
	}
	out := make([]Podcast, 0, len(albums))
	for _, a := range albums {
		out = append(out, toPodcast(a))
	}
	return out, nil
}

// HotPodcasts returns limit of the most popular podcasts from offset, and
// whether there are more. The free shelf is what a listener without a
// subscription can hear, so that is what it counts.
func HotPodcasts(limit, offset int) ([]Podcast, bool, error) {
	c, err := kuGou()
	if err != nil {
		return nil, false, err
	}
	if limit <= 0 {
		limit = 30
	}
	albums, more, err := kugou.PodcastLibrary(c, 906, offset/limit+1, limit, true)
	if err != nil {
		return nil, false, err
	}
	out := make([]Podcast, 0, len(albums))
	for _, a := range albums {
		out = append(out, toPodcast(a))
	}
	return out, more, nil
}

// SubscribedPodcasts returns the podcasts the listener follows. KuGou's long
// audio has no subscription to read: a listener saves an album to a playlist.
func SubscribedPodcasts() ([]Podcast, error) {
	return nil, ErrUnsupported
}

// SubscribePodcast subscribes the listener to a podcast, or unsubscribes
// them. Long audio has nothing to subscribe to.
func SubscribePodcast(id int64, subscribe bool) error {
	return ErrUnsupported
}

// LikeProgram likes an episode for the listener, or unlikes it.
func LikeProgram(id int64, like bool) error {
	c, err := kuGou()
	if err != nil {
		return err
	}
	_ = c
	return ErrUnsupported
}

// ProgramsLiked reports which of the episodes the listener liked, by number.
func ProgramsLiked(ids []int64) (map[int64]bool, error) {
	if _, err := kuGou(); err != nil {
		return nil, err
	}
	// Nothing to ask: an episode's like is not something KuGou keeps.
	return map[int64]bool{}, nil
}

func programThread(id int64) string { return "A_DJ_1_" + fmt.Sprint(id) }
