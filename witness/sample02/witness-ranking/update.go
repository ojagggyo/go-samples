package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

type Span struct {
	Name     string
	Subtract func(time.Time) time.Time
}

func spans() []Span {
	return []Span{
		{
			Name: "hour",
			Subtract: func(t time.Time) time.Time {
				return t.Add(-1 * time.Hour)
			},
		},
		{
			Name: "2hours",
			Subtract: func(t time.Time) time.Time {
				return t.Add(-2 * time.Hour)
			},
		},
		{
			Name: "day",
			Subtract: func(t time.Time) time.Time {
				return t.AddDate(0, 0, -1)
			},
		},
		{
			Name: "week",
			Subtract: func(t time.Time) time.Time {
				return t.AddDate(0, 0, -7)
			},
		},
		{
			Name: "month",
			Subtract: func(t time.Time) time.Time {
				return t.AddDate(0, -1, 0)
			},
		},
		{
			Name: "year",
			Subtract: func(t time.Time) time.Time {
				return t.AddDate(-1, 0, 0)
			},
		},
	}
}

func RunUpdate(cfg Config) error {
	now := time.Now()

	log.Printf("fetch witnesses from %s", cfg.RPCURL)

	current, err := FetchWitnesses(cfg.RPCURL)
	if err != nil {
		return err
	}

	log.Printf("fetched %d witnesses", len(current))

	currentFile := filepath.Join(cfg.DataDir, "ranking.csv")
	if err := WriteWitnessCSV(currentFile, current, false); err != nil {
		return fmt.Errorf("write ranking.csv: %w", err)
	}

	historyFile := filepath.Join(HistoryDir(cfg), HistoryFilename(now))
	if err := WriteWitnessCSV(historyFile, current, false); err != nil {
		return fmt.Errorf("write history: %w", err)
	}

	log.Printf("saved %s", historyFile)

	for _, span := range spans() {
		target := span.Subtract(now)

		oldFile, err := FindHistoryAtOrBefore(cfg, target)
		if err != nil {
			// 現在のShellと同様、過去ファイルがなければ現在の値を使う。
			log.Printf("[%s] %v; use current data", span.Name, err)

			oldFile = filepath.Join(cfg.DataDir, "ranking_last_"+span.Name+".csv")

			if err := WriteWitnessCSV(oldFile, current, false); err != nil {
				return err
			}
		} else {
			lastFile := filepath.Join(cfg.DataDir, "ranking_last_"+span.Name+".csv")

			oldData, err := ReadWitnessCSV(oldFile)
			if err != nil {
				return fmt.Errorf("read old %s: %w", span.Name, err)
			}

			if err := WriteWitnessCSV(lastFile, oldData, false); err != nil {
				return err
			}

			log.Printf("[%s] compare with %s", span.Name, oldFile)
		}

		previous, err := ReadWitnessCSV(oldFile)
		if err != nil {
			return fmt.Errorf("read previous %s: %w", span.Name, err)
		}

		compared := CompareWitnesses(current, previous)

		out := filepath.Join(cfg.DataDir, "ranking_"+span.Name+".csv")
		if err := WriteWitnessCSV(out, compared, true); err != nil {
			return fmt.Errorf("write %s: %w", out, err)
		}

		log.Printf("[%s] wrote %s", span.Name, out)
	}

	// TODO:
	// ここに現在の detail.sh / detail_all.sh 相当の処理を追加する。
	// 既存Shellを当面残す場合は os/exec で呼び出すことも可能。
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "detail")); err == nil {
		log.Printf("detail directory exists (detail generation TODO)")
	}

	log.Println("update finished")
	return nil
}
