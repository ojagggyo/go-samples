package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

// login supports both a single form and an ID -> Next -> password flow.
func login(ctx context.Context, cfg Config, user, pass string) error {
	userTarget := loginTarget(cfg.UsernameSelectors, "")
	passwordTarget := loginTarget(cfg.PasswordSelectors, "")
	nextSelectors := cfg.NextSelectors
	if len(nextSelectors) == 0 {
		nextSelectors = cfg.SubmitSelectors
	}
	nextTarget := loginTarget(nextSelectors, "次へ")
	if err := fillLoginField(ctx, userTarget, user); err != nil {
		return fmt.Errorf("ログインID欄: %w", err)
	}
	// Wait until either the password field or the next button is usable.
	if err := waitLoginTarget(ctx, "("+passwordTarget+" || "+nextTarget+")"); err != nil {
		return fmt.Errorf("パスワード欄または次へボタン: %w", err)
	}
	var hasPassword bool
	if err := chromedp.Run(ctx, chromedp.Evaluate("Boolean("+passwordTarget+")", &hasPassword)); err != nil {
		return err
	}
	if !hasPassword {
		if err := chromedp.Run(ctx, chromedp.Click(nextTarget, chromedp.ByJSPath)); err != nil {
			return fmt.Errorf("次へボタン: %w", err)
		}
	}
	if err := fillLoginField(ctx, passwordTarget, pass); err != nil {
		return fmt.Errorf("パスワード欄: %w", err)
	}
	submitTarget := loginTarget(cfg.SubmitSelectors, "ログイン")
	if err := waitLoginTarget(ctx, submitTarget); err != nil {
		return fmt.Errorf("ログインボタン: %w", err)
	}
	return chromedp.Run(ctx, chromedp.Click(submitTarget, chromedp.ByJSPath))
}

// Return a DOM expression selecting a visible, enabled element. Exact button
// labels also cover buttons that have no explicit type="submit" attribute.
func loginTarget(selectors []string, label string) string {
	if selectors == nil {
		selectors = []string{}
	}
	s, _ := json.Marshal(selectors)
	l, _ := json.Marshal(label)
	return fmt.Sprintf(`(() => {
		const usable = e => e.getClientRects().length > 0 &&
			getComputedStyle(e).visibility !== 'hidden' && !e.disabled;
		const label = %s;
		if (label) {
			const button = Array.from(document.querySelectorAll('button, input[type="submit"], input[type="button"], a, [role="button"]'))
				.find(e => usable(e) && (e.innerText || e.value || '').trim() === label);
			if (button) return button;
		}
		for (const selector of %s) {
			const element = Array.from(document.querySelectorAll(selector)).find(usable);
			if (element) return element;
		}
		return null;
	})()`, l, s)
}

func waitLoginTarget(ctx context.Context, target string) error {
	// Keep the browser context alive so a stage timeout can still be captured.
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		if err := chromedp.Run(ctx, chromedp.Evaluate("Boolean("+target+")", &ready)); err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("30秒待機しましたが表示されません。アクセス拒否・追加認証・画面変更を確認してください")
		case <-ticker.C:
		}
	}
}

func fillLoginField(ctx context.Context, target, value string) error {
	if err := waitLoginTarget(ctx, target); err != nil {
		return err
	}
	// SendKeys generates input events needed by dynamically rendered forms.
	return chromedp.Run(ctx,
		chromedp.Evaluate("("+target+").value = ''", nil),
		chromedp.SendKeys(target, value, chromedp.ByJSPath),
	)
}
