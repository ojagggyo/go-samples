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
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Format string  `json:"format"`
	Type   string  `json:"type"`   // photo / video
	Source string  `json:"source"` // exif / json
	path   string
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
	folderCounts = make(map[string]*FolderCount)
	mu           sync.RWMutex
)

type TakeoutJSON struct {
	Title       string          `json:"title"`
	GeoDataExif TakeoutLocation `json:"geoDataExif"`
	GeoData     TakeoutLocation `json:"geoData"`
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
	lat float64
	lng float64
}

func main() {
	dir := flag.String("photos", "./photos", "Google Photos/Takeout を展開したフォルダ")
	addr := flag.String("addr", "127.0.0.1:8080", "待受アドレス")
	rebuildCache := flag.Bool("rebuild-cache", false, "保存済みキャッシュを使わず位置情報を再解析")
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

		if !ok {
			key := mediaKey(filepath.Dir(path), filepath.Base(path))
			c, found := jsonLocations[key]
			if !found {
				return nil
			}
			lat, lng = c.lat, c.lng
			ok = validLocation(lat, lng)
			if ok {
				source = "json"
			}
		}

		if !ok || !validLocation(lat, lng) {
			return nil
		}

		item := Media{
			Name:   filepath.Base(path),
			Lat:    lat,
			Lng:    lng,
			Format: strings.TrimPrefix(ext, "."),
			Type:   mediaType,
			Source: source,
			path:   path,
		}

		mu.Lock()
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
		if !entry.OK {
			return nil
		}
		c := coordinates{lat: entry.Lat, lng: entry.Lng}

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

	if err != nil || id < 0 || id >= len(media) {
		http.NotFound(w, r)
		return
	}

	p := media[id]
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
    .card figcaption { padding: 5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
    .popup { width: 220px; max-height: 170px; object-fit: cover; }
    .popup-video { width: 240px; max-height: 180px; background: #111; }
    .media-note { padding: 12px; font-size: 12px; background: white; }
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
    <p id="status" role="status" aria-live="polite" aria-busy="true">データを読み込み中…</p>
    <div id="photos"></div>
  </aside>
</main>
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
    const convert = await loadHeicLibrary();
    const response = await fetch(src);
    if (!response.ok) throw new Error('画像を読み込めません');
    return convert({ blob: await response.blob(), type: 'image/jpeg', quality: 0.8 });
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
  marker.bindPopup(content, { autoPan: false });
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
      console.error('HEIC preview:', error);
    }
  });
  marker.on('popupclose', () => { generation++; release(); });
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
      if (isHEIC) bindHeicPopup(marker, p, src);
      else marker.bindPopup(popupHTML);
      marker.on('popupclose', () => {
        if (!visibleMediaIDs.has(p.id)) {
          markers.delete(p.id);
          layer.removeLayer(marker);
        }
      });
    }
    const fig = document.createElement('figure');
    fig.className = 'card';

    if (isVideo) {
      const video = document.createElement('video');
      video.src = src;
      video.controls = true;
      video.preload = 'metadata';
      fig.append(video);
    } else if (isHEIC) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'media-note';
      button.textContent = 'HEIC/HEIF画像を表示';
      button.onclick = () => marker.openPopup();
      fig.append(button);
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

map.on('moveend', refresh);
refresh();
</script>
</body>
</html>`
