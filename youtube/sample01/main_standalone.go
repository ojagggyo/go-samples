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
		"https://youtu.be/FjKQDfFPsZU?si=QrRCkkD8iiaxvpuD",
		"https://youtu.be/baS4JsU6gkk?si=yDk4jThqicMyNppA",
		"https://youtu.be/0-1rdQXHadw?si=EsA-DMs2efDFsBuv",
		"https://youtu.be/VNT6VNG_5Y8?si=Bn6rVlexQKapMITr",
		"https://youtu.be/VezhR47IwXI?si=hn6BhI4dBmW6oZhI",
		"https://youtu.be/1gU67kB-y_E?si=ewz7NcVu2G5Eq-yY",
		"https://youtu.be/v9jKpQzwGtY?si=PCHgku18OsOJ-FVn",
		"https://youtu.be/tiKFuzpX-NA?si=QS9OCQMIE9gNGyAw",
		"https://youtu.be/A_Gk12ny9NA?si=FA6TzayeL-7T6Rjn",
		"https://www.youtube.com/watch?v=1wZUdgHYs34",
		"https://youtu.be/yTzyJ7kDLL4?si=bkrxp53VdcGA38z5",
		"https://youtu.be/ohWU8j_2iEg?si=9cDqZKhStXWvcvz9",
		"https://youtu.be/Zi_XLOBDo_Y?si=eScNoumN9Q1_ymLv",
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
