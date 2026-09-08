package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"golang.org/x/oauth2"
)

func authenticatedClient(ctx context.Context, cfg *oauth2.Config, path string) (*http.Client, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: 30 * time.Second})
	var token oauth2.Token
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &token); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return savedClient(ctx, cfg, path, &token), nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	listenAddr, callbackPath := "127.0.0.1:0", "/callback"
	kakao := cfg.Endpoint.AuthURL == "https://kauth.kakao.com/oauth/authorize"
	if kakao {
		u, err := url.Parse(cfg.RedirectURL)
		if err != nil {
			return nil, err
		}
		listenAddr, callbackPath = "127.0.0.1:"+u.Port(), u.Path
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}
	defer listener.Close()
	if !kakao {
		cfg.RedirectURL = "http://" + listener.Addr().String() + callbackPath
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	state := hex.EncodeToString(random[:])
	verifier := oauth2.GenerateVerifier()
	codes := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid state", 400)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Authorization failed", 400)
			return
		}
		select {
		case codes <- code:
		default:
		}
		fmt.Fprintln(w, "Authorization received. You can close this window.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	fmt.Println("ブラウザで認証してください:")
	authOptions := []oauth2.AuthCodeOption{}
	exchangeOptions := []oauth2.AuthCodeOption{}
	if !kakao {
		authOptions = append(authOptions, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier))
		exchangeOptions = append(exchangeOptions, oauth2.VerifierOption(verifier))
	}
	fmt.Println(cfg.AuthCodeURL(state, authOptions...))
	select {
	case code := <-codes:
		t, err := cfg.Exchange(ctx, code, exchangeOptions...)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			return nil, err
		}
		return savedClient(ctx, cfg, path, t), nil
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("認証がタイムアウトしました")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type savedTokenSource struct {
	source oauth2.TokenSource
	path   string
	last   *oauth2.Token
}

func (s *savedTokenSource) Token() (*oauth2.Token, error) {
	t, err := s.source.Token()
	if err != nil {
		return nil, err
	}
	if t.AccessToken != s.last.AccessToken || t.RefreshToken != s.last.RefreshToken || !t.Expiry.Equal(s.last.Expiry) {
		if err := writeJSON(s.path, t); err != nil {
			return nil, err
		}
		s.last = t
	}
	return t, nil
}

func savedClient(ctx context.Context, cfg *oauth2.Config, path string, t *oauth2.Token) *http.Client {
	return oauth2.NewClient(ctx, &savedTokenSource{source: cfg.TokenSource(ctx, t), path: path, last: t})
}
