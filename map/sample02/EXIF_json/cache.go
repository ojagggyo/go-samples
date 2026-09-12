package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

const cacheVersion = 2

type cachedLocation struct {
	Size    int64
	ModTime int64
	JSON    bool
	Title   string
	Lat     float64
	Lng     float64
	OK      bool
	TakenAt int64
}

type cacheFile struct {
	Version int
	Root    string
	Entries map[string]cachedLocation
}

type metadataCache struct {
	progress *loadProgress
	path     string
	root     string
	old      map[string]cachedLocation
	next     map[string]cachedLocation
	hits     int
	misses   int
}

func defaultCachePath(root string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		log.Printf("キャッシュ保存先を取得できません: %v", err)
		return ""
	}
	return filepath.Join(dir, "photo-map-sample", fmt.Sprintf("%x.json", sha256.Sum256([]byte(root))))
}

func openMetadataCache(path, root string, rebuild bool) *metadataCache {
	c := &metadataCache{path: path, root: root, next: make(map[string]cachedLocation)}
	if rebuild || path == "" {
		return c
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("キャッシュを読み込めないため再解析します: %v", err)
		}
		return c
	}
	var saved cacheFile
	if err := json.Unmarshal(data, &saved); err != nil || saved.Version != cacheVersion || saved.Root != root {
		log.Print("キャッシュが無効なため再解析します")
		return c
	}
	c.old = saved.Entries
	return c
}

// 内容を開く前にサイズと更新日時を比較する。位置情報のないファイルも再利用する。
func (c *metadataCache) read(path string, isJSON bool) cachedLocation {
	info, err := os.Stat(path)
	if err != nil {
		return cachedLocation{}
	}
	if entry, found := c.old[path]; found && entry.Size == info.Size() && entry.ModTime == info.ModTime().UnixNano() && entry.JSON == isJSON && (!entry.OK || validLocation(entry.Lat, entry.Lng)) {
		c.hits++
		c.next[path] = entry
		return entry
	}
	c.misses++
	entry := cachedLocation{Size: info.Size(), ModTime: info.ModTime().UnixNano(), JSON: isJSON}
	if isJSON {
		data, err := os.ReadFile(path)
		if err != nil {
			return entry // 一時的に読めないファイルは次回再試行する。
		}
		var metadata TakeoutJSON
		if json.Unmarshal(data, &metadata) == nil {
			location, ok := takeoutCoordinates(metadata)
			entry.Title, entry.Lat, entry.Lng, entry.OK = metadata.Title, location.lat, location.lng, ok
			entry.TakenAt, _ = strconv.ParseInt(metadata.PhotoTakenTime.Timestamp, 10, 64)
		}
	} else {
		entry.Lat, entry.Lng, entry.OK, err = locationFromEXIF(path)
		if err != nil {
			return entry
		}
	}
	if !entry.OK {
		entry.Lat, entry.Lng = 0, 0
	}
	// 解析中に書き換わった場合は、次回の再解析対象にする。
	if after, err := os.Stat(path); err == nil && after.Size() == entry.Size && after.ModTime().UnixNano() == entry.ModTime {
		c.next[path] = entry
	}
	return entry
}

func (c *metadataCache) save() error {
	if c.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.path), "metadata-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := json.NewEncoder(f).Encode(cacheFile{Version: cacheVersion, Root: c.root, Entries: c.next}); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), c.path)
}
