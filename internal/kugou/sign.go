package kugou

import (
	"sort"
	"strings"
)

// The salts the client signs with, from KuGouMusicApi's util/helper.js. Every
// signature is MD5(salt + sorted parameters + body + salt); only the salt and
// the order the parameters are joined in differ between the flavours.

const (
	saltAndroid     = "OIlwieks28dk2k092lksi2UIkp"
	saltAndroidLite = "LnT6xpN3khm36zse0QzvmgTZ3waWdRSA"
	saltWeb         = "NVPh5oo715z5DIWAeQlhMDsWXXQV4hwt"
	saltSignKey     = "57ae12eb6890223e355ccfcb74edf70d"
	saltSignKeyLite = "185672dd44712f60bb1736df5a377e82"
	saltRegister    = "1014"
)

// signAndroid is signatureAndroidParams: parameters sorted by name, joined key
// by key with an "=", then the request body, salted on both sides.
func signAndroid(params map[string]string, body string) string {
	return signAndroidWith(params, body, saltAndroid)
}

// signAndroidWith signs with the salt of a build: the standard client and the
// 概念版 sign the same way under different salts, and each refuses the other's.
func signAndroidWith(params map[string]string, body, salt string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(params[k])
	}
	return md5Hex(salt + b.String() + body + salt)
}

// signWeb is signatureWebParams: as signAndroid, but sorted as whole "key=value"
// strings rather than by name, and the H5 client puts a POST body in it too.
func signWeb(params map[string]string, body string) string {
	pairs := make([]string, 0, len(params))
	for k, v := range params {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return md5Hex(saltWeb + strings.Join(pairs, "") + body + saltWeb)
}

// signRegister is signatureRegisterParams: the values alone, sorted and joined,
// salted with "1014". The device registration is the only user.
func signRegister(params map[string]string) string {
	values := make([]string, 0, len(params))
	for _, v := range params {
		values = append(values, v)
	}
	sort.Strings(values)
	return md5Hex(saltRegister + strings.Join(values, "") + saltRegister)
}

// keyFor is signKey: the "key" parameter the player's URL endpoint wants, made
// from the song's hash and this device.
func keyFor(hash, mid, userid, appid string) string {
	return keyForWith(hash, mid, userid, appid, saltSignKey)
}

// keyForWith is keyFor under the salt of a build, as the 概念版 has its own.
func keyForWith(hash, mid, userid, appid, salt string) string {
	return md5Hex(hash + salt + appid + mid + userid)
}

// signParamsKey is signParamsKey: a "key" for the endpoints that want the app
// and the build it is as well as the moment it asked — the recommended songs,
// the radio, and both login services.
func signParamsKey(data string) string {
	return signParamsKeyFor(appID, clientVer, saltAndroid, data)
}

// signParamsKeyFor is signParamsKey for a named build: the 概念版 signs with
// its own app, version and salt, and the standard client's key is another.
func signParamsKeyFor(appid, ver, salt, data string) string {
	return md5Hex(appid + salt + ver + data)
}
