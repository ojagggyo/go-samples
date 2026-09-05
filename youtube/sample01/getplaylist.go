//go:build ignore

/*
Youtube動画から、プレイリストを取得する。
デスクトップアプリケーション。
Go言語＋Pythonライブラリ。

アプリケーション起動方法
cd D:\workpython\golang
go run getplaylist.go

*/

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {

	urls := []string{
		"https://www.youtube.com/watch?v=8KdkcLhXmu8&list=PL272srkmrzAEjreOdTYlKX_ftYgSlxTT0",
	}

	args := []string{
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:153.0) Gecko/20100101 Firefox/153.0",
		"--flat-playlist",
		"--print",
		"%(webpage_url)s - %(title)s",
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

	fmt.Println("Print completed")
}
