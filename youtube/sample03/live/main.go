package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/youtube/v3"
)

const (
	clientSecretFile = "client_secret.json"
	tokenFile        = "token.json"

	redirectURL = "http://127.0.0.1:8080/oauth2callback"

	oauthScope = youtube.YoutubeScope
)

type Config struct {
	Title       string
	Description string
	WindowTitle string
	Privacy     string
	FFmpegPath  string
}

func loadConfig() Config {
	cfg := Config{
		Title:       os.Getenv("YOUTUBE_TITLE"),
		Description: os.Getenv("YOUTUBE_DESCRIPTION"),
		WindowTitle: os.Getenv("WINDOW_TITLE"),
		Privacy:     os.Getenv("YOUTUBE_PRIVACY"),
		FFmpegPath:  os.Getenv("FFMPEG_PATH"),
	}

	if cfg.Title == "" {
		cfg.Title = "Go YouTube Live"
	}

	if cfg.Description == "" {
		cfg.Description = "Go + FFmpeg YouTube Live"
	}

	if cfg.Privacy == "" {
		cfg.Privacy = "unlisted"
	}

	if cfg.FFmpegPath == "" {
		cfg.FFmpegPath = `ffmpeg\ffmpeg.exe`
	}

	if cfg.WindowTitle == "" {
		log.Fatal("WINDOW_TITLE が .env に設定されていません")
	}

	return cfg
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println(".env がありません。環境変数を使用します。")
	}

	cfg := loadConfig()

	ctx := context.Background()

	// --------------------------------------------------
	// OAuth
	// --------------------------------------------------

	client, err := getYouTubeClient(ctx)
	if err != nil {
		log.Fatal(err)
	}

	service, err := youtube.New(client)
	if err != nil {
		log.Fatalf("YouTube client作成失敗: %v", err)
	}

	log.Println("YouTube API 接続OK")

	// --------------------------------------------------
	// Live Broadcast作成
	// --------------------------------------------------

	startTime := time.Now().UTC().Add(1 * time.Minute)

	broadcast := &youtube.LiveBroadcast{
		Snippet: &youtube.LiveBroadcastSnippet{
			Title:              cfg.Title,
			Description:        cfg.Description,
			ScheduledStartTime: startTime.Format(time.RFC3339),
		},
		Status: &youtube.LiveBroadcastStatus{
			PrivacyStatus: cfg.Privacy,
		},
		ContentDetails: &youtube.LiveBroadcastContentDetails{
			EnableDvr:       true,
			RecordFromStart: true,
			EnableAutoStart: true, // YouTubeに自動的にLIVEへ切り替えさせる
			EnableAutoStop:  true, // YouTubeに自動的にLIVEへ切り替えさせる
		},
	}

	log.Println("YouTube Liveを作成しています...")

	broadcast, err = service.LiveBroadcasts.
		Insert([]string{"snippet", "status", "contentDetails"}, broadcast).
		Do()

	if err != nil {
		log.Fatalf("Live Broadcast作成失敗: %v", err)
	}

	log.Println("Broadcast ID:", broadcast.Id)

	// --------------------------------------------------
	// Live Stream作成
	// --------------------------------------------------

	stream := &youtube.LiveStream{
		Snippet: &youtube.LiveStreamSnippet{
			Title: cfg.Title + " Stream",
		},
		Cdn: &youtube.CdnSettings{
			IngestionType: "rtmp",
			Resolution:    "720p",
			FrameRate:     "30fps",
		},
	}

	log.Println("YouTube Streamを作成しています...")

	stream, err = service.LiveStreams.
		Insert([]string{"snippet", "cdn"}, stream).
		Do()

	if err != nil {
		log.Fatalf("Live Stream作成失敗: %v", err)
	}

	log.Println("Stream ID:", stream.Id)

	if stream.Cdn == nil ||
		stream.Cdn.IngestionInfo == nil {

		log.Fatal("YouTubeからRTMP情報を取得できませんでした")
	}

	ingestionAddress := stream.Cdn.IngestionInfo.IngestionAddress
	streamName := stream.Cdn.IngestionInfo.StreamName

	rtmpURL := ingestionAddress + "/" + streamName

	// --------------------------------------------------
	// BroadcastとStreamをBind
	// --------------------------------------------------

	log.Println("BroadcastとStreamをBindしています...")

	_, err = service.LiveBroadcasts.
		Bind(broadcast.Id, []string{"id", "contentDetails"}).
		StreamId(stream.Id).
		Do()

	if err != nil {
		log.Fatalf("Bind失敗: %v", err)
	}

	log.Println("Bind OK")

	// --------------------------------------------------
	// FFmpeg起動
	// --------------------------------------------------

	log.Println("FFmpegを起動します")
	log.Println("配信対象:", cfg.WindowTitle)

	ffmpeg := exec.Command(
		cfg.FFmpegPath,

		"-f", "gdigrab",
		"-framerate", "30",
		"-draw_mouse", "1",
		"-i", "title="+cfg.WindowTitle,

		// 無音の音声を生成
		"-f", "lavfi",
		"-i", "anullsrc=channel_layout=stereo:sample_rate=44100",

		"-vf", "scale=1280:720",

		"-c:v", "libx264",
		"-preset", "veryfast",
		"-tune", "zerolatency",

		"-b:v", "4500k",
		"-maxrate", "4500k",
		"-bufsize", "9000k",

		"-g", "60",

		"-pix_fmt", "yuv420p",

		//"-an", // 音声なし
		"-c:a", "aac",
		"-b:a", "128k",
		"-ar", "44100",

		"-f", "flv",
		rtmpURL,
	)

	ffmpeg.Stdout = os.Stdout
	ffmpeg.Stderr = os.Stderr

	if err := ffmpeg.Start(); err != nil {
		log.Fatalf("FFmpeg起動失敗: %v", err)
	}

	log.Println("FFmpeg起動OK")

	// --------------------------------------------------
	// YouTube側のStreamがactiveになるまで待つ
	// --------------------------------------------------

	log.Println("YouTubeのStream状態を確認しています...")

	if err := waitForStreamActive(ctx, service, stream.Id); err != nil {
		ffmpeg.Process.Kill()
		log.Fatal(err)
	}

	log.Println("YouTube Stream: ACTIVE")

	// --------------------------------------------------
	// Live開始
	// --------------------------------------------------

	log.Println("YouTube Liveを開始します...")

	// _, err = service.LiveBroadcasts.
	// 	Transition("live", broadcast.Id, []string{"id", "status"}).
	// 	Do()

	// if err != nil {
	// 	ffmpeg.Process.Kill()
	// 	log.Fatalf("Live開始失敗: %v", err)
	// }

	log.Println("")
	log.Println("======================================")
	log.Println(" YouTube Live 配信中")
	log.Println("======================================")
	log.Println("Broadcast ID:", broadcast.Id)
	log.Println("https://www.youtube.com/watch?v=" + broadcast.Id)
	log.Println("")
	log.Println("Ctrl+C でFFmpegを停止してください")
	log.Println("")

	// --------------------------------------------------
	// FFmpeg終了待ち
	// --------------------------------------------------

	if err := ffmpeg.Wait(); err != nil {
		log.Println("FFmpeg終了:", err)
	}

	log.Println("FFmpegが終了しました")

	// --------------------------------------------------
	// YouTube Live終了
	// --------------------------------------------------

	log.Println("YouTube Liveを終了します...")

	_, err = service.LiveBroadcasts.
		Transition("complete", broadcast.Id, []string{"id", "status"}).
		Do()

	if err != nil {
		log.Printf("Live終了処理失敗: %v", err)
	} else {
		log.Println("YouTube Live終了")
	}
}

// ======================================================
// OAuth
// ======================================================

func getYouTubeClient(ctx context.Context) (*http.Client, error) {

	b, err := os.ReadFile(clientSecretFile)
	if err != nil {
		return nil, fmt.Errorf(
			"%s がありません: %w",
			clientSecretFile,
			err,
		)
	}

	config, err := google.ConfigFromJSON(
		b,
		oauthScope,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"client_secret.json解析失敗: %w",
			err,
		)
	}

	config.RedirectURL = redirectURL

	// 既存tokenがあれば使用
	if token, err := loadToken(tokenFile); err == nil {
		return config.Client(ctx, token), nil
	}

	// 初回OAuth
	token, err := authorize(ctx, config)
	if err != nil {
		return nil, err
	}

	if err := saveToken(tokenFile, token); err != nil {
		return nil, err
	}

	return config.Client(ctx, token), nil
}

func authorize(
	ctx context.Context,
	config *oauth2.Config,
) (*oauth2.Token, error) {

	state := "youtube-live-state"

	codeCh := make(chan string)

	server := &http.Server{
		Addr: "127.0.0.1:8080",
	}

	http.HandleFunc(
		"/oauth2callback",
		func(w http.ResponseWriter, r *http.Request) {

			if r.URL.Query().Get("state") != state {
				http.Error(
					w,
					"invalid state",
					http.StatusBadRequest,
				)
				return
			}

			code := r.URL.Query().Get("code")

			if code == "" {
				http.Error(
					w,
					"authorization code not found",
					http.StatusBadRequest,
				)
				return
			}

			fmt.Fprintln(
				w,
				"認証が完了しました。この画面を閉じてください。",
			)

			codeCh <- code
		},
	)

	go func() {
		_ = server.ListenAndServe()
	}()

	authURL := config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
	)

	fmt.Println("")
	fmt.Println("======================================")
	fmt.Println(" Google OAuth 認証")
	fmt.Println("======================================")
	fmt.Println("")
	fmt.Println("ブラウザで次のURLを開いてください:")
	fmt.Println("")
	fmt.Println(authURL)
	fmt.Println("")

	// Windowsの既定ブラウザで開く
	_ = exec.Command(
		"rundll32",
		"url.dll,FileProtocolHandler",
		authURL,
	).Start()

	var code string

	select {
	case code = <-codeCh:

	case <-time.After(5 * time.Minute):
		server.Shutdown(ctx)
		return nil, fmt.Errorf("OAuth認証がタイムアウトしました")
	}

	server.Shutdown(ctx)

	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf(
			"OAuth token取得失敗: %w",
			err,
		)
	}

	return token, nil
}

// ======================================================
// Token
// ======================================================

func loadToken(filename string) (*oauth2.Token, error) {

	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	var token oauth2.Token

	if err := json.NewDecoder(f).Decode(&token); err != nil {
		return nil, err
	}

	return &token, nil
}

func saveToken(
	filename string,
	token *oauth2.Token,
) error {

	f, err := os.OpenFile(
		filename,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0600,
	)

	if err != nil {
		return err
	}

	defer f.Close()

	return json.NewEncoder(f).Encode(token)
}

// ======================================================
// Stream ACTIVE待ち
// ======================================================

func waitForStreamActive(
	ctx context.Context,
	service *youtube.Service,
	streamID string,
) error {

	for i := 0; i < 60; i++ {

		response, err := service.LiveStreams.
			List([]string{"status"}).
			Id(streamID).
			Do()

		if err != nil {
			return fmt.Errorf(
				"Stream状態取得失敗: %w",
				err,
			)
		}

		if len(response.Items) == 0 {
			return fmt.Errorf(
				"Streamが見つかりません",
			)
		}

		status := response.Items[0].Status.StreamStatus

		log.Println("Stream status:", status)

		if status == "active" {
			return nil
		}

		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf(
		"StreamがACTIVEになりませんでした",
	)
}

// ======================================================
// 未使用 import 回避
// ======================================================

var _ = filepath.Join
var _ = url.QueryEscape
