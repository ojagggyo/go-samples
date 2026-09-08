package main

import (
	"encoding/base64"
	"fmt"
)

// 文字列がbase64形式か判定する。
func main() {
	s := "0b649a44a78b91d0a8d282844814db71cea2d61aebc80097c841be64e3e2b12747f75d63f18edd7957dc66d1f1f710d9118db66fd322c7bbc3de59f2bc6f81e9411900cbb117c18c5a6eeac1cc3f260d"

	_, err := base64.StdEncoding.DecodeString(s)

	if err != nil {
		fmt.Println("Base64ではありません")
	} else {
		fmt.Println("Base64です")
	}
}
