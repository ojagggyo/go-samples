package main

import (
	"encoding/base64"
	"fmt"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"google.golang.org/api/gmail/v1"
	"io"
	"mime"
	"strings"
	"unicode/utf8"
)

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
	// Gmail can return UTF-8 body bytes while retaining the original legacy charset header.
	// ISO-2022-JP is ASCII bytes with escape sequences, so UTF-8 validity alone is insufficient.
	utf8Body := utf8.Valid(b) && strings.IndexFunc(text, func(r rune) bool { return r > 127 }) >= 0
	if enc := params["charset"]; enc != "" && !utf8Body {
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
