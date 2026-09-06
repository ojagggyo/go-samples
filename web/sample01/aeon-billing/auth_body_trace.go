package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func isAuthPost(r *network.Request, origin *url.URL) bool {
	u, err := url.Parse(r.URL)
	return err == nil && r.Method == "POST" && u.Scheme == origin.Scheme && u.Host == origin.Host &&
		u.Path == "/auth/realms/msweb/login-actions/authenticate"
}

// Explicit opt-in only: unlike the ordinary trace, this file contains raw
// authentication form data. Never print its contents to the console.
func startAuthBodyTrace(ctx context.Context, path, loginURL string) (func(), error) {
	origin, err := url.Parse(loginURL)
	if err != nil {
		return nil, fmt.Errorf("ログインURLが不正です")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	closed := false
	listenerCtx, stop := context.WithCancel(ctx)
	closeTrace := func() {
		stop()
		mu.Lock()
		defer mu.Unlock()
		if !closed {
			closed = true
			_ = f.Close()
		}
	}
	chromedp.ListenTarget(listenerCtx, func(event any) {
		e, ok := event.(*network.EventRequestWillBeSent)
		if !ok || !isAuthPost(e.Request, origin) {
			return
		}
		// CDP calls must run outside the synchronous event callback.
		go func() {
			readCtx, cancel := context.WithTimeout(listenerCtx, 5*time.Second)
			defer cancel()
			var body string
			var readErr error
			if e.Request.HasPostData {
				readErr = chromedp.Run(readCtx, chromedp.ActionFunc(func(ctx context.Context) error {
					var err error
					body, err = network.GetRequestPostData(e.RequestID).Do(ctx)
					return err
				}))
			}
			entry := struct {
				Time        time.Time         `json:"time"`
				ID          network.RequestID `json:"request_id"`
				URL         string            `json:"url"`
				HasPostData bool              `json:"has_post_data"`
				ContentType string            `json:"content_type"`
				Body        string            `json:"body"`
				ReadFailed  bool              `json:"read_failed"`
			}{Time: time.Now().UTC(), ID: e.RequestID, URL: traceURL(e.Request.URL), HasPostData: e.Request.HasPostData, Body: body, ReadFailed: readErr != nil}
			for k, v := range e.Request.Headers {
				if k == "Content-Type" || k == "content-type" {
					entry.ContentType, _ = v.(string)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if closed {
				return
			}
			if err := json.NewEncoder(f).Encode(entry); err != nil {
				log.Printf("認証bodyログの保存に失敗: %v", err)
				return
			}
			log.Printf("認証POST bodyを記録しました（%d bytes、取得失敗=%t）", len(body), readErr != nil)
		}()
	})
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		closeTrace()
		return nil, err
	}
	return closeTrace, nil
}
