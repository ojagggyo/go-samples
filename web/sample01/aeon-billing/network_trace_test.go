package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestTraceURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://user:secret@example.test/auth?code=secret#secret": "https://example.test/auth",
		"https://example.test/auth?":                               "https://example.test/auth",
		"data:text/plain,secret":                                   "[omitted]",
		"://secret":                                                "[omitted]",
	} {
		if got := traceURL(raw); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestNetworkTraceRedirectAndPost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Set-Cookie", "session=secret; HttpOnly")
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/form?session=secret", http.StatusFound)
		case "/form":
			_, _ = w.Write([]byte(`<form method="post" action="/authenticate?code=secret"><input name="password" value="secret"><button id="submit">Submit</button></form>`))
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("Access Denied secret"))
		}
	}))
	defer server.Close()
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 60*time.Second)
	defer timeout()
	if err := chromedp.Run(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "network.jsonl")
	closeTrace, err := startNetworkTrace(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrace()
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL+"/start"), chromedp.Click("#submit", chromedp.ByQuery), chromedp.WaitNotPresent("#submit", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	closeTrace()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "secret") || strings.Contains(text, "password") || strings.Contains(text, "Set-Cookie") {
		t.Fatal("trace contains sensitive data")
	}
	for _, want := range []string{`"event":"redirect"`, `"status":302`, `"method":"POST"`, `"status":403`} {
		if !strings.Contains(text, want) {
			t.Fatalf("trace missing %s", want)
		}
	}
}
