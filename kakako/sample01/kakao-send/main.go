package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	redirectURI = "http://localhost:8080/oauth/callback"
	tokenURL    = "https://kauth.kakao.com/oauth/token"
	sendURL     = "https://kapi.kakao.com/v2/api/talk/memo/default/send"
)

type token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	ExpiresAt    int64  `json:"expires_at"`
}

func main() {
	message := flag.String("message", "YouTube動画", "送信する説明文")
	videoURL := flag.String("url", "", "送信するYouTube URL")
	loginOnly := flag.Bool("login", false, "ログインとトークン保存だけ行う")
	flag.Parse()

	clientID := strings.TrimSpace(os.Getenv("KAKAO_REST_API_KEY"))
	clientSecret := strings.TrimSpace(os.Getenv("KAKAO_CLIENT_SECRET"))
	if clientID == "" {
		fatal("環境変数 KAKAO_REST_API_KEY が設定されていません")
	}

	tokenPath, err := tokenFilePath()
	if err != nil {
		fatal(err.Error())
	}
	tok, err := loadToken(tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		tok, err = login(clientID, clientSecret)
		if err == nil {
			err = saveToken(tokenPath, tok)
		}
	}
	if err != nil {
		fatal("ログインに失敗しました: " + err.Error())
	}

	if time.Now().Unix() >= tok.ExpiresAt-60 {
		tok, err = refresh(clientID, clientSecret, tok)
		if err != nil {
			fatal("トークン更新に失敗しました: " + err.Error() + "\n再認証する場合は kakao_token.json を削除してください")
		}
		if err := saveToken(tokenPath, tok); err != nil {
			fatal(err.Error())
		}
	}

	if *loginOnly {
		fmt.Println("ログインとトークン保存が完了しました:", tokenPath)
		return
	}
	if err := validateYouTubeURL(*videoURL); err != nil {
		fatal(err.Error())
	}
	if err := sendToMe(tok.AccessToken, *message, *videoURL); err != nil {
		fatal("送信に失敗しました: " + err.Error())
	}
	fmt.Println("自分のカカオトークへ送信しました")
}

func login(clientID, clientSecret string) (token, error) {
	stateBytes := make([]byte, 24)
	if _, err := rand.Read(stateBytes); err != nil {
		return token{}, err
	}
	state := hex.EncodeToString(stateBytes)
	authURL := "https://kauth.kakao.com/oauth/authorize?" + url.Values{
		"client_id":     {clientID},
		"redirect_uri": {redirectURI},
		"response_type": {"code"},
		"scope":         {"talk_message"},
		"state":         {state},
	}.Encode()

	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		return token{}, fmt.Errorf("localhost:8080を使用できません: %w", err)
	}
	defer listener.Close()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- errors.New("認証stateが一致しません")
			return
		}
		if e := r.URL.Query().Get("error"); e != "" {
			http.Error(w, "Kakao login failed", http.StatusBadRequest)
			errCh <- fmt.Errorf("%s: %s", e, r.URL.Query().Get("error_description"))
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "code missing", http.StatusBadRequest)
			errCh <- errors.New("認証コードがありません")
			return
		}
		fmt.Fprintln(w, "Kakao login completed. You can close this window.")
		codeCh <- code
	})
	server := &http.Server{Handler: mux}
	go server.Serve(listener)

	fmt.Println("ブラウザでカカオログインを行ってください:")
	fmt.Println(authURL)
	_ = openBrowser(authURL)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return token{}, err
	case <-time.After(5 * time.Minute):
		return token{}, errors.New("認証待ちがタイムアウトしました")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	return requestToken(url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"redirect_uri": {redirectURI},
		"code":          {code},
	}, clientSecret)
}

func refresh(clientID, clientSecret string, old token) (token, error) {
	if old.RefreshToken == "" {
		return token{}, errors.New("リフレッシュトークンがありません")
	}
	t, err := requestToken(url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {old.RefreshToken},
	}, clientSecret)
	if err != nil {
		return token{}, err
	}
	if t.RefreshToken == "" {
		t.RefreshToken = old.RefreshToken
	}
	return t, nil
}

func requestToken(form url.Values, clientSecret string) (token, error) {
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return token{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return token{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return token{}, fmt.Errorf("token API: HTTP %d: %s", resp.StatusCode, body)
	}
	var t token
	if err := json.Unmarshal(body, &t); err != nil {
		return token{}, err
	}
	if t.AccessToken == "" {
		return token{}, errors.New("アクセストークンが返されませんでした")
	}
	t.ExpiresAt = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second).Unix()
	return t, nil
}

func sendToMe(accessToken, message, videoURL string) error {
	template := map[string]any{
		"object_type": "text",
		"text":        strings.TrimSpace(message) + "\n" + videoURL,
		"link": map[string]string{
			"web_url":        videoURL,
			"mobile_web_url": videoURL,
		},
		"button_title": "YouTubeを開く",
	}
	b, err := json.Marshal(template)
	if err != nil {
		return err
	}
	form := url.Values{"template_object": {string(b)}}
	req, err := http.NewRequest(http.MethodPost, sendURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("message API: HTTP %d: %s", resp.StatusCode, body)
	}
	return nil
}

func validateYouTubeURL(raw string) error {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("-url に有効なYouTube URLを指定してください")
	}
	host := strings.ToLower(u.Hostname())
	if host != "youtu.be" && host != "youtube.com" && !strings.HasSuffix(host, ".youtube.com") {
		return errors.New("-url は youtube.com または youtu.be のURLにしてください")
	}
	return nil
}

func tokenFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "kakao-send")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "kakao_token.json"), nil
}

func loadToken(path string) (token, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return token{}, err
	}
	var t token
	if err := json.Unmarshal(b, &t); err != nil {
		return token{}, err
	}
	return t, nil
}

func saveToken(path string, t token) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

func openBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	case "darwin":
		cmd = exec.Command("open", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "エラー:", message)
	os.Exit(1)
}
