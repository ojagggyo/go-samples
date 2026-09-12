package main

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestConfiguredMediaLimit(t *testing.T) {
	previousMedia, previousLimit := media, mediaLimit
	t.Cleanup(func() { media, mediaLimit = previousMedia, previousLimit })
	media = make([]Media, 700)
	for i := range media {
		media[i] = Media{ID: i, Lat: 35, Lng: 139}
	}
	for _, limit := range []int{1, 7, 100, 500, 650, 1000} {
		mediaLimit = limit
		response := httptest.NewRecorder()
		mediaHandler(response, httptest.NewRequest(http.MethodGet, "/api/media?minLat=20&maxLat=50&minLng=120&maxLng=150", nil))
		if got := strings.Count(response.Body.String(), `"id":`); got != min(limit, len(media)) {
			t.Errorf("limit %d: got %d items", limit, got)
		}
		page := httptest.NewRecorder()
		indexHandler(page, httptest.NewRequest(http.MethodGet, "/", nil))
		if !strings.Contains(page.Body.String(), "最大"+strconv.Itoa(limit)+"件表示") {
			t.Errorf("limit %d missing from page", limit)
		}
	}
}

func TestSpreadMediaIncludesSparseLocations(t *testing.T) {
	items := make([]Media, 600)
	for i := range items {
		items[i] = Media{ID: i, Lat: 35, Lng: 139}
	}
	// ファイル順で密集地の後にある地点も選ばれること。
	items = append(items, Media{ID: 600, Lat: 43, Lng: 141}, Media{ID: 601, Lat: 26, Lng: 127})
	got := spreadMedia(items, 20, 50, 120, 150, 500)
	if len(got) != 500 {
		t.Fatalf("got %d items, want 500", len(got))
	}
	seen := make(map[int]bool)
	for _, item := range got {
		if seen[item.ID] {
			t.Fatalf("duplicate ID %d", item.ID)
		}
		seen[item.ID] = true
	}
	if !seen[600] || !seen[601] {
		t.Fatal("sparse locations were hidden by the dense location")
	}
}

func TestSpreadMediaBoundsAndZoom(t *testing.T) {
	items := []Media{
		{ID: 0, Lat: 20, Lng: 120},
		{ID: 1, Lat: 50, Lng: 150},
		{ID: 2, Lat: 35, Lng: 139},
		{ID: 3, Lat: 60, Lng: 160},
	}
	got := spreadMedia(items, 20, 50, 120, 150, 500)
	if len(got) != 3 {
		t.Fatalf("got %d items, want all 3 in bounds including edges", len(got))
	}
	got = spreadMedia(items, 34, 36, 138, 140, 500)
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("zoomed selection: %+v", got)
	}
	if empty := spreadMedia(nil, 20, 50, 120, 150, 500); empty == nil || len(empty) != 0 {
		t.Fatal("empty selection must encode as []")
	}
}

func TestSpreadMediaCoversEveryOccupiedCell(t *testing.T) {
	var items []Media
	for row := 0; row < 20; row++ {
		y := (float64(row) + 0.5) / 20 * math.Log(math.Tan(math.Pi/4+60*math.Pi/360))
		lat := (2*math.Atan(math.Exp(y)) - math.Pi/2) * 180 / math.Pi
		for col := 0; col < 25; col++ {
			for n := 0; n < 3; n++ {
				items = append(items, Media{ID: len(items), Lat: lat, Lng: 100 + float64(col) + 0.5})
			}
		}
	}
	got := spreadMedia(items, 0, 60, 100, 125, 500)
	seen := make(map[int]bool)
	for _, item := range got {
		seen[item.ID/3] = true
	}
	if len(got) != 500 || len(seen) != 500 {
		t.Fatalf("got %d items covering %d cells, want 500 each", len(got), len(seen))
	}
}

func TestMediaHandlerRejectsInvalidBounds(t *testing.T) {
	for _, query := range []string{
		"minLat=NaN&maxLat=50&minLng=120&maxLng=150",
		"minLat=20&maxLat=Inf&minLng=120&maxLng=150",
		"minLat=50&maxLat=20&minLng=120&maxLng=150",
		"minLat=20&maxLat=50&minLng=120&maxLng=120",
	} {
		response := httptest.NewRecorder()
		mediaHandler(response, httptest.NewRequest(http.MethodGet, "/api/media?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("query %s: got status %d", query, response.Code)
		}
	}
}
