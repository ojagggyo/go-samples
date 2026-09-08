package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

const (
	credentialsFile = "client_secret.json"
	tokenFile       = "token.json"
)

func main() {
	ctx := context.Background()

	// OAuth設定を読み込む
	config, err := loadOAuthConfig()
	if err != nil {
		log.Fatal(err)
	}

	// Gmail APIクライアント取得
	client := getClient(ctx, config)

	// Gmail APIサービス
	service, err := gmail.NewService(
		ctx,
		option.WithHTTPClient(client),
	)
	if err != nil {
		log.Fatal(err)
	}

	// ユーザー作成ラベルを取得
	labels, err := service.Users.Labels.List("me").Do()
	if err != nil {
		log.Fatal(err)
	}

	// ユーザー作成ラベルだけ抽出
	var userLabels []*gmail.Label

	for _, label := range labels.Labels {
		if label.Type == "user" {
			userLabels = append(userLabels, label)
		}
	}

	// ラベルがない場合
	if len(userLabels) == 0 {
		fmt.Println("削除対象のユーザー作成ラベルはありません。")
		return
	}

	// 削除対象を表示
	fmt.Println()
	fmt.Println("========================================")
	fmt.Printf("削除対象ラベル: %d個\n", len(userLabels))
	fmt.Println("========================================")

	for i, label := range userLabels {
		fmt.Printf("%3d: %s\n", i+1, label.Name)
	}

	fmt.Println()
	fmt.Println("これらのラベルをすべて削除します。")
	fmt.Println("※ ラベルを削除してもメール本体は削除されません。")
	fmt.Println()
	fmt.Print("削除する場合は DELETE と入力してください: ")

	var answer string
	fmt.Scanln(&answer)

	if strings.TrimSpace(answer) != "DELETE" {
		fmt.Println()
		fmt.Println("キャンセルしました。")
		return
	}

	fmt.Println()
	fmt.Println("ラベルを削除しています...")
	fmt.Println()

	// ラベルを削除
	success := 0
	failed := 0

	for _, label := range userLabels {

		err := service.Users.Labels.Delete(
			"me",
			label.Id,
		).Do()

		if err != nil {
			fmt.Printf("削除失敗: %s : %v\n", label.Name, err)
			failed++
			continue
		}

		fmt.Printf("削除: %s\n", label.Name)
		success++
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("削除処理が完了しました")
	fmt.Println("========================================")
	fmt.Printf("成功: %d個\n", success)
	fmt.Printf("失敗: %d個\n", failed)
}

// OAuth設定を読み込む
func loadOAuthConfig() (*oauth2.Config, error) {

	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf(
			"%s を読み込めません: %w",
			credentialsFile,
			err,
		)
	}

	config, err := google.ConfigFromJSON(
		b,
		gmail.GmailModifyScope,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"OAuth設定の読み込みに失敗しました: %w",
			err,
		)
	}

	return config, nil
}

// OAuthクライアントを取得
func getClient(
	ctx context.Context,
	config *oauth2.Config,
) *http.Client {

	tok, err := tokenFromFile(tokenFile)

	if err != nil {
		fmt.Println()
		fmt.Println("保存済みのOAuthトークンがありません。")
		fmt.Println("ブラウザでGoogle認証を行います。")
		fmt.Println()

		tok = getTokenFromWeb(ctx, config)

		saveToken(tokenFile, tok)
	}

	return config.Client(ctx, tok)
}

// 保存済みトークンを読み込む
func tokenFromFile(file string) (*oauth2.Token, error) {

	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	tok := &oauth2.Token{}

	err = json.NewDecoder(f).Decode(tok)
	if err != nil {
		return nil, err
	}

	return tok, nil
}

// ブラウザでOAuth認証
func getTokenFromWeb(
	ctx context.Context,
	config *oauth2.Config,
) *oauth2.Token {

	authURL := config.AuthCodeURL(
		"state-token",
		oauth2.AccessTypeOffline,
	)

	fmt.Println("ブラウザで次のURLを開いて認証してください:")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()

	fmt.Print("認証コードを入力してください: ")

	var code string

	// &code が重要
	if _, err := fmt.Scan(&code); err != nil {
		log.Fatal(err)
	}

	tok, err := config.Exchange(
		ctx,
		code,
	)

	if err != nil {
		log.Fatal(err)
	}

	return tok
}

// OAuthトークンを保存
func saveToken(
	file string,
	token *oauth2.Token,
) {

	f, err := os.Create(file)
	if err != nil {
		log.Fatal(err)
	}

	defer f.Close()

	err = json.NewEncoder(f).Encode(token)
	if err != nil {
		log.Fatal(err)
	}
}
