package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

func registerCalendar(ctx context.Context, cfg Config, b Billing) (string, bool, error) {
	client, err := googleClient(ctx, cfg.GoogleCredentialsFile, cfg.GoogleTokenFile)
	if err != nil { return "", false, err }
	svc, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil { return "", false, err }
	d, err := time.Parse("2006-01-02", b.PaymentDate)
	if err != nil { return "", false, err }
	loc, _ := time.LoadLocation("Asia/Tokyo")
	start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	title := "💳 イオンカード お支払い日"
	list, err := svc.Events.List(cfg.GoogleCalendarID).TimeMin(start.Format(time.RFC3339)).TimeMax(end.Format(time.RFC3339)).SingleEvents(true).Q(title).Do()
	if err != nil { return "", false, err }
	for _, e := range list.Items {
		if e.Summary == title { return e.HtmlLink, false, nil }
	}
	e := &calendar.Event{
		Summary: title,
		Description: fmt.Sprintf("お支払い金額：%d円\n請求対象月：%s\n取得元：暮らしのマネーサイト", b.AmountYen, b.BillingMonth),
		Start: &calendar.EventDateTime{Date: b.PaymentDate, TimeZone: "Asia/Tokyo"},
		End: &calendar.EventDateTime{Date: end.Format("2006-01-02"), TimeZone: "Asia/Tokyo"},
	}
	created, err := svc.Events.Insert(cfg.GoogleCalendarID, e).Do()
	if err != nil { return "", false, err }
	return created.HtmlLink, true, nil
}

func googleClient(ctx context.Context, credentialsFile, tokenFile string) (*http.Client, error) {
	b, err := os.ReadFile(credentialsFile)
	if err != nil { return nil, fmt.Errorf("%sを読めません: %w", credentialsFile, err) }
	cfg, err := google.ConfigFromJSON(b, calendar.CalendarEventsScope)
	if err != nil { return nil, err }
	tok, err := readToken(tokenFile)
	if err != nil {
		tok, err = authorize(ctx, cfg)
		if err != nil { return nil, err }
		if err := saveToken(tokenFile, tok); err != nil { return nil, err }
	}
	return cfg.Client(ctx, tok), nil
}

func authorize(ctx context.Context, cfg *oauth2.Config) (*oauth2.Token, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { return nil, err }
	defer ln.Close()
	cfg.RedirectURL = "http://" + ln.Addr().String() + "/oauth2callback"
	state := fmt.Sprintf("aeon-%d", time.Now().UnixNano())
	codeCh, errCh := make(chan string, 1), make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state { errCh <- errors.New("OAuth stateが一致しません"); http.Error(w, "invalid state", http.StatusBadRequest); return }
		codeCh <- r.URL.Query().Get("code")
		fmt.Fprintln(w, "認証が完了しました。この画面を閉じてください。")
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Shutdown(context.Background())
	url := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Println("初回のみGoogle認証が必要です。次のURLを開いて許可してください:")
	fmt.Println(url)
	_ = openBrowser(url)
	select {
	case code := <-codeCh:
		if code == "" { return nil, errors.New("認証コードがありません") }
		return cfg.Exchange(ctx, code)
	case err := <-errCh:
		return nil, err
	case <-time.After(5 * time.Minute):
		return nil, errors.New("Google認証がタイムアウトしました")
	}
}

func readToken(path string) (*oauth2.Token, error) {
	f, err := os.Open(path); if err != nil { return nil, err }; defer f.Close()
	var t oauth2.Token
	if err := json.NewDecoder(f).Decode(&t); err != nil { return nil, err }
	return &t, nil
}

func saveToken(path string, t *oauth2.Token) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil { return err }; defer f.Close()
	return json.NewEncoder(f).Encode(t)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows": cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin": cmd = exec.Command("open", url)
	default: cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
