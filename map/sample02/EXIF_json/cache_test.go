package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheReusesAndRefreshesFiles(t *testing.T) {
	previousMedia, previousCounts := media, folderCounts
	t.Cleanup(func() { media, folderCounts = previousMedia, previousCounts })
	root := t.TempDir()
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	photo := filepath.Join(root, "photo.jpg")
	sidecar := photo + ".json"
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(photo, "photo without EXIF")
	write(sidecar, `{"title":"photo.jpg","geoData":{"latitude":35,"longitude":139}}`)
	run := func(rebuild bool) *metadataCache {
		t.Helper()
		c := openMetadataCache(cachePath, root, rebuild)
		if err := scanMedia(root, c); err != nil {
			t.Fatal(err)
		}
		if err := c.save(); err != nil {
			t.Fatal(err)
		}
		return c
	}
	if c := run(false); c.misses != 2 || c.hits != 0 || len(media) != 1 {
		t.Fatalf("cold scan: hits=%d misses=%d media=%d", c.hits, c.misses, len(media))
	}
	if c := run(false); c.hits != 2 || c.misses != 0 || len(media) != 1 || media[0].path != photo || folderCounts[root].PhotoJSON != 1 {
		t.Fatal("warm scan failed to restore location, file path or counts")
	}
	// 写真が同じでも、JSONだけの変更が反映される。
	write(sidecar, `{"title":"photo.jpg","geoData":{"latitude":36.5,"longitude":140}}`)
	if c := run(false); c.hits != 1 || c.misses != 1 || media[0].Lat != 36.5 {
		t.Fatal("sidecar update was not reflected")
	}
	write(photo, "changed photo without EXIF")
	if c := run(false); c.hits != 1 || c.misses != 1 {
		t.Fatal("photo update was not reparsed")
	}
	write(filepath.Join(root, "video.mp4"), "video")
	write(filepath.Join(root, "video.mp4.json"), `{"geoData":{"latitude":40,"longitude":140}}`)
	if c := run(false); c.misses != 1 || len(media) != 2 {
		t.Fatal("new video was not discovered")
	}
	if err := os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	if c := run(false); len(media) != 1 || media[0].Type != "video" || len(c.next) != 2 {
		t.Fatal("deleted sidecar was retained")
	}
	if c := run(true); c.hits != 0 || c.misses != 2 {
		t.Fatal("forced rebuild reused cache")
	}
	write(cachePath, "broken cache")
	if c := run(false); c.hits != 0 || c.misses != 2 || len(media) != 1 {
		t.Fatal("broken cache did not recover")
	}
	if err := os.Remove(filepath.Join(root, "video.mp4")); err != nil {
		t.Fatal(err)
	}
	run(false)
	if len(media) != 0 {
		t.Fatal("deleted media was retained")
	}
}

func TestCacheChecksTimestampAndRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "meta.json")
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte(`{"geoData":{"latitude":35,"longitude":139}}`), 0600); err != nil {
		t.Fatal(err)
	}
	c := openMetadataCache(cachePath, root, false)
	c.read(path, true)
	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime().Add(-2000000000), info.ModTime().Add(-2000000000)); err != nil {
		t.Fatal(err)
	}
	c = openMetadataCache(cachePath, root, false)
	c.read(path, true)
	if c.misses != 1 {
		t.Fatal("timestamp-only change was ignored")
	}
	c = openMetadataCache(cachePath, "another root", false)
	c.read(path, true)
	if c.hits != 0 {
		t.Fatal("cache for a different root was reused")
	}
}
