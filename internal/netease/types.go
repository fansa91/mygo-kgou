// Package netease is what the app asks for music: search, playlists, charts,
// albums, artists, podcasts and the listener's own library.
//
// It was written against NetEase, and the app grew on its shape; the shapes
// here are still NetEase's, because the app is built on them. What answers
// them is KuGou now, through internal/kugou: this package is the adapter that
// keeps the app's idea of a song, a playlist and a chart intact, and turns
// KuGou's hashes, collection ids and album audios into the numbers it names
// things by.
package netease

import (
	"strconv"
	"strings"
	"time"
)

// Track is a song, as the app knows it.
type Track struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Artists  []Artist `json:"ar"`
	Album    Album    `json:"al"`
	Duration int64    `json:"dt"` // milliseconds
	Fee      int      `json:"fee"`
	Alias    []string `json:"alia"`
	// Reason says why a daily recommendation was picked.
	Reason string `json:"reason"`
	// Privilege is what the listener may do with the track, if known.
	Privilege *Privilege `json:"privilege"`
	// A podcast's episode plays as a track of its audio, ID, with the
	// episode's ProgramID, the PodcastID it is in, and what it is about.
	ProgramID   int64  `json:"programId,omitempty"`
	PodcastID   int64  `json:"podcastId,omitempty"`
	Description string `json:"description,omitempty"`
	// Hash is the file KuGou plays the song from (standard 128kbps).
	Hash string `json:"hash,omitempty"`
	// HashHigh is the 320kbps hash.
	HashHigh string `json:"hashHigh,omitempty"`
	// HashLossl is the lossless/FLAC hash.
	HashLossl string `json:"hashLossl,omitempty"`
	// HashHires is the highest quality (Hi-Res) hash.
	HashHires string `json:"hashHires,omitempty"`
	// PublishTime is when the episode was published (Unix milliseconds).
	// Used for podcast/audiobook episodes.
	PublishTime int64 `json:"publishTime,omitempty"`
	// Year is when the song's album came out ("2006" or "2006-11-23"),
	// TrackNo its number on the album and DiscNo the disc it is on; empty
	// and 0 where the source did not say. The download writes them into the
	// file's tags.
	Year    string `json:"year,omitempty"`
	TrackNo int    `json:"trackNo,omitempty"`
	DiscNo  int    `json:"discNo,omitempty"`
	// VIP marks a song that is sold by subscription. It is apart from
	// playability on purpose: the 概念版 may play one at standard quality,
	// and the row still says why it is not on the free tier.
	VIP bool `json:"vip,omitempty"`
}

// ArtistNames joins the track's artists as the credits read.
func (t *Track) ArtistNames() string {
	names := make([]string, len(t.Artists))
	for i, a := range t.Artists {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

// Playable reports whether the listener can play the whole track. KuGou says
// so of a song it sells, and of one it gives a minute of; the adapter keeps
// that in Privilege, so a song the app greys out is one KuGou would refuse to
// stream in full.
func (t *Track) Playable() bool {
	if t.ProgramID > 0 {
		// An episode's audio has no privilege: whether it streams is up to
		// its stream, which the player asks for when it needs it.
		return true
	}
	if t.Privilege != nil {
		return t.Privilege.St >= 0 && t.Privilege.Pl > 0
	}
	return t.Fee != 1 && t.Fee != 4
}

// HighestQuality returns the highest quality level this track has a hash for.
// Levels from low to high: standard (128), higher (320), exhigh (320), lossless (FLAC), hires (Hi-Res).
func (t *Track) HighestQuality() string {
	if t.HashHires != "" {
		return "hires"
	}
	if t.HashLossl != "" {
		return "lossless"
	}
	if t.HashHigh != "" {
		return "exhigh"
	}
	if t.Hash != "" {
		return "standard"
	}
	return ""
}

// QualityLabel returns the display label for a quality level.
func QualityLabel(level string) string {
	switch level {
	case "hires":
		return "Hi-Res"
	case "lossless":
		return "无损"
	case "exhigh", "higher":
		return "高品"
	case "standard":
		return "标准"
	default:
		return ""
	}
}

// Length is how long the track plays.
func (t *Track) Length() time.Duration { return time.Duration(t.Duration) * time.Millisecond }

// URL is the track's page, where its site has one.
func (t *Track) URL() string {
	if t.ProgramID > 0 {
		return "https://www.kugou.com/album/" + strconv.FormatInt(t.PodcastID, 10) + ".html"
	}
	hash := t.Hash
	if hash == "" {
		// A song restored from the queue of an older run carries no hash;
		// the table of numbers knows it, if this run met the song.
		if k, ok := app.index.Track(t.ID); ok {
			hash = k.Hash
		}
	}
	if hash == "" {
		return ""
	}
	return "https://www.kugou.com/song/#hash=" + strings.ToUpper(hash)
}

// Thread names the track's comments.
func (t *Track) Thread() string {
	if t.ProgramID > 0 {
		return "A_DJ_1_" + strconv.FormatInt(t.ProgramID, 10)
	}
	return "R_SO_4_" + strconv.FormatInt(t.ID, 10)
}

// Artist is someone who made a song.
type Artist struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Album is what a song was published in.
type Album struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	PicURL string `json:"picUrl"`
}

// Privilege is what the listener may do with a track, if known. KuGou calls
// a song it sells one whose privilege says so.
type Privilege struct {
	ID int64 `json:"id"`
	St int   `json:"st"` // below 0: taken down
	Pl int   `json:"pl"` // the best bit rate the listener may play; 0 for none
}

// Playlist is a collection of songs: a playlist, a chart, or the listener's
// liked ones.
type Playlist struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	CoverURL    string  `json:"coverImgUrl"`
	PicURL      string  `json:"picUrl"`
	Description string  `json:"description"`
	PlayCount   float64 `json:"playCount"`
	TrackCount  int     `json:"trackCount"`
	UpdateFreq  string  `json:"updateFrequency"`
	SpecialType int     `json:"specialType"` // 5: the listener's liked songs
	Privacy     int     `json:"privacy"`     // 10: only its creator sees it
	Subscribed  bool    `json:"subscribed"`  // the listener saved it
	Creator     struct {
		UserID   int64  `json:"userId"`
		Nickname string `json:"nickname"`
	} `json:"creator"`
	Tracks   []Track `json:"tracks"`
	TrackIDs []struct {
		ID int64 `json:"id"`
	} `json:"trackIds"`
	// Preview holds a chart's top songs, as its card shows them.
	Preview []ChartLine `json:"-"`
}

// Cover is the playlist's picture, whichever field the service filled.
func (p *Playlist) Cover() string {
	if p.CoverURL != "" {
		return p.CoverURL
	}
	return p.PicURL
}

// ChartLine is a line of a chart's card: the songs it leads with.
type ChartLine struct {
	First  string `json:"first"`
	Second string `json:"second"`
}

// AlbumProfile is an album, as its page shows it.
type AlbumProfile struct {
	Album
	Artists     []Artist `json:"artists"`
	PublishTime int64    `json:"publishTime"` // Unix milliseconds
	Size        int      `json:"size"`        // how many songs it has
	Description string   `json:"description"`
	Songs       []Track  `json:"-"`
}

// ArtistNames joins the album's artists' names, as its credits read.
func (al *AlbumProfile) ArtistNames() string {
	return (&Track{Artists: al.Artists}).ArtistNames()
}

// ArtistProfile is a singer, as their page shows them.
type ArtistProfile struct {
	Artist
	Avatar    string   `json:"img1v1Url"` // square
	PicURL    string   `json:"picUrl"`    // wide
	Alias     []string `json:"alias"`
	Trans     string   `json:"trans"`
	BriefDesc string   `json:"briefDesc"`
	MusicSize int      `json:"musicSize"`
	AlbumSize int      `json:"albumSize"`
	Followed  bool     `json:"followed"` // by the listener signed in
	// AccountID is the artist's own account, which posts their events, or 0.
	AccountID int64   `json:"accountId"`
	Songs     []Track `json:"-"`
}

// User is someone with an account.
type User struct {
	UserID    int64  `json:"userId"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
	Signature string `json:"signature"`
	Followeds int    `json:"followeds"` // their followers
	Follows   int    `json:"follows"`
}

// Podcast is a channel of episodes.
type Podcast struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	PicURL       string  `json:"picUrl"`
	Description  string  `json:"desc"`
	Category     string  `json:"category"`
	DJ           User    `json:"dj"`
	ProgramCount int     `json:"programCount"`
	SubCount     int64   `json:"subCount"`
	PlayCount    float64 `json:"playCount"`
	Subed        bool    `json:"subed"`    // by the listener signed in
	RcmdText     string  `json:"rcmdtext"` // a line on why to listen, in lists
	// LastProgram is the name of its newest episode, in the listener's
	// subscriptions.
	LastProgram string `json:"lastProgramName"`
}

// Program is an episode of a podcast.
type Program struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	CoverURL    string      `json:"coverUrl"`
	Duration    int64       `json:"duration"`   // milliseconds
	CreateTime  int64       `json:"createTime"` // Unix milliseconds
	Description string      `json:"description"`
	MainSong    legacyTrack `json:"mainSong"` // its audio
}

// legacyTrack is a song in the shape older endpoints send.
type legacyTrack struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Artists  []Artist `json:"artists"`
	Album    Album    `json:"album"`
	Duration int64    `json:"duration"`
}

// Track is the episode as it plays: its audio under the episode's name,
// cover and host.
func (p *Program) Track(pod *Podcast) Track {
	cover := p.CoverURL
	if cover == "" {
		cover = pod.PicURL
	}
	t := Track{
		ID: p.MainSong.ID, Name: p.Name, Duration: p.Duration,
		Album:     Album{ID: pod.ID, Name: pod.Name, PicURL: cover},
		ProgramID: p.ID, PodcastID: pod.ID, Description: p.Description,
	}
	if pod.DJ.Nickname != "" {
		t.Artists = []Artist{{Name: pod.DJ.Nickname}}
	}
	return t
}

// Comment is what a listener wrote about a song or an episode.
type Comment struct {
	ID   int64 `json:"commentId"`
	User struct {
		ID        int64  `json:"userId"`
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatarUrl"`
	} `json:"user"`
	Content    string `json:"content"`
	Time       int64  `json:"time"` // Unix milliseconds
	LikedCount int    `json:"likedCount"`
	Liked      bool   `json:"liked"` // by the listener signed in
	IPLocation struct {
		Location string `json:"location"`
	} `json:"ipLocation"`
	// BeReplied holds the comment this one replies to.
	BeReplied []struct {
		User struct {
			Nickname string `json:"nickname"`
		} `json:"user"`
		Content string `json:"content"`
	} `json:"beReplied"`
}

// Comments is a page of what listeners wrote.
type Comments struct {
	// Hot are the comments most liked, on the first page only.
	Hot    []Comment `json:"hotComments"`
	Latest []Comment `json:"comments"`
	Total  int       `json:"total"`
	More   bool      `json:"more"`
}

// SearchType is what a search looks for.
type SearchType int

const (
	SearchSongs     SearchType = 1
	SearchAlbums    SearchType = 10
	SearchArtists   SearchType = 100
	SearchPlaylists SearchType = 1000
	SearchUsers     SearchType = 1002
	SearchPodcasts  SearchType = 1009
)

// SearchResult is a page of what a search found, of the one type it looked
// for, and how many it found in all.
type SearchResult struct {
	Songs     []Track
	Albums    []AlbumProfile
	Artists   []ArtistProfile
	Playlists []Playlist
	Users     []User
	Podcasts  []Podcast
	Total     int
}

// Len is how many the page has.
func (r *SearchResult) Len() int {
	return len(r.Songs) + len(r.Albums) + len(r.Artists) + len(r.Playlists) + len(r.Users) + len(r.Podcasts)
}

// Append adds the next page of a search to r.
func (r *SearchResult) Append(next SearchResult) {
	r.Songs = append(r.Songs, next.Songs...)
	r.Albums = append(r.Albums, next.Albums...)
	r.Artists = append(r.Artists, next.Artists...)
	r.Playlists = append(r.Playlists, next.Playlists...)
	r.Users = append(r.Users, next.Users...)
	r.Podcasts = append(r.Podcasts, next.Podcasts...)
	r.Total = next.Total
}

// Suggestions are the songs, singers, albums and playlists a search box
// offers while typing.
type Suggestions struct {
	Songs     []Track
	Artists   []Artist
	Albums    []AlbumProfile
	Playlists []Playlist
	Order     []string // "songs", "artists", "albums" and "playlists"
}

// Stream is where a track streams from, and what it is.
type Stream struct {
	URL  string `json:"-"`
	Size int64  `json:"size"` // in bytes
	// Type is the format: mp3, or flac for the lossless levels.
	Type string `json:"type"`
	// Level is the level served, which may be below the one asked for.
	Level string `json:"level"`
	// Gain is how many dB to change the track by to sound as loud as
	// others, and Peak its loudest sample, 1 at full scale, or 0 if
	// unknown.
	Gain float64 `json:"gain"`
	Peak float64 `json:"peak"`
}

// Play is one song the listener played.
type Play struct {
	Track  Track
	At     time.Time
	Device string
}

// Event is something a listener posted: a song, an album, a playlist.
type Event struct {
	ID       int64 `json:"id"`
	Time     int64 `json:"eventTime"` // Unix milliseconds
	User     User  `json:"user"`
	Pictures []struct {
		SquareURL string `json:"squareUrl"`
	} `json:"pics"`
	Info struct {
		LikedCount   int `json:"likedCount"`
		CommentCount int `json:"commentCount"`
		ShareCount   int `json:"shareCount"`
	} `json:"info"`
	Content EventContent `json:"-"`
}

// EventContent is what an event posts.
type EventContent struct {
	Msg      string
	Song     *Track
	Album    *AlbumProfile
	Artist   *ArtistProfile
	Playlist *Playlist
	Video    bool // an MV or video, which the app can't play
	// Forwarded is the event it passes on, as its poster's name and words.
	Forwarded string
}

// Profile is the listener signed in.
// VIPProduct is one subscription an account holds, and the day it runs out.
// The 概念版 sells two, 超级会员 and 豪华会员, and an account may hold both.
type VIPProduct struct {
	Name string `json:"name"`
	End  string `json:"end"`
}

type Profile struct {
	UserID    int64  `json:"userId"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
	// VIP says the account holds a subscription, Label what its badge reads
	// ("VIP", "概念版" or both), and Lite that it signed in as the 概念版.
	VIP   bool   `json:"vip,omitempty"`
	Label string `json:"vipLabel,omitempty"`
	Lite  bool   `json:"concept,omitempty"`
	// Products are the subscriptions in force, each with its last day, for
	// the account's own page to say when they end.
	Products []VIPProduct `json:"products,omitempty"`
}
