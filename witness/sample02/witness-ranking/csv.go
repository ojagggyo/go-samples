package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

func ReadWitnessCSV(filename string) ([]Witness, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	var list []Witness

	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filename, err)
		}
		if len(row) < 6 {
			continue
		}

		totalMissed, _ := strconv.ParseInt(row[4], 10, 64)

		w := Witness{
			Name:           row[0],
			Votes:          row[1],
			RunningVersion: row[2],
			SigningKey:     row[3],
			TotalMissed:    totalMissed,
			LastUpdate:     row[5],
		}

		if len(row) >= 7 {
			w.Miss, _ = strconv.ParseInt(row[6], 10, 64)
		}

		if len(row) >= 8 {
			w.SigningKeyChange, _ = strconv.Atoi(row[7])
		}

		list = append(list, w)
	}

	return list, nil
}

func WriteWitnessCSV(filename string, list []Witness, includeCompare bool) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	for _, item := range list {
		row := []string{
			item.Name,
			item.Votes,
			item.RunningVersion,
			item.SigningKey,
			strconv.FormatInt(item.TotalMissed, 10),
			item.LastUpdate,
		}

		if includeCompare {
			row = append(row,
				strconv.FormatInt(item.Miss, 10),
				strconv.Itoa(item.SigningKeyChange),
			)
		}

		if err := w.Write(row); err != nil {
			return err
		}
	}

	return w.Error()
}
