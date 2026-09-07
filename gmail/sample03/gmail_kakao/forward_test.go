package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestExtractCode(t *testing.T) {
	b, err := os.ReadFile("config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(cfg.CodePattern)
	cases := []struct {
		body, want string
		fail       bool
	}{
		{"認証コード：００１２３４", "001234", false},
		{"暮らしのマネーサイトへのログインに必要な、ワンタイムパスワードをご案内いたします。\nワンタイムパスワード：　 123456\n有効期限：10分\n上記を入力して認証手続きをお願いいたします。", "123456", false},
		{"【ワンタイムパスワード】\n123456", "123456", false},
		{"Verification code: 765432", "765432", false},
		{"認証コード: 123456\n認証コード: 123456", "123456", false},
		{"認証コード: 123456\n認証コード: 654321", "", true},
		{"電話番号 123456 / 注文番号 543210", "", true},
		{"認証コード: 123456789", "", true},
	}
	for _, c := range cases {
		got, err := extractCode(c.body, pattern)
		if (err != nil) != c.fail || got != c.want {
			t.Fatalf("got=%q err=%v", got, err)
		}
	}
}

func TestForwardState(t *testing.T) {
	for _, fail := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "state.json")
		state := map[string]string{}
		calls := 0
		send := func(context.Context, string) error {
			calls++
			if fail {
				return errors.New("timeout")
			}
			return nil
		}
		err := forwardOnce(context.Background(), path, state, "id", "code", send)
		if (err != nil) != fail {
			t.Fatal(err)
		}
		reloaded, err := loadState(path)
		if err != nil {
			t.Fatal(err)
		}
		expected := "sent"
		if fail {
			expected = "pending"
		}
		if reloaded["id"] != expected {
			t.Fatal(reloaded)
		}
		if err := forwardOnce(context.Background(), path, reloaded, "id", "code", send); err != nil {
			t.Fatal(err)
		}
		if calls != 1 {
			t.Fatalf("sent %d times", calls)
		}
	}
}

func TestScanFiltersAndOrdering(t *testing.T) {
	t.Chdir(t.TempDir())
	now := time.Now()
	makeMessage := func(id, from, code string, received time.Time) *gmail.Message {
		return &gmail.Message{Id: id, InternalDate: received.UnixMilli(), Payload: &gmail.MessagePart{MimeType: "text/plain", Headers: []*gmail.MessagePartHeader{{Name: "From", Value: from}}, Body: &gmail.MessagePartBody{Data: base64.RawURLEncoding.EncodeToString([]byte("認証コード: " + code))}}}
	}
	msgs := map[string]*gmail.Message{
		"a":     makeMessage("a", "otp@example.com", "222222", now.Add(-time.Minute)),
		"z":     makeMessage("z", "otp@example.com", "111111", now.Add(-2*time.Minute)),
		"old":   makeMessage("old", "otp@example.com", "333333", now.Add(-time.Hour)),
		"other": makeMessage("other", "other@example.com", "444444", now.Add(-time.Minute)),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if id == "messages" {
			json.NewEncoder(w).Encode(&gmail.ListMessagesResponse{Messages: []*gmail.Message{{Id: "a"}, {Id: "old"}, {Id: "other"}, {Id: "z"}}})
			return
		}
		if msg, ok := msgs[id]; ok {
			json.NewEncoder(w).Encode(msg)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	gs, err := gmail.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	var delivered []string
	deliver := func(_ context.Context, text string) error { delivered = append(delivered, text); return nil }
	state := map[string]string{}
	for i := 0; i < 2; i++ {
		err := scan(context.Background(), gs, Config{MaxAgeSeconds: 600}, map[string]bool{"otp@example.com": true}, regexp.MustCompile(`認証コード: ([0-9]{6})`), state, deliver)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(delivered) != 2 || delivered[0] != "111111" || delivered[1] != "222222" {
		t.Fatal("wrong delivery order or filtering")
	}
}

func TestKakaoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/api/talk/memo/default/send" {
			t.Error("unexpected default endpoint")
		}
		if r.Method != "POST" {
			t.Error(r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		var template map[string]any
		if err := json.Unmarshal([]byte(r.Form.Get("template_object")), &template); err != nil {
			t.Error(err)
		}
		if template["object_type"] != "text" || template["text"] != "123456" {
			t.Error("unexpected template")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"result_code":0}`))
	}))
	defer server.Close()
	if err := sendKakaoCode(context.Background(), server.Client(), server.URL, "https://mail.google.com", "", "123456"); err != nil {
		t.Fatal(err)
	}
}
