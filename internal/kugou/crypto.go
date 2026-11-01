package kugou

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
)

// The primitives KuGou's client uses, ported from KuGouMusicApi's
// util/crypto.js and util/util.js. Signing is plain MD5 with a salt; only
// the device registration and the cloud playlist service need AES and RSA.

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// midFromGUID is calculateMid: the hex digest read as a base-16 integer and
// printed in base 10, which is the device MID the client sends.
func midFromGUID(guid string) string {
	n, ok := new(big.Int).SetString(md5Hex(guid), 16)
	if !ok {
		return "0"
	}
	return n.String()
}

// randomKey returns n characters of the pool the client draws device
// identifiers from: digits and capitals, no I/O confusion beyond that.
func randomKey(n int) string {
	const pool = "1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	for i := range b {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(pool))))
		if err != nil {
			b[i] = pool[0]
			continue
		}
		b[i] = pool[v.Int64()]
	}
	return string(b)
}

// newGUID is getGuid: a v4 UUID, whose MD5 names this install.
func newGUID() string {
	e := func() string {
		var b [2]byte
		rand.Read(b[:])
		return hex.EncodeToString(b[:])
	}
	return e() + e() + "-" + e() + "-" + e() + "-" + e() + "-" + e() + e() + e()
}

func pkcs7Pad(b []byte, size int) []byte {
	n := size - len(b)%size
	return append(b, bytes.Repeat([]byte{byte(n)}, n)...)
}

func pkcs7Unpad(b []byte, size int) ([]byte, error) {
	if len(b) == 0 || len(b)%size != 0 {
		return nil, errors.New("kugou: ciphertext is not a whole number of blocks")
	}
	n := int(b[len(b)-1])
	if n == 0 || n > size || n > len(b) {
		return nil, errors.New("kugou: bad padding")
	}
	for _, c := range b[len(b)-n:] {
		if int(c) != n {
			return nil, errors.New("kugou: bad padding")
		}
	}
	return b[:len(b)-n], nil
}

func aesCBCEncrypt(plain, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain = pkcs7Pad(plain, block.BlockSize())
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plain)
	return out, nil
}

func aesCBCDecrypt(sealed, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) == 0 || len(sealed)%block.BlockSize() != 0 {
		return nil, errors.New("kugou: ciphertext is not a whole number of blocks")
	}
	out := make([]byte, len(sealed))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, sealed)
	return pkcs7Unpad(out, block.BlockSize())
}

// sealed is what the client calls the pair a small random key makes: the key
// itself, and the base64 ciphertext under the AES key and IV derived from it.
type sealed struct {
	Key  string // 6 characters, as the client draws them
	Body string // base64
}

// seal is playlistAesEncrypt: a fresh 6-character key per request, MD5'd to
// give the AES key (first 16 hex characters) and IV (the next 16).
func seal(plain []byte) (sealed, error) {
	key := strings.ToLower(randomKey(6))
	sum := md5Hex(key)
	out, err := aesCBCEncrypt(plain, []byte(sum[:16]), []byte(sum[16:32]))
	if err != nil {
		return sealed{}, err
	}
	return sealed{Key: key, Body: base64.StdEncoding.EncodeToString(out)}, nil
}

// sealFixed encrypts plain under a key and IV the client keeps, in hex as the
// login services take it: the 概念版 seals two of its parameters this way,
// under keys that never change.
func sealFixed(plain, key, iv string) string {
	out, err := aesCBCEncrypt([]byte(plain), []byte(key), []byte(iv))
	if err != nil {
		return ""
	}
	return hex.EncodeToString(out)
}

// open undoes seal.
func open(body string, key string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, err
	}
	return openRaw(raw, key)
}

// openRaw undoes seal on bytes that never went through base64, as the risk
// service's answer comes.
func openRaw(body []byte, key string) ([]byte, error) {
	sum := md5Hex(key)
	return aesCBCDecrypt(body, []byte(sum[:16]), []byte(sum[16:32]))
}

// sealHex is cryptoAesEncrypt as the login services take it: a fresh
// 16-character key per request, and the ciphertext in hex rather than base64.
// Its AES key is all 32 characters of that key's MD5, so AES-256, with their
// last 16 as the IV. The 16-character key of a playlist's AES is another
// scheme, and the login services read nothing written with it.
func sealHex(plain []byte) (sealed, error) {
	return sealHexWith(plain, strings.ToLower(randomKey(16)))
}

// sealHexWith seals plain under a key of the caller's, which only a test
// wants: the login draws a new one per request.
func sealHexWith(plain []byte, key string) (sealed, error) {
	sum := md5Hex(key)
	out, err := aesCBCEncrypt(plain, []byte(sum), []byte(sum[16:32]))
	if err != nil {
		return sealed{}, err
	}
	return sealed{Key: key, Body: hex.EncodeToString(out)}, nil
}

func openHex(body string, key string) ([]byte, error) {
	raw, err := hex.DecodeString(body)
	if err != nil {
		return nil, err
	}
	sum := md5Hex(key)
	return aesCBCDecrypt(raw, []byte(sum), []byte(sum[16:32]))
}

// The public key the client registers its device with.
const registerPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDIAG7QOELSYoIJvTFJhMpe1s/g
bjDJX51HBNnEl5HXqTW6lQ7LC8jr9fWZTwusknp+sVGzwd40MwP6U5yDE27M/X1+
UR4tvOGOqp94TJtQ1EPnWGWXngpeIW5GxoQGao1rmYWAu6oi1z9XkChrsUdC6DJE
5E221wf/4WLFxwAtRQIDAQAB
-----END PUBLIC KEY-----`

// The 概念版's public key: the lite client encrypts the same parameters under
// a key of its own, and the login service refuses the standard client's.
const litePublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDECi0Np2UR87scwrvTr72L6oO01rBb
bBPriSDFPxr3Z5syug0O24QyQO8bg27+0+4kBzTBTBOZ/WWU0WryL1JSXRTXLgFVxtzI
Y41Pe7lPOgsfTCn5kZcvKhYKJesKnnJDNr5/abvTGf+rHG3YRwsCHcQ08/q6ifSioBs
zvb3QiwIDAQAB
-----END PUBLIC KEY-----`

var (
	registerKey *rsa.PublicKey
	liteKey     *rsa.PublicKey
)

// rsaSeal encrypts data with the client's public key, PKCS#1 v1.5, and
// returns it in lowercase hex, as the device's "p" travels.
func rsaSeal(data []byte) (string, error) {
	key, err := registrationKey()
	if err != nil {
		return "", err
	}
	out, err := rsa.EncryptPKCS1v15(rand.Reader, key, data)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(out), nil
}

// rsaSealRaw is cryptoRSAEncrypt: the same key, but the plaintext is padded
// with leading zeros rather than PKCS#1 and raised to the public exponent
// directly. The login services check the "p" and "pk" parameters that way,
// so they are built here rather than with rsaSeal.
func rsaSealRaw(data []byte) (string, error) {
	key, err := registrationKey()
	if err != nil {
		return "", err
	}
	return rsaSealWith(key, data)
}

// rsaSealRawKey is rsaSealRaw under the key of the build this client is.
func (c *Client) rsaSealRawKey(data []byte) (string, error) {
	key, err := registrationKey()
	if c.Lite {
		key, err = liteSigningKey()
	}
	if err != nil {
		return "", err
	}
	return rsaSealWith(key, data)
}

// rsaSealWith raises the padded plaintext to the key's exponent: the
// services check "p" and "pk" this way, without PKCS#1.
func rsaSealWith(key *rsa.PublicKey, data []byte) (string, error) {
	size := (key.N.BitLen() + 7) / 8
	if len(data) > size {
		return "", errors.New("kugou: the payload is bigger than the key")
	}
	padded := make([]byte, size)
	copy(padded[size-len(data):], data)
	m := new(big.Int).SetBytes(padded)
	c := new(big.Int).Exp(m, big.NewInt(int64(key.E)), key.N)
	out := c.Bytes()
	hexed := hex.EncodeToString(out)
	for len(hexed) < size*2 {
		hexed = "0" + hexed
	}
	return strings.ToUpper(hexed), nil
}

// liteSigningKey is the public key the 概念版 encrypts its parameters with.
func liteSigningKey() (*rsa.PublicKey, error) {
	if liteKey != nil {
		return liteKey, nil
	}
	key, err := parsePublicKey(litePublicKey, "the concept client's key")
	if err != nil {
		return nil, err
	}
	liteKey = key
	return key, nil
}

// parsePublicKey reads a PEM public key.
func parsePublicKey(pemText, what string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("kugou: " + what + " does not parse")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("kugou: " + what + " is not RSA")
	}
	return key, nil
}

func registrationKey() (*rsa.PublicKey, error) {
	if registerKey != nil {
		return registerKey, nil
	}
	block, _ := pem.Decode([]byte(registerPublicKey))
	if block == nil {
		return nil, errors.New("kugou: the registration key does not parse")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("kugou: the registration key is not RSA")
	}
	registerKey = key
	return key, nil
}

// rsaSealPKCS1Key is rsaSealPKCS1 under the key of the build this client is.
func (c *Client) rsaSealPKCS1Key(data []byte) (string, error) {
	key, err := registrationKey()
	if c.Lite {
		key, err = liteSigningKey()
	}
	if err != nil {
		return "", err
	}
	out, err := rsa.EncryptPKCS1v15(rand.Reader, key, data)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(out), nil
}

// rsaSealPKCS1 is rsaEncrypt2: the same key with PKCS#1 v1.5 padding, which
// the cloud playlist service's "p" parameter is made with.
func rsaSealPKCS1(data []byte) (string, error) {
	key, err := registrationKey()
	if err != nil {
		return "", err
	}
	out, err := rsa.EncryptPKCS1v15(rand.Reader, key, data)
	if err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(out)), nil
}

// krcKey is the 16-byte key KRC lyrics are XOR'd with.
var krcKey = [16]byte{64, 71, 97, 119, 94, 50, 116, 71, 81, 54, 49, 45, 206, 210, 110, 105}

// DecodeKRC turns the base64 KRC lyrics the lyrics service returns into the
// plain text the format holds: a 4-byte header, then the body XOR'd with a
// 16-byte key and deflated. Word timing survives, so a KRC is this app's YRC.
func DecodeKRC(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", err
	}
	if len(raw) <= 4 {
		return "", errors.New("kugou: the lyrics are too short to decode")
	}
	body := raw[4:]
	for i := range body {
		body[i] ^= krcKey[i%len(krcKey)]
	}
	zr, err := zlib.NewReader(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("kugou: the lyrics do not inflate: %w", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		return "", fmt.Errorf("kugou: the lyrics do not inflate: %w", err)
	}
	return string(out), nil
}
