package kugou

// Track is a song as this client knows it. KuGou names a song by a hash per
// quality and by the album audio it is; the app needs one number, which the
// adapter layer derives, so nothing here speaks of IDs.
type Track struct {
	// AlbumAudioID is the song's id in the album it was published in, the
	// number every endpoint after the search takes.
	AlbumAudioID int64
	AlbumID      int64
	Name         string
	Singer       string
	// Singers are the song's credits with the number each singer has, which
	// is what opens their page: a name alone cannot.
	Singers []Singer
	Album   string
	Duration int64 // milliseconds
	Size     int64 // bytes, of the standard file
	// Image is the album picture, with the size the services leave in it
	// as "{size}" for the caller to fill in.
	Image string
	// Hashes are the same song at each quality; a quality that was never
	// uploaded keeps the standard hash, which the player's URL endpoint
	// then refuses, as it should.
	Hash      string // 128kbps mp3
	HashHigh  string // 320kbps mp3
	HashLossl string // FLAC
	HashHires string // the highest the catalogue holds
	PayType   int    // 0: free, 3: needs a subscription or a purchase
	Privilege int
	// FileID is the number the cloud list service gave this song in the
	// playlist it was read from; it is what taking the song out again
	// names. Zero for a song that came from anywhere else.
	FileID int64
	// PublishTime is when the episode was published (Unix milliseconds).
	// Used for long audio (audiobook) episodes.
	PublishTime int64
}

// Singer is one of a song's credits: the name it is credited by, and the
// number that singer's page answers to. A credit the service knows no number
// for keeps a zero, and stays plain text in the app.
type Singer struct {
	ID   int64
	Name string
}

// HashFor is the hash to ask for at level, or the standard one when that
// quality was never uploaded.
func (t *Track) HashFor(level string) string {
	var h string
	switch level {
	case "higher", "exhigh":
		h = t.HashHigh
	case "lossless":
		h = t.HashLossl
	case "hires":
		h = t.HashHires
	}
	if h == "" {
		return t.Hash
	}
	return h
}

// SearchResult is a page of what a search found.
type SearchResult struct {
	Songs []Track
	Total int
}
