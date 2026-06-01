package main

import "testing"

// ─── classifyFailure ──────────────────────────────────────────────────────────

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		ket  string
		want string
	}{
		// Keterangan REAL dari data history khfy
		{"HTTP_CLIENT_RESPONSE_BODY_ERR", "retry"},
		{"stock transaksi habis", "ghost"},
		{"Stock Transaksi Habis", "ghost"}, // case-insensitive
		{"stok kosong tidak tersedia", "ghost"},
		{"Tunggu Transaksi selesai RC=xxx masihproses", "lock"},
		{"menunggu jawaban provider", "lock"},
		{"lock product", "lock"},
		{"maintenance scheduled", "lock"},
		{"Saldo tidak mencukupi, isi saldo dlu bos", "saldo"},
		{"saldo kurang", "saldo"},
		{"SALDO TIDAK CUKUP", "saldo"},
		{"produk tidak tersedia", "ghost"},
		{"some unknown error", "retry"},
		{"", "retry"},
	}
	for _, c := range cases {
		got := classifyFailure(c.ket)
		if got != c.want {
			t.Errorf("classifyFailure(%q) = %q, want %q", c.ket, got, c.want)
		} else {
			t.Logf("✅ %-50s → %s", c.ket, got)
		}
	}
}

// ─── extractExistingReffID ────────────────────────────────────────────────────

func TestExtractExistingReffID(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{
			"Tunggu Transaksi selesai RC=2a73da6b-a4c1-49ba-ad75-77326841361b TrxID=930740",
			"2a73da6b-a4c1-49ba-ad75-77326841361b",
		},
		{
			"RC=123e4567-e89b-12d3-a456-426614174000 masihproses",
			"123e4567-e89b-12d3-a456-426614174000",
		},
		{"no rc here", ""},
		{"RC=short", ""},
	}
	for _, c := range cases {
		got := extractExistingReffID(c.msg)
		if got != c.want {
			t.Errorf("extractExistingReffID(%q)\n  got  %q\n  want %q", c.msg, got, c.want)
		} else {
			t.Logf("✅ RC extracted: %q", got)
		}
	}
}

// ─── retryDelay ───────────────────────────────────────────────────────────────

func TestRetryDelay(t *testing.T) {
	cases2 := []struct {
		attempt int
		wantSec float64
	}{
		{1, 5},
		{2, 15},
		{3, 30},
		{4, 60},
		{5, 120},
		{9, 120}, // beyond max → clamp ke 120s
	}
	for _, c := range cases2 {
		got := retryDelay(c.attempt)
		if got.Seconds() != c.wantSec {
			t.Errorf("retryDelay(%d) = %.0fs, want %.0fs", c.attempt, got.Seconds(), c.wantSec)
		} else {
			t.Logf("✅ attempt %d → delay %.0fs", c.attempt, got.Seconds())
		}
	}
}
