package main

import (
	"encoding/base64"
	"fmt"
	stdhtml "html"
	"io"
	"mime"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/unicode/norm"
	"google.golang.org/api/gmail/v1"
)

type Payment struct {
	Company string
	Date    time.Time
	Amount  string
}

// Only accept values directly following payment-specific labels.
var dateRE = regexp.MustCompile(`(?:お支払日|お支払い日|支払日|支払い日|口座振替日|お引落日|お引き落とし日|引落日)[\s:：]*([0-9]{4})[年/\-]([0-9]{1,2})[月/\-]([0-9]{1,2})(?:日|\b)`)
var amountRE = regexp.MustCompile(`(?:お支払金額|お支払い金額|ご請求金額|請求金額|お引落金額|お引き落とし金額|ご請求額)[\s:：]*[¥￥]?[\s]*([0-9][0-9,]*)\s*円`)
var yenRE = regexp.MustCompile(`^(?:[0-9]+|[0-9]{1,3}(?:,[0-9]{3})+)$`)

// Rakuten places a confirmation label between the monthly heading and amount.
var rakutenAmountRE = regexp.MustCompile(`お支払い金額[\s:：]*確定[\s:：]*([0-9][0-9,]*)\s*円`)

func parsePayment(company, body string) (Payment, error) {
	p := Payment{Company: company}
	if company != "セゾン" && company != "楽天" {
		return p, fmt.Errorf("未対応の送信元")
	}
	body = norm.NFKC.String(stdhtml.UnescapeString(body))
	dates := map[string]time.Time{}
	for _, m := range dateRE.FindAllStringSubmatch(body, -1) {
		year, _ := strconv.Atoi(m[1])
		month, _ := strconv.Atoi(m[2])
		day, _ := strconv.Atoi(m[3])
		d := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60))
		if d.Year() != year || int(d.Month()) != month || d.Day() != day {
			return p, fmt.Errorf("支払日が不正です")
		}
		dates[d.Format("2006-01-02")] = d
	}
	if len(dates) != 1 {
		return p, fmt.Errorf("年を含む支払日を一意に抽出できません")
	}
	for _, d := range dates {
		p.Date = d
	}
	amounts := map[string]bool{}
	matches := amountRE.FindAllStringSubmatch(body, -1)
	if company == "楽天" {
		matches = append(matches, rakutenAmountRE.FindAllStringSubmatch(body, -1)...)
	}
	for _, m := range matches {
		if !yenRE.MatchString(m[1]) {
			return p, fmt.Errorf("金額の桁区切りが不正です")
		}
		amount, err := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
		if err != nil || amount < 0 {
			return p, fmt.Errorf("支払金額が不正です")
		}
		amounts[strconv.FormatInt(amount, 10)] = true
	}
	if len(amounts) != 1 {
		return p, fmt.Errorf("支払金額を一意に抽出できません")
	}
	for amount := range amounts {
		p.Amount = amount
	}
	return p, nil
}

func messageText(part *gmail.MessagePart) (string, error) {
	if part == nil {
		return "", fmt.Errorf("本文がありません")
	}
	if part.Filename != "" {
		return "", nil
	}
	if strings.HasPrefix(part.MimeType, "multipart/") {
		var texts []string
		// Prefer plain text in multipart/alternative to avoid counting both representations.
		if part.MimeType == "multipart/alternative" {
			for _, child := range part.Parts {
				if child.MimeType == "text/plain" {
					return messageText(child)
				}
			}
		}
		for _, child := range part.Parts {
			s, err := messageText(child)
			if err != nil {
				return "", err
			}
			texts = append(texts, s)
		}
		return strings.Join(texts, "\n"), nil
	}
	if part.MimeType != "text/plain" && part.MimeType != "text/html" {
		return "", nil
	}
	if part.Body == nil || part.Body.AttachmentId != "" {
		return "", fmt.Errorf("本文データがありません（外部格納された本文は未対応）")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(part.Body.Data, "="))
	if err != nil {
		return "", err
	}
	_, params, err := mime.ParseMediaType(header(part, "Content-Type"))
	if err != nil && header(part, "Content-Type") != "" {
		return "", err
	}
	text := string(b)
	if enc := params["charset"]; enc != "" {
		reader, err := charset.NewReaderLabel(enc, strings.NewReader(text))
		if err != nil {
			return "", err
		}
		decoded, err := io.ReadAll(reader)
		if err != nil {
			return "", err
		}
		text = string(decoded)
	}
	if part.MimeType == "text/html" {
		root, err := html.Parse(strings.NewReader(text))
		if err != nil {
			return "", err
		}
		var result strings.Builder
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
				return
			}
			if n.Type == html.TextNode {
				result.WriteString(n.Data)
			}
			block := n.Type == html.ElementNode && strings.Contains("|br|p|div|tr|td|li|table|", "|"+n.Data+"|")
			if block {
				result.WriteString("\n")
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			if block {
				result.WriteString("\n")
			}
		}
		walk(root)
		text = result.String()
	}
	return text, nil
}
