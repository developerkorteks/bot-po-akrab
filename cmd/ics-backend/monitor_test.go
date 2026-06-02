package main

import "testing"

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		ket  string
		want string
	}{
		{"Transaksi gagal - saldo dikembalikan", "retry"},
		{"Nomor tujuan salah", "retry"},
		{"insufficient balance", "saldo"},
		{"saldo kurang", "saldo"},
		{"stock kosong", "ghost"},
		{"product unavailable", "ghost"},
		{"maintenance provider", "lock"},
		{"still processing", "lock"},
		{"some weird error", "retry"},
	}
	for _, c := range cases {
		if got := classifyFailure(c.ket); got != c.want {
			t.Fatalf("classifyFailure(%q) = %q want %q", c.ket, got, c.want)
		}
	}
}

func TestRetryDelay(t *testing.T) {
	if got := retryDelay(1).Seconds(); got != 5 {
		t.Fatalf("retryDelay(1) = %.0f", got)
	}
	if got := retryDelay(9).Seconds(); got != 120 {
		t.Fatalf("retryDelay(9) = %.0f", got)
	}
}
