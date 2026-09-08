package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
)

func authenticatedClient(ctx context.Context, cfg *oauth2.Config, path string) (*http.Client, error) {
	var token oauth2.Token
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &token); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return cfg.Client(ctx, &token), nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer listener.Close()
	cfg.RedirectURL = "http://" + listener.Addr().String() + "/callback"
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	state := hex.EncodeToString(random[:])
	verifier := oauth2.GenerateVerifier()
	codes := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
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
	fmt.Println(cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier)))
	select {
	case code := <-codes:
		t, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
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
		return cfg.Client(ctx, t), nil
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("認証がタイムアウトしました")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
