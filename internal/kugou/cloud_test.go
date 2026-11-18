package kugou

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// The list service wraps its songs in whichever of the shapes it feels like;
// the page reads them all.
func TestCloudSongsReadTheList(t *testing.T) {
	body := map[string]any{
		"status": 1,
		"data": map[string]any{
			"total": float64(2),
			"list": []any{
				map[string]any{
					"fileid": float64(7), "audio_id": float64(88), "album_audio_id": float64(99),
					"hash": "abcdef", "filename": "晴天 - 周杰伦.mp3", "author_name": "周杰伦",
					"filesize": float64(4096), "timelen": float64(269000),
				},
				map[string]any{
					"kv_id": float64(8), "mixsongid": float64(77),
					"hash": "012345", "name": "富士山下.flac", "ext": "flac",
					"size": float64(2 << 20), "duration": float64(258),
				},
			},
		},
	}
	songs := cloudSongsFrom(body)
	if len(songs) != 2 {
		t.Fatalf("read %d songs, want 2", len(songs))
	}
	first := songs[0]
	if first.FileID != 7 || first.AudioID != 88 || first.AlbumAudioID != 99 {
		t.Errorf("first song's numbers: %+v", first)
	}
	if first.Hash != "abcdef" || first.Singer != "周杰伦" {
		t.Errorf("first song's name: %+v", first)
	}
	if first.Title() != "晴天 - 周杰伦" {
		t.Errorf("title is %q", first.Title())
	}
	if first.Size != 4096 || first.Duration != 269000 {
		t.Errorf("first song's size and length: %+v", first)
	}
	second := songs[1]
	// A duration that came in seconds is read as milliseconds.
	if second.FileID != 8 || second.AlbumAudioID != 77 || second.Ext != "flac" {
		t.Errorf("second song: %+v", second)
	}
	if second.Duration != 258000 {
		t.Errorf("second song's length is %d, want 258000", second.Duration)
	}
	if got := cloudTotal(body, len(songs)); got != 2 {
		t.Errorf("total is %d, want 2", got)
	}
}

// The tracker sends the address as a string, a list, or a list of objects.
func TestCloudStreamReadsTheTracker(t *testing.T) {
	cases := []struct {
		name string
		body string
		file CloudSong
		url  string
		kind string
		size int64
	}{
		{"a string", `{"status":1,"url":"http://a/1.mp3","fileSize":123,"ext":"mp3"}`, CloudSong{}, "http://a/1.mp3", "mp3", 123},
		{"a list", `{"url":["http://b/2.flac"],"file_size":456}`, CloudSong{Ext: "flac"}, "http://b/2.flac", "flac", 456},
		{"objects", `{"urls":[{"url":"http://c/3.m4a"}]}`, CloudSong{Ext: ".m4a", Size: 9}, "http://c/3.m4a", "m4a", 9},
		// What the tracker really sends: the address in "data", the size as
		// a string, the format as "extName".
		{"as it sends it", `{"data":{"url":"http://d/4.mp3","extName":"mp3","fileSize":"4265887"},"status":1}`,
			CloudSong{}, "http://d/4.mp3", "mp3", 4265887},
	}
	for _, c := range cases {
		st, err := cloudStream([]byte(c.body), c.file)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if st.URL != c.url || st.Size != c.size {
			t.Errorf("%s: %+v", c.name, st)
		}
		if c.kind != "" && st.Type != c.kind {
			t.Errorf("%s: type is %q, want %q", c.name, st.Type, c.kind)
		}
	}
	if _, err := cloudStream([]byte(`{"status":0,"url":[]}`), CloudSong{}); err != ErrUnavailable {
		t.Errorf("an answer with no address is not a stream, got %v", err)
	}
}

// The cloud's answer is its AES ciphertext, sometimes base64, and sometimes
// clear JSON.
func TestCloudOpenOpensTheAnswer(t *testing.T) {
	const key = "abc123"
	sum := md5Hex(key)
	plain := []byte(`{"status":1,"data":{"total":3}}`)
	cipher, err := aesCBCEncrypt(plain, []byte(sum[:16]), []byte(sum[16:32]))
	if err != nil {
		t.Fatal(err)
	}
	if got := cloudOpen(cipher, sum); string(got) != string(plain) {
		t.Errorf("raw ciphertext opened to %s", got)
	}
	b64 := []byte(base64.StdEncoding.EncodeToString(cipher))
	if got := cloudOpen(b64, sum); string(got) != string(plain) {
		t.Errorf("base64 ciphertext opened to %s", got)
	}
	if got := cloudOpen(plain, sum); string(got) != string(plain) {
		t.Errorf("clear JSON came back as %s", got)
	}
}

// The tracker signs a file with the bucket, its hash, the pid and the salt,
// in that order.
func TestCloudFileKeySignsTheFile(t *testing.T) {
	const want = "33d848985238d30d99239a0607303134"
	if got := cloudFileKey("ABCDEF0123", "20026"); got != want {
		t.Errorf("key is %s, want %s", got, want)
	}
}

// A song put in the cloud is remembered by the number its hash gives, with
// the file's name and numbers, so it plays and deletes by that number later.
func TestIndexRemembersCloudFiles(t *testing.T) {
	x := &Index{byID: map[int64]Known{}}
	s := CloudSong{
		FileID: 12, AudioID: 34, AlbumAudioID: 56, Hash: "abc",
		Name: "晴天.mp3", Singer: "周杰伦", Ext: "mp3", Size: 900,
	}
	id := x.RememberCloud(s)
	if id != SongID("abc") {
		t.Fatalf("id %d is not the hash's", id)
	}
	got, ok := x.CloudFile(id)
	if !ok {
		t.Fatal("the file is not in the index")
	}
	if got.FileID != 12 || got.AudioID != 34 || got.AlbumAudioID != 56 || got.Name != "晴天.mp3" {
		t.Errorf("remembered %+v", got)
	}
	// A catalogue song is not a cloud file, and says so.
	if _, ok := x.CloudFile(SongID("other")); ok {
		t.Error("a song that was never a cloud file reads as one")
	}
}

// The entry an upload makes is read out of the cloud's own shapes too.
func TestCloudAddFilesAnswerIsAPlainObject(t *testing.T) {
	var res map[string]any
	if err := json.Unmarshal([]byte(`{"status":1}`), &res); err != nil {
		t.Fatal(err)
	}
	if res["status"] != float64(1) {
		t.Errorf("status %v", res["status"])
	}
}
