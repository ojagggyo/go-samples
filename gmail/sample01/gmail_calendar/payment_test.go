package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/text/encoding/japanese"
	"google.golang.org/api/gmail/v1"
)

func TestParsePayment(t *testing.T) {
	cases := []struct {
		name, company, body, date, amount string
		fail                              bool
	}{
		{"saison", "セゾン", "ご利用日: 2026/9/1\nご利用金額: 999円\nお支払日：2026年10月5日\nお支払金額：12,345円", "2026-10-05", "12345", false},
		{"rakuten", "楽天", "お支払い日\n２０２６年１０月２７日\nご請求金額：９８，７６５円", "2026-10-27", "98765", false},
		{"same repeated", "楽天", "お支払日:2026-10-27 ご請求金額:123円\nお支払日:2026-10-27 ご請求金額:123円", "2026-10-27", "123", false},
		{"missing year", "楽天", "お支払日:10月27日 ご請求金額:123円", "", "", true},
		{"invalid date", "セゾン", "お支払日:2026年2月30日 ご請求金額:123円", "", "", true},
		{"multiple amounts", "楽天", "お支払日:2026/10/27 ご請求金額:123円 ご請求金額:456円", "", "", true},
		{"multiple dates", "楽天", "お支払日:2026/10/27 お支払日:2026/11/27 ご請求金額:123円", "", "", true},
		{"bad separator", "楽天", "お支払日:2026/10/27 ご請求金額:1,23円", "", "", true},
		{"no amount", "楽天", "お支払日:2026/10/27 会員サイトで金額を確認してください", "", "", true},
		{"zero", "楽天", "お支払日:2026/10/27 ご請求金額:0円", "2026-10-27", "0", false},
		{"rakuten confirmed nonzero", "楽天", "2026年8月 お支払い金額\n確定\n12,345円\nお支払い日\n2026/08/27", "2026-08-27", "12345", false},
		{"rakuten unconfirmed", "楽天", "2026年8月 お支払い金額\n未確定\n100円\nお支払い日\n2026/08/27", "", "", true},
		{"rakuten conflicting amounts", "楽天", "お支払い金額\n確定\n100円\nご請求金額:200円\nお支払い日\n2026/08/27", "", "", true},
		{"confirmed format limited to rakuten", "セゾン", "お支払い金額\n確定\n100円\nお支払い日\n2026/08/27", "", "", true},
		{"unknown", "別会社", "お支払日:2026/10/27 ご請求金額:123円", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := parsePayment(c.company, c.body)
			if (err != nil) != c.fail {
				t.Fatalf("payment=%+v error=%v", p, err)
			}
			if !c.fail && (p.Date.Format("2006-01-02") != c.date || p.Amount != c.amount) {
				t.Fatalf("payment=%+v", p)
			}
		})
	}
}

func TestProvidedMailSamples(t *testing.T) {
	cases := []struct{ company, body, date, amount string }{
		{"セゾン", "お支払金額のお知らせ\nセゾンカードインターナショナル\nお支払日 \t2026年9月4日(金)\n口座へのご準備期日 \t2026年9月3日(木)\nお支払金額 \t70円\n※お支払金額の変更(まとめてリボ)の期日は、8月24日(月)20:00まで", "2026-09-04", "70"},
		{"楽天", "2026年8月 お支払い金額のご案内\n2026年8月　お支払い金額\n確定 \t&#x9;\n0円\nご利用カード\n楽天PINKカード(Visa)\nお支払い日\n2026/08/27", "2026-08-27", "0"},
	}
	for _, c := range cases {
		for _, kind := range []string{"text/plain", "text/html"} {
			t.Run(c.company+"/"+kind, func(t *testing.T) {
				body := c.body
				if kind == "text/html" {
					body = "<div>" + strings.ReplaceAll(body, "\n", "</div><div>") + "</div>"
				}
				text, err := messageText(textPart(kind, body))
				if err != nil {
					t.Fatal(err)
				}
				p, err := parsePayment(c.company, text)
				if err != nil {
					t.Fatal(err)
				}
				if p.Date.Format("2006-01-02") != c.date || p.Amount != c.amount {
					t.Fatalf("unexpected payment: %+v", p)
				}
				event := paymentEvent(p, "sample", "sender@example.com", "件名")
				if event.Start.Date != c.date || event.Summary != c.company+" 支払い "+c.amount+"円" {
					t.Fatalf("unexpected event: %+v", event)
				}
			})
		}
	}
}

func TestBracketedMIMEPayment(t *testing.T) {
	cases := []struct{ company, body, charset, date, amount string }{
		{"セゾン", "【対象カード】\r\nセゾンカードインターナショナル\r\n【お支払日】\r\n2026年9月4日(金)\r\n【口座へのご準備期日】\r\n2026年9月3日(木)\r\n【お支払金額】\r\n70円\r\n※お支払金額の変更(まとめてリボ)は2026年8月24日(月)20:00までにお願いします。", "utf-8", "2026-09-04", "70"},
		{"楽天", "[ご利用カード]\r\n楽天PINKカード(Visa)\r\n[お支払い日]\r\n2026/08/27\r\n[お支払い金額]\r\n0円", "iso-2022-jp", "2026-08-27", "0"},
	}
	for _, c := range cases {
		t.Run(c.company, func(t *testing.T) {
			data := []byte(c.body)
			if c.charset == "iso-2022-jp" {
				var err error
				data, err = japanese.ISO2022JP.NewEncoder().Bytes(data)
				if err != nil {
					t.Fatal(err)
				}
			}
			plain := textPart("text/plain", string(data))
			plain.Headers = []*gmail.MessagePartHeader{{Name: "Content-Type", Value: "text/plain; charset=" + c.charset}}
			part := &gmail.MessagePart{MimeType: "multipart/alternative", Parts: []*gmail.MessagePart{plain, textPart("text/html", "<p>別表現</p>")}}
			body, err := messageText(part)
			if err != nil {
				t.Fatal(err)
			}
			p, err := parsePayment(c.company, body)
			if err != nil {
				t.Fatal(err)
			}
			if p.Date.Format("2006-01-02") != c.date || p.Amount != c.amount {
				t.Fatalf("unexpected payment: %+v", p)
			}
		})
	}
}

func TestUTF8BodyWithLegacyCharsetHeader(t *testing.T) {
	part := textPart("text/plain", "[お支払い日]\r\n2026/08/27\r\n[お支払い金額]\r\n0円(税込)")
	part.Headers = []*gmail.MessagePartHeader{{Name: "Content-Type", Value: `text/plain; charset="iso-2022-jp"`}}
	body, err := messageText(part)
	if err != nil {
		t.Fatal(err)
	}
	p, err := parsePayment("楽天", body)
	if err != nil {
		t.Fatal(err)
	}
	if p.Date.Format("2006-01-02") != "2026-08-27" || p.Amount != "0" {
		t.Fatalf("unexpected payment: %+v", p)
	}
}

func textPart(kind, text string) *gmail.MessagePart {
	return &gmail.MessagePart{MimeType: kind, Body: &gmail.MessagePartBody{Data: base64.RawURLEncoding.EncodeToString([]byte(text))}}
}

func TestHTMLAndAlternative(t *testing.T) {
	part := textPart("text/html", `<style>ご請求金額:999円</style><table><tr><td>お支払日</td><td>2026年10月27日</td></tr><tr><td>ご請求金額</td><td><span>12,345</span>円</td></tr></table>`)
	body, err := messageText(part)
	if err != nil {
		t.Fatal(err)
	}
	p, err := parsePayment("楽天", body)
	if err != nil || p.Amount != "12345" {
		t.Fatalf("%+v %v: %s", p, err, body)
	}
	alternative := &gmail.MessagePart{MimeType: "multipart/alternative", Parts: []*gmail.MessagePart{textPart("text/plain", "plain"), part}}
	body, err = messageText(alternative)
	if err != nil || body != "plain" {
		t.Fatalf("%q %v", body, err)
	}
	attachment := textPart("text/plain", "attachment")
	attachment.Filename = "bill.txt"
	body, err = messageText(attachment)
	if err != nil || body != "" {
		t.Fatalf("%q %v", body, err)
	}
}

func TestPaymentEvent(t *testing.T) {
	p, err := parsePayment("セゾン", "お支払日:2026年12月31日 お支払金額:100円")
	if err != nil {
		t.Fatal(err)
	}
	a := paymentEvent(p, "mail1", "sender@example.com", "件名")
	b := paymentEvent(p, "mail1", "sender@example.com", "件名")
	c := paymentEvent(p, "mail2", "sender@example.com", "件名")
	if a.Id != b.Id || a.Id == c.Id {
		t.Fatal("event IDs must be stable per message")
	}
	if a.Start.Date != "2026-12-31" || a.End.Date != "2027-01-01" {
		t.Fatalf("start=%v end=%v", a.Start, a.End)
	}
}
