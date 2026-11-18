package kugou

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The listener's music cloud: the files they uploaded themselves, which
// KuGou keeps for the account and streams back like any other song. It is
// the same cloud the phone app and the standard player write to, so what is
// put there from here shows up there, and the other way round.
//
// The cloud service is sealed rather than signed: the body is AES under a
// key of the call's own, and the key travels RSA-encrypted in "p". The songs
// themselves are asked for by hash and audio id, as the catalogue's are, but
// from the cloud tracker and under the bucket "musicclound" — KuGou's
// spelling, not this file's.

const (
	cloudHost   = "https://mcloudservice.kugou.com"
	cloudBucket = "musicclound"
	cloudPID    = "20026"
	// cloudSalt is what the tracker's key is salted with.
	cloudSalt = "ebd1ac3134c880bda6a2194537843caa0162e2e7"
)

// CloudSong is one file of the music cloud: what it is called, who sings it,
// and the numbers the tracker and the delete take.
type CloudSong struct {
	// FileID is the file's number in the cloud, "kv_id" on the wire.
	FileID int64
	// AudioID and AlbumAudioID are the ids the tracker asks the file's
	// stream by.
	AudioID      int64
	AlbumAudioID int64
	// Hash is the file's hash, as the cloud stores it.
	Hash string
	// Name is the file's name in the cloud, extension and all, which the
	// tracker wants along with the hash.
	Name   string
	Ext    string
	Singer string
	Size   int64 // bytes
	// Duration is how long it plays, in milliseconds.
	Duration int64
	// AddTime is when it was uploaded, in Unix seconds.
	AddTime int64
}

// Title is the song's name: its file's, without the extension.
func (s CloudSong) Title() string {
	if s.Name == "" {
		return ""
	}
	return strings.TrimSuffix(s.Name, filepath.Ext(s.Name))
}

// Key is the tracker's signature over the file: the bucket, the hash, the
// pid and the salt it signs them with.
func cloudFileKey(hash, pid string) string {
	return md5Hex(cloudBucket + hash + pid + cloudSalt)
}

// CloudSongs returns a page of the cloud's songs and how many there are.
func CloudSongs(c *Client, page, size int) ([]CloudSong, int, error) {
	if !c.SignedIn() {
		return nil, 0, ErrNoSession
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 100
	}
	var res map[string]any
	err := cloudSealed(c, "/v1/get_list", map[string]any{
		"page": page, "pagesize": size, "getkmr": 1,
	}, &res)
	if err != nil {
		return nil, 0, err
	}
	out := cloudSongsFrom(res)
	return out, cloudTotal(res, len(out)), nil
}

// CloudURL returns where the cloud streams a file from. It is the tracker's
// answer, the same shape the catalogue's streams come in.
func CloudURL(c *Client, s CloudSong) (*Stream, error) {
	hash := strings.ToLower(strings.TrimSpace(s.Hash))
	if hash == "" {
		return nil, ErrUnavailable
	}
	name := s.Name
	if name == "" {
		name = s.Title() + "." + s.Ext
	}
	raw, err := c.retry("GET", call{
		Path: "/bsstrackercdngz/v2/query_musicclound_url",
		Params: map[string]string{
			"hash":           hash,
			"ssa_flag":       "is_fromtrack",
			"version":        "20102",
			"ssl":            "0",
			"album_audio_id": strconv.FormatInt(s.AlbumAudioID, 10),
			"pid":            cloudPID,
			"audio_id":       strconv.FormatInt(s.AudioID, 10),
			"kv_id":          "2",
			"key":            cloudFileKey(hash, cloudPID),
			"bucket":         cloudBucket,
			"name":           name,
			"with_res_tag":   "0",
		},
	})
	if err != nil {
		return nil, err
	}
	return cloudStream(raw, s)
}

// cloudStream reads the tracker's answer: where the file is, how big it is
// and what it is, in whichever of the shapes the service sends. The address
// rides in "data" with the name "url", the size as a number or a string, and
// the format as "ext" or "extName".
func cloudStream(raw []byte, s CloudSong) (*Stream, error) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("kugou: the cloud's answer is not JSON: %w", err)
	}
	url := firstURL(cloudValue(body, "url", "urls"))
	if url == "" {
		return nil, ErrUnavailable
	}
	size := cloudNumber(cloudValue(body, "fileSize", "file_size", "size"))
	if size == 0 {
		size = s.Size
	}
	ext := cloudText(cloudValue(body, "ext", "extName", "extname", "format"))
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		ext = strings.TrimPrefix(strings.ToLower(s.Ext), ".")
	}
	kind := ""
	switch ext {
	case "mp3":
		kind = "mp3"
	case "flac":
		kind = "flac"
	case "m4a", "aac":
		kind = "m4a"
	case "wav":
		kind = "wav"
	case "ogg":
		kind = "ogg"
	}
	return &Stream{URL: url, Size: size, Type: kind, Level: cloudText(cloudValue(body, "level"))}, nil
}

// cloudValue finds the first of keys the answer holds, at its top or in the
// "data" it wraps everything in.
func cloudValue(body map[string]any, keys ...string) any {
	for _, scope := range []map[string]any{body, dataOf(body)} {
		if scope == nil {
			continue
		}
		for _, k := range keys {
			if v, ok := scope[k]; ok && v != nil {
				return v
			}
		}
	}
	return nil
}

// dataOf returns the object an answer wrapped its result in, or nil.
func dataOf(body map[string]any) map[string]any {
	if d, ok := body["data"].(map[string]any); ok {
		return d
	}
	return nil
}

// cloudText reads a string out of a value that may be a string or a number.
func cloudText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// cloudNumber reads a number out of a value that may be a number or a string
// of digits, as the tracker sends sizes.
func cloudNumber(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	}
	return 0
}

// firstURL reads a stream's address out of a value that may be the address
// itself, a list of them, or a list of objects that hold one.
func firstURL(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		for _, item := range x {
			switch it := item.(type) {
			case string:
				if it != "" {
					return it
				}
			case map[string]any:
				if u, ok := it["url"].(string); ok && u != "" {
					return u
				}
			}
		}
	case map[string]any:
		if u, ok := x["url"].(string); ok {
			return u
		}
	}
	return ""
}

// CloudDelete takes the songs out of the cloud. A song that is not there is
// not an error: the file is gone either way.
func CloudDelete(c *Client, songs []CloudSong) error {
	if !c.SignedIn() {
		return ErrNoSession
	}
	if len(songs) == 0 {
		return nil
	}
	data := make([]map[string]any, 0, len(songs))
	for _, s := range songs {
		data = append(data, map[string]any{
			"kv_id":          s.FileID,
			"album_audio_id": s.AlbumAudioID,
		})
	}
	var res map[string]any
	return cloudSealed(c, "/v1/del_files", map[string]any{"data": data}, &res)
}

// CloudFile is a file to upload: where it is, and what it says it is. The
// name and the singer become the song's credits in the cloud; the duration
// is what the row shows before it plays.
type CloudFile struct {
	Path     string
	Name     string
	Singer   string
	Duration int64 // milliseconds
	Bitrate  int
}

// CloudUpload uploads a file to the cloud and returns the song it made.
//
// It goes in five steps, as the client's does: an authorisation, an
// initiation that says whether the file is already there, the parts, the
// completion, and the entry in the cloud's list. A file the cloud already
// holds (it knows files by their MD5) finishes after the initiation, which
// is why the upload of a song that came from KuGou takes no time at all.
func CloudUpload(c *Client, f CloudFile) (CloudSong, error) {
	if !c.SignedIn() {
		return CloudSong{}, ErrNoSession
	}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return CloudSong{}, err
	}
	if len(data) == 0 {
		return CloudSong{}, fmt.Errorf("kugou: %s is empty", f.Path)
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(f.Path)), ".")
	if ext == "" {
		ext = "mp3"
	}
	sum := md5.Sum(data)
	filename := hex.EncodeToString(sum[:])
	name := f.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path))
	}
	file := f.Singer
	if file != "" {
		file += " - "
	}
	file += name + "." + ext

	auth, err := c.cloudAuthorize(filename)
	if err != nil {
		return CloudSong{}, err
	}
	host, uploadID, hash, err := c.cloudInitiate(filename, ext, auth)
	if err != nil {
		return CloudSong{}, err
	}
	if uploadID != "" {
		if err := c.cloudParts(host, auth, filename, uploadID, data); err != nil {
			return CloudSong{}, err
		}
		hash, err = c.cloudComplete(host, auth, filename, uploadID, len(data))
		if err != nil {
			return CloudSong{}, err
		}
	}
	if hash == "" {
		hash = filename
	}
	title := name
	if title == "" {
		title = strings.TrimSuffix(file, "."+ext)
	}
	var res map[string]any
	err = cloudSealed(c, "/v1/add_files", map[string]any{
		"data": []map[string]any{{
			"name":           file,
			"ext":            ext,
			"author_name":    f.Singer,
			"hash":           hash,
			"hash_std":       filename,
			"audio_id":       0,
			"bitrate":        orInt(f.Bitrate, 4),
			"album_audio_id": 0,
			"size":           len(data),
			"timelen":        f.Duration,
		}},
		"list_ver": 0,
	}, &res)
	if err != nil {
		return CloudSong{}, err
	}
	return CloudSong{
		Hash: hash, Name: file, Ext: ext, Singer: f.Singer,
		Size: int64(len(data)), Duration: f.Duration, AddTime: now(),
	}, nil
}

// cloudAuthorize asks for the authorisation the rest of the upload carries.
func (c *Client) cloudAuthorize(filename string) (string, error) {
	loginType := "0"
	if c.SignedIn() {
		loginType = "1"
	}
	params := c.cloudUploadParams(map[string]string{
		"bucket":       cloudBucket,
		"filename":     filename,
		"method":       "POST",
		"loginType":    loginType,
		"buVerifyCode": md5Hex(c.appID() + cloudBucket + "8ae10344e9738dcb"),
		"extranet":     "1",
		"version":      c.clientVer(),
	})
	var res struct {
		Status int `json:"status"`
		Data   struct {
			Authorization string `json:"authorization"`
		} `json:"data"`
	}
	err := c.decode("GET", call{
		Path:   "/bsstrackercdngz/v1/upload/auth",
		Params: params,
		Bare:   true,
		NoSign: true,
	}, &res)
	if err != nil {
		return "", err
	}
	if res.Data.Authorization == "" {
		return "", fmt.Errorf("kugou: the cloud gave no authorisation")
	}
	return res.Data.Authorization, nil
}

// cloudInitiate starts the upload, and says whether the cloud already has
// the file: an upload id means it does not, and the parts have to come.
func (c *Client) cloudInitiate(filename, ext, auth string) (host, uploadID, hash string, err error) {
	params := c.cloudUploadParams(map[string]string{
		"bucket":        cloudBucket,
		"filename":      filename,
		"ssl":           "1",
		"extendname":    ext,
		"version":       c.clientVer(),
		"authorization": auth,
	})
	var res struct {
		Status int `json:"status"`
		Data   struct {
			ExternalHost string `json:"external_host"`
			UploadID     string `json:"upload_id"`
			BSSFilename  string `json:"x-bss-filename"`
		} `json:"data"`
	}
	err = c.decode("POST", call{
		Path:   "/v2/multipart/initiate/music",
		Base:   "http://bssulbig.kugou.com",
		Params: params,
		Bare:   true,
		NoSign: true,
	}, &res)
	if err != nil {
		return "", "", "", err
	}
	host = withProtocol(res.Data.ExternalHost)
	uploadID = res.Data.UploadID
	hash = res.Data.BSSFilename
	if uploadID != "" && host == "" {
		return "", "", "", fmt.Errorf("kugou: the cloud gave no host to upload to")
	}
	return host, uploadID, hash, nil
}

// cloudParts sends the file in pieces of a megabyte, as the client does.
func (c *Client) cloudParts(host, auth, filename, uploadID string, data []byte) error {
	const partSize = 1 << 20
	parts := (len(data) + partSize - 1) / partSize
	if parts == 0 {
		parts = 1
	}
	for i := 0; i < parts; i++ {
		end := min((i+1)*partSize, len(data))
		part := data[i*partSize : end]
		params := c.cloudUploadParams(map[string]string{
			"bucket":        cloudBucket,
			"authorization": auth,
			"filename":      filename,
			"partnumber":    strconv.Itoa(i + 1),
			"upload_id":     uploadID,
			"body_empty":    "1",
			"version":       c.clientVer(),
		})
		var res struct {
			Status int `json:"status"`
		}
		err := c.decode("POST", call{
			Path:   "/v3/multipart/upload",
			Base:   host,
			Params: params,
			Body:   string(part),
			Type:   "application/octet-stream",
			Hdr:    map[string]string{"Authorization": auth},
			Bare:   true,
			NoSign: true,
		}, &res)
		if err != nil {
			return err
		}
		if res.Status != 1 {
			return fmt.Errorf("kugou: part %d of %d was refused", i+1, parts)
		}
	}
	return nil
}

// cloudComplete closes the upload and answers the hash the cloud filed the
// file under, which the tracker asks for by.
func (c *Client) cloudComplete(host, auth, filename, uploadID string, size int) (string, error) {
	parts := (size + (1 << 20) - 1) / (1 << 20)
	if parts == 0 {
		parts = 1
	}
	params := c.cloudUploadParams(map[string]string{
		"bucket":        cloudBucket,
		"authorization": auth,
		"filename":      filename,
		"partnumber":    strconv.Itoa(parts),
		"upload_id":     uploadID,
		"md5":           filename,
		"if_id3":        "1",
		"version":       c.clientVer(),
	})
	var res struct {
		Status int `json:"status"`
		Data   struct {
			BSSFilename string `json:"x-bss-filename"`
		} `json:"data"`
	}
	err := c.decode("POST", call{
		Path:   "/v3/multipart/complete",
		Base:   host,
		Params: params,
		Hdr:    map[string]string{"Authorization": auth},
		Bare:   true,
		NoSign: true,
	}, &res)
	if err != nil {
		return "", err
	}
	if res.Status != 1 {
		return "", fmt.Errorf("kugou: the cloud did not finish the upload")
	}
	if res.Data.BSSFilename != "" {
		return res.Data.BSSFilename, nil
	}
	return filename, nil
}

// cloudUploadParams is what every step of the upload sends, signed the way
// that service checks it: the device, the build, the account, and whatever
// the step itself adds.
func (c *Client) cloudUploadParams(extra map[string]string) map[string]string {
	params := map[string]string{
		"dfid":       c.Dev.DFID,
		"mid":        c.Dev.MID,
		"uuid":       "-",
		"appid":      c.appID(),
		"clientver":  c.clientVer(),
		"clienttime": strconv.FormatInt(now(), 10),
		"userid":     strconv.FormatInt(c.UserID, 10),
		"token":      c.Token,
	}
	for k, v := range extra {
		params[k] = v
	}
	params["signature"] = signAndroidWith(params, "", c.salt())
	return params
}

// cloudSealed is one call to the cloud service: the body is AES-encrypted
// under a key of this call's own, the key travels RSA-encrypted in "p", and
// the answer comes back the same way. It is what the list, the delete and
// the entry of an upload are.
func cloudSealed(c *Client, path string, body any, out any) error {
	if !c.SignedIn() {
		return ErrNoSession
	}
	aes := strings.ToLower(randomKey(6))
	sum := md5Hex(aes)
	plain, err := json.Marshal(body)
	if err != nil {
		return err
	}
	cipher, err := aesCBCEncrypt(plain, []byte(sum[:16]), []byte(sum[16:32]))
	if err != nil {
		return err
	}
	p, err := c.rsaSealPKCS1Key([]byte("{" + quote("aes") + ":" + quote(aes) +
		"," + quote("uid") + ":" + quote(strconv.FormatInt(c.UserID, 10)) +
		"," + quote("token") + ":" + quote(c.Token) + "}"))
	if err != nil {
		return err
	}
	ct := strconv.FormatInt(now(), 10)
	raw, err := c.do("POST", call{
		Path: path,
		Base: cloudHost,
		Params: map[string]string{
			"clienttime": ct,
			"mid":        c.Dev.MID,
			"key":        c.signKey(ct),
			"clientver":  c.clientVer(),
			"appid":      c.appID(),
			"p":          strings.ToUpper(p),
		},
		Body:   string(cipher),
		Type:   "application/octet-stream",
		Raw:    true,
		Bare:   true,
		NoSign: true,
	})
	if err != nil {
		return err
	}
	return json.Unmarshal(cloudOpen(raw, sum), out)
}

// cloudOpen opens the cloud's answer, which is the AES ciphertext of the
// JSON, and sometimes the JSON itself: a refusal, for one, comes in clear.
func cloudOpen(raw []byte, sum string) []byte {
	key, iv := []byte(sum[:16]), []byte(sum[16:32])
	if plain, err := aesCBCDecrypt(raw, key, iv); err == nil && json.Valid(plain) {
		return plain
	}
	if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); err == nil {
		if plain, err := aesCBCDecrypt(decoded, key, iv); err == nil && json.Valid(plain) {
			return plain
		}
	}
	return raw
}

// cloudSongsFrom reads the songs of the cloud out of whatever the list
// service wrapped them in.
func cloudSongsFrom(body map[string]any) []CloudSong {
	for _, v := range []any{body, body["data"]} {
		list := firstArray(v, "list", "lists", "info", "songs", "files")
		if len(list) == 0 {
			continue
		}
		out := make([]CloudSong, 0, len(list))
		for _, l := range list {
			s := CloudSong{
				FileID:       pickInt(l, "fileid", "file_id", "kv_id", "id"),
				AudioID:      pickInt(l, "audio_id", "audioId"),
				AlbumAudioID: pickInt(l, "album_audio_id", "mixsongid", "mix_id", "mixid"),
				Hash:         pickString(l, "hash", "hash_std"),
				Name:         pickString(l, "filename", "file_name", "name", "audio_name"),
				Ext:          pickString(l, "ext", "extname", "suffix", "format"),
				Singer:       pickString(l, "author_name", "singername", "singer_name", "artist"),
				Size:         pickInt(l, "filesize", "file_size", "size"),
				Duration:     pickInt(l, "timelen", "duration", "time_length"),
				AddTime:      pickInt(l, "addtime", "add_time", "create_time"),
			}
			if s.Name == "" {
				s.Name = pickString(l, "songname", "ori_audio_name", "audio_name")
			}
			if s.Title() == "" && s.Hash == "" {
				continue
			}
			if s.Duration > 0 && s.Duration < 1000 {
				// Some rows count in seconds, most in milliseconds.
				s.Duration *= 1000
			}
			out = append(out, s)
		}
		return out
	}
	return nil
}

// cloudTotal is how many songs the cloud holds, as the answer said, and the
// ones read when it did not say.
func cloudTotal(body map[string]any, read int) int {
	for _, v := range []any{body, body["data"]} {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"total", "count", "total_count"} {
			if n := pickInt(m, key); n > 0 {
				return int(n)
			}
		}
	}
	return read
}

// withProtocol puts a scheme in front of a host that came without one.
func withProtocol(host string) string {
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	return "http://" + host
}

// orInt is n, or d when n is zero.
func orInt(n, d int) int {
	if n == 0 {
		return d
	}
	return n
}
