package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWaitForAuthentication(t *testing.T) {
	otp := authPage{URL: "https://example.test/auth/verify", Body: "ご本人さま確認 本人確認（ワンタイムパスワード） 送信"}
	success := authPage{URL: "https://example.test/app/", Body: "マイページ ログアウト"}
	for _, tc := range []struct {
		name      string
		pages     []authPage
		headless  bool
		wantError string
	}{
		{"normal", []authPage{{Login: true}, success}, false, ""},
		{"mail verification", []authPage{otp, {URL: otp.URL, OTP: true}, success}, false, ""},
		{"headless verification", []authPage{otp}, true, "headless を false"},
		{"access denied", []authPage{{Body: "Access Denied"}}, false, "アクセスが拒否"},
		{"denied during verification", []authPage{otp, {Body: "Access Denied"}}, false, "アクセスが拒否"},
		{"verification timeout", []authPage{otp}, false, "追加認証の待機時間"},
		{"blank is not success", []authPage{{URL: success.URL}}, false, "ログイン完了を確認できません"},
		{"untrusted origin", []authPage{{URL: "https://other.test/app/", Body: success.Body}}, false, "ログイン完了を確認できません"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			index := 0
			err := waitForAuthentication(context.Background(), Config{BillingURL: "https://example.test/app/statement/", Headless: tc.headless}, 20*time.Millisecond, 30*time.Millisecond, time.Millisecond,
				func(context.Context) (authPage, error) {
					p := tc.pages[index]
					if index < len(tc.pages)-1 {
						index++
					}
					return p, nil
				})
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
				if index != len(tc.pages)-1 {
					t.Fatal("returned before authentication completed")
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected %q, got %v", tc.wantError, err)
			}
		})
	}
}
