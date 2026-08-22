package main

import (
	"encoding/csv"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

type csvResponse struct {
	CSV [][]string `json:"csv"`
}

func RunServer(cfg Config) error {
	mux := http.NewServeMux()

	// 同じGoサーバーからHTML/JS/CSS/APIを配信するため、
	// ranking.js から /api/... を呼べば通常CORSは不要。
	mux.HandleFunc("/api/csv", func(w http.ResponseWriter, r *http.Request) {
		handleCSV(cfg, w, r)
	})

	mux.Handle("/data/", http.StripPrefix("/data/",
		http.FileServer(http.Dir(cfg.DataDir))))

	mux.Handle("/", http.FileServer(http.Dir(cfg.WebDir)))

	log.Printf("listen on %s", cfg.ListenAddr)
	log.Printf("web dir : %s", cfg.WebDir)
	log.Printf("data dir: %s", cfg.DataDir)

	return http.ListenAndServe(cfg.ListenAddr, mux)
}

func handleCSV(cfg Config, w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("filename")

	if filename == "" {
		http.Error(w, "filename is required", http.StatusBadRequest)
		return
	}

	// path traversal 防止。
	filename = filepath.Base(filename)

	if filepath.Ext(filename) == "" {
		filename += ".csv"
	}

	fullpath := filepath.Join(cfg.DataDir, filename)

	if _, err := os.Stat(fullpath); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, csvResponse{CSV: [][]string{}})
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	list, err := ReadRawCSV(fullpath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	title := r.URL.Query().Has("title")
	tail := parseInt(r.URL.Query().Get("tail"))
	head := parseInt(r.URL.Query().Get("head"))

	result := applyRange(list, title, tail, head)

	writeJSON(w, csvResponse{CSV: result})
}

func parseInt(s string) int {
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func ReadRawCSV(filename string) ([][]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	return r.ReadAll()
}

// getcsv2.phpの現在の動作に近い形で title / tail / head を処理する。
func applyRange(rows [][]string, title bool, tail, head int) [][]string {
	if len(rows) == 0 {
		return rows
	}

	result := make([][]string, 0, len(rows))

	data := rows

	if title {
		result = append(result, rows[0])
		data = rows[1:]
	}

	if tail > 0 && tail < len(data) {
		data = data[len(data)-tail:]
	}

	if head > 0 && head < len(data) {
		data = data[:head]
	}

	result = append(result, data...)
	return result
}
