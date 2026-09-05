package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// const listenAddr = "127.0.0.1:18081"
const listenAddr = ":18081"

// const pdfBaseURL = "https://www.city.sendai.jp/biseibutsu/kurashi/kenkotofukushi/kenkoiryo/ese/kansen/documents"
const pdfBaseURL = "https://www.city.sendai.jp/bisebutsu/kurashi/kenkotofukushi/kenkoiryo/ese/kansen/documents"

var dateRe = regexp.MustCompile(`令和([0-9]+)年([0-9]+)月([0-9]+)日`)

type csvResponse struct {
	CSV [][]string `json:"csv"`
}

func main() {
	update := flag.Bool("update", false, "COVID-19週報を取得してCSVを更新")
	week := flag.Int("week", 0, "週番号。省略時は前週")
	addr := flag.String("addr", listenAddr, "HTTP listen address")
	flag.Parse()

	if *update {
		if *week == 0 {
			_, w := time.Now().ISOWeek()
			*week = w - 1
			if *week <= 0 {
				*week = 52
			}
		}
		if err := updateCOVID19(*week); err != nil {
			fmt.Fprintln(os.Stderr, "update error:", err)
			os.Exit(1)
		}
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/index.html", indexHandler)
	mux.HandleFunc("/csvread.js", csvreadHandler)
	mux.HandleFunc("/api/getcsv", getCSVHandler)

	server := &http.Server{
		Addr:              *addr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Printf("COVID19 server listening on http://%s\n", *addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func baseDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(baseDir(), "index.html"))
}

func csvreadHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/csvread.js" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Join(baseDir(), "csvread.js"))
}

func getCSVHandler(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("filename")
	if filename == "" {
		filename = "covid19"
	}

	// CSVファイル名はサービスディレクトリ直下の単一ファイル名だけ許可。
	if filepath.Base(filename) != filename || strings.ContainsAny(filename, `/\`) {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	lines := 0
	if s := r.URL.Query().Get("lines"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			http.Error(w, "invalid lines", http.StatusBadRequest)
			return
		}
		lines = n
	}

	includeTitle := r.URL.Query().Has("title")

	data, err := readCSVForChart(filepath.Join(baseDir(), filename+".csv"), lines, includeTitle)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSResponse(w, csvResponse{CSV: [][]string{}})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSResponse(w, csvResponse{CSV: data})
}

func writeJSResponse(w http.ResponseWriter, v csvResponse) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	b, _ := json.Marshal(v)
	fmt.Fprintf(w, "getcsv(%s)", b)
}

func readCSVForChart(path string, lines int, includeTitle bool) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	var rows [][]string
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		return [][]string{}, nil
	}

	start := 0
	if includeTitle {
		start = 1
	}

	available := len(rows) - start
	if available < 0 {
		available = 0
	}
	if lines <= 0 || lines > available {
		lines = available
	}

	begin := len(rows) - lines
	if begin < start {
		begin = start
	}

	result := make([][]string, 0, lines+1)
	if includeTitle {
		result = append(result, rows[0])
	}
	result = append(result, rows[begin:]...)
	return result, nil
}

func updateCOVID19(week int) error {
	if week <= 0 {
		return fmt.Errorf("invalid week: %d", week)
	}

	dir := baseDir()

	// PDFは必ず ./download に保存。
	downloadDir := filepath.Join(dir, "download")
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return err
	}

	baseName := fmt.Sprintf("shuhou2026_%d", week)
	pdfPath := filepath.Join(downloadDir, baseName+".pdf")

	// 既に通常版PDFがあれば再ダウンロードしない。
	if _, err := os.Stat(pdfPath); err == nil {
		fmt.Println("download skip:", pdfPath)
	} else if errors.Is(err, os.ErrNotExist) {
		candidates := []string{
			baseName + ".pdf",
			baseName + "_1.pdf",
			baseName + "zantei.pdf",
		}

		found := false
		for _, name := range candidates {
			url := pdfBaseURL + "/" + name
			fmt.Println("download:", url)

			if err := downloadFile(url, pdfPath); err != nil {
				fmt.Println("download failed:", err)
				_ = os.Remove(pdfPath)
				continue
			}

			ok, err := isPDF(pdfPath)
			if err != nil {
				_ = os.Remove(pdfPath)
				continue
			}
			if ok {
				found = true
				break
			}

			_ = os.Remove(pdfPath)
		}

		if !found {
			return fmt.Errorf("PDF not found for week %d", week)
		}
	} else {
		return err
	}

	text, err := pdfToText(pdfPath)
	if err != nil {
		return err
	}

	date, err := extractDate(text)
	if err != nil {
		return err
	}

	covid, err := extractCount(text, "新型コロナウイルス感染症", 8, 6)
	if err != nil {
		return fmt.Errorf("COVID-19 count: %w", err)
	}

	flu, err := extractCount(text, "インフルエンザ", 9, 6)
	if err != nil {
		return fmt.Errorf("influenza count: %w", err)
	}

	total := covid + flu
	line := fmt.Sprintf("%s,%d,%d,%d,week=%d\n", date, covid, flu, total, week)

	// 既存CSVに追記する。
	// 同じ週が既に存在する場合は重複させず、その週のデータを更新する。
	csvPath := filepath.Join(dir, "covid19.csv")
	if err := appendOrUpdateCSV(csvPath, []string{
		date,
		strconv.Itoa(covid),
		strconv.Itoa(flu),
		strconv.Itoa(total),
		fmt.Sprintf("week=%d", week),
	}); err != nil {
		return err
	}

	fmt.Print(line)
	return nil
}

// appendOrUpdateCSV は既存CSVを保持したまま1週分を追加する。
// 同じ week=XX が存在する場合は、その行を新しい値に置き換える。
func appendOrUpdateCSV(path string, newRow []string) error {
	rows := make([][]string, 0)

	f, err := os.Open(path)
	if err == nil {
		reader := csv.NewReader(f)
		reader.FieldsPerRecord = -1
		reader.TrimLeadingSpace = true

		for {
			row, readErr := reader.Read()
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				f.Close()
				return readErr
			}
			if len(row) == 0 {
				continue
			}
			for i := range row {
				row[i] = strings.TrimSpace(row[i])
			}
			rows = append(rows, row)
		}
		if err := f.Close(); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	header := []string{"日付", "コロナ件数", "インフル件数", "合計", "週"}
	dataRows := make([][]string, 0, len(rows)+1)

	if len(rows) > 0 {
		header = rows[0]
		dataRows = append(dataRows, rows[1:]...)
	}

	// 既存CSVが旧形式（4列）でも、週情報がない行はそのまま保持する。
	newWeek := newRow[4]
	replaced := false
	for i, row := range dataRows {
		if len(row) >= 5 && row[4] == newWeek {
			dataRows[i] = append([]string(nil), newRow...)
			replaced = true
			break
		}
	}

	if !replaced {
		dataRows = append(dataRows, append([]string(nil), newRow...))
	}

	// 日付順に並べ替える。
	for i := 0; i < len(dataRows); i++ {
		for j := i + 1; j < len(dataRows); j++ {
			ti, errI := parseCSVDate(dataRows[i])
			tj, errJ := parseCSVDate(dataRows[j])
			if errI == nil && errJ == nil && ti.After(tj) {
				dataRows[i], dataRows[j] = dataRows[j], dataRows[i]
			}
		}
	}

	tmpPath := path + ".tmp"
	f, err = os.Create(tmpPath)
	if err != nil {
		return err
	}

	writer := csv.NewWriter(f)
	if err := writer.Write(header); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	writer.WriteAll(dataRows)
	writer.Flush()

	if err := writer.Error(); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	_ = os.Remove(path)
	return os.Rename(tmpPath, path)
}

func parseCSVDate(row []string) (time.Time, error) {
	if len(row) == 0 {
		return time.Time{}, errors.New("empty CSV row")
	}
	return time.Parse("2006/1/2", row[0])
}

func downloadFile(url, path string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

func isPDF(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, 5)
	if _, err := io.ReadFull(f, buf); err != nil {
		return false, err
	}
	return string(buf) == "%PDF-", nil
}

func pdfToText(path string) (string, error) {
	// Linux: pdftotext / Windows: pdftotext.exe
	exe := "pdftotext"
	if os.PathSeparator == '\\' {
		exe = "pdftotext.exe"
	}

	cmd := exec.Command(exe, "-layout", path, "-")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("%s failed: %s", exe, string(ee.Stderr))
		}
		return "", fmt.Errorf("%s が必要です: %w", exe, err)
	}
	return string(out), nil
}

func extractDate(text string) (string, error) {
	m := dateRe.FindStringSubmatch(text)
	if len(m) != 4 {
		return "", errors.New("date not found")
	}

	reiwa, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])

	return fmt.Sprintf("%d/%d/%d", reiwa+2018, month, day), nil
}

func extractCount(text, keyword string, primaryIndex, fallbackIndex int) (int, error) {
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		if !strings.Contains(line, keyword) {
			continue
		}

		fields := strings.Fields(strings.Join(strings.Fields(line), " "))
		if n, ok := fieldNumber(fields, primaryIndex); ok {
			return n, nil
		}

		if i > 0 {
			prevFields := strings.Fields(strings.Join(strings.Fields(lines[i-1]), " "))
			if n, ok := fieldNumber(prevFields, fallbackIndex); ok {
				return n, nil
			}
		}
	}

	return 0, fmt.Errorf("%q not found", keyword)
}

func fieldNumber(fields []string, awkField int) (int, bool) {
	if awkField <= 0 || len(fields) < awkField {
		return 0, false
	}
	s := strings.ReplaceAll(fields[awkField-1], ",", "")
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		fmt.Printf("%s %s %s %v\n", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}
