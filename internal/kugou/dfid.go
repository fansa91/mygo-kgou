package kugou

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// KuGou refuses the player's URL endpoint to a device it has not registered:
// it answers "本次请求需要验证" instead of a stream. Registering is a POST of a
// device fingerprint, sealed with a key the answer is sealed with too, and the
// "p" parameter carries that key to the risk service, RSA-encrypted.

const registerPath = "/risk/v3/r_register_dev"

// fingerprint is the device KuGou's risk service is told about. The values are
// the ones KuGouMusicApi registers with: a phone that looks like any other.
type fingerprint struct {
	AvailableRamSize   int64  `json:"availableRamSize"`
	AvailableRomSize   int64  `json:"availableRomSize"`
	AvailableSDSize    int64  `json:"availableSDSize"`
	BasebandVer        string `json:"basebandVer"`
	BatteryLevel       int    `json:"batteryLevel"`
	BatteryStatus      int    `json:"batteryStatus"`
	Brand              string `json:"brand"`
	BuildSerial        string `json:"buildSerial"`
	Device             string `json:"device"`
	IMEI               string `json:"imei"`
	IMSI               string `json:"imsi"`
	Manufacturer       string `json:"manufacturer"`
	UUID               string `json:"uuid"`
	Accelerometer      bool   `json:"accelerometer"`
	AccelerometerValue string `json:"accelerometerValue"`
	Gravity            bool   `json:"gravity"`
	GravityValue       string `json:"gravityValue"`
	Gyroscope          bool   `json:"gyroscope"`
	GyroscopeValue     string `json:"gyroscopeValue"`
	Light              bool   `json:"light"`
	LightValue         string `json:"lightValue"`
	Magnetic           bool   `json:"magnetic"`
	MagneticValue      string `json:"magneticValue"`
	Orientation        bool   `json:"orientation"`
	OrientationValue   string `json:"orientationValue"`
	Pressure           bool   `json:"pressure"`
	PressureValue      string `json:"pressureValue"`
	StepCounter        bool   `json:"step_counter"`
	StepCounterValue   string `json:"step_counterValue"`
	Temperature        bool   `json:"temperature"`
	TemperatureValue   string `json:"temperatureValue"`
}

func (c *Client) fingerprint() fingerprint {
	return fingerprint{
		AvailableRamSize: 4983533568,
		AvailableRomSize: 48114719,
		AvailableSDSize:  48114717,
		BatteryLevel:     100,
		BatteryStatus:    3,
		Brand:            "Redmi",
		BuildSerial:      "unknown",
		Device:           "marble",
		IMEI:             c.Dev.GUID,
		Manufacturer:     "Xiaomi",
		UUID:             c.Dev.GUID,
	}
}

// Register asks the risk service for this device's DFID, keeping it in the
// user data directory for the next run. Registering twice is harmless but
// needless, so it only happens while the device has no DFID.
func (c *Client) Register() error {
	deviceMu.Lock()
	defer deviceMu.Unlock()
	if c.Dev.DFID != "" {
		return nil
	}

	plain, err := json.Marshal(c.fingerprint())
	if err != nil {
		return err
	}
	s, err := seal(plain)
	if err != nil {
		return err
	}
	// The risk service is handed the key the body was sealed with, so it can
	// answer in kind. JS writes {aes, uid, token} in that order.
	portrait := struct {
		AES   string `json:"aes"`
		UID   int64  `json:"uid"`
		Token string `json:"token"`
	}{AES: s.Key, UID: c.UserID, Token: c.Token}
	p, err := rsaSeal([]byte(fmt.Sprintf(`{"aes":%q,"uid":%d,"token":%q}`, portrait.AES, portrait.UID, portrait.Token)))
	if err != nil {
		return err
	}

	raw, err := c.do("POST", call{
		Base:   "https://userservice.kugou.com",
		Path:   registerPath,
		Body:   s.Body,
		Params: map[string]string{"part": "1", "platid": "1", "p": p},
		Raw:    true,
	})
	if err != nil {
		return err
	}
	text, err := openRaw(raw, s.Key)
	if err != nil {
		// Some versions answer plain JSON; take it as it comes.
		text = raw
	}
	var res struct {
		Status int `json:"status"`
		Data   struct {
			DFID string `json:"dfid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(text, &res); err != nil {
		return fmt.Errorf("kugou: registering the device: %w (answer %s)", err, snippet(text))
	}
	if res.Status != 1 || res.Data.DFID == "" {
		return errors.New("kugou: registering the device: the risk service said no")
	}
	c.Dev.DFID = res.Data.DFID
	return saveDevice(c.Dir, c.Dev)
}

// EnsureDevice registers this install's device if it never was, which the
// search and the player's URL endpoint both insist on before they answer.
func (c *Client) EnsureDevice() error { return c.ensureDFID() }

// ensureDFID registers the device the first time something needs it.
func (c *Client) ensureDFID() error {
	if c.Dev.DFID != "" {
		return nil
	}
	return c.Register()
}

// DFID is the device fingerprint KuGou issued this install, if any.
func (c *Client) DFID() string { return c.Dev.DFID }

// SetUser gives the client a signed-in listener, whose token and ID every
// request then carries.
func (c *Client) SetUser(userid int64, token string) {
	c.UserID = userid
	c.Token = token
}

// SetVIP says what subscription the account holds, which the tracker reads
// before it serves a song it sells.
func (c *Client) SetVIP(vipType int, vipToken string) {
	c.VIPType = vipType
	c.VIPToken = vipToken
}

// String shows what this client is, for logs.
func (c *Client) String() string {
	return "kugou device mid=" + c.Dev.MID + " dfid=" + strconv.Quote(c.Dev.DFID)
}

// snippet shows the start of what a service answered, for a failure nobody can
// see otherwise: the registration answers in bytes, not JSON.
func snippet(b []byte) string {
	if len(b) > 160 {
		b = b[:160]
	}
	return fmt.Sprintf("%q", b)
}
