package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func TestAuthBodyFilter(t *testing.T) {
	origin, _ := url.Parse("https://example.test/app/")
	for _, tc := range []struct {
		method, raw string
		want        bool
	}{
		{"POST", "https://example.test/auth/realms/msweb/login-actions/authenticate?session_code=test", true},
		{"GET", "https://example.test/auth/realms/msweb/login-actions/authenticate", false},
		{"POST", "https://other.test/auth/realms/msweb/login-actions/authenticate", false},
		{"POST", "https://example.test/other", false},
	} {
		if got := isAuthPost(&network.Request{URL: tc.raw, Method: tc.method}, origin); got != tc.want {
			t.Fatal("incorrect auth request filter")
		}
	}
}

func TestAuthBodyCapture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<form method="post" action="/auth/realms/msweb/login-actions/authenticate"><input name="encrypted_username" value="abc123"><input name="encrypted_password" value="def456"><input name="url_on_browser" value="https://example.test/auth?code=test"><button id="submit">Submit</button></form>`))
	}))
	defer server.Close()
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 60*time.Second)
	defer timeout()
	if err := chromedp.Run(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "body.jsonl")
	closeTrace, err := startAuthBodyTrace(ctx, path, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrace()
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL), chromedp.Click("#submit", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil && strings.HasSuffix(string(b), "\n") {
			var entry struct {
				Body        string `json:"body"`
				ReadFailed  bool   `json:"read_failed"`
				HasPostData bool   `json:"has_post_data"`
			}
			if err := json.Unmarshal(b, &entry); err != nil {
				t.Fatal(err)
			}
			form, err := url.ParseQuery(entry.Body)
			if err != nil || entry.ReadFailed || !entry.HasPostData || form.Get("encrypted_username") != "abc123" || form.Get("encrypted_password") != "def456" || form.Get("url_on_browser") != "https://example.test/auth?code=test" {
				t.Fatal("body was not captured correctly")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no body captured")
}
