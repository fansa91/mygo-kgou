package kugou

import (
	"strings"
	"testing"
)

// The values these tests hold were computed from KuGouMusicApi's own
// algorithms, so a change to the signing shows up before KuGou says no.

func TestSignAndroid(t *testing.T) {
	params := map[string]string{
		"dfid": "ABC123", "mid": "123456789", "uuid": "-",
		"appid": "1005", "clientver": "20489", "clienttime": "1700000000",
		"keyword": "晴天", "page": "1",
	}
	const want = "2ed5ddc2328daa154509338a2ace8efd"
	if got := signAndroid(params, ""); got != want {
		t.Fatalf("signAndroid = %s, want %s", got, want)
	}
}

func TestMidFromGUID(t *testing.T) {
	const guid = "9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c"
	const want = "116460815549131020397401260746167243049"
	if got := midFromGUID(guid); got != want {
		t.Fatalf("midFromGUID = %s, want %s", got, want)
	}
}

func TestSealRoundTrip(t *testing.T) {
	plain := []byte(`{"brand":"Redmi","batteryLevel":100}`)
	s, err := seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Key) != 6 || s.Body == "" {
		t.Fatalf("seal gave %+v", s)
	}
	back, err := open(s.Body, s.Key)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(plain) {
		t.Fatalf("open gave %s, want %s", back, plain)
	}
}

func TestSongIDIsStable(t *testing.T) {
	const hash = "b3a52a7a958bf0aed0ebfba2e9a818b7"
	first := SongID(hash)
	if first <= 0 {
		t.Fatalf("SongID is not positive: %d", first)
	}
	if again := SongID(hash); again != first {
		t.Fatalf("SongID is not stable: %d then %d", first, again)
	}
	if other := SongID("6d5db4310e1573ffb6152712e68e009c"); other == first {
		t.Fatal("two songs share an ID")
	}
}

// The login signs with the app's build and the moment it asked, and hides
// the code behind a key the service can only reach through its own RSA key.
// Both were computed from the reference client's own algorithms.
func TestSignParamsKey(t *testing.T) {
	const want = "a1f65b6a8fe7e191521406ce8661ae02"
	if got := signParamsKey("1700000000"); got != want {
		t.Fatalf("signParamsKey = %s, want %s", got, want)
	}
}

func TestRSASealRaw(t *testing.T) {
	const want = "7BAE5EF5BFA2193C544DB3E3BEAFF70AC9BA3874753DBE9F4E387592CD9034E8" +
		"673A9B2B17169F27EED479339362BA6DFDD32CC6D936FE7A1B13C0FF2EC8E68E" +
		"936223C9B52E0DEAF2E1E4A53CBE4BCBD4749C2502547ED54F95237CEAB1E5E8B" +
		"26D3B2703DD04C25532D864EA54CE4A1ADB5538E6C86F4AB60D44EB3736C3C3"
	got, err := rsaSealRaw([]byte(`{"clienttime_ms":1700000000000,"key":"abc123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("rsaSealRaw =\n%s\nwant\n%s", got, want)
	}
}

func TestSealHexRoundTrip(t *testing.T) {
	const plain = `{"mobile":"13800000000","code":"123456"}`
	s, err := sealHex([]byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Key) != 16 {
		t.Fatalf("the key is %q, want 16 characters", s.Key)
	}
	back, err := openHex(s.Body, s.Key)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != plain {
		t.Fatalf("openHex gave %s, want %s", back, plain)
	}
}

// The login's AES takes its key from all 32 characters of the MD5, so AES-256,
// while a playlist's takes the first 16. Sealing the short way left KuGou
// reading nothing, and the login stayed on the code it was given; the
// ciphertext below is what KuGouMusicApi's cryptoAesEncrypt makes of the same
// key and plaintext.
func TestSealHexMatchesClient(t *testing.T) {
	const key = "0123456789abcdef"
	const plain = `{"mobile":"13800000000","code":"123456"}`
	const want = "b79546a82d451f5199a800b32023d648b4194fdd4f667acbe03ad530cd88f42f6ec2b4a4525a700c2aed61950c845c9d"
	s, err := sealHexWith([]byte(plain), key)
	if err != nil {
		t.Fatal(err)
	}
	if s.Key != key || s.Body != want {
		t.Fatalf("sealHexWith = %q, want %q", s.Body, want)
	}
	back, err := openHex(want, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != plain {
		t.Fatalf("openHex gave %s, want %s", back, plain)
	}
}

// The login sends the number masked, its head and its last digit.
func TestMaskMobile(t *testing.T) {
	if got := maskMobile("13812345678"); got != "13*****8" {
		t.Fatalf("maskMobile = %q", got)
	}
	if isMobile("1381234567") || isMobile("23812345678") || !isMobile("13812345678") {
		t.Fatal("the number check is wrong")
	}
}

// The liked list is the one named for it, or the one KuGou flags.
func TestLooksLiked(t *testing.T) {
	if !LooksLiked(UserList{Name: "我喜欢的音乐"}) {
		t.Fatal("a list named for the liked songs is not the liked list")
	}
	if !LooksLiked(UserList{Default: true}) {
		t.Fatal("the flagged list is not the liked list")
	}
	if LooksLiked(UserList{Name: "跑步用", Type: 0, ID: 7}) {
		t.Fatal("a playlist of one's own is the liked list")
	}
}

// Each service names the album picture differently, and the size it leaves in
// the URL is what the app fills in; the same names elsewhere hold the
// singer's portrait, which is not the song's.
func TestCoverOf(t *testing.T) {
	cases := map[string]map[string]any{
		"search": {"Image": "http://imge.kugou.com/stdmusic/{size}/20230920/1.jpg", "SingerName": "周杰伦"},
		"daily":  {"sizable_cover": "http://imge.kugou.com/stdmusic/{size}/20261030/2.jpg"},
		"list":   {"trans_param": map[string]any{"union_cover": "http://imge.kugou.com/stdmusic/{size}/3.jpg"}},
		"rank":   {"audio_info": map[string]any{"hash_128": "ABC"}, "album_info": map[string]any{"sizable_cover": "http://imge.kugou.com/stdmusic/{size}/4.jpg"}},
	}
	want := map[string]string{
		"search": "https://imge.kugou.com/stdmusic/480/20230920/1.jpg",
		"daily":  "https://imge.kugou.com/stdmusic/480/20261030/2.jpg",
		"list":   "https://imge.kugou.com/stdmusic/480/3.jpg",
		"rank":   "https://imge.kugou.com/stdmusic/480/4.jpg",
	}
	for name, l := range cases {
		if got := coverOf(l); got != want[name] {
			t.Fatalf("%s: coverOf = %q, want %q", name, got, want[name])
		}
	}
	// A portrait of the singer is not a cover, and neither is nothing.
	if got := coverOf(map[string]any{"img": "http://imge.kugou.com/portrait/1.jpg"}); got != "" {
		t.Fatalf("a picture off KuGou's cover CDN was taken as the cover: %q", got)
	}
	if got := coverOf(map[string]any{"SingerName": "周杰伦"}); got != "" {
		t.Fatalf("a song without a picture got one: %q", got)
	}
}

// A refusal by number is the login services' way of saying no: status 0
// beside error_code 20006 is a wrong code, and the listener is owed the
// words for it rather than a blank failure.
func TestFailureReadsErrorCode(t *testing.T) {
	err := failure("/v7/login_by_verifycode", 200, []byte(`{"data":null,"status":0,"error_code":20006}`))
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("a refusal is not an *Error: %v", err)
	}
	if e.Code != 20006 {
		t.Fatalf("code = %d, want 20006", e.Code)
	}
	if e.Msg != codeText[20006] {
		t.Fatalf("message = %q, want %q", e.Msg, codeText[20006])
	}
	// An answer with status 1 says yes, whatever else it carries.
	if err := failure("/v2/search/song", 200, []byte(`{"status":1,"data":{}}`)); err != nil {
		t.Fatalf("status 1 was taken as a refusal: %v", err)
	}
}

// The 概念版 is another client: another app, another build and another salt,
// and each signs what it sends so the other's signature is refused.
func TestConceptSignsAsItsOwnClient(t *testing.T) {
	std, lite := &Client{}, &Client{Lite: true}
	if std.appID() == lite.appID() || std.clientVer() == lite.clientVer() {
		t.Fatalf("the two builds sign as the same app: %s/%s", lite.appID(), lite.clientVer())
	}
	if std.salt() == lite.salt() || std.keySalt() == lite.keySalt() {
		t.Fatal("the two builds sign with the same salt")
	}
	// The standard client is untouched by the concept client's arrival.
	params := map[string]string{"appid": appID, "clientver": clientVer, "mid": "1"}
	if got := signAndroidWith(params, "", std.salt()); got != signAndroid(params, "") {
		t.Fatalf("the standard signature changed: %s", got)
	}
	if signAndroidWith(params, "", lite.salt()) == signAndroid(params, "") {
		t.Fatal("the concept signature is the standard one")
	}
	// A key is salted per build too.
	if keyFor("hash", "mid", "1", appID) == keyForWith("hash", "mid", "1", appID, lite.keySalt()) {
		t.Fatal("the concept key is the standard one")
	}
	if std.signKey("1700000000") != signParamsKey("1700000000") {
		t.Fatal("the standard key changed")
	}
	if lite.signKey("1700000000") == signParamsKey("1700000000") {
		t.Fatal("the concept key is the standard one")
	}
}

// A playlist's file list calls a song's length "timelen" and its picture
// "cover"; the names the other services use found neither, so the rows of a
// playlist showed no time at all.
func TestPlaylistFileReadsLengthAndCover(t *testing.T) {
	l := map[string]any{
		"name":       "周杰伦 - 晴天",
		"singerinfo": []any{map[string]any{"name": "周杰伦"}},
		"timelen":    float64(269000),
		"cover":      "http://imge.kugou.com/stdmusic/{size}/20230920/20230920142503632013.jpg",
		"hash":       "B3A52A7A958BF0AED0EBFBA2E9A818B7",
	}
	tr := trackFromFile(l)
	if tr.Duration != 269000 {
		t.Fatalf("duration = %d, want 269000", tr.Duration)
	}
	if tr.Image != "https://imge.kugou.com/stdmusic/480/20230920/20230920142503632013.jpg" {
		t.Fatalf("picture = %q", tr.Image)
	}
	if tr.Hash != "b3a52a7a958bf0aed0ebfba2e9a818b7" {
		t.Fatalf("hash = %q", tr.Hash)
	}
}

// The 概念版 sells two subscriptions, and an account may hold both: each is
// kept with the day it ends rather than merged into one.
func TestProductsKeepTheirOwnDays(t *testing.T) {
	got := []Product{
		{Type: "svip", End: "2026-12-13 16:44:46"},
		{Type: "tvip", End: "2026-12-14 15:51:51"},
	}
	if got[0].Name() != "超级会员" || got[1].Name() != "豪华会员" {
		t.Fatalf("names = %q, %q", got[0].Name(), got[1].Name())
	}
	if !after(got[0].End) || !after(got[1].End) {
		t.Fatal("a date in the future read as past")
	}
	if after("2022-01-01 00:00:00") {
		t.Fatal("a date long past read as running")
	}
}

// A comment comes as the phone's song page serves it: its writer, when it
// was written in Beijing time, how many liked it, and the singer's reply
// under it, named apart from the account that wrote the comment.
func TestCommentReadsWriterAndReply(t *testing.T) {
	m := map[string]any{
		"id":        "944948805",
		"content":   "周柏豪说过:一辈子很短，爱一个人要抓紧。\n",
		"addtime":   "2022-10-14 21:23:01",
		"location":  "广东",
		"user_name": "不懂事",
		"user_pic":  "http://imge.kugou.com/kugouicon/165/20221010/20221010103209134093.jpg",
		"like":      map[string]any{"count": float64(5028)},
		"reply_num": float64(114),
		"replys": []any{map[string]any{
			"user_name":   "不懂事",
			"author_name": "周柏豪",
			"content":     "握不住的东西连伸手都是多余的",
			"addtime":     "2023-02-14 05:28:02",
		}},
	}
	cm := commentFrom(m)
	if cm.ID != 944948805 || cm.UserName != "不懂事" || cm.Likes != 5028 || cm.ReplyNum != 114 {
		t.Fatalf("comment = %+v", cm)
	}
	if cm.Content != "周柏豪说过:一辈子很短，爱一个人要抓紧。" {
		t.Fatalf("content = %q", cm.Content)
	}
	if cm.Location != "广东" {
		t.Fatalf("location = %q", cm.Location)
	}
	if got, want := cm.At.UTC().Format("2006-01-02 15:04"), "2022-10-14 13:23"; got != want {
		t.Fatalf("time = %s, want %s (Beijing time read as UTC)", got, want)
	}
	if !strings.HasPrefix(cm.UserPic, "https://") {
		t.Fatalf("picture = %q, want it over https", cm.UserPic)
	}
	if cm.Reply == nil || cm.Reply.UserName != "周柏豪" || cm.Reply.Content != "握不住的东西连伸手都是多余的" {
		t.Fatalf("reply = %+v", cm.Reply)
	}
}

// A song's row carries the year its album came out, and an album's song list
// says where the song sits on the album and on which disc: the download
// writes both into the file's tag.
func TestTrackReadsYearAndTrackNo(t *testing.T) {
	// A flat row, as a singer's songs and the search send them.
	flat := trackFrom(map[string]any{
		"SongName": "晴天", "SingerName": "周杰伦", "publish_date": "2003-07-31",
	})
	if flat.Year != "2003-07-31" {
		t.Errorf("flat year = %q, want 2003-07-31", flat.Year)
	}
	// A nested row, as an album's song list sends it: the date under the
	// album, and the number and the disc under "extend".
	nested := trackFromParts(map[string]any{
		"base":       map[string]any{"album_audio_id": float64(1), "album_id": float64(2), "audio_name": "晴天", "author_name": "周杰伦"},
		"audio_info": map[string]any{"hash": "ABC", "duration_128": float64(269000)},
		"album_info": map[string]any{"album_name": "叶惠美", "publish_date": "2003-07-31"},
		"copyright":  map[string]any{"privilege": float64(8)},
		"deprecated": map[string]any{"pay_type": float64(0)},
		"extend":     map[string]any{"sort": float64(3), "disc": float64(1)},
		"authors":    []any{map[string]any{"author_id": float64(7), "author_name": "周杰伦"}},
	})
	if nested.Year != "2003-07-31" || nested.TrackNo != 3 || nested.DiscNo != 1 {
		t.Errorf("nested = year %q, track %d, disc %d; want 2003-07-31, 3, 1", nested.Year, nested.TrackNo, nested.DiscNo)
	}
}

// A song's qualities come by the level KuGou names them by, not by their bit
// rate: a FLAC runs anywhere from 450 to 990kbps, and read by its number
// alone it passes for a 320 and is lost. The rate is kept for the rows that
// send no level.
func TestQualityGoodsByLevel(t *testing.T) {
	var tr Track
	tr.put("low", 2, 128)
	tr.put("high", 4, 320)
	tr.put("flac", 5, 680)
	tr.put("hires", 6, 468)
	if tr.Hash != "low" || tr.HashHigh != "high" || tr.HashLossl != "flac" || tr.HashHires != "hires" {
		t.Errorf("by level: standard %q, high %q, lossless %q, hires %q; want low, high, flac, hires",
			tr.Hash, tr.HashHigh, tr.HashLossl, tr.HashHires)
	}

	var old Track
	old.put("a", 0, 128)
	old.put("b", 0, 320)
	old.put("c", 0, 1000)
	old.put("d", 0, 2000)
	if old.Hash != "a" || old.HashHigh != "b" || old.HashLossl != "c" || old.HashHires != "d" {
		t.Errorf("by rate: standard %q, high %q, lossless %q, hires %q; want a, b, c, d",
			old.Hash, old.HashHigh, old.HashLossl, old.HashHires)
	}
}

// A playlist's row says what each of its songs was uploaded as, under the
// goods it was read from.
func TestFileRowKeepsQualities(t *testing.T) {
	tr := trackFromFile(map[string]any{
		"hash": "AAA",
		"relate_goods": []any{
			map[string]any{"hash": "AAA", "level": float64(2), "bitrate": float64(128)},
			map[string]any{"hash": "BBB", "level": float64(4), "bitrate": float64(320)},
			map[string]any{"hash": "CCC", "level": float64(5), "bitrate": float64(742)},
			map[string]any{"hash": "DDD", "level": float64(6), "bitrate": float64(955)},
		},
	})
	if tr.Hash != "aaa" || tr.HashHigh != "bbb" || tr.HashLossl != "ccc" || tr.HashHires != "ddd" {
		t.Errorf("qualities: standard %q, high %q, lossless %q, hires %q; want aaa, bbb, ccc, ddd",
			tr.Hash, tr.HashHigh, tr.HashLossl, tr.HashHires)
	}
}

// A song is the album audio it is, which both services number the same; the
// hashes they hold for it often differ, so the qualities go by that number
// first and by the hash only for a song the public list does not number.
func TestQualitiesGoByTheAlbumAudio(t *testing.T) {
	q := Qualities{
		byAudio: map[int64]qualities{7: {high: "h1"}},
		byHash:  map[string]qualities{"x": {high: "h2"}},
	}
	songs := []Track{
		{AlbumAudioID: 7, Hash: "x"},
		{AlbumAudioID: 8, Hash: "x"},
		{AlbumAudioID: 9, Hash: "y"},
	}
	q.Fill(songs)
	if songs[0].HashHigh != "h1" {
		t.Errorf("a numbered song = %q, want h1", songs[0].HashHigh)
	}
	if songs[1].HashHigh != "h2" {
		t.Errorf("a song without its number = %q, want h2", songs[1].HashHigh)
	}
	if songs[2].HashHigh != "" {
		t.Errorf("a song neither service named got %q", songs[2].HashHigh)
	}
}
