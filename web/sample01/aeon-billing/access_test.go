package main

import "testing"

func TestAccessDeniedError(t *testing.T) {
	for _, tc := range []struct {
		body   string
		denied bool
	}{
		{"Access Denied\nYou don't have permission to access this server.", true},
		{"\n access denied\nReference #18", true},
		{"ログイン\nAEON Pay ID\n次へ", false},
		{"お支払い日 2026年10月2日\nご請求金額 12,345円", false},
	} {
		if got := accessDeniedError(tc.body) != nil; got != tc.denied {
			t.Errorf("accessDeniedError(%q): got %v, want %v", tc.body, got, tc.denied)
		}
	}
}
