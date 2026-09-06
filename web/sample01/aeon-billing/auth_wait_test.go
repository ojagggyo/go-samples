package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
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

func TestAuthenticationBrowserSurvivesLoginTimeoutContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<body>ご本人さま確認 ワンタイムパスワード</body>"))
	}))
	defer server.Close()
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 60*time.Second)
	defer timeout()
	if err := chromedp.Run(ctx); err != nil {
		t.Fatal(err)
	}
	loginCtx, stopLogin := context.WithTimeout(ctx, 10*time.Second)
	if err := chromedp.Run(loginCtx, chromedp.Navigate(server.URL)); err != nil {
		stopLogin()
		t.Fatal(err)
	}
	stopLogin()
	// Simulate completion in the same browser after the login task has ended.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`setTimeout(() => { document.body.innerText = 'マイページ ログアウト'; }, 1500)`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := waitForLoginCompletion(ctx, Config{BillingURL: server.URL}); err != nil {
		t.Fatal(err)
	}
}
