package main

import (
	"database/sql"
	"log"
	"strings"
	"time"
)

type StockEvent struct {
	Produk    string
	Nama      string
	Harga     float64
	Available bool
}

type StockMonitor struct {
	cfg    *Config
	client *KhfyClient
	db     *sql.DB
	events chan StockEvent
}

func NewStockMonitor(cfg *Config, client *KhfyClient, db *sql.DB) *StockMonitor {
	return &StockMonitor{
		cfg:    cfg,
		client: client,
		db:     db,
		events: make(chan StockEvent, 100),
	}
}

func (m *StockMonitor) Events() <-chan StockEvent {
	return m.events
}

func (m *StockMonitor) Run() {
	ticker := time.NewTicker(time.Duration(m.cfg.PollIntervalSec) * time.Second)
	defer ticker.Stop()
	log.Printf("[MONITOR] polling setiap %ds", m.cfg.PollIntervalSec)
	for ; ; <-ticker.C {
		m.poll()
	}
}

func (m *StockMonitor) poll() {
	products, err := m.client.ListProduct()
	if err != nil {
		log.Printf("[MONITOR] list_product error: %v", err)
		logActivity(m.db, "error", "", "", "list_product error: "+err.Error())
		return
	}

	avail := 0
	for _, p := range products {
		m.processProduct(p)
		if p.Available {
			avail++
		}
	}
	log.Printf("[MONITOR] poll OK: %d produk, %d tersedia", len(products), avail)
}

func (m *StockMonitor) processProduct(p Product) {
	var prevAvail int
	err := m.db.QueryRow(
		`SELECT available FROM product_cache WHERE produk=?`, p.Produk,
	).Scan(&prevAvail)

	avail := 0
	if p.Available {
		avail = 1
	}

	if err == sql.ErrNoRows {
		m.db.Exec(`INSERT INTO product_cache(produk,nama,harga,available,updated_at) VALUES(?,?,?,?,?)`,
			p.Produk, p.Nama, p.Harga, avail, time.Now().UTC())
		return
	}

	m.db.Exec(`UPDATE product_cache SET nama=?,harga=?,available=?,updated_at=? WHERE produk=?`,
		p.Nama, p.Harga, avail, time.Now().UTC(), p.Produk)

	// Restock: tidak tersedia → tersedia
	if prevAvail == 0 && avail == 1 {
		// Reset suspect_ghost saat restock nyata terdeteksi
		m.db.Exec(`UPDATE product_cache SET suspect_ghost=0, ghost_count=0, lock_count=0 WHERE produk=?`, p.Produk)
		log.Printf("[MONITOR] RESTOCK: %s (%s) Rp%.0f", p.Produk, p.Nama, p.Harga)
		logActivity(m.db, "restock", p.Produk, "", "Restock terdeteksi: "+p.Nama)
		m.events <- StockEvent{
			Produk:    p.Produk,
			Nama:      p.Nama,
			Harga:     p.Harga,
			Available: true,
		}
	}

	// Out of stock: tersedia → tidak tersedia
	if prevAvail == 1 && avail == 0 {
		log.Printf("[MONITOR] OUT_OF_STOCK: %s", p.Produk)
		// Jika sudah sering ghost, langsung tandai suspect_ghost
		var ghostCount int
		m.db.QueryRow(`SELECT ghost_count FROM product_cache WHERE produk=?`, p.Produk).Scan(&ghostCount)
		if ghostCount >= 3 {
			m.db.Exec(`UPDATE product_cache SET suspect_ghost=1 WHERE produk=?`, p.Produk)
			log.Printf("[MONITOR] SUSPECT_GHOST flagged: %s (ghost_count=%d)", p.Produk, ghostCount)
		}
	}
}

// classifyFailure returns "ghost", "lock", "saldo", atau "retry"
func classifyFailure(keterangan string) string {
	k := strings.ToLower(keterangan)
	// Saldo kurang — gagal permanen sampai top up
	if strings.Contains(k, "saldo") && (strings.Contains(k, "tidak") || strings.Contains(k, "kurang") || strings.Contains(k, "mencukupi")) {
		return "saldo"
	}
	// Ghost stock — stok habis/tidak tersedia (dari data real khfy)
	if strings.Contains(k, "stock transaksi habis") || strings.Contains(k, "stok habis") ||
		strings.Contains(k, "tidak tersedia") || strings.Contains(k, "not available") ||
		strings.Contains(k, "stok kosong") || strings.Contains(k, "produk kosong") {
		return "ghost"
	}
	// Lock — transaksi masih berjalan atau produk locked
	if strings.Contains(k, "tunggu transaksi") || strings.Contains(k, "masihproses") ||
		strings.Contains(k, "masih proses") || strings.Contains(k, "lock") ||
		strings.Contains(k, "maintenance") || strings.Contains(k, "menunggu jawaban") {
		return "lock"
	}
	// HTTP_CLIENT_RESPONSE_BODY_ERR dan error teknis lainnya → retry biasa
	return "retry"
}

// retryDelay returns backoff delay per attempt number (1-indexed)
func retryDelay(attempt int) time.Duration {
	delays := []time.Duration{0, 5 * time.Second, 15 * time.Second,
		30 * time.Second, 60 * time.Second, 120 * time.Second}
	if attempt < len(delays) {
		return delays[attempt]
	}
	return 120 * time.Second
}
