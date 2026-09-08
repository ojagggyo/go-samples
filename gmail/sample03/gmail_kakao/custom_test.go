package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCustomTemplateRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/api/talk/memo/send" {
			t.Error("unexpected custom endpoint")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("template_id") != "12345" || r.Form.Get("template_object") != "" {
			t.Error("unexpected template selection")
		}
		var args map[string]string
		if err := json.Unmarshal([]byte(r.Form.Get("template_args")), &args); err != nil {
			t.Error(err)
		}
		if len(args) != 1 || args["CODE"] != "001234" {
			t.Error("unexpected code arguments")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"result_code":0}`))
	}))
	defer server.Close()
	if err := sendKakaoCode(context.Background(), server.Client(), server.URL, "", "12345", "001234"); err != nil {
		t.Fatal(err)
	}
}
