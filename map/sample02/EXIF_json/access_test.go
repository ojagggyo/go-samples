package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func adminTestRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	return r.WithContext(context.WithValue(r.Context(), adminContextKey{}, true))
}

func TestAccountPermissions(t *testing.T) {
	preserveLocationState(t)
	root := t.TempDir()
	known, missing := filepath.Join(root, "known.jpg"), filepath.Join(root, "missing.jpg")
	for name, content := range map[string]string{known: "located photo", missing: "private photo"} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	media = []Media{{ID: 0, Name: "known.jpg", Lat: 35, Lng: 139, path: known}}
	unlocated = []Media{{ID: -1, Name: "missing.jpg", path: missing}}
	folderCounts = make(map[string]*FolderCount)
	assignments = locationAssignments{Root: root, Locations: make(map[string]savedLocation)}
	assignmentsPath = filepath.Join(root, "locations.json")
	datedMedia = nil
	p, err := newAccessPolicy("photos-admin", "127.0.0.1,::1")
	if err != nil {
		t.Fatal(err)
	}
	handler := appHandler(p)
	request := func(user, method, target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, strings.NewReader(`{"ids":[-1],"lat":36,"lng":140}`))
		r.RemoteAddr = "127.0.0.1:50000"
		r.Host = "steememory.com"
		r.Header.Set("X-Photo-User", user)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://steememory.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("response can be cached across accounts")
		}
		return w
	}
	for _, user := range []string{"photos", "photos-admin"} {
		page := request(user, "GET", "/")
		if page.Code != 200 || strings.Contains(page.Body.String(), `<option value="unlocated">`) != (user == "photos-admin") {
			t.Fatalf("%s page: status %d / incorrect menu", user, page.Code)
		}
		for _, path := range []string{"/api/media?minLat=20&maxLat=50&minLng=120&maxLng=150", "/api/photos?minLat=20&maxLat=50&minLng=120&maxLng=150", "/media?id=0", "/photo?id=0"} {
			if w := request(user, "GET", path); w.Code != 200 || strings.Contains(w.Body.String(), "private photo") || strings.Contains(w.Body.String(), "missing.jpg") {
				t.Fatalf("%s %s: status %d or private data leaked", user, path, w.Code)
			}
		}
		for _, path := range []string{"/api/unlocated", "/media?id=-1", "/photo?id=-1"} {
			want := 403
			if user == "photos-admin" {
				want = 200
			}
			if w := request(user, "GET", path); w.Code != want {
				t.Fatalf("%s %s: %d want %d", user, path, w.Code, want)
			}
		}
	}
	for _, method := range []string{"GET", "HEAD"} {
		if w := request("photos", method, "/media?id=-1"); w.Code != 403 {
			t.Fatal("private file accessible", method)
		}
	}
	if w := request("photos", "POST", "/api/location"); w.Code != 403 {
		t.Fatal("general account can save locations")
	}
	if len(media) != 1 || len(assignments.Locations) != 0 {
		t.Fatal("unauthorized save changed state")
	}
	if _, err := os.Stat(assignmentsPath); !os.IsNotExist(err) {
		t.Fatal("unauthorized save wrote a file")
	}
	if w := request("photos-admin", "POST", "/api/location"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(media) != 2 || unlocated[0].Source != "manual" {
		t.Fatal("admin save not applied")
	}
	if w := request("photos", "GET", "/media?id=1"); w.Code != 200 || w.Body.String() != "private photo" {
		t.Fatal("located photo not published after admin save")
	}
}

func TestProxyIdentityCannotBeSpoofed(t *testing.T) {
	p, err := newAccessPolicy("photos-admin", "127.0.0.1,::1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, remote string
		users        []string
		want         int
	}{
		{"missing identity", "127.0.0.1:1", nil, 403},
		{"blank identity", "127.0.0.1:1", []string{" "}, 403},
		{"untrusted proxy", "192.0.2.1:1", []string{"photos-admin"}, 403},
		{"malformed peer", "invalid", []string{"photos-admin"}, 403},
		{"duplicate identity", "127.0.0.1:1", []string{"photos", "photos-admin"}, 403},
		{"general user", "127.0.0.1:1", []string{"photos"}, 403},
		{"different case", "127.0.0.1:1", []string{"PHOTOS-ADMIN"}, 403},
		{"admin", "127.0.0.1:1", []string{"photos-admin"}, 204},
		{"IPv6 admin", "[::1]:1", []string{"photos-admin"}, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			r.Header["X-Photo-User"] = tc.users
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			r.SetBasicAuth("photos-admin", "unverified-password")
			w := httptest.NewRecorder()
			p.protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requireAdmin(w, r) {
					w.WriteHeader(204)
				}
			})).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
		})
	}
	for _, value := range []string{"", "0.0.0.0", "::", "192.0.2.0/24", "localhost"} {
		if _, err := newAccessPolicy("photos-admin", value); err == nil {
			t.Fatalf("invalid proxy accepted: %q", value)
		}
	}
}
