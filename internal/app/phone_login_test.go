package app

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Signing in by text message: the number typed is the one the code is asked
// for, the code typed is the one traded for the session, and a refusal is
// shown rather than swallowed.
func TestPhoneLogin(t *testing.T) {
	asked := make(chan string, 2)
	sent := make(chan [2]string, 2)
	sendLoginCode = func(mobile string) error { asked <- mobile; return nil }
	loginWithCode = func(mobile, code string) (*netease.Profile, error) {
		sent <- [2]string{mobile, code}
		return nil, errors.New("kugou: 验证码错误")
	}
	defer func() {
		sendLoginCode, loginWithCode = netease.SendLoginCode, netease.LoginWithCode
	}()

	a := newTestApp()
	a.settings = defaultSettings()
	a.login = loginState{open: true, gen: 3, mode: 1}
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()

	if !tt.HasText("手机号") || !tt.HasText("获取验证码") {
		t.Fatalf("the phone form is not showing: %v", tt.Texts())
	}
	tt.Type("13800000000")
	tt.Frame()
	if a.login.phone != "13800000000" {
		t.Fatalf("the number typed became %q", a.login.phone)
	}
	if err := tt.Click("获取验证码"); err != nil {
		t.Fatal(err)
	}
	if got := <-asked; got != "13800000000" {
		t.Fatalf("asked for a code for %q", got)
	}
	a.codeSent(3, nil)
	if a.login.countdown != 60 {
		t.Fatalf("countdown is %d, want 60", a.login.countdown)
	}

	a.login.sms = "123456"
	tt.Frame()
	if err := tt.Click("验证码登录"); err != nil {
		t.Fatal(err)
	}
	if got := <-sent; got != [2]string{"13800000000", "123456"} {
		t.Fatalf("signed in as %q", got)
	}
	a.codeTried(3, nil, errors.New("kugou: 验证码错误"))
	if a.login.err != "验证码错误" {
		t.Fatalf("the refusal reads %q", a.login.err)
	}
	if a.login.finished {
		t.Fatal("a refused code counted as a login")
	}
}

// A number that is not one is refused before KuGou is asked.
func TestPhoneLoginChecksNumber(t *testing.T) {
	fired := false
	sendLoginCode = func(string) error { fired = true; return nil }
	defer func() { sendLoginCode = netease.SendLoginCode }()

	a := newTestApp()
	a.settings = defaultSettings()
	a.login = loginState{open: true, gen: 1, mode: 1, phone: "138000"}
	a.sendCode()
	if fired {
		t.Fatal("asked KuGou for a code for a number that is not one")
	}
	if a.login.err == "" {
		t.Fatal("the short number was not refused")
	}
}
