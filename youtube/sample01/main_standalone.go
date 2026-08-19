//go:build ignore

/*
Youtube動画から、音声のみ抽出する。
デスクトップアプリケーション。
Go言語＋Pythonライブラリ。

アプリケーション起動方法
cd D:\workpython\golang
go run main_standalone.go

このプロジェクト内にmp3ファイルが保存される。
cd D:\workpython\golang\downloads
dir

*/

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {

	urls := []string{
		"https://youtu.be/X5uZHW3SRTg?si=ZRWrDVv7iJJWbUg7",
	}

	args := []string{
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:153.0) Gecko/20100101 Firefox/153.0",
		"-f", "bestaudio/best",
		"--ffmpeg-location", "D:/ffmpeg/ffmpeg-8.1.2-full_build/bin",
		"-o", "./downloads/%(title)s.%(ext)s",
		"--extractor-args", "youtube:player_client=android,web,ios",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "192K",
	}

	args = append(args, urls...)

	cmd := exec.Command(
		"C:/Users/ojagg/AppData/Local/Python/pythoncore-3.14-64/Scripts/yt-dlp.exe",
		args...,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()

	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Download completed")
}
