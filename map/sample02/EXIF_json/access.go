package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type adminContextKey struct{}

type accessPolicy struct {
	admins  map[string]bool
	proxies map[netip.Addr]bool
}

func newAccessPolicy(adminUsers, trustedProxies string) (*accessPolicy, error) {
	p := &accessPolicy{admins: make(map[string]bool), proxies: make(map[netip.Addr]bool)}
	for _, name := range strings.Split(adminUsers, ",") {
		if name = strings.TrimSpace(name); name != "" {
			p.admins[name] = true
		}
	}
	for _, value := range strings.Split(trustedProxies, ",") {
		addr, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || addr.Zone() != "" || addr.IsUnspecified() {
			return nil, fmt.Errorf("trusted-proxies にはプロキシの接続元IPを指定してください: %q", value)
		}
		p.proxies[addr.Unmap()] = true
	}
	return p, nil
}

// Only the immediate trusted proxy may assert an authenticated identity.
// OpenResty must overwrite X-Photo-User with $remote_user after Basic auth.
func (p *accessPolicy) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		addr, parseErr := netip.ParseAddr(host)
		users := r.Header.Values("X-Photo-User")
		reason := ""
		switch {
		case err != nil || parseErr != nil:
			reason = "接続元IPを解析できません"
		case !p.proxies[addr.Unmap()]:
			reason = "信頼対象外の接続元です。OpenRestyの接続元IPと -trusted-proxies を確認してください"
		case len(users) != 1 || strings.TrimSpace(users[0]) == "":
			reason = "認証ユーザー名がありません。OpenRestyのBasic認証と proxy_set_header X-Photo-User $remote_user を確認してください"
		}
		if reason != "" {
			log.Printf("写真アクセス拒否: remote=%q: %s", r.RemoteAddr, reason)
			http.Error(w, "認証済みの写真ページからアクセスしてください", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), adminContextKey{}, p.admins[users[0]])
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isAdmin(r *http.Request) bool {
	admin, _ := r.Context().Value(adminContextKey{}).(bool)
	return admin
}

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !isAdmin(r) {
		http.Error(w, "管理者のみ利用できます", http.StatusForbidden)
		return false
	}
	return true
}

func appHandler(policy *accessPolicy) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/api/photos", mediaHandler)
	mux.HandleFunc("/api/media", mediaHandler)
	mux.HandleFunc("/photo", mediaFileHandler)
	mux.HandleFunc("/media", mediaFileHandler)
	mux.HandleFunc("/api/unlocated", unlocatedHandler)
	mux.HandleFunc("/api/location", assignLocationHandler)
	return policy.protect(mux)
}
