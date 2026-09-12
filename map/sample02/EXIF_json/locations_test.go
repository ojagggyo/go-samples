package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func preserveLocationState(t *testing.T) {
	t.Helper()
	m, u, c, a, p, d := media, unlocated, folderCounts, assignments, assignmentsPath, datedMedia
	t.Cleanup(func() { media, unlocated, folderCounts, assignments, assignmentsPath, datedMedia = m, u, c, a, p, d })
}

func TestManualLocationPersistsWithoutChangingPhoto(t *testing.T) {
	preserveLocationState(t)
	root := t.TempDir()
	photo := filepath.Join(root, "missing.jpg")
	for name, content := range map[string]string{
		"missing.jpg":      "original photo bytes",
		"missing.jpg.json": `{"title":"missing.jpg","photoTakenTime":{"timestamp":"1700000000"}}`,
		"known.jpg":        "known photo bytes",
		"known.jpg.json":   `{"title":"known.jpg","photoTakenTime":{"timestamp":"1700000300"},"geoData":{"latitude":35,"longitude":139}}`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "locations.json")
	scan := func() {
		t.Helper()
		if err := scanMedia(root, openMetadataCache("", root, false)); err != nil {
			t.Fatal(err)
		}
		if err := loadLocationAssignments(path, root); err != nil {
			t.Fatal(err)
		}
	}
	scan()
	if len(unlocated) != 1 || len(media) != 1 {
		t.Fatalf("missing=%d located=%d", len(unlocated), len(media))
	}
	candidate := suggestLocation(unlocated[0])
	if candidate == nil || candidate.Difference != 300 {
		t.Fatalf("candidate: %+v", candidate)
	}
	listing := httptest.NewRecorder()
	unlocatedHandler(listing, httptest.NewRequest("GET", "/api/unlocated", nil))
	if !strings.Contains(listing.Body.String(), `"total":1`) || !strings.Contains(listing.Body.String(), `"suggestion"`) {
		t.Fatal(listing.Body.String())
	}
	request := httptest.NewRequest("POST", "/api/location", strings.NewReader(`{"id":-1,"lat":36,"lng":140}`))
	request.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	assignLocationHandler(result, request)
	if result.Code != http.StatusOK {
		t.Fatal(result.Code, result.Body.String())
	}
	if len(media) != 2 || unlocated[0].Source != "manual" {
		t.Fatal("assignment not applied")
	}
	b, err := os.ReadFile(photo)
	if err != nil || string(b) != "original photo bytes" {
		t.Fatal("photo changed", err)
	}
	scan()
	if len(media) != 2 || unlocated[0].Lat != 36 || unlocated[0].Lng != 140 || unlocated[0].Source != "manual" {
		t.Fatal("assignment not restored")
	}
	if len(datedMedia) != 1 {
		t.Fatal("manual locations must not become suggestion sources")
	}
	content := httptest.NewRecorder()
	mediaFileHandler(content, httptest.NewRequest("GET", "/media?id=-1", nil))
	if content.Code != 200 || content.Body.String() != "original photo bytes" {
		t.Fatal("unlocated media cannot be viewed")
	}
}

func TestLocationValidationAndSaveFailure(t *testing.T) {
	preserveLocationState(t)
	root := t.TempDir()
	unlocated = []Media{{ID: -1, path: filepath.Join(root, "a.jpg")}}
	media = nil
	folderCounts = make(map[string]*FolderCount)
	assignments = locationAssignments{Root: root, Locations: make(map[string]savedLocation)}
	assignmentsPath = filepath.Join(root, "absent-directory", "locations.json")
	for _, tc := range []struct {
		body, origin string
		code         int
	}{
		{`{"id":-1,"lat":91,"lng":1}`, "", 400},
		{`{"id":-1,"lat":35}`, "", 400},
		{`{"id":-2,"lat":35,"lng":139}`, "", 404},
		{`{"id":-1,"lat":35,"lng":139}`, "https://other.example", 403},
		{`{"id":-1,"lat":35,"lng":139}`, "", 500},
	} {
		r := httptest.NewRequest("POST", "/api/location", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		assignLocationHandler(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s: %d want %d", tc.body, w.Code, tc.code)
		}
		if len(media) != 0 || len(assignments.Locations) != 0 || unlocated[0].Source != "" {
			t.Fatal("failed save changed state")
		}
	}
}

func TestSuggestionWindowAndPagination(t *testing.T) {
	preserveLocationState(t)
	datedMedia = []Media{{Name: "earlier", TakenAt: 10000}, {Name: "later", TakenAt: 13000}}
	if got := suggestLocation(Media{TakenAt: 12800}); got == nil || got.Name != "later" {
		t.Fatal(got)
	}
	if suggestLocation(Media{TakenAt: 20000}) != nil || suggestLocation(Media{}) != nil {
		t.Fatal("unexpected suggestion")
	}
	unlocated = make([]Media, 35)
	unlocated[0].Source = "manual"
	w := httptest.NewRecorder()
	unlocatedHandler(w, httptest.NewRequest("GET", "/api/unlocated?offset=30", nil))
	var data struct {
		Items         []Media
		Total, Offset int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 34 || len(data.Items) != 4 || data.Offset != 30 {
		t.Fatalf("%+v", data)
	}
}

func TestBatchLocationSaveIsAtomic(t *testing.T) {
	preserveLocationState(t)
	root := t.TempDir()
	unlocated = []Media{{ID: -1, path: filepath.Join(root, "one.jpg")}, {ID: -2, path: filepath.Join(root, "two.jpg")}}
	media = nil
	folderCounts = make(map[string]*FolderCount)
	assignments = locationAssignments{Root: root, Locations: make(map[string]savedLocation)}
	assignmentsPath = filepath.Join(root, "locations.json")
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/location", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		assignLocationHandler(w, r)
		return w
	}
	for _, body := range []string{`{"ids":[],"lat":35,"lng":139}`, `{"ids":[-1,-99],"lat":35,"lng":139}`, `{"id":-1,"ids":[-2],"lat":35,"lng":139}`} {
		if w := post(body); w.Code < 400 {
			t.Fatal("invalid batch accepted", body)
		}
		if len(media) != 0 || len(assignments.Locations) != 0 || unlocated[0].Source != "" {
			t.Fatal("invalid batch partially applied")
		}
		if _, err := os.Stat(assignmentsPath); !os.IsNotExist(err) {
			t.Fatal("invalid batch wrote a file")
		}
	}
	assignmentsPath = filepath.Join(root, "missing", "locations.json")
	if w := post(`{"ids":[-1,-2],"lat":35,"lng":139}`); w.Code != 500 {
		t.Fatal(w.Code)
	}
	if len(media) != 0 || unlocated[0].Source != "" || unlocated[1].Source != "" {
		t.Fatal("failed batch partially applied")
	}
	assignmentsPath = filepath.Join(root, "locations.json")
	if w := post(`{"ids":[-1,-2,-1],"lat":35,"lng":139}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if len(media) != 2 || len(assignments.Locations) != 2 {
		t.Fatal("batch or deduplication failed")
	}
	for _, p := range unlocated {
		if p.Source != "manual" || p.Lat != 35 || p.Lng != 139 {
			t.Fatal(p)
		}
	}
	data, err := os.ReadFile(assignmentsPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved locationAssignments
	if err := json.Unmarshal(data, &saved); err != nil || len(saved.Locations) != 2 {
		t.Fatal("batch was not persisted", err)
	}
}

func TestUnlocatedDateFilters(t *testing.T) {
	preserveLocationState(t)
	// UTCでは前年でも、日本時間では1月1日になる境界。
	boundary := time.Date(2023, 12, 31, 15, 0, 0, 0, time.UTC).Unix()
	unlocated = []Media{{ID: -1, TakenAt: boundary - 1}, {ID: -2, TakenAt: boundary}, {ID: -3}, {ID: -4, TakenAt: boundary, Source: "manual"}}
	for _, tc := range []struct {
		query        string
		total, first int
	}{
		{"year=2023", 1, -1}, {"year=2024&month=1", 1, -2}, {"month=12", 1, -1}, {"year=unknown", 1, -3}, {"year=2024&month=2", 0, 0}, {"", 3, -1},
	} {
		w := httptest.NewRecorder()
		unlocatedHandler(w, httptest.NewRequest("GET", "/api/unlocated?"+tc.query, nil))
		var result struct {
			Items []Media
			Total int
			Years []int
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Total != tc.total || (tc.total > 0 && result.Items[0].ID != tc.first) {
			t.Fatalf("%s: %+v", tc.query, result)
		}
		if len(result.Years) != 2 || result.Years[0] != 2024 || result.Years[1] != 2023 {
			t.Fatal(result.Years)
		}
	}
	for _, q := range []string{"year=abc", "month=13", "year=unknown&month=1"} {
		w := httptest.NewRecorder()
		unlocatedHandler(w, httptest.NewRequest("GET", "/api/unlocated?"+q, nil))
		if w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
	for i := 0; i < 35; i++ {
		unlocated = append(unlocated, Media{ID: -5 - i, TakenAt: boundary})
	}
	w := httptest.NewRecorder()
	unlocatedHandler(w, httptest.NewRequest("GET", "/api/unlocated?year=2024&month=1&offset=30", nil))
	var result struct {
		Items         []Media
		Total, Offset int
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Total != 36 || result.Offset != 30 || len(result.Items) != 6 {
		t.Fatalf("filtered pagination: %+v", result)
	}
}
