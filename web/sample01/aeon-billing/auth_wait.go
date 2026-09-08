package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

type authPage struct {
	URL   string `json:"url"`
	Body  string `json:"body"`
	OTP   bool   `json:"otp"`
	Login bool   `json:"login"`
}

func (p authPage) needsOTP() bool {
	return p.OTP || (strings.Contains(p.Body, "ワンタイムパスワード") &&
		(strings.Contains(p.Body, "ご本人さま確認") || strings.Contains(p.Body, "本人確認")))
}

func (p authPage) authenticated(billingURL string) bool {
	u, err := url.Parse(p.URL)
	if err != nil {
		return false
	}
	b, err := url.Parse(billingURL)
	if err != nil {
		return false
	}
	// Require a positive logged-in indicator on the application origin. A
	// changed URL or disappearance of the OTP field alone is not completion.
	return u.Scheme == b.Scheme && u.Host == b.Host &&
		!strings.Contains(u.Path, "/auth/") && !p.Login && !p.needsOTP() &&
		strings.Contains(p.Body, "ログアウト")
}

func waitForLoginCompletion(ctx context.Context, cfg Config) error {
	return waitForAuthentication(ctx, cfg, 30*time.Second, 5*time.Minute, time.Second,
		func(ctx context.Context) (authPage, error) {
			var p authPage
			err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				const visible = e => e.getClientRects().length > 0 && getComputedStyle(e).visibility !== 'hidden';
				return {url: location.href, body: document.body ? document.body.innerText : '',
					otp: Array.from(document.querySelectorAll('input[autocomplete="one-time-code"],input[autocomplete="one-time-password"]')).some(visible),
					login: Array.from(document.querySelectorAll('input[name="username"],input[type="password"]')).some(visible)};
			})()`, &p))
			return p, err
		})
}

func waitForAuthentication(ctx context.Context, cfg Config, normalWait, otpWait, interval time.Duration, read func(context.Context) (authPage, error)) error {
	log.Print("ログイン: 認証結果を待機しています")
	timer := time.NewTimer(normalWait)
	defer timer.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	manual := false
	for {
		// Bound each browser read without ending the parent browser session.
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		p, err := read(readCtx)
		cancel()
		if err != nil {
			return err
		}
		if err := accessDeniedError(p.Body); err != nil {
			return err
		}
		if p.needsOTP() && !manual {
			if cfg.Headless {
				return fmt.Errorf("メールによる追加認証が必要です。config.json の headless を false にして再実行し、ブラウザで認証してください")
			}
			manual = true
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(otpWait)
			log.Print("追加認証: ブラウザで「送信」を押し、メールのワンタイムパスワードを入力して認証を完了してください。最大5分待機します")
		}
		if p.authenticated(cfg.BillingURL) {
			log.Print("ログイン: 完了を確認しました。請求取得へ進みます")
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if manual {
				return fmt.Errorf("追加認証の待機時間が終了しました。認証完了後の画面を確認してください")
			}
			return fmt.Errorf("ログイン完了を確認できません。ブラウザのエラー表示・追加認証を確認してください")
		case <-ticker.C:
		}
	}
}
