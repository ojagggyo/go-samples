package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type MP3File struct {
	Path    string
	Name    string
	ModTime int64
}

type RenameInfo struct {
	TempPath string
	NewPath  string
	OldName  string
	NewName  string
}

func main() {

	// ========================================
	// 対象フォルダ
	// ========================================
	targetDir := `D:\mp3\20260801`

	// ========================================
	// 先頭に連続する「数字_」の既存番号を取り除く
	// 例:
	// 001_test.mp3      → test.mp3
	// 999_999_abc.mp3   → abc.mp3
	// test.mp3          → test.mp3
	// ========================================
	numberPrefix := regexp.MustCompile(`^(?:[0-9]+_)+`)

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		fmt.Println("フォルダ読み込みエラー:", err)
		return
	}

	var files []MP3File

	// ========================================
	// MP3ファイルを取得
	// ========================================
	for _, entry := range entries {

		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// 大文字・小文字を区別せずmp3を判定
		if !strings.EqualFold(filepath.Ext(name), ".mp3") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			fmt.Println("ファイル情報取得エラー:", name)
			continue
		}

		files = append(files, MP3File{
			Path:    filepath.Join(targetDir, name),
			Name:    name,
			ModTime: info.ModTime().UnixNano(),
		})
	}

	if len(files) == 0 {
		fmt.Println("MP3ファイルがありません。")
		return
	}

	// ========================================
	// 更新日時の古い順
	// ========================================
	sort.Slice(files, func(i, j int) bool {

		if files[i].ModTime == files[j].ModTime {
			return strings.ToLower(files[i].Name) <
				strings.ToLower(files[j].Name)
		}

		return files[i].ModTime < files[j].ModTime
	})

	fmt.Printf("MP3ファイル: %d個\n\n", len(files))

	// ========================================
	// 既存の番号を除去した名前を作る
	// ========================================
	var renameList []RenameInfo

	for i, file := range files {

		// 先頭に連続する既存の001_、999_などをすべて除去
		baseName := numberPrefix.ReplaceAllString(file.Name, "")

		// 新しい番号
		newName := fmt.Sprintf("%03d_%s", i+1, baseName)

		// 一時ファイル名
		tempName := fmt.Sprintf(
			"__rename_tmp_%06d_%s",
			i+1,
			file.Name,
		)

		tempPath := filepath.Join(targetDir, tempName)
		newPath := filepath.Join(targetDir, newName)

		// ====================================
		// まず一時ファイル名に変更
		// ====================================
		err := os.Rename(file.Path, tempPath)
		if err != nil {
			fmt.Println("一時リネーム失敗:", file.Name)
			fmt.Println(err)
			return
		}

		renameList = append(renameList, RenameInfo{
			TempPath: tempPath,
			NewPath:  newPath,
			OldName:  file.Name,
			NewName:  newName,
		})
	}

	// ========================================
	// 正式な名前に変更
	// ========================================
	fmt.Println("リネーム結果")
	fmt.Println("----------------------------------------")

	for _, r := range renameList {

		err := os.Rename(r.TempPath, r.NewPath)

		if err != nil {
			fmt.Println("リネーム失敗:", r.NewName)
			fmt.Println(err)
			continue
		}

		fmt.Printf("%s\n", r.OldName)
		fmt.Printf("  ↓\n")
		fmt.Printf("%s\n\n", r.NewName)
	}

	fmt.Println("----------------------------------------")
	fmt.Println("完了しました。")
}
