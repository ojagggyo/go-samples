package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

type Config struct {
	LoginURL              string   `json:"login_url"`
	BillingURL            string   `json:"billing_url"`
	Headless              bool     `json:"headless"`
	TimeoutSeconds        int      `json:"timeout_seconds"`
	UsernameSelectors     []string `json:"username_selectors"`
	PasswordSelectors     []string `json:"password_selectors"`
	SubmitSelectors       []string `json:"submit_selectors"`
	NextSelectors         []string `json:"next_selectors"`
	GoogleCredentialsFile string   `json:"google_credentials_file"`
	GoogleTokenFile       string   `json:"google_token_file"`
	GoogleCalendarID      string   `json:"google_calendar_id"`
}

type Billing struct {
	CardName     string    `json:"card_name,omitempty"`
	BillingMonth string    `json:"billing_month,omitempty"`
	PaymentDate  string    `json:"payment_date"`
	AmountYen    int64     `json:"amount_yen"`
	FetchedAt    time.Time `json:"fetched_at"`
	SourceURL    string    `json:"source_url"`
}

func main() {
	configPath := flag.String("config", "config.json", "設定ファイル")
	outputPath := flag.String("output", "result.json", "JSON出力先")
	noCalendar := flag.Bool("no-calendar", false, "Googleカレンダーへ登録せずJSONだけ出力")
	debug := flag.Bool("debug", false, "画面本文を標準出力へ表示")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	user := strings.TrimSpace(os.Getenv("AEON_LOGIN_ID"))
	pass := os.Getenv("AEON_PASSWORD")
	if user == "" || pass == "" {
		log.Fatal("AEON_LOGIN_ID と AEON_PASSWORD を環境変数に設定してください")
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", cfg.Headless),
		chromedp.Flag("disable-gpu", false),
		chromedp.Flag("no-sandbox", runtime.GOOS == "linux" && os.Getenv("AEON_NO_SANDBOX") == "1"),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()
	ctx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	ctx, cancelTimeout := context.WithTimeout(ctx, timeout)
	defer cancelTimeout()

	var body, currentURL string
	err = chromedp.Run(ctx,
		chromedp.Navigate(cfg.LoginURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return login(ctx, cfg, user, pass)
		}),
		chromedp.Sleep(5*time.Second),
		chromedp.Location(&currentURL),
	)
	if err != nil {
		saveScreenshot(ctx, "error.png")
		log.Fatalf("ログイン操作に失敗しました: %v（error.png を確認してください）", err)
	}

	if strings.Contains(currentURL, "login") {
		saveScreenshot(ctx, "error.png")
		log.Fatal("ログイン後もログイン画面です。ID・パスワードまたは追加認証を確認してください")
	}

	err = chromedp.Run(ctx,
		chromedp.Navigate(cfg.BillingURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(5*time.Second),
		chromedp.Text("body", &body, chromedp.ByQuery),
		chromedp.Location(&currentURL),
	)
	if err != nil {
		saveScreenshot(ctx, "error.png")
		log.Fatalf("請求画面を取得できません: %v", err)
	}
	if *debug {
		fmt.Println(body)
	}

	billing, err := parseBilling(body, currentURL)
	if err != nil {
		saveScreenshot(ctx, "error.png")
		log.Fatalf("請求情報を抽出できません: %v（-debug と error.png で画面を確認してください）", err)
	}
	if err := writeJSON(*outputPath, billing); err != nil {
		log.Fatal(err)
	}
	if !*noCalendar {
		url, created, err := registerCalendar(context.Background(), cfg, billing)
		if err != nil {
			log.Fatalf("Googleカレンダー登録に失敗しました: %v", err)
		}
		if created {
			fmt.Printf("カレンダー登録完了: %s\n", url)
		} else {
			fmt.Printf("同日の予定があるため登録を省略: %s\n", url)
		}
	}
	fmt.Printf("取得完了: 支払日=%s 金額=%d円 出力=%s\n", billing.PaymentDate, billing.AmountYen, *outputPath)
}

func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("設定ファイルを読めません: %w", err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("設定ファイルのJSONが不正です: %w", err)
	}
	if c.LoginURL == "" || c.BillingURL == "" {
		return c, errors.New("login_url と billing_url は必須です")
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = 120
	}
	if c.GoogleCredentialsFile == "" {
		c.GoogleCredentialsFile = "client_secret.json"
	}
	if c.GoogleTokenFile == "" {
		c.GoogleTokenFile = "token.json"
	}
	if c.GoogleCalendarID == "" {
		c.GoogleCalendarID = "primary"
	}
	return c, nil
}

func parseBilling(text, sourceURL string) (Billing, error) {
	clean := strings.NewReplacer("\u00a0", " ", "，", ",", "￥", "¥").Replace(text)
	dateRE := regexp.MustCompile(`(?m)(?:お支払(?:い)?日|引落(?:し)?日)[^0-9]{0,30}((?:20)?[0-9]{2})[年/]\s*([0-9]{1,2})[月/]\s*([0-9]{1,2})日?`)
	amountRE := regexp.MustCompile(`(?m)(?:ご請求金額|お支払(?:い)?金額|請求額)[^0-9¥-]{0,40}[¥]?\s*([0-9][0-9,]*)\s*円?`)
	dm := dateRE.FindStringSubmatch(clean)
	am := amountRE.FindStringSubmatch(clean)
	if len(dm) != 4 {
		return Billing{}, errors.New("支払日が見つかりません")
	}
	if len(am) != 2 {
		return Billing{}, errors.New("請求金額が見つかりません")
	}
	year := dm[1]
	if len(year) == 2 {
		year = "20" + year
	}
	y, _ := strconv.Atoi(year)
	mo, _ := strconv.Atoi(dm[2])
	day, _ := strconv.Atoi(dm[3])
	date := fmt.Sprintf("%04d-%02d-%02d", y, mo, day)
	amountText := strings.ReplaceAll(am[1], ",", "")
	var amount int64
	if _, err := fmt.Sscan(amountText, &amount); err != nil {
		return Billing{}, fmt.Errorf("請求金額が不正です: %w", err)
	}
	monthRE := regexp.MustCompile(`((?:20)?[0-9]{2})年\s*([0-9]{1,2})月(?:度)?`)
	month := ""
	if m := monthRE.FindStringSubmatch(clean); len(m) == 3 {
		my, _ := strconv.Atoi(m[1])
		if my < 100 {
			my += 2000
		}
		mm, _ := strconv.Atoi(m[2])
		month = fmt.Sprintf("%04d-%02d", my, mm)
	}
	return Billing{CardName: "イオンカード", BillingMonth: month, PaymentDate: date, AmountYen: amount, FetchedAt: time.Now(), SourceURL: sourceURL}, nil
}

func writeJSON(path string, v Billing) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

func saveScreenshot(ctx context.Context, name string) {
	var buf []byte
	if chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90)) == nil {
		_ = os.WriteFile(filepath.Clean(name), buf, 0600)
	}
}
