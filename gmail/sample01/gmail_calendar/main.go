package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/mail"
	"os"
	"strings"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type Config struct {
	CalendarID string              `json:"calendar_id"`
	Query      string              `json:"query"`
	Senders    map[string][]string `json:"senders"`
}

func main() {
	configPath := flag.String("config", "config.json", "設定ファイル")
	apply := flag.Bool("apply", false, "カレンダーへ登録（既定は確認のみ）")
	flag.Parse()
	if err := run(*configPath, *apply); err != nil {
		log.Fatal(err)
	}
}

func run(path string, apply bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	if cfg.CalendarID == "" || cfg.Query == "" {
		return fmt.Errorf("calendar_id と query は必須です")
	}
	senders := map[string]string{}
	for company, addresses := range cfg.Senders {
		if company != "セゾン" && company != "楽天" {
			return fmt.Errorf("未対応の会社: %s", company)
		}
		for _, address := range addresses {
			parsed, err := mail.ParseAddress(address)
			if err != nil || parsed.Address != address {
				return fmt.Errorf("送信元にはメールアドレスのみを指定してください")
			}
			key := strings.ToLower(address)
			if _, exists := senders[key]; exists {
				return fmt.Errorf("送信元の設定が重複しています")
			}
			senders[key] = company
		}
	}
	if len(senders) == 0 {
		return fmt.Errorf("senders に実際の送信元アドレスを設定してください")
	}
	ctx := context.Background()
	b, err = os.ReadFile("client_secret.json")
	if err != nil {
		return err
	}
	scopes := []string{gmail.GmailReadonlyScope}
	tokenPath := "token_readonly.json"
	if apply {
		scopes = append(scopes, calendar.CalendarEventsScope)
		tokenPath = "token_calendar.json"
	}
	oauthConfig, err := google.ConfigFromJSON(b, scopes...)
	if err != nil {
		return err
	}
	client, err := authenticatedClient(ctx, oauthConfig, tokenPath)
	if err != nil {
		return err
	}
	gs, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return err
	}
	var cs *calendar.Service
	if apply {
		cs, err = calendar.NewService(ctx, option.WithHTTPClient(client))
		if err != nil {
			return err
		}
	}
	failures := 0
	err = gs.Users.Messages.List("me").Q(cfg.Query).MaxResults(100).Pages(ctx, func(page *gmail.ListMessagesResponse) error {
		for _, item := range page.Messages {
			msg, err := gs.Users.Messages.Get("me", item.Id).Format("full").Context(ctx).Do()
			if err != nil {
				return err
			}
			from, subject := header(msg.Payload, "From"), header(msg.Payload, "Subject")
			address, err := mail.ParseAddress(from)
			if err != nil {
				continue
			}
			company, ok := senders[strings.ToLower(address.Address)]
			if !ok {
				continue
			}
			body, err := messageText(msg.Payload)
			var payment Payment
			if err == nil {
				payment, err = parsePayment(company, body)
			}
			if err != nil {
				fmt.Printf("要確認: message=%s (%s): %v\n", msg.Id, company, err)
				failures++
				continue
			}
			event := paymentEvent(payment, msg.Id, from, subject)
			if !apply {
				fmt.Printf("確認: %s / %s / message=%s\n", event.Start.Date, event.Summary, msg.Id)
				continue
			}
			_, err = cs.Events.Insert(cfg.CalendarID, event).SendUpdates("none").Context(ctx).Do()
			var apiErr *googleapi.Error
			if errors.As(err, &apiErr) && apiErr.Code == 409 {
				fmt.Printf("登録済み: message=%s\n", msg.Id)
				continue
			}
			if err != nil {
				return err
			}
			fmt.Printf("登録: %s / %s\n", event.Start.Date, event.Summary)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if failures > 0 {
		return fmt.Errorf("%d 件は抽出できず登録していません", failures)
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

func paymentEvent(p Payment, messageID, from, subject string) *calendar.Event {
	id := sha256.Sum256([]byte("gmail-payment:" + messageID))
	return &calendar.Event{
		Id:           fmt.Sprintf("%x", id),
		Summary:      fmt.Sprintf("%s 支払い %s円", p.Company, p.Amount),
		Description:  fmt.Sprintf("会社: %s\n金額: %s円\n送信元: %s\n件名: %s\n元メール: https://mail.google.com/mail/u/0/#all/%s", p.Company, p.Amount, from, subject, messageID),
		Start:        &calendar.EventDateTime{Date: p.Date.Format("2006-01-02")},
		End:          &calendar.EventDateTime{Date: p.Date.AddDate(0, 0, 1).Format("2006-01-02")},
		Transparency: "transparent",
	}
}
