package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type csvResponse struct {
	CSV [][]string `json:"csv"`
}

type witnessHistoryPoint struct {
	Time  string  `json:"time"`
	Votes float64 `json:"votes"`
	Miss  int64   `json:"miss"`
}

type witnessHistoryResponse struct {
	User   string                `json:"user"`
	Span   string                `json:"span"`
	Points []witnessHistoryPoint `json:"points"`
}

func RunServer(cfg Config) error {
	mux := http.NewServeMux()

	// 同じGoサーバーからHTML/JS/CSS/APIを配信するため、
	// ranking.js から /api/... を呼べば通常CORSは不要。

	// CSV取得API
	mux.HandleFunc("/api/csv", func(w http.ResponseWriter, r *http.Request) {
		handleCSV(cfg, w, r)
	})

	// 上位Witnessの最新ブロック時刻取得API
	mux.HandleFunc("/api/block-times", handleBlockTimes)

	// Witnessの投票・MISS履歴取得API
	mux.HandleFunc("/api/witness-history", func(w http.ResponseWriter, r *http.Request) {
		handleWitnessHistory(cfg, w, r)
	})

	mux.HandleFunc("/api/witness-blocks", handleWitnessBlocks)

	// CSVデータファイル
	mux.Handle(
		"/data/",
		http.StripPrefix(
			"/data/",
			http.FileServer(
				http.Dir(cfg.DataDir),
			),
		),
	)

	// HTML / JavaScript / CSS
	mux.Handle(
		"/",
		http.FileServer(
			http.Dir(cfg.WebDir),
		),
	)

	log.Printf("listen on %s", cfg.ListenAddr)
	log.Printf("web dir : %s", cfg.WebDir)
	log.Printf("data dir: %s", cfg.DataDir)

	return http.ListenAndServe(
		cfg.ListenAddr,
		mux,
	)
}

func handleCSV(
	cfg Config,
	w http.ResponseWriter,
	r *http.Request,
) {
	filename := r.URL.Query().Get("filename")

	if filename == "" {
		http.Error(
			w,
			"filename is required",
			http.StatusBadRequest,
		)
		return
	}

	// path traversal 防止。
	filename = filepath.Base(filename)

	if filepath.Ext(filename) == "" {
		filename += ".csv"
	}

	fullpath := filepath.Join(
		cfg.DataDir,
		filename,
	)

	if _, err := os.Stat(fullpath); err != nil {
		if os.IsNotExist(err) {
			writeJSON(
				w,
				csvResponse{
					CSV: [][]string{},
				},
			)
			return
		}

		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	list, err := ReadRawCSV(fullpath)

	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	title := r.URL.Query().Has("title")

	tail := parseInt(
		r.URL.Query().Get("tail"),
	)

	head := parseInt(
		r.URL.Query().Get("head"),
	)

	result := applyRange(
		list,
		title,
		tail,
		head,
	)

	writeJSON(
		w,
		csvResponse{
			CSV: result,
		},
	)
}

func parseInt(s string) int {
	if s == "" {
		return 0
	}

	n, _ := strconv.Atoi(s)

	return n
}

func writeJSON(
	w http.ResponseWriter,
	v interface{},
) {
	w.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)

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

// getcsv2.phpの現在の動作に近い形で
// title / tail / head を処理する。
func applyRange(
	rows [][]string,
	title bool,
	tail int,
	head int,
) [][]string {
	if len(rows) == 0 {
		return rows
	}

	result := make(
		[][]string,
		0,
		len(rows),
	)

	data := rows

	if title {
		result = append(
			result,
			rows[0],
		)

		data = rows[1:]
	}

	if tail > 0 && tail < len(data) {
		data = data[len(data)-tail:]
	}

	if head > 0 && head < len(data) {
		data = data[:head]
	}

	result = append(
		result,
		data...,
	)

	return result
}

// handleWitnessHistory は data/ranking の履歴CSVから、指定Witnessの
// Votes と TotalMissed の推移を取得する。
func handleWitnessHistory(
	cfg Config,
	w http.ResponseWriter,
	r *http.Request,
) {
	user := strings.TrimSpace(
		r.URL.Query().Get("user"),
	)

	if user == "" {
		http.Error(
			w,
			"user is required",
			http.StatusBadRequest,
		)
		return
	}

	span := r.URL.Query().Get("span")

	duration, ok := historyDuration(span)
	if !ok {
		http.Error(
			w,
			"invalid span",
			http.StatusBadRequest,
		)
		return
	}

	dir := filepath.Join(
		cfg.DataDir,
		"ranking",
	)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(
				w,
				witnessHistoryResponse{
					User:   user,
					Span:   span,
					Points: []witnessHistoryPoint{},
				},
			)
			return
		}

		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	now := time.Now()
	from := now.Add(-duration)

	type historyFile struct {
		name string
		time time.Time
	}

	files := make(
		[]historyFile,
		0,
		len(entries),
	)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		if !strings.HasPrefix(
			name,
			"ranking_",
		) || !strings.HasSuffix(
			name,
			".csv",
		) {
			continue
		}

		stamp := strings.TrimSuffix(
			strings.TrimPrefix(
				name,
				"ranking_",
			),
			".csv",
		)

		if len(stamp) != len("200601021504") {
			continue
		}

		t, err := time.ParseInLocation(
			"200601021504",
			stamp,
			time.Local,
		)

		if err != nil {
			continue
		}

		if t.Before(from) || t.After(now) {
			continue
		}

		files = append(
			files,
			historyFile{
				name: name,
				time: t,
			},
		)
	}

	sort.Slice(
		files,
		func(i, j int) bool {
			return files[i].time.Before(
				files[j].time,
			)
		},
	)

	// 長期間でもブラウザとサーバーの負荷が大きくなりすぎないよう、
	// 最大500点に均等間引きする。
	const maxPoints = 500

	if len(files) > maxPoints {
		sampled := make(
			[]historyFile,
			0,
			maxPoints,
		)

		for i := 0; i < maxPoints; i++ {
			index :=
				i * (len(files) - 1) /
					(maxPoints - 1)

			sampled = append(
				sampled,
				files[index],
			)
		}

		files = sampled
	}

	points := make(
		[]witnessHistoryPoint,
		0,
		len(files),
	)

	var baseMissed int64
	baseMissedSet := false

	for _, item := range files {
		rows, err := ReadRawCSV(
			filepath.Join(
				dir,
				item.name,
			),
		)

		if err != nil {
			continue
		}

		for _, row := range rows {
			if len(row) < 5 || row[0] != user {
				continue
			}

			votes, err :=
				strconv.ParseFloat(
					row[1],
					64,
				)

			if err != nil {
				break
			}

			totalMissed, err :=
				strconv.ParseInt(
					row[4],
					10,
					64,
				)

			if err != nil {
				break
			}

			if !baseMissedSet {
				baseMissed = totalMissed
				baseMissedSet = true
			}

			points = append(
				points,
				witnessHistoryPoint{
					Time: item.time.Format(
						"2006-01-02 15:04",
					),
					Votes: votes / 1000000000000,
					Miss:  totalMissed - baseMissed,
				},
			)

			break
		}
	}

	writeJSON(
		w,
		witnessHistoryResponse{
			User:   user,
			Span:   span,
			Points: points,
		},
	)
}

func handleWitnessBlocks(
	w http.ResponseWriter,
	r *http.Request,
) {
	user := strings.TrimSpace(
		r.URL.Query().Get("user"),
	)

	if user == "" {
		http.Error(w, "user is required", http.StatusBadRequest)
		return
	}

	span := r.URL.Query().Get("span")
	duration, ok := historyDuration(span)
	if !ok {
		http.Error(w, "invalid span", http.StatusBadRequest)
		return
	}

	now := time.Now()
	from := now.Add(-duration)

	blockCtx, cancel := context.WithTimeout(
		r.Context(),
		60*time.Second,
	)
	defer cancel()

	blocks, err := getWitnessBlocks(
		blockCtx,
		user,
		from,
		now,
	)
	if err != nil {
		http.Error(
			w,
			"failed to get witness block history: "+err.Error(),
			http.StatusBadGateway,
		)
		return
	}

	writeJSON(
		w,
		struct {
			User   string         `json:"user"`
			Span   string         `json:"span"`
			Blocks []witnessBlock `json:"blocks"`
		}{
			User:   user,
			Span:   span,
			Blocks: blocks,
		},
	)
}

func historyDuration(
	span string,
) (
	time.Duration,
	bool,
) {
	switch span {
	case "hour":
		return time.Hour, true

	case "day":
		return 24 * time.Hour, true

	case "week":
		return 7 * 24 * time.Hour, true

	case "month":
		return 30 * 24 * time.Hour, true

	case "year":
		return 365 * 24 * time.Hour, true
	}

	return 0, false
}
