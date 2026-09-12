package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

type Media struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Format  string  `json:"format"`
	Type    string  `json:"type"`   // photo / video
	Source  string  `json:"source"` // exif / json
	TakenAt int64   `json:"takenAt,omitempty"`
	path    string
}

type FolderCount struct {
	Photo     int
	Video     int
	PhotoEXIF int
	PhotoJSON int
	VideoJSON int
}

var (
	mediaLimit   = 100
	media        []Media
	unlocated    []Media
	folderCounts = make(map[string]*FolderCount)
	mu           sync.RWMutex
)

type TakeoutJSON struct {
	Title          string          `json:"title"`
	GeoDataExif    TakeoutLocation `json:"geoDataExif"`
	GeoData        TakeoutLocation `json:"geoData"`
	PhotoTakenTime struct {
		Timestamp string `json:"timestamp"`
	} `json:"photoTakenTime"`
}

type TakeoutLocation struct {
	Latitude  FlexibleFloat `json:"latitude"`
	Longitude FlexibleFloat `json:"longitude"`
}

type FlexibleFloat float64

func (f *FlexibleFloat) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = FlexibleFloat(v)
	return nil
}

type coordinates struct {
	lat     float64
	lng     float64
	takenAt int64
}

func main() {
	dir := flag.String("photos", "./photos", "Google Photos/Takeout を展開したフォルダ")
	addr := flag.String("addr", "127.0.0.1:8080", "待受アドレス")
	rebuildCache := flag.Bool("rebuild-cache", false, "保存済みキャッシュを使わず位置情報を再解析")
	locationsPath := flag.String("locations", "photo-locations.json", "手動で紐づけた位置情報の保存先")
	flag.IntVar(&mediaLimit, "limit", 100, "地図に表示する最大件数（1以上）")
	flag.Parse()
	if mediaLimit < 1 {
		log.Fatal("-limit は1以上の整数で指定してください")
	}

	abs, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatal(err)
	}

	started := time.Now()
	progress, stopProgress := startLoadProgress()
	cache := openMetadataCache(defaultCachePath(abs), abs, *rebuildCache)
	cache.progress = progress
	if err := scanMedia(abs, cache); err != nil {
		log.Fatal(err)
	}
	if err := loadLocationAssignments(*locationsPath, abs); err != nil {
		log.Fatal(err)
	}
	progress.setPhase("キャッシュを保存中")
	if err := cache.save(); err != nil {
		log.Printf("キャッシュを保存できません: %v", err)
	}
	stopProgress()
	log.Printf("読み込み完了: %s（キャッシュ再利用=%d件 / 解析=%d件）", time.Since(started).Round(time.Millisecond), cache.hits, cache.misses)

	printFolderCounts(abs)

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/api/photos", mediaHandler) // 旧URL互換
	http.HandleFunc("/api/media", mediaHandler)
	http.HandleFunc("/photo", mediaFileHandler) // 旧URL互換
	http.HandleFunc("/media", mediaFileHandler)
	http.HandleFunc("/api/unlocated", unlocatedHandler)
	http.HandleFunc("/api/location", assignLocationHandler)

	photoCount, videoCount, jsonCount := totalCounts()
	log.Printf("位置情報付き写真: %d枚", photoCount)
	log.Printf("位置情報付き動画: %d本", videoCount)
	log.Printf("Takeout JSONから位置情報を取得: %d件", jsonCount)
	log.Printf("位置情報付きメディア合計: %d件", len(media))
	log.Printf("ブラウザで http://%s を開いてください", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func scanMedia(root string, cache *metadataCache) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("写真フォルダではありません: %s", root)
	}
	mu.Lock()
	media = nil
	unlocated = nil
	folderCounts = make(map[string]*FolderCount)
	mu.Unlock()
	// Google Takeout の JSON を先にすべて読み込む。
	// 写真だけでなく動画の sidecar JSON も同じ仕組みで扱える。
	jsonLocations, err := loadTakeoutLocations(root, cache)
	if err != nil {
		return err
	}

	cache.progress.setPhase("写真・動画を確認中")
	return filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("スキップ: %s: %v", path, walkErr)
			return nil
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		mediaType := mediaTypeFromExt(ext)
		if mediaType == "" {
			return nil
		}
		defer cache.progress.advance()

		var lat, lng float64
		var ok bool
		source := ""

		if mediaType == "photo" {
			// JPG/JPEGを中心に写真本体のEXIF GPSを確認する。
			// HEIC/HEIF/PNGはライブラリが読めないことがあるため、
			// 読めなければTakeout JSONへフォールバックする。
			entry := cache.read(path, false)
			lat, lng, ok = entry.Lat, entry.Lng, entry.OK
			if ok {
				source = "exif"
			}
		}

		key := mediaKey(filepath.Dir(path), filepath.Base(path))
		c := jsonLocations[key]
		if !ok {
			lat, lng = c.lat, c.lng
			ok = validLocation(lat, lng)
			if ok {
				source = "json"
			}
		}

		item := Media{
			Name:    filepath.Base(path),
			Lat:     lat,
			Lng:     lng,
			Format:  strings.TrimPrefix(ext, "."),
			Type:    mediaType,
			Source:  source,
			path:    path,
			TakenAt: c.takenAt,
		}

		mu.Lock()
		if !ok || !validLocation(lat, lng) {
			if mediaType == "photo" {
				item.ID = -len(unlocated) - 1
				unlocated = append(unlocated, item)
			}
			mu.Unlock()
			return nil
		}
		item.ID = len(media)
		media = append(media, item)
		countMedia(filepath.Dir(path), item)
		mu.Unlock()

		return nil
	})
}

func mediaTypeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".heic", ".heif", ".webp":
		return "photo"
	case ".mp4", ".mov", ".m4v", ".3gp", ".3g2", ".webm", ".avi", ".mts", ".m2ts":
		return "video"
	default:
		return ""
	}
}

func countMedia(dir string, item Media) {
	fc := folderCounts[dir]
	if fc == nil {
		fc = &FolderCount{}
		folderCounts[dir] = fc
	}

	if item.Type == "photo" {
		fc.Photo++
		if item.Source == "exif" {
			fc.PhotoEXIF++
		} else if item.Source == "json" {
			fc.PhotoJSON++
		}
	} else if item.Type == "video" {
		fc.Video++
		if item.Source == "json" {
			fc.VideoJSON++
		}
	}
}

func printFolderCounts(root string) {
	mu.RLock()
	defer mu.RUnlock()

	dirs := make([]string, 0, len(folderCounts))
	for dir := range folderCounts {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	log.Println("--- サブフォルダ別 位置情報付きメディア ---")
	log.Println("フォルダ : 写真 / 動画 / EXIF / JSON(写真) / JSON(動画) / JSON合計")

	for _, dir := range dirs {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			rel = dir
		}
		if rel == "." {
			rel = "(指定フォルダ直下)"
		}

		fc := folderCounts[dir]
		log.Printf("%s : 写真=%d枚 動画=%d本 EXIF=%d件 JSON写真=%d件 JSON動画=%d件 JSON合計=%d件",
			rel,
			fc.Photo,
			fc.Video,
			fc.PhotoEXIF,
			fc.PhotoJSON,
			fc.VideoJSON,
			fc.PhotoJSON+fc.VideoJSON,
		)
	}
}

func totalCounts() (photo, video, jsonCount int) {
	mu.RLock()
	defer mu.RUnlock()
	for _, item := range media {
		switch item.Type {
		case "photo":
			photo++
		case "video":
			video++
		}
		if item.Source == "json" {
			jsonCount++
		}
	}
	return
}

func locationFromEXIF(path string) (float64, float64, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false, err
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err != nil {
		return 0, 0, false, nil
	}

	lat, lng, err := x.LatLong()
	return lat, lng, err == nil && validLocation(lat, lng), nil
}

func loadTakeoutLocations(root string, cache *metadataCache) (map[string]coordinates, error) {
	cache.progress.setPhase("JSONを読み込み中")
	locations := make(map[string]coordinates)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("JSONをスキップ: %s: %v", path, walkErr)
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}

		defer cache.progress.advance()
		entry := cache.read(path, true)
		if !entry.OK && entry.TakenAt == 0 {
			return nil
		}
		c := coordinates{lat: entry.Lat, lng: entry.Lng, takenAt: entry.TakenAt}

		dir := filepath.Dir(path)

		// JSONのtitleが、実際の写真/動画ファイル名と一致する場合に対応。
		// 例: IMG_1234.HEIC / VID_1234.MP4
		if entry.Title != "" {
			locations[mediaKey(dir, entry.Title)] = c
		}

		// 一般的なGoogle Takeout sidecarファイル名にも対応。
		// VID_1234.MP4.json
		// VID_1234.MP4.supplemental-metadata.json
		name := d.Name()
		lower := strings.ToLower(name)

		switch {
		case strings.HasSuffix(lower, ".supplemental-metadata.json"):
			name = name[:len(name)-len(".supplemental-metadata.json")]
		case strings.HasSuffix(lower, ".json"):
			name = name[:len(name)-len(".json")]
		}

		locations[mediaKey(dir, name)] = c
		return nil
	})

	return locations, err
}

func takeoutCoordinates(metadata TakeoutJSON) (coordinates, bool) {
	lat := float64(metadata.GeoDataExif.Latitude)
	lng := float64(metadata.GeoDataExif.Longitude)
	if validLocation(lat, lng) {
		return coordinates{lat: lat, lng: lng}, true
	}

	lat = float64(metadata.GeoData.Latitude)
	lng = float64(metadata.GeoData.Longitude)
	if validLocation(lat, lng) {
		return coordinates{lat: lat, lng: lng}, true
	}

	return coordinates{}, false
}

func mediaKey(dir, name string) string {
	return strings.ToLower(filepath.Clean(filepath.Join(dir, name)))
}

func validLocation(lat, lng float64) bool {
	if math.IsNaN(lat) || math.IsNaN(lng) ||
		math.IsInf(lat, 0) || math.IsInf(lng, 0) {
		return false
	}

	return lat >= -90 && lat <= 90 &&
		lng >= -180 && lng <= 180 &&
		(lat != 0 || lng != 0)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := template.Must(template.New("index").Parse(indexHTML)).Execute(w, struct{ Limit int }{mediaLimit}); err != nil {
		log.Println(err)
	}
}

func mediaHandler(w http.ResponseWriter, r *http.Request) {
	minLat, e1 := strconv.ParseFloat(r.URL.Query().Get("minLat"), 64)
	maxLat, e2 := strconv.ParseFloat(r.URL.Query().Get("maxLat"), 64)
	minLng, e3 := strconv.ParseFloat(r.URL.Query().Get("minLng"), 64)
	maxLng, e4 := strconv.ParseFloat(r.URL.Query().Get("maxLng"), 64)

	if e1 != nil || e2 != nil || e3 != nil || e4 != nil ||
		!finiteBounds(minLat, maxLat, minLng, maxLng) || minLat >= maxLat || minLng >= maxLng {
		http.Error(w, "地図範囲が不正です", http.StatusBadRequest)
		return
	}

	mu.RLock()
	result := spreadMedia(media, minLat, maxLat, minLng, maxLng, mediaLimit)
	mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func finiteBounds(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// 地図を指定上限以下の区画に分割し、各区画から1件ずつ選ぶ。
// 全区画を一巡してから2件目を選ぶため、密集地が上限を独占しない。
func spreadMedia(items []Media, minLat, maxLat, minLng, maxLng float64, limit int) []Media {
	result := make([]Media, 0)
	limit = min(limit, len(items))
	if limit < 1 {
		return result
	}
	columns := max(1, int(math.Sqrt(float64(limit)*1.25)))
	rows := max(1, limit/columns)
	cells := make([][]Media, columns*rows)
	// Leafletの地図と同じメルカトル座標で縦方向を区切る。
	project := func(lat float64) float64 {
		lat = math.Max(-85.05112878, math.Min(85.05112878, lat))
		return math.Log(math.Tan(math.Pi/4 + lat*math.Pi/360))
	}
	bottom, top := project(minLat), project(maxLat)
	for _, p := range items {
		if !validLocation(p.Lat, p.Lng) || p.Lat < minLat || p.Lat > maxLat || p.Lng < minLng || p.Lng > maxLng {
			continue
		}
		x := int((p.Lng - minLng) / (maxLng - minLng) * float64(columns))
		y := 0
		if top > bottom {
			y = int((project(p.Lat) - bottom) / (top - bottom) * float64(rows))
		}
		x = max(0, min(columns-1, x))
		y = max(0, min(rows-1, y))
		index := y*columns + x
		// 1区画から上限以上の件数を選ぶことはない。
		if len(cells[index]) < limit {
			cells[index] = append(cells[index], p)
		}
	}
	for round := 0; len(result) < limit; round++ {
		before := len(result)
		for _, cell := range cells {
			if round < len(cell) {
				result = append(result, cell[round])
				if len(result) == limit {
					return result
				}
			}
		}
		if len(result) == before {
			break
		}
	}
	return result
}

func mediaFileHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))

	mu.RLock()
	defer mu.RUnlock()

	if err != nil || (id >= 0 && id >= len(media)) || (id < 0 && (id < -len(unlocated))) {
		http.NotFound(w, r)
		return
	}

	var p Media
	if id < 0 {
		p = unlocated[-id-1]
	} else {
		p = media[id]
	}
	switch strings.ToLower(filepath.Ext(p.path)) {
	case ".heic":
		w.Header().Set("Content-Type", "image/heic")
	case ".heif":
		w.Header().Set("Content-Type", "image/heif")
	case ".mp4", ".m4v":
		w.Header().Set("Content-Type", "video/mp4")
	case ".mov":
		w.Header().Set("Content-Type", "video/quicktime")
	case ".webm":
		w.Header().Set("Content-Type", "video/webm")
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", p.Name))
	http.ServeFile(w, r, p.path)
}

const indexHTML = `<!doctype html>
<html lang="ja">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>写真・動画マップ</title>
  <link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css">
  <style>
    * { box-sizing: border-box; }
    body { margin: 0; font-family: system-ui, sans-serif; background: #f5f5f5; }
    header { height: 48px; padding: 12px 16px; background: #202124; color: white; }
    main { display: grid; grid-template-columns: 2fr 1fr; height: calc(100vh - 48px); }
    #map { min-height: 360px; }
    #side { overflow: auto; padding: 10px; }
    #status { margin: 0 0 10px; color: #444; }
    #status[aria-busy="true"]::before { content: ''; display: inline-block; width: 12px; height: 12px; margin-right: 8px; border: 2px solid #bbb; border-top-color: #1769aa; border-radius: 50%; animation: spin 0.8s linear infinite; }
    @keyframes spin { to { transform: rotate(360deg); } }
    @media (prefers-reduced-motion: reduce) { #status[aria-busy="true"]::before { animation: none; } }
    #photos { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
    .card { margin: 0; background: white; border-radius: 6px; overflow: hidden; box-shadow: 0 1px 4px #bbb; }
    .card img, .card video { display: block; width: 100%; height: 130px; object-fit: cover; background: #111; }
    .card img { cursor: pointer; }
    .heic-thumbnail { display: block; width: 100%; height: 130px; padding: 0; border: 0; background: #eee; cursor: pointer; }
    .card figcaption { padding: 5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
    .popup { width: 220px; max-height: 170px; object-fit: cover; cursor: zoom-in; }
    #photo-viewer { max-width: 96vw; max-height: 96vh; padding: 12px; border: 0; border-radius: 8px; background: #202124; color: white; }
    #photo-viewer::backdrop { background: rgba(0,0,0,.8); }
    #photo-viewer img { display: block; max-width: 90vw; max-height: 80vh; object-fit: contain; margin: auto; }
    #photo-viewer-caption { overflow-wrap: anywhere; margin: 8px 0; }
    #photo-viewer-close { display: block; margin: 0 0 8px auto; cursor: pointer; }
    .popup-video { width: 240px; max-height: 180px; background: #111; }
    .media-note { padding: 12px; font-size: 12px; background: white; }
    #location-editor { margin: 10px 0; padding: 10px; background: #fff; border: 2px solid #1769aa; }
    #location-editor p { overflow-wrap: anywhere; margin: 8px 0; }
    .location-select { width: 100%; padding: 8px; cursor: pointer; }
    #side button { cursor: pointer; }
    #side button:disabled { cursor: default; }
    @media (max-width: 800px) {
      main { grid-template-columns: 1fr; grid-template-rows: 55% 45%; }
    }
  </style>
</head>
<body>
<header>写真・動画マップ ― Google Takeout JSON対応</header>
<main>
  <div id="map"></div>
  <aside id="side">
    <form id="coordinate-form">
      <label for="coordinate-input">緯度・経度</label>
      <input id="coordinate-input" type="text" placeholder="38.9088661,140.8097197" style="width:100%;margin:4px 0" autocomplete="off">
      <button type="submit">この位置へ移動</button>
      <p id="coordinate-result" role="status"></p>
    </form>
    <label>表示 <select id="view-mode"><option value="map">地図の写真・動画</option><option value="unlocated">位置情報のない写真</option></select></label>
    <div id="selection-tools" hidden><button id="select-page" type="button">このページをすべて選択</button> <button id="clear-selection" type="button">すべて解除</button> <span id="selection-count">0枚選択</span></div>
    <div id="date-filters" hidden>
      <label>撮影年 <select id="filter-year"><option value="">すべての年</option><option value="unknown">撮影日時なし</option></select></label>
      <label>月 <select id="filter-month"><option value="">すべての月</option><option value="1">1月</option><option value="2">2月</option><option value="3">3月</option><option value="4">4月</option><option value="5">5月</option><option value="6">6月</option><option value="7">7月</option><option value="8">8月</option><option value="9">9月</option><option value="10">10月</option><option value="11">11月</option><option value="12">12月</option></select></label>
      <small>撮影日時は日本時間。絞り込み前の選択も保持します。</small>
    </div>
    <section id="location-editor" hidden>
      <strong>選択した写真に同じ場所を紐づける</strong>
      <p id="selected-photo"></p>
      <p>地図をクリックして場所を指定してください。青い丸はドラッグで調整できます。</p>
      <p id="location-suggestion"></p>
      <button id="use-suggestion" type="button" hidden>候補の場所を地図で確認</button>
      <p id="selected-coordinates">場所が未指定です</p>
      <button id="save-location" type="button" disabled>選択した写真にこの場所を保存</button>
      <button id="cancel-location" type="button">選択を解除</button>
    </section>
    <p id="location-result" role="status"></p>
    <p id="status" role="status" aria-live="polite" aria-busy="true">データを読み込み中…</p>
    <div id="photos"></div>
    <div id="unlocated-pages" hidden><button id="previous-page" type="button">前へ</button> <span id="page-info"></span> <button id="next-page" type="button">次へ</button></div>
  </aside>
</main>
<dialog id="photo-viewer" aria-label="写真を拡大表示">
  <button id="photo-viewer-close" type="button" autofocus>閉じる ×</button>
  <img id="photo-viewer-image" alt="">
  <p id="photo-viewer-caption"></p>
</dialog>
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
<script>
const map = L.map('map').setView([38.2682, 140.8694], 8);
L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
  maxZoom: 19,
  attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap contributors</a>'
}).addTo(map);

const layer = L.layerGroup().addTo(map);
const markers = new Map();
let visibleMediaIDs = new Set();
let requestNo = 0;

let heicLibrary;
let enlargedPhotoURL;
function closeEnlargedPhoto() {
  document.getElementById('photo-viewer-image').removeAttribute('src');
  if (enlargedPhotoURL) URL.revokeObjectURL(enlargedPhotoURL);
  enlargedPhotoURL = undefined;
}
function enlargePhoto(src, name, blob) {
  closeEnlargedPhoto();
  const img = document.getElementById('photo-viewer-image');
  if (blob) enlargedPhotoURL = URL.createObjectURL(blob);
  img.src = enlargedPhotoURL || src;
  img.alt = name;
  img.onerror = () => { document.getElementById('photo-viewer-caption').textContent = '画像を表示できません: ' + name; };
  document.getElementById('photo-viewer-caption').textContent = name;
  document.getElementById('photo-viewer').showModal();
}
let heicQueue = Promise.resolve();
const heicPreviews = new Map();

function loadHeicLibrary() {
  if (!heicLibrary) {
    heicLibrary = new Promise((resolve, reject) => {
      const script = document.createElement('script');
      script.src = 'https://cdn.jsdelivr.net/npm/heic-to@1.5.2/dist/iife/heic-to.js';
      script.onload = () => typeof window.HeicTo === 'function'
        ? resolve(window.HeicTo) : reject(new Error('HEIC変換機能を読み込めません'));
      script.onerror = () => { script.remove(); reject(new Error('HEIC変換機能を読み込めません')); };
      document.head.append(script);
    }).catch(error => { heicLibrary = undefined; throw error; });
  }
  return heicLibrary;
}

function heicPreview(src) {
  if (heicPreviews.has(src)) {
    const cached = heicPreviews.get(src);
    heicPreviews.delete(src);
    heicPreviews.set(src, cached);
    return cached;
  }
  // 大きい画像の同時変換でメモリを使い切らないよう直列に処理する。
  const pending = heicQueue.then(async () => {
    const response = await fetch(src);
    if (!response.ok) throw new Error('画像を読み込めません');
    const blob = await response.blob();
    // Takeoutには拡張子がHEICでも中身がJPEG/PNGのファイルがある。
    const header = new Uint8Array(await blob.slice(0, 12).arrayBuffer());
    let type;
    if (header[0] === 0xff && header[1] === 0xd8 && header[2] === 0xff) type = 'image/jpeg';
    else if ([137, 80, 78, 71, 13, 10, 26, 10].every((byte, i) => header[i] === byte)) type = 'image/png';
    if (type) return blob.slice(0, blob.size, type);
    const convert = await loadHeicLibrary();
    return convert({ blob, type: 'image/jpeg', quality: 0.8 });
  });
  heicQueue = pending.catch(() => {});
  heicPreviews.set(src, pending);
  pending.catch(() => {
    if (heicPreviews.get(src) === pending) heicPreviews.delete(src);
  });
  while (heicPreviews.size > 12) heicPreviews.delete(heicPreviews.keys().next().value);
  return pending;
}

function bindHeicPopup(marker, p, src) {
  const content = document.createElement('div');
  let generation = 0;
  let objectURL;
  const release = () => {
    if (objectURL) URL.revokeObjectURL(objectURL);
    objectURL = undefined;
  };
  // 自動パンによるmoveendでマーカーが作り直されるのを避ける。
  marker.bindPopup(content, { autoPan: false, minWidth: 220 });
  marker.on('popupopen', async () => {
    const current = ++generation;
    release();
    content.textContent = 'HEIC画像を読み込み中…';
    try {
      const blob = await heicPreview(src);
      if (current !== generation || !marker.isPopupOpen()) return;
      objectURL = URL.createObjectURL(blob);
      const img = document.createElement('img');
      img.className = 'popup';
      img.alt = p.name;
      img.title = 'クリックで拡大';
      img.onclick = () => enlargePhoto(src, p.name, blob);
      img.onload = () => marker.getPopup().update();
      img.onerror = () => { content.textContent = '画像を表示できません: ' + p.name; release(); };
      img.src = objectURL;
      const caption = document.createElement('div');
      caption.textContent = p.name;
      content.replaceChildren(img, caption);
      marker.getPopup().update();
    } catch (error) {
      if (current !== generation || !marker.isPopupOpen()) return;
      content.textContent = 'HEIC画像を表示できません。再度クリックしてお試しください。';
      marker.getPopup().update();
      console.error('HEIC preview:', error);
    }
  });
  marker.on('popupclose', () => { generation++; release(); });
}

const thumbnailCleanups = [];

function heicThumbnail(p, src, open) {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'heic-thumbnail';
  button.textContent = '読み込み待ち…';
  button.title = p.name;
  let disposed = false;
  let objectURL;
  let loading = false;
  let ready = false;
  const release = () => {
    if (objectURL) URL.revokeObjectURL(objectURL);
    objectURL = undefined;
  };
  const load = async () => {
    if (disposed || loading || ready) return;
    loading = true;
    button.textContent = '読み込み中…';
    try {
      const blob = await heicPreview(src);
      if (disposed) return;
      const img = document.createElement('img');
      img.alt = p.name;
      img.onload = release;
      img.onerror = () => {
        release();
        ready = false;
        button.textContent = '表示できません。クリックで再試行';
      };
      objectURL = URL.createObjectURL(blob);
      img.src = objectURL;
      button.replaceChildren(img);
      ready = true;
    } catch (error) {
      if (!disposed) button.textContent = '表示できません。クリックで再試行';
      console.error('HEIC thumbnail:', error);
    } finally {
      loading = false;
    }
  };
  button.onclick = () => { if (ready) open(); else load(); };
  const observer = new IntersectionObserver(entries => {
    if (entries.some(entry => entry.isIntersecting)) {
      observer.disconnect();
      load();
    }
  }, { root: document.getElementById('side'), rootMargin: '100px' });
  observer.observe(button);
  thumbnailCleanups.push(() => { disposed = true; observer.disconnect(); release(); });
  return button;
}

function reconcileMarkers(items) {
  visibleMediaIDs = new Set(items.map(p => p.id));
  for (const [id, marker] of markers) {
    // 検索結果から外れても、ユーザーが開いているプレビューは保持する。
    if (!visibleMediaIDs.has(id) && !marker.isPopupOpen()) {
      layer.removeLayer(marker);
      markers.delete(id);
    }
  }
}

async function refresh() {
  if (document.getElementById('view-mode').value !== 'map') return;
  const myRequest = ++requestNo;
  const status = document.getElementById('status');
  status.textContent = '地図の範囲のデータを読み込み中…';
  status.setAttribute('aria-busy', 'true');
  const b = map.getBounds();
  const q = new URLSearchParams({
    minLat: b.getSouth(), maxLat: b.getNorth(),
    minLng: b.getWest(), maxLng: b.getEast()
  });

  let items;
  try {
    const response = await fetch('/api/media?' + q);
    if (!response.ok) throw new Error('HTTP ' + response.status);
    items = await response.json();
    if (!Array.isArray(items)) throw new Error('Invalid media response');
  } catch (error) {
    if (myRequest !== requestNo) return;
    status.textContent = '読み込みに失敗しました。地図を動かすか、ページを再読み込みしてください。';
    status.setAttribute('aria-busy', 'false');
    console.error('Media loading:', error);
    return;
  }
  if (myRequest !== requestNo) return;
  status.setAttribute('aria-busy', 'false');

  reconcileMarkers(items);
  const box = document.getElementById('photos');
  thumbnailCleanups.splice(0).forEach(cleanup => cleanup());
  box.replaceChildren();

  const photos = items.filter(p => p.type === 'photo').length;
  const videos = items.filter(p => p.type === 'video').length;
  document.getElementById('status').textContent =
    '写真 ' + photos + '枚 / 動画 ' + videos + '本（地図全体から分散して最大{{.Limit}}件表示）';

  for (const p of items) {
    const src = '/media?id=' + p.id;
    const isVideo = p.type === 'video';
    const isHEIC = p.format === 'heic' || p.format === 'heif';

    let popupHTML;
    if (isVideo) {
      popupHTML = '<video class="popup-video" src="' + src + '" controls preload="metadata"></video><br>' + escapeHTML(p.name);
    } else if (isHEIC) {
      popupHTML = '<div class="media-note">HEIC/HEIF画像<br>' + escapeHTML(p.name) + '</div>';
    } else {
      popupHTML = '<img class="popup" src="' + src + '"><br>' + escapeHTML(p.name);
    }

    let marker = markers.get(p.id);
    if (!marker) {
      marker = L.marker([p.lat, p.lng]).addTo(layer);
      markers.set(p.id, marker);
      marker.on('click', () => {
        const position = marker.getLatLng();
        pickLocation(position.lat, position.lng);
      });
      if (isHEIC) bindHeicPopup(marker, p, src);
      else {
        marker.bindPopup(popupHTML);
        if (!isVideo) marker.on('popupopen', () => {
          const img = marker.getPopup().getElement().querySelector('img.popup');
          if (img) {
            img.alt = p.name;
            img.title = 'クリックで拡大';
            img.onclick = () => enlargePhoto(src, p.name);
          }
        });
      }
      marker.on('popupclose', () => {
        if (!visibleMediaIDs.has(p.id)) {
          markers.delete(p.id);
          layer.removeLayer(marker);
        }
      });
    }
    marker.setLatLng([p.lat, p.lng]);
    const fig = document.createElement('figure');
    fig.className = 'card';

    if (isVideo) {
      const video = document.createElement('video');
      video.src = src;
      video.controls = true;
      video.preload = 'metadata';
      fig.append(video);
    } else if (isHEIC) {
      fig.append(heicThumbnail(p, src, () => {
        map.setView([p.lat, p.lng], Math.max(map.getZoom(), 15));
        marker.openPopup();
      }));
    } else {
      const img = document.createElement('img');
      img.src = src;
      img.loading = 'lazy';
      img.alt = p.name;
      img.onerror = () => {
        const note = document.createElement('div');
        note.className = 'media-note';
        note.textContent = isHEIC ? 'HEIC/HEIF画像（ブラウザで直接表示できません）' : '画像を表示できません';
        img.replaceWith(note);
      };
      img.onclick = () => {
        map.setView([p.lat, p.lng], Math.max(map.getZoom(), 15));
        marker.openPopup();
      };
      fig.append(img);
    }

    const cap = document.createElement('figcaption');
    cap.textContent = (isVideo ? '🎬 ' : '📷 ') + p.name + ' [' + p.source.toUpperCase() + ']';
    fig.append(cap);
    box.append(fig);
  }
}

function escapeHTML(s) {
  const e = document.createElement('div');
  e.textContent = s;
  return e.innerHTML;
}

let unlocatedOffset = 0;
let selectedPhoto;
const selectedPhotos = new Map();
let unlocatedPage = [];
const selectionCheckboxes = new Map();
let pickedLocation;
let pickedMarker;
let savingLocation = false;
function clearLocationSelection() {
  selectedPhotos.clear();
  selectedPhoto = undefined;
  pickedLocation = undefined;
  if (pickedMarker) map.removeLayer(pickedMarker);
  pickedMarker = undefined;
  document.getElementById('location-editor').hidden = true;
  syncPhotoSelection();
}
function syncPhotoSelection() {
  for (const [id, checkbox] of selectionCheckboxes) {
    checkbox.checked = selectedPhotos.has(id);
    checkbox.disabled = savingLocation;
  }
  document.getElementById('selection-count').textContent = selectedPhotos.size + '枚選択';
  document.getElementById('select-page').disabled = savingLocation;
  document.getElementById('clear-selection').disabled = savingLocation;
}
function pickLocation(lat, lng) {
  if (savingLocation) return;
  if (lng < -180 || lng > 180) lng = ((lng + 180) % 360 + 360) % 360 - 180;
  document.getElementById('coordinate-input').value = lat + ',' + lng;
  document.getElementById('coordinate-result').textContent = '';
  if (!selectedPhoto) return;
  pickedLocation = { lat, lng };
  if (!pickedMarker) {
    pickedMarker = L.marker([lat, lng], { draggable: true, icon: L.divIcon({ html: '<div style="width:20px;height:20px;border:3px solid white;border-radius:50%;background:#1769aa;box-shadow:0 0 4px #333"></div>', className: '', iconSize: [20,20], iconAnchor: [10,10] }) }).addTo(map);
    pickedMarker.on('dragend', () => { const p = pickedMarker.getLatLng(); pickLocation(p.lat, p.lng); });
    pickedMarker.on('click', () => { const p = pickedMarker.getLatLng(); pickLocation(p.lat, p.lng); });
  } else pickedMarker.setLatLng([lat, lng]);
  document.getElementById('selected-coordinates').textContent = '緯度 ' + lat.toFixed(6) + ' / 経度 ' + lng.toFixed(6);
  document.getElementById('save-location').disabled = false;
  document.getElementById('location-result').textContent = '';
}
function selectUnlocatedPhoto(p, checked = !selectedPhotos.has(p.id)) {
  if (savingLocation) return;
  if (checked) selectedPhotos.set(p.id, p); else selectedPhotos.delete(p.id);
  syncPhotoSelection();
  if (!selectedPhotos.size) { clearLocationSelection(); return; }
  selectedPhoto = selectedPhotos.values().next().value;
  p = selectedPhoto;
  document.getElementById('location-editor').hidden = false;
  document.getElementById('selected-photo').textContent = selectedPhotos.size + '枚選択（別ページ・絞り込み外の選択を含む）。候補の基準: ' + p.name + (p.takenAt ? ' / ' + new Date(p.takenAt * 1000).toLocaleString() : ' / 撮影日時なし');
  if (!pickedLocation) document.getElementById('selected-coordinates').textContent = '場所が未指定です';
  document.getElementById('save-location').disabled = !pickedLocation;
  document.getElementById('location-result').textContent = '';
  const suggestion = p.suggestion;
  document.getElementById('location-suggestion').textContent = suggestion
    ? '候補: ' + suggestion.name + '（撮影時刻の差 ' + Math.round(suggestion.differenceSeconds / 60) + '分）。同じ場所とは限らないため地図で確認してください。'
    : '撮影日時が前後1時間以内の位置情報付き写真は見つかりません。日時はTakeout JSONを使用します。';
  const use = document.getElementById('use-suggestion');
  use.hidden = !suggestion;
  use.onclick = () => {
    pickLocation(suggestion.lat, suggestion.lng);
    map.setView([suggestion.lat, suggestion.lng], 15);
  };
}
async function loadUnlocated() {
  const current = ++requestNo;
  const status = document.getElementById('status');
  status.textContent = '位置情報のない写真を読み込み中…';
  status.setAttribute('aria-busy', 'true');
  try {
    const query = new URLSearchParams({ offset: unlocatedOffset, year: document.getElementById('filter-year').value, month: document.getElementById('filter-month').value });
    const response = await fetch('/api/unlocated?' + query);
    if (!response.ok) throw new Error('一覧を読み込めません');
    const data = await response.json();
    if (current !== requestNo) return;
    const yearSelect = document.getElementById('filter-year');
    const selectedYear = yearSelect.value;
    const years = new Set(data.years);
    if (selectedYear && selectedYear !== 'unknown') years.add(Number(selectedYear));
    yearSelect.replaceChildren();
    for (const [value, text] of [['', 'すべての年'], ...Array.from(years).sort((a,b) => b-a).map(year => [String(year), year + '年']), ['unknown', '撮影日時なし']]) {
      const option = document.createElement('option'); option.value = value; option.textContent = text; yearSelect.append(option);
    }
    yearSelect.value = selectedYear;
    unlocatedOffset = data.offset;
    unlocatedPage = data.items;
    selectionCheckboxes.clear();
    thumbnailCleanups.splice(0).forEach(cleanup => cleanup());
    const box = document.getElementById('photos');
    box.replaceChildren();
    for (const p of data.items) {
      const fig = document.createElement('figure'); fig.className = 'card';
      const src = '/media?id=' + p.id;
      if (p.format === 'heic' || p.format === 'heif') fig.append(heicThumbnail(p, src, () => selectUnlocatedPhoto(p)));
      else {
        const img = document.createElement('img'); img.loading = 'lazy'; img.src = src; img.alt = p.name;
        img.onclick = () => selectUnlocatedPhoto(p); fig.append(img);
      }
      const caption = document.createElement('figcaption'); caption.textContent = p.name; caption.title = p.name; fig.append(caption);
      const date = document.createElement('div'); date.textContent = p.takenAt ? new Date(p.takenAt * 1000).toLocaleDateString('ja-JP', {timeZone:'Asia/Tokyo'}) : '撮影日時なし'; fig.append(date);
      const label = document.createElement('label'); label.className = 'location-select';
      const checkbox = document.createElement('input'); checkbox.type = 'checkbox';
      checkbox.checked = selectedPhotos.has(p.id); checkbox.disabled = savingLocation;
      checkbox.onchange = () => selectUnlocatedPhoto(p, checkbox.checked);
      selectionCheckboxes.set(p.id, checkbox);
      label.append(checkbox, document.createTextNode('この写真を選択')); fig.append(label);
      box.append(fig);
    }
    status.textContent = '位置情報のない写真 ' + data.total + '枚';
    document.getElementById('previous-page').disabled = unlocatedOffset === 0;
    document.getElementById('next-page').disabled = unlocatedOffset + data.items.length >= data.total;
    document.getElementById('page-info').textContent = data.total ? (unlocatedOffset + 1) + '–' + (unlocatedOffset + data.items.length) + ' / ' + data.total : '0枚';
  } catch (error) { if (current === requestNo) status.textContent = '一覧の読み込みに失敗しました。表示を切り替えて再試行してください。'; }
  finally { if (current === requestNo) status.setAttribute('aria-busy', 'false'); }
}
document.getElementById('coordinate-form').onsubmit = event => {
  event.preventDefault();
  const result = document.getElementById('coordinate-result');
  if (savingLocation) { result.textContent = '保存が終わってから移動してください。'; return; }
  const parts = document.getElementById('coordinate-input').value.trim().replace(/，/g, ',').split(',');
  const decimal = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$/;
  if (parts.length !== 2 || !parts.every(part => decimal.test(part.trim()))) {
    result.textContent = '緯度,経度の順で入力してください（例: 38.9088661,140.8097197）。'; return;
  }
  const [lat, lng] = parts.map(Number);
  if (!Number.isFinite(lat) || !Number.isFinite(lng) || lat < -90 || lat > 90 || lng < -180 || lng > 180) {
    result.textContent = '緯度は-90〜90、経度は-180〜180で入力してください。'; return;
  }
  pickLocation(lat, lng);
  map.setView([lat, lng], 15);
  result.textContent = selectedPhotos.size
    ? '指定位置へ移動しました。選択した写真への紐づけは保存ボタンで確定します。'
    : '指定位置へ移動しました。';
};
document.getElementById('view-mode').onchange = () => {
  ++requestNo;
  clearLocationSelection();
  const editing = document.getElementById('view-mode').value === 'unlocated';
  document.getElementById('unlocated-pages').hidden = !editing;
  document.getElementById('selection-tools').hidden = !editing;
  document.getElementById('date-filters').hidden = !editing;
  if (editing) { unlocatedOffset = 0; loadUnlocated(); } else refresh();
};
document.getElementById('previous-page').onclick = () => { unlocatedOffset = Math.max(0, unlocatedOffset - 30); loadUnlocated(); };
function changeDateFilter() {
  const unknown = document.getElementById('filter-year').value === 'unknown';
  const month = document.getElementById('filter-month');
  month.disabled = unknown;
  if (unknown) month.value = '';
  unlocatedOffset = 0;
  loadUnlocated();
}
document.getElementById('filter-year').onchange = changeDateFilter;
document.getElementById('filter-month').onchange = changeDateFilter;
document.getElementById('next-page').onclick = () => { unlocatedOffset += 30; loadUnlocated(); };
document.getElementById('cancel-location').onclick = clearLocationSelection;
document.getElementById('clear-selection').onclick = clearLocationSelection;
document.getElementById('select-page').onclick = () => {
  if (savingLocation) return;
  for (const p of unlocatedPage) selectedPhotos.set(p.id, p);
  if (unlocatedPage.length) selectUnlocatedPhoto(unlocatedPage[0], true);
};
map.on('click', e => pickLocation(e.latlng.lat, e.latlng.lng));
document.getElementById('save-location').onclick = async () => {
  if (!selectedPhoto || !pickedLocation || savingLocation) return;
  savingLocation = true;
  syncPhotoSelection();
  const ids = Array.from(selectedPhotos.keys());
  const button = document.getElementById('save-location'); button.disabled = true;
  document.getElementById('cancel-location').disabled = true;
  document.getElementById('view-mode').disabled = true;
  if (pickedMarker) pickedMarker.dragging.disable();
  try {
    const response = await fetch('/api/location', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids, ...pickedLocation }) });
    if (!response.ok) throw new Error(await response.text());
    clearLocationSelection();
    document.getElementById('location-result').textContent = ids.length + '枚の位置情報を保存しました。地図の写真・動画に切り替えると反映されます。';
    await loadUnlocated();
  } catch (error) { document.getElementById('location-result').textContent = '保存に失敗しました: ' + error.message; }
  finally {
    savingLocation = false; button.disabled = !pickedLocation || !selectedPhotos.size;
    syncPhotoSelection();
    document.getElementById('cancel-location').disabled = false;
    document.getElementById('view-mode').disabled = false;
    if (pickedMarker) pickedMarker.dragging.enable();
  }
};

map.on('moveend', refresh);
const photoViewer = document.getElementById('photo-viewer');
document.getElementById('photo-viewer-close').onclick = () => photoViewer.close();
photoViewer.addEventListener('close', closeEnlargedPhoto);
photoViewer.addEventListener('click', event => { if (event.target === photoViewer) photoViewer.close(); });
refresh();
</script>
</body>
</html>`
