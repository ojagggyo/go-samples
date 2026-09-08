package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

func decrypt(cipherTextBase64, password string) ([]byte, error) {
	// パスワードから32バイトの鍵を作る
	hash := sha256.Sum256([]byte(password))
	key := hash[:]

	// AES
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Base64 → バイナリ
	data, err := base64.StdEncoding.DecodeString(cipherTextBase64)
	if err != nil {
		return nil, err
	}

	// 先頭にNonceが入っている想定
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("暗号文が短すぎます")
	}

	nonce := data[:nonceSize]
	cipherText := data[nonceSize:]

	// 復号
	plainText, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return nil, fmt.Errorf("復号失敗: %w", err)
	}

	return plainText, nil
}

func main() {
	var password string
	var encrypted string

	fmt.Print("パスワード: ")
	fmt.Scanln(&password)

	fmt.Print("暗号文(Base64): ")
	fmt.Scanln(&encrypted)

	plainText, err := decrypt(encrypted, password)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("復号結果:")
	fmt.Println(string(plainText))
}