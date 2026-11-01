package kugou

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// device is what the service remembers about this install: the identity the
// player presents on every request. KuGou trusts a device it has seen before,
// so it is made once and kept in the user data directory, as the NetEase side
// keeps its device ID.
type device struct {
	GUID string `json:"guid"` // a v4 UUID, hashed: names the install
	MID  string `json:"mid"`  // GUID's MD5 read as a base-10 integer
	Dev  string `json:"dev"`  // a 10-character development device ID
	// DFID is what the risk service hands out when a device registers. The
	// player's URL endpoint refuses requests without it.
	DFID string `json:"dfid"`
	// MAC is the hardware address the 概念版 names when it signs in. This
	// machine has more than one; it only has to stay the same.
	MAC string `json:"mac"`
}

var deviceMu sync.Mutex

func loadDevice(dir string) (device, error) {
	path := filepath.Join(dir, "kugou-device.json")
	b, err := os.ReadFile(path)
	if err == nil {
		var d device
		if json.Unmarshal(b, &d) == nil && d.GUID != "" && d.MID != "" {
			return d, nil
		}
	}
	d := newDevice()
	if err := saveDevice(dir, d); err != nil {
		return d, err
	}
	return d, nil
}

func newDevice() device {
	guid := md5Hex(newGUID())
	return device{
		GUID: guid,
		MID:  midFromGUID(guid),
		Dev:  strings.ToUpper(randomKey(10)),
		MAC:  fakeMAC(),
	}
}

// fakeMAC makes a hardware address of the usual shape, from the install's own
// random key: the login service only compares it with the one it was shown.
func fakeMAC() string {
	k := strings.ToUpper(randomKey(12))
	parts := make([]string, 0, 6)
	for i := 0; i+2 <= len(k); i += 2 {
		parts = append(parts, k[i:i+2])
	}
	return strings.Join(parts, ":")
}

func saveDevice(dir string, d device) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "kugou-device.json"), b, 0o600)
}
