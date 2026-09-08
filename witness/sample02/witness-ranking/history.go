package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const historyPrefix = "ranking_"
const historyLayout = "200601021504"

func HistoryDir(cfg Config) string {
	return filepath.Join(cfg.DataDir, "ranking")
}

func HistoryFilename(t time.Time) string {
	return historyPrefix + t.Format(historyLayout) + ".csv"
}

// target以前の履歴のうち、もっとも新しいファイルを返す。
// 現在のShell:
// ranking_${DATETIME}99 > filename
// の動作に相当する。
func FindHistoryAtOrBefore(cfg Config, target time.Time) (string, error) {
	dir := HistoryDir(cfg)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	targetName := HistoryFilename(target)

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()

		if !strings.HasPrefix(name, historyPrefix) || !strings.HasSuffix(name, ".csv") {
			continue
		}

		if name <= targetName {
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return "", fmt.Errorf("history not found before %s", target.Format(time.RFC3339))
	}

	sort.Strings(names)
	return filepath.Join(dir, names[len(names)-1]), nil
}
