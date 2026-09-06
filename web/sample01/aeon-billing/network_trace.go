package main

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// traceURL excludes credentials, query strings, fragments and non-HTTP URLs.
func traceURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "[omitted]"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

type traceEntry struct {
	Time          time.Time             `json:"time"`
	Event         string                `json:"event"`
	ID            network.RequestID     `json:"request_id,omitempty"`
	URL           string                `json:"url,omitempty"`
	Method        string                `json:"method,omitempty"`
	Type          network.ResourceType  `json:"type,omitempty"`
	Status        int64                 `json:"status,omitempty"`
	Protocol      string                `json:"protocol,omitempty"`
	Location      string                `json:"location,omitempty"`
	UserAgent     string                `json:"user_agent,omitempty"`
	Initiator     string                `json:"initiator,omitempty"`
	BlockedReason network.BlockedReason `json:"blocked_reason,omitempty"`
	Canceled      bool                  `json:"canceled,omitempty"`
}

func responseTrace(kind string, id network.RequestID, r *network.Response) traceEntry {
	e := traceEntry{Event: kind, ID: id, URL: traceURL(r.URL), Status: r.Status, Protocol: r.Protocol}
	for k, v := range r.Headers {
		if strings.EqualFold(k, "Location") {
			if s, ok := v.(string); ok {
				base, err := url.Parse(r.URL)
				rel, relErr := url.Parse(s)
				if err == nil && relErr == nil {
					e.Location = traceURL(base.ResolveReference(rel).String())
				}
			}
		}
	}
	return e
}

func startNetworkTrace(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	closed := false
	encoder := json.NewEncoder(f)
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
	write := func(e traceEntry) {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		e.Time = time.Now().UTC()
		// Write directly, without buffering: log.Fatal exits without defers.
		if err := encoder.Encode(e); err != nil {
			log.Printf("通信ログ書き込み失敗: %v", err)
		}
	}
	chromedp.ListenTarget(listenerCtx, func(event any) {
		switch e := event.(type) {
		case *network.EventRequestWillBeSent:
			if e.RedirectResponse != nil {
				write(responseTrace("redirect", e.RequestID, e.RedirectResponse))
			}
			r := traceEntry{Event: "request", ID: e.RequestID, URL: traceURL(e.Request.URL), Method: e.Request.Method, Type: e.Type}
			if e.Initiator != nil {
				r.Initiator = string(e.Initiator.Type)
			}
			for k, v := range e.Request.Headers {
				if strings.EqualFold(k, "User-Agent") {
					r.UserAgent, _ = v.(string)
				}
			}
			write(r)
		case *network.EventResponseReceived:
			r := responseTrace("response", e.RequestID, e.Response)
			r.Type = e.Type
			write(r)
		case *network.EventLoadingFailed:
			write(traceEntry{Event: "failed", ID: e.RequestID, Type: e.Type, BlockedReason: e.BlockedReason, Canceled: e.Canceled})
		}
	})
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		closeTrace()
		return nil, err
	}
	return closeTrace, nil
}
