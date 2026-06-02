package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type APIServer struct {
	cfg    *Config
	db     *sql.DB
	client *KhfyClient
	mux    *http.ServeMux
}

func NewAPIServer(cfg *Config, db *sql.DB, client *KhfyClient) *APIServer {
	s := &APIServer{cfg: cfg, db: db, client: client, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *APIServer) routes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/provider", s.handleProvider)
	s.mux.HandleFunc("/api/products", s.handleProducts)
	s.mux.HandleFunc("/api/preorders", s.handlePreorders)
	s.mux.HandleFunc("/api/preorders/", s.handlePreorderDelete)
	s.mux.HandleFunc("/api/logs", s.handleLogs)
	s.mux.HandleFunc("/api/stats", s.handleStats)
	s.mux.HandleFunc("/api/events", s.handleSSE)
	s.mux.HandleFunc("/api/saldo", s.handleSaldo)
	s.mux.HandleFunc("/webhook/khfy", s.handleWebhookKhfy)
	s.mux.Handle("/", http.FileServer(http.Dir("static")))
}

func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, map[string]any{
		"ok":       true,
		"provider": "khfy",
		"service":  "khfy-backend",
	})
}

func (s *APIServer) handleProvider(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, map[string]string{
		"provider": "khfy",
		"service":  "khfy-backend",
	})
}

func (s *APIServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *APIServer) Serve() error {
	log.Printf("[API] listening on %s", s.cfg.ListenAddr)
	return http.ListenAndServe(s.cfg.ListenAddr, s)
}

func jsonResp(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// GET /api/products
func (s *APIServer) handleProducts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT produk, nama, harga, available, ghost_count, lock_count, updated_at
		FROM product_cache ORDER BY available DESC, nama ASC`)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	type Product struct {
		Produk     string  `json:"produk"`
		Nama       string  `json:"nama"`
		Harga      float64 `json:"harga"`
		Available  bool    `json:"available"`
		GhostCount int     `json:"ghost_count"`
		LockCount  int     `json:"lock_count"`
		UpdatedAt  string  `json:"updated_at"`
	}
	var products []Product
	for rows.Next() {
		var p Product
		var avail int
		rows.Scan(&p.Produk, &p.Nama, &p.Harga, &avail, &p.GhostCount, &p.LockCount, &p.UpdatedAt)
		p.Available = avail == 1
		products = append(products, p)
	}
	if products == nil {
		products = []Product{}
	}
	jsonResp(w, 200, products)
}

// GET /api/preorders  POST /api/preorders
func (s *APIServer) handlePreorders(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := s.db.Query(`
			SELECT id, produk, produk_nama, tujuan, reff_id, status, attempts, max_attempts,
			       ghost_fail_count, lock_fail_count, note, created_at, updated_at
			FROM preorders ORDER BY created_at DESC LIMIT 200`)
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		type PO struct {
			ID             string `json:"id"`
			Produk         string `json:"produk"`
			ProdukNama     string `json:"produk_nama"`
			Tujuan         string `json:"tujuan"`
			ReffID         string `json:"reff_id"`
			Status         string `json:"status"`
			Attempts       int    `json:"attempts"`
			MaxAttempts    int    `json:"max_attempts"`
			GhostFailCount int    `json:"ghost_fail_count"`
			LockFailCount  int    `json:"lock_fail_count"`
			Note           string `json:"note"`
			CreatedAt      string `json:"created_at"`
			UpdatedAt      string `json:"updated_at"`
		}
		var list []PO
		for rows.Next() {
			var p PO
			var note sql.NullString
			rows.Scan(&p.ID, &p.Produk, &p.ProdukNama, &p.Tujuan, &p.ReffID,
				&p.Status, &p.Attempts, &p.MaxAttempts,
				&p.GhostFailCount, &p.LockFailCount, &note, &p.CreatedAt, &p.UpdatedAt)
			p.Note = note.String
			list = append(list, p)
		}
		if list == nil {
			list = []PO{}
		}
		jsonResp(w, 200, list)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Produk     string `json:"produk"`
			ProdukNama string `json:"produk_nama"`
			Tujuan     string `json:"tujuan"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Produk == "" || req.Tujuan == "" {
			jsonResp(w, 400, map[string]string{"error": "produk dan tujuan wajib diisi"})
			return
		}
		id := newUUID()
		reffID := newUUID()
		now := time.Now().UTC()
		_, err := s.db.Exec(`
			INSERT INTO preorders(id,produk,produk_nama,tujuan,reff_id,status,max_attempts,created_at,updated_at)
			VALUES(?,?,?,?,?,'pending',?,?,?)`,
			id, req.Produk, req.ProdukNama, req.Tujuan, reffID, s.cfg.MaxRetries, now, now)
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		logActivity(s.db, "info", req.Produk, id, fmt.Sprintf("Pre-order dibuat: %s → %s", req.Produk, req.Tujuan))
		jsonResp(w, 201, map[string]string{"id": id, "reff_id": reffID})
		return
	}

	jsonResp(w, 405, map[string]string{"error": "method not allowed"})
}

// DELETE /api/preorders/{id}
func (s *APIServer) handlePreorderDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		jsonResp(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/preorders/")
	if id == "" {
		jsonResp(w, 400, map[string]string{"error": "id required"})
		return
	}
	res, err := s.db.Exec(`UPDATE preorders SET status='cancelled', updated_at=? WHERE id=? AND status IN ('pending','retry')`,
		time.Now().UTC(), id)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonResp(w, 404, map[string]string{"error": "pre-order tidak ditemukan atau sudah selesai"})
		return
	}
	jsonResp(w, 200, map[string]string{"ok": "cancelled"})
}

// GET /api/logs
func (s *APIServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT id, level, produk, preorder_id, message, created_at
		FROM activity_logs ORDER BY id DESC LIMIT 100`)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	type Log struct {
		ID         int    `json:"id"`
		Level      string `json:"level"`
		Produk     string `json:"produk"`
		PreorderID string `json:"preorder_id"`
		Message    string `json:"message"`
		CreatedAt  string `json:"created_at"`
	}
	var logs []Log
	for rows.Next() {
		var l Log
		var pid sql.NullString
		rows.Scan(&l.ID, &l.Level, &l.Produk, &pid, &l.Message, &l.CreatedAt)
		l.PreorderID = pid.String
		logs = append(logs, l)
	}
	if logs == nil {
		logs = []Log{}
	}
	jsonResp(w, 200, logs)
}

// GET /api/stats
func (s *APIServer) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]int{}
	for _, status := range []string{"pending", "buying", "success", "failed", "retry", "cancelled"} {
		var n int
		s.db.QueryRow(`SELECT COUNT(*) FROM preorders WHERE status=?`, status).Scan(&n)
		stats[status] = n
	}
	var avail int
	s.db.QueryRow(`SELECT COUNT(*) FROM product_cache WHERE available=1`).Scan(&avail)
	stats["products_available"] = avail
	var total int
	s.db.QueryRow(`SELECT COUNT(*) FROM product_cache`).Scan(&total)
	stats["products_total"] = total
	jsonResp(w, 200, stats)
}

// GET /api/saldo — cek saldo reseller dari dashboard khfy
func (s *APIServer) handleSaldo(w http.ResponseWriter, r *http.Request) {
	dash, err := s.client.GetDashboard()
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if !dash.Ok || len(dash.Data) == 0 {
		jsonResp(w, 200, map[string]any{"ok": false})
		return
	}
	info := dash.Data[0]
	jsonResp(w, 200, map[string]any{
		"ok":                   true,
		"kode_reseller":        info.KodeReseller,
		"saldo":                info.Saldo,
		"bonus":                info.Bonus,
		"trx_total_hari_ini":   info.TrxTotalHariIni,
		"trx_sukses_hari_ini":  info.TrxSuksesHariIni,
		"trx_gagal_hari_ini":   info.TrxGagalHariIni,
		"trx_pending_hari_ini": info.TrxPendingHariIni,
	})
}

// POST /webhook/khfy — Menerima notifikasi real-time dari KhfyPay
// KhfyPay mengirim POST ke URL ini saat status transaksi berubah.
// Konfigurasi callback URL di panel KhfyPay ke: https://<domain>/webhook/khfy
func (s *APIServer) handleWebhookKhfy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	// KhfyPay mengirim JSON sesuai format HistoryItem
	var payload struct {
		ReffID     string  `json:"reff_id"`
		TrxID      string  `json:"trxid"`
		KodeProduk string  `json:"kode_produk"`
		Tujuan     string  `json:"tujuan"`
		StatusText string  `json:"status_text"` // SUKSES / GAGAL / PENDING
		Keterangan *string `json:"keterangan"`
		SN         *string `json:"sn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	if payload.ReffID == "" {
		w.WriteHeader(200)
		return
	}

	log.Printf("[WEBHOOK] khfy reff_id=%s status=%s", payload.ReffID, payload.StatusText)

	// Cari preorder berdasarkan reff_id
	var preorderID, produk, produkNama, tujuan string
	err := s.db.QueryRow(
		`SELECT id, produk, produk_nama, tujuan FROM preorders WHERE reff_id=? AND status NOT IN ('success','failed','cancelled')`,
		payload.ReffID,
	).Scan(&preorderID, &produk, &produkNama, &tujuan)
	if err != nil {
		// Tidak ditemukan atau sudah final — abaikan
		w.WriteHeader(200)
		return
	}

	ket := ""
	if payload.Keterangan != nil {
		ket = *payload.Keterangan
	} else if payload.SN != nil {
		ket = *payload.SN
	}

	now := time.Now().UTC()
	status := strings.ToLower(payload.StatusText)

	switch {
	case strings.Contains(status, "sukses") || strings.Contains(status, "success"):
		s.db.Exec(`UPDATE preorders SET status='success', note=?, updated_at=? WHERE id=?`, ket, now, preorderID)
		logActivity(s.db, "success", produk, preorderID, fmt.Sprintf("[webhook] Sukses: %s → %s | %s", produkNama, tujuan, ket))
		log.Printf("[WEBHOOK] SUCCESS preorder=%s produk=%s", preorderID, produk)

	case strings.Contains(status, "gagal") || strings.Contains(status, "failed") || strings.Contains(status, "batal"):
		s.db.Exec(`UPDATE preorders SET status='failed', note=?, updated_at=? WHERE id=?`, ket, now, preorderID)
		logActivity(s.db, "error", produk, preorderID, fmt.Sprintf("[webhook] Gagal: %s", ket))
		log.Printf("[WEBHOOK] FAILED preorder=%s ket=%s", preorderID, ket)

	default:
		// PENDING atau status lain — update note saja
		logActivity(s.db, "info", produk, preorderID, fmt.Sprintf("[webhook] Update status: %s", payload.StatusText))
	}

	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// GET /api/events — Server-Sent Events untuk real-time dashboard
func (s *APIServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", 500)
		return
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// Kirim stats terbaru
			stats := map[string]int{}
			for _, status := range []string{"pending", "buying", "success", "failed", "retry"} {
				var n int
				s.db.QueryRow(`SELECT COUNT(*) FROM preorders WHERE status=?`, status).Scan(&n)
				stats[status] = n
			}
			b, _ := json.Marshal(stats)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}
