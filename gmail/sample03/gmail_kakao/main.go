package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type Config struct {
	GmailCredentials string   `json:"gmail_credentials"`
	GmailToken       string   `json:"gmail_token"`
	KakaoToken       string   `json:"kakao_token"`
	KakaoLink        string   `json:"kakao_link"`
	Query            string   `json:"query"`
	MaxAgeSeconds    int      `json:"max_age_seconds"`
	PollSeconds      int      `json:"poll_seconds"`
	Senders          []string `json:"senders"`
	CodePattern      string   `json:"code_pattern"`
}

func main() {
	path := flag.String("config", "config.json", "設定ファイル")
	send := flag.Bool("send", false, "Kakaoの自分とのトークへ送信")
	watch := flag.Bool("watch", false, "一定間隔で繰り返す（Ctrl+Cで終了）")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *path, *send, *watch); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, path string, send, watch bool) error {
	if err := loadEnv(".env"); err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	if cfg.MaxAgeSeconds < 1 || cfg.PollSeconds < 5 || cfg.GmailCredentials == "" || cfg.GmailToken == "" {
		return fmt.Errorf("認証ファイル、max_age_seconds>0、poll_seconds>=5 を設定してください")
	}
	allow := map[string]bool{}
	for _, s := range cfg.Senders {
		a, err := mail.ParseAddress(s)
		if err != nil || a.Address != s {
			return fmt.Errorf("senders には送信元メールアドレスのみを設定してください")
		}
		allow[strings.ToLower(s)] = true
	}
	if len(allow) == 0 {
		return fmt.Errorf("senders に転送対象の送信元を設定してください")
	}
	pattern, err := regexp.Compile(cfg.CodePattern)
	if err != nil || pattern.NumSubexp() != 1 {
		return fmt.Errorf("code_pattern はコードだけを取り出すキャプチャグループを1つ指定してください")
	}
	if send {
		u, err := url.Parse(cfg.KakaoLink)
		if err != nil || u.Scheme != "https" || u.Host == "" || cfg.KakaoToken == "" || os.Getenv("KAKAO_REST_API_KEY") == "" {
			return fmt.Errorf("KAKAO_REST_API_KEY、kakao_token、httpsのkakao_link を設定してください")
		}
	}
	lock, err := os.OpenFile("gmail_kakao.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("他の実行または前回のロックを確認してください: %w", err)
	}
	lock.Close()
	defer os.Remove("gmail_kakao.lock")
	state, err := loadState("state.json")
	if err != nil {
		return err
	}
	b, err = os.ReadFile(cfg.GmailCredentials)
	if err != nil {
		return err
	}
	gcfg, err := google.ConfigFromJSON(b, gmail.GmailReadonlyScope)
	if err != nil {
		return err
	}
	fmt.Println("Gmail認証を確認中…")
	gc, err := authenticatedClient(ctx, gcfg, cfg.GmailToken)
	if err != nil {
		return err
	}
	gc.Timeout = 30 * time.Second
	gs, err := gmail.NewService(ctx, option.WithHTTPClient(gc))
	if err != nil {
		return err
	}
	var deliver func(context.Context, string) error
	if send {
		kcfg := &oauth2.Config{ClientID: os.Getenv("KAKAO_REST_API_KEY"), ClientSecret: os.Getenv("KAKAO_CLIENT_SECRET"), RedirectURL: "http://localhost:8080/oauth/callback", Scopes: []string{"talk_message"}, Endpoint: oauth2.Endpoint{AuthURL: "https://kauth.kakao.com/oauth/authorize", TokenURL: "https://kauth.kakao.com/oauth/token", AuthStyle: oauth2.AuthStyleInParams}}
		fmt.Println("Kakao認証を確認中…")
		kc, err := authenticatedClient(ctx, kcfg, cfg.KakaoToken)
		if err != nil {
			return err
		}
		kc.Timeout = 30 * time.Second
		deliver = func(ctx context.Context, text string) error {
			return sendToMe(ctx, kc, "https://kapi.kakao.com/v2/api/talk/memo/default/send", cfg.KakaoLink, text)
		}
	}
	for {
		fmt.Println("新しいコードメールを確認中…")
		cycle, cancel := context.WithTimeout(ctx, 60*time.Second)
		err := scan(cycle, gs, cfg, allow, pattern, state, deliver)
		cancel()
		if err != nil {
			return err
		}
		if !watch {
			fmt.Println("確認を終了しました")
			return nil
		}
		fmt.Printf("%d秒後に再確認します（Ctrl+Cで終了）\n", cfg.PollSeconds)
		timer := time.NewTimer(time.Duration(cfg.PollSeconds) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func scan(ctx context.Context, gs *gmail.Service, cfg Config, allow map[string]bool, pattern *regexp.Regexp, state map[string]string, deliver func(context.Context, string) error) error {
	cutoff := time.Now().Add(-time.Duration(cfg.MaxAgeSeconds) * time.Second)
	var ids []*gmail.Message
	query := fmt.Sprintf("(%s) after:%d", cfg.Query, cutoff.Unix())
	if cfg.Query == "" {
		query = fmt.Sprintf("after:%d", cutoff.Unix())
	}
	err := gs.Users.Messages.List("me").Q(query).MaxResults(100).Pages(ctx, func(p *gmail.ListMessagesResponse) error { ids = append(ids, p.Messages...); return nil })
	if err != nil {
		return err
	}
	var messages []*gmail.Message
	for _, item := range ids {
		if status := state[item.Id]; status != "" {
			if status == "pending" {
				fmt.Printf("送信結果の確認が必要: message=%s\n", item.Id)
			}
			continue
		}
		msg, err := gs.Users.Messages.Get("me", item.Id).Format("full").Context(ctx).Do()
		if err != nil {
			return err
		}
		messages = append(messages, msg)
	}
	// Use Gmail's receipt timestamp, not the opaque message ID, to order delivery.
	sort.SliceStable(messages, func(i, j int) bool { return messages[i].InternalDate < messages[j].InternalDate })
	for _, msg := range messages {
		received := time.UnixMilli(msg.InternalDate)
		if time.Since(received) > time.Duration(cfg.MaxAgeSeconds)*time.Second || received.After(time.Now().Add(time.Minute)) {
			continue
		}
		addr, err := mail.ParseAddress(header(msg.Payload, "From"))
		if err != nil || !allow[strings.ToLower(addr.Address)] {
			continue
		}
		body, err := messageText(msg.Payload)
		if err != nil {
			fmt.Printf("本文を取得できません: message=%s\n", msg.Id)
			continue
		}
		code, err := extractCode(body, pattern)
		if err != nil {
			fmt.Printf("コードを一意に抽出できません: message=%s\n", msg.Id)
			continue
		}
		if deliver == nil {
			fmt.Printf("転送候補: %s / コード%d文字 / message=%s\n", addr.Address, len([]rune(code)), msg.Id)
			continue
		}
		text := "認証コード: " + code + "\n送信元: " + addr.Address
		if len([]rune(text)) > 200 {
			return fmt.Errorf("Kakaoメッセージが200文字を超えています")
		}
		if err := forwardOnce(ctx, "state.json", state, msg.Id, text, deliver); err != nil {
			return err
		}
		fmt.Printf("転送しました: message=%s\n", msg.Id)
	}
	return nil
}

func header(part *gmail.MessagePart, name string) string {
	if part != nil {
		for _, h := range part.Headers {
			if strings.EqualFold(h.Name, name) {
				return h.Value
			}
		}
	}
	return ""
}
