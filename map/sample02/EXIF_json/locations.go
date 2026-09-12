package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type savedLocation struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}
type locationAssignments struct {
	Root      string                   `json:"root"`
	Locations map[string]savedLocation `json:"locations"`
}

var assignments locationAssignments
var assignmentsPath string
var datedMedia []Media

func loadLocationAssignments(filename, root string) error {
	assignmentsPath = filename
	assignments = locationAssignments{Root: root, Locations: make(map[string]savedLocation)}
	b, err := os.ReadFile(filename)
	if err == nil {
		if err := json.Unmarshal(b, &assignments); err != nil {
			return fmt.Errorf("位置情報ファイルを読み込めません: %w", err)
		}
		if assignments.Root != root {
			return fmt.Errorf("位置情報ファイルは別の写真フォルダ用です。-locations で別の保存先を指定してください")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if assignments.Locations == nil {
		assignments.Locations = make(map[string]savedLocation)
	}
	datedMedia = nil
	for _, p := range media {
		if p.TakenAt > 0 {
			datedMedia = append(datedMedia, p)
		}
	}
	sort.SliceStable(datedMedia, func(i, j int) bool { return datedMedia[i].TakenAt < datedMedia[j].TakenAt })
	for i := range unlocated {
		key, err := filepath.Rel(root, unlocated[i].path)
		if err != nil {
			return err
		}
		if loc, ok := assignments.Locations[key]; ok {
			if !validLocation(loc.Lat, loc.Lng) {
				return fmt.Errorf("保存された座標が不正です: %s", key)
			}
			applyLocation(i, loc)
		}
	}
	return nil
}

func applyLocation(index int, loc savedLocation) {
	p := &unlocated[index]
	p.Lat, p.Lng, p.Source = loc.Lat, loc.Lng, "manual"
	for i := range media {
		if media[i].path == p.path {
			media[i].Lat, media[i].Lng = loc.Lat, loc.Lng
			return
		}
	}
	copy := *p
	copy.ID = len(media)
	media = append(media, copy)
	countMedia(filepath.Dir(p.path), copy)
}

type locationSuggestion struct {
	Name       string  `json:"name"`
	Lat        float64 `json:"lat"`
	Lng        float64 `json:"lng"`
	TakenAt    int64   `json:"takenAt"`
	Difference int64   `json:"differenceSeconds"`
}

func suggestLocation(p Media) *locationSuggestion {
	if p.TakenAt <= 0 || len(datedMedia) == 0 {
		return nil
	}
	pos := sort.Search(len(datedMedia), func(i int) bool { return datedMedia[i].TakenAt >= p.TakenAt })
	var best *locationSuggestion
	for _, i := range []int{pos - 1, pos} {
		if i < 0 || i >= len(datedMedia) {
			continue
		}
		ref := datedMedia[i]
		delta := ref.TakenAt - p.TakenAt
		if delta < 0 {
			delta = -delta
		}
		if delta <= 3600 && (best == nil || delta < best.Difference) {
			best = &locationSuggestion{ref.Name, ref.Lat, ref.Lng, ref.TakenAt, delta}
		}
	}
	return best
}

func unlocatedHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	yearText, monthText := r.URL.Query().Get("year"), r.URL.Query().Get("month")
	year, month := 0, 0
	if yearText != "" && yearText != "unknown" {
		value, err := strconv.Atoi(yearText)
		if err != nil || value < 1 || value > 9999 {
			http.Error(w, "年が不正です", 400)
			return
		}
		year = value
	}
	if monthText != "" {
		value, err := strconv.Atoi(monthText)
		if err != nil || value < 1 || value > 12 || yearText == "unknown" {
			http.Error(w, "月が不正です", 400)
			return
		}
		month = value
	}
	zone := time.FixedZone("JST", 9*60*60)
	matches := func(p Media) bool {
		if p.Source == "manual" {
			return false
		}
		if yearText == "unknown" {
			return p.TakenAt <= 0
		}
		if year == 0 && month == 0 {
			return true
		}
		if p.TakenAt <= 0 {
			return false
		}
		d := time.Unix(p.TakenAt, 0).In(zone)
		return (year == 0 || d.Year() == year) && (month == 0 || int(d.Month()) == month)
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	mu.RLock()
	defer mu.RUnlock()
	type item struct {
		Media
		Suggestion *locationSuggestion `json:"suggestion,omitempty"`
	}
	result := struct {
		Items  []item `json:"items"`
		Total  int    `json:"total"`
		Offset int    `json:"offset"`
		Years  []int  `json:"years"`
	}{Items: []item{}, Years: []int{}}
	years := make(map[int]bool)
	for _, p := range unlocated {
		if p.Source != "manual" && p.TakenAt > 0 {
			years[time.Unix(p.TakenAt, 0).In(zone).Year()] = true
		}
		if matches(p) {
			result.Total++
		}
	}
	for y := range years {
		result.Years = append(result.Years, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(result.Years)))
	if offset >= result.Total {
		offset = max(0, ((result.Total-1)/30)*30)
	}
	result.Offset = offset
	n := 0
	for _, p := range unlocated {
		if !matches(p) {
			continue
		}
		if n >= offset && len(result.Items) < 30 {
			result.Items = append(result.Items, item{p, suggestLocation(p)})
		}
		n++
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func assignLocationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "同じ画面から操作してください", http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
		http.Error(w, "同じ画面から操作してください", http.StatusForbidden)
		return
	}
	var input struct {
		ID  *int     `json:"id"`
		IDs []int    `json:"ids"`
		Lat *float64 `json:"lat"`
		Lng *float64 `json:"lng"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "入力が不正です", 400)
		return
	}
	if decoder.Decode(new(any)) != io.EOF || input.Lat == nil || input.Lng == nil || !validLocation(*input.Lat, *input.Lng) {
		http.Error(w, "座標が不正です", 400)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if input.ID != nil {
		if input.IDs != nil {
			http.Error(w, "id と ids は同時に指定できません", 400)
			return
		}
		input.IDs = []int{*input.ID}
	}
	if len(input.IDs) == 0 {
		http.Error(w, "写真を選択してください", 400)
		return
	}
	next := locationAssignments{Root: assignments.Root, Locations: make(map[string]savedLocation)}
	for k, v := range assignments.Locations {
		next.Locations[k] = v
	}
	loc := savedLocation{*input.Lat, *input.Lng}
	indices := make(map[int]bool)
	for _, id := range input.IDs {
		if id >= 0 || id < -len(unlocated) {
			http.Error(w, "写真が見つかりません", 404)
			return
		}
		index := -id - 1
		key, err := filepath.Rel(assignments.Root, unlocated[index].path)
		if err != nil {
			http.Error(w, "写真の保存先が不正です", 500)
			return
		}
		next.Locations[key] = loc
		indices[index] = true
	}
	if err := saveAssignments(next); err != nil {
		http.Error(w, "位置情報を保存できません: "+err.Error(), 500)
		return
	}
	assignments = next
	for index := range indices {
		applyLocation(index, loc)
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"ok":true}`)
}

func saveAssignments(value locationAssignments) error {
	f, err := os.CreateTemp(filepath.Dir(assignmentsPath), "locations-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := json.NewEncoder(f).Encode(value); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), assignmentsPath)
}
