package main

import "testing"

func TestParseBilling(t *testing.T) {
	b, err := parseBilling("2026年9月度 ご請求金額 12,345円 お支払い日 2026年10月2日", "https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	if b.PaymentDate != "2026-10-02" || b.AmountYen != 12345 {
		t.Fatalf("unexpected: %+v", b)
	}
}
