package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

func extractCode(body string, pattern *regexp.Regexp) (string, error) {
	values := map[string]bool{}
	for _, m := range pattern.FindAllStringSubmatch(norm.NFKC.String(html.UnescapeString(body)), -1) {
		if len(m) != 2 || len(m[1]) < 4 || len(m[1]) > 32 {
			return "", fmt.Errorf("コードの形式が不正です")
		}
		if !regexp.MustCompile(`^[A-Za-z0-9-]+$`).MatchString(m[1]) {
			return "", fmt.Errorf("コードに未対応の文字が含まれています")
		}
		values[m[1]] = true
	}
	if len(values) != 1 {
		return "", fmt.Errorf("コードを一意に抽出できません")
	}
	for code := range values {
		return code, nil
	}
	panic("unreachable")
}

func loadState(path string) (map[string]string, error) {
	state := map[string]string{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("state.jsonが不正です")
	}
	return state, nil
}

func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", b, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func forwardOnce(ctx context.Context, path string, state map[string]string, id, text string, send func(context.Context, string) error) error {
	if state[id] != "" {
		return nil
	}
	state[id] = "pending"
	if err := writeJSON(path, state); err != nil {
		return err
	}
	if err := send(ctx, text); err != nil {
		return fmt.Errorf("送信結果が未確認です。Kakaoを確認してください（message=%s）: %w", id, err)
	}
	state[id] = "sent"
	return writeJSON(path, state)
}

func sendToMe(ctx context.Context, client *http.Client, endpoint, link, text string) error {
	template := map[string]any{"object_type": "text", "text": text, "link": map[string]string{"web_url": link, "mobile_web_url": link}, "button_title": "メールを開く"}
	b, err := json.Marshal(template)
	if err != nil {
		return err
	}
	form := url.Values{"template_object": {string(b)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Kakao API: HTTP %d", resp.StatusCode)
	}
	var result struct {
		Code *int `json:"result_code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.Code == nil || *result.Code != 0 {
		return fmt.Errorf("Kakao APIの送信結果が成功ではありません")
	}
	return nil
}
