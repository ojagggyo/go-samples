package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/rwcarlsen/goexif/exif"
)

type Photo struct {
	ID   int     `json:"id"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
	path string
}

var (
	photos []Photo
	mu     sync.RWMutex
)

func main() {
	dir := flag.String("photos", "./photos", "写真フォルダ")
	addr := flag.String("addr", "127.0.0.1:8080", "待受アドレス")
	flag.Parse()

	abs, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatal(err)
	}
	if err := scanPhotos(abs); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/api/photos", photosHandler)
	http.HandleFunc("/photo", photoHandler)

	log.Printf("位置情報付き写真: %d枚", len(photos))
	log.Printf("ブラウザで http://%s を開いてください", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func scanPhotos(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			log.Printf("スキップ: %s: %v", path, err)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".jpg" && ext != ".jpeg" {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		x, err := exif.Decode(f)
		f.Close()
		if err != nil {
			return nil
		}
		lat, lng, err := x.LatLong()
		if err != nil {
			return nil
		}
		mu.Lock()
		photos = append(photos, Photo{ID: len(photos), Name: filepath.Base(path), Lat: lat, Lng: lng, path: path})
		mu.Unlock()
		return nil
	})
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := template.Must(template.New("index").Parse(indexHTML)).Execute(w, nil); err != nil {
		log.Println(err)
	}
}

func photosHandler(w http.ResponseWriter, r *http.Request) {
	minLat, e1 := strconv.ParseFloat(r.URL.Query().Get("minLat"), 64)
	maxLat, e2 := strconv.ParseFloat(r.URL.Query().Get("maxLat"), 64)
	minLng, e3 := strconv.ParseFloat(r.URL.Query().Get("minLng"), 64)
	maxLng, e4 := strconv.ParseFloat(r.URL.Query().Get("maxLng"), 64)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		http.Error(w, "地図範囲が不正です", http.StatusBadRequest)
		return
	}

	result := make([]Photo, 0)
	mu.RLock()
	for _, p := range photos {
		if p.Lat >= minLat && p.Lat <= maxLat && p.Lng >= minLng && p.Lng <= maxLng {
			result = append(result, p)
			if len(result) >= 500 {
				break
			}
		}
	}
	mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func photoHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	mu.RLock()
	defer mu.RUnlock()
	if err != nil || id < 0 || id >= len(photos) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", photos[id].Name))
	http.ServeFile(w, r, photos[id].path)
}

const indexHTML = `<!doctype html>
<html lang="ja">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>写真マップ</title>
  <link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css">
  <style>
    * { box-sizing: border-box; }
    body { margin: 0; font-family: system-ui, sans-serif; background: #f5f5f5; }
    header { height: 48px; padding: 12px 16px; background: #202124; color: white; }
    main { display: grid; grid-template-columns: 2fr 1fr; height: calc(100vh - 48px); }
    #map { min-height: 360px; }
    #side { overflow: auto; padding: 10px; }
    #status { margin: 0 0 10px; color: #444; }
    #photos { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
    .card { margin: 0; background: white; border-radius: 6px; overflow: hidden; box-shadow: 0 1px 4px #bbb; }
    .card img { display: block; width: 100%; height: 130px; object-fit: cover; cursor: pointer; }
    .card figcaption { padding: 5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
    .popup { width: 180px; max-height: 140px; object-fit: cover; }
    @media (max-width: 800px) { main { grid-template-columns: 1fr; grid-template-rows: 55% 45%; } }
  </style>
</head>
<body>
  <header>写真マップ ― 地図を移動すると表示範囲の写真が切り替わります</header>
  <main><div id="map"></div><aside id="side"><p id="status">検索中…</p><div id="photos"></div></aside></main>
  <script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
  <script>
    const map = L.map('map').setView([38.2682, 140.8694], 8);
    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap contributors</a>'
    }).addTo(map);
    const layer = L.layerGroup().addTo(map);
    let requestNo = 0;

    async function refresh() {
      const myRequest = ++requestNo;
      const b = map.getBounds();
      const q = new URLSearchParams({minLat:b.getSouth(), maxLat:b.getNorth(), minLng:b.getWest(), maxLng:b.getEast()});
      const response = await fetch('/api/photos?' + q);
      const items = await response.json();
      if (myRequest !== requestNo) return;
      layer.clearLayers();
      const box = document.getElementById('photos');
      box.replaceChildren();
      document.getElementById('status').textContent = items.length + '枚（最大500枚）';
      for (const p of items) {
        const src = '/photo?id=' + p.id;
        const marker = L.marker([p.lat, p.lng]).addTo(layer).bindPopup('<img class="popup" src="' + src + '"><br>' + escapeHTML(p.name));
        const fig = document.createElement('figure');
        fig.className = 'card';
        const img = document.createElement('img');
        img.src = src;
        img.loading = 'lazy';
        img.alt = p.name;
        img.onclick = () => { map.setView([p.lat, p.lng], Math.max(map.getZoom(), 15)); marker.openPopup(); };
        const cap = document.createElement('figcaption');
        cap.textContent = p.name;
        fig.append(img, cap);
        box.append(fig);
      }
    }
    function escapeHTML(s) { const e=document.createElement('div'); e.textContent=s; return e.innerHTML; }
    map.on('moveend', refresh);
    refresh();
  </script>
</body>
</html>`
