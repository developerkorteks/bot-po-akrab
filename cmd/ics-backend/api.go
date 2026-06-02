package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type APIServer struct {
	cfg    *Config
	db     *sql.DB
	client *ICSClient
	mux    *http.ServeMux
}

func NewAPIServer(cfg *Config, db *sql.DB, client *ICSClient) *APIServer {
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
	s.mux.HandleFunc("/webhook/ics", s.handleWebhookICS)
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
	return http.ListenAndServe(s.cfg.ListenAddr, s)
}

func jsonResp(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, map[string]any{"ok": true, "provider": "ics", "service": "ics-backend"})
}

func (s *APIServer) handleProvider(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, map[string]string{"provider": "ics", "service": "ics-backend"})
}

func (s *APIServer) handleProducts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT produk, nama, harga, available, ghost_count, lock_count, updated_at FROM product_cache ORDER BY available DESC, nama ASC`)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	type product struct {
		Produk     string  `json:"produk"`
		Nama       string  `json:"nama"`
		Harga      float64 `json:"harga"`
		Available  bool    `json:"available"`
		GhostCount int     `json:"ghost_count"`
		LockCount  int     `json:"lock_count"`
		UpdatedAt  string  `json:"updated_at"`
	}
	var products []product
	for rows.Next() {
		var p product
		var avail int
		rows.Scan(&p.Produk, &p.Nama, &p.Harga, &avail, &p.GhostCount, &p.LockCount, &p.UpdatedAt)
		p.Available = avail == 1
		products = append(products, p)
	}
	if products == nil {
		products = []product{}
	}
	jsonResp(w, 200, products)
}

func (s *APIServer) handlePreorders(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := s.db.Query(`
			SELECT id, produk, produk_nama, tujuan, reff_id, provider_ref_id, status, attempts, max_attempts,
			       ghost_fail_count, lock_fail_count, note, created_at, updated_at
			FROM preorders ORDER BY created_at DESC LIMIT 200`)
		if err != nil {
			jsonResp(w, 500, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		type po struct {
			ID             string `json:"id"`
			Produk         string `json:"produk"`
			ProdukNama     string `json:"produk_nama"`
			Tujuan         string `json:"tujuan"`
			ReffID         string `json:"reff_id"`
			ProviderRefID  string `json:"provider_ref_id"`
			Status         string `json:"status"`
			Attempts       int    `json:"attempts"`
			MaxAttempts    int    `json:"max_attempts"`
			GhostFailCount int    `json:"ghost_fail_count"`
			LockFailCount  int    `json:"lock_fail_count"`
			Note           string `json:"note"`
			CreatedAt      string `json:"created_at"`
			UpdatedAt      string `json:"updated_at"`
		}
		var list []po
		for rows.Next() {
			var p po
			var note, providerRef sql.NullString
			rows.Scan(&p.ID, &p.Produk, &p.ProdukNama, &p.Tujuan, &p.ReffID, &providerRef, &p.Status, &p.Attempts, &p.MaxAttempts,
				&p.GhostFailCount, &p.LockFailCount, &note, &p.CreatedAt, &p.UpdatedAt)
			p.Note = note.String
			p.ProviderRefID = providerRef.String
			list = append(list, p)
		}
		if list == nil {
			list = []po{}
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

func (s *APIServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, level, produk, preorder_id, message, created_at FROM activity_logs ORDER BY id DESC LIMIT 100`)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	type logItem struct {
		ID         int    `json:"id"`
		Level      string `json:"level"`
		Produk     string `json:"produk"`
		PreorderID string `json:"preorder_id"`
		Message    string `json:"message"`
		CreatedAt  string `json:"created_at"`
	}
	var logs []logItem
	for rows.Next() {
		var l logItem
		var pid sql.NullString
		rows.Scan(&l.ID, &l.Level, &l.Produk, &pid, &l.Message, &l.CreatedAt)
		l.PreorderID = pid.String
		logs = append(logs, l)
	}
	if logs == nil {
		logs = []logItem{}
	}
	jsonResp(w, 200, logs)
}

func (s *APIServer) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]int{}
	for _, status := range []string{"pending", "buying", "success", "failed", "retry", "cancelled"} {
		var n int
		s.db.QueryRow(`SELECT COUNT(*) FROM preorders WHERE status=?`, status).Scan(&n)
		stats[status] = n
	}
	var avail, total int
	s.db.QueryRow(`SELECT COUNT(*) FROM product_cache WHERE available=1`).Scan(&avail)
	s.db.QueryRow(`SELECT COUNT(*) FROM product_cache`).Scan(&total)
	stats["products_available"] = avail
	stats["products_total"] = total
	jsonResp(w, 200, stats)
}

func (s *APIServer) handleSaldo(w http.ResponseWriter, r *http.Request) {
	profile, err := s.client.GetProfile()
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if !profile.Success {
		jsonResp(w, 200, map[string]any{"ok": false})
		return
	}
	jsonResp(w, 200, map[string]any{
		"ok":                   true,
		"kode_reseller":        profile.Data.KodeReseller,
		"saldo":                profile.Data.Saldo,
		"bonus":                0,
		"trx_total_hari_ini":   0,
		"trx_sukses_hari_ini":  0,
		"trx_gagal_hari_ini":   0,
		"trx_pending_hari_ini": 0,
		"status":               profile.Data.Status,
		"username":             profile.Data.Username,
	})
}

func (s *APIServer) handleWebhookICS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var payload struct {
		Success      bool    `json:"success"`
		Status       string  `json:"status"`
		RefID        string  `json:"refid"`
		SupplierTrx  string  `json:"supplier_trxid"`
		KodeProduk   string  `json:"kode_produk"`
		NomorTujuan  string  `json:"nomor_tujuan"`
		Harga        float64 `json:"harga"`
		KodeReseller string  `json:"kodereseller"`
		Message      string  `json:"message"`
		Note         string  `json:"note"`
		UpdatedAt    string  `json:"updated_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	if payload.RefID == "" {
		w.WriteHeader(200)
		return
	}

	var preorderID, produk, produkNama, tujuan string
	err := s.db.QueryRow(
		`SELECT id, produk, produk_nama, tujuan FROM preorders
		 WHERE (provider_ref_id=? OR reff_id=?) AND status NOT IN ('success','failed','cancelled')
		 ORDER BY updated_at DESC LIMIT 1`,
		payload.RefID, payload.RefID,
	).Scan(&preorderID, &produk, &produkNama, &tujuan)
	if err != nil {
		w.WriteHeader(200)
		return
	}
	ket := firstNonEmpty(payload.Note, payload.Message)
	now := time.Now().UTC()
	status := strings.ToLower(payload.Status)
	switch status {
	case "success":
		s.db.Exec(`UPDATE preorders SET status='success', provider_ref_id=?, note=?, last_raw_status=?, last_raw_message=?, updated_at=? WHERE id=?`,
			payload.RefID, ket, payload.Status, ket, now, preorderID)
		logActivity(s.db, "success", produk, preorderID, fmt.Sprintf("[webhook] Sukses: %s → %s | %s", produkNama, tujuan, ket))
	case "failed":
		s.db.Exec(`UPDATE preorders SET status='failed', provider_ref_id=?, note=?, last_raw_status=?, last_raw_message=?, updated_at=? WHERE id=?`,
			payload.RefID, ket, payload.Status, ket, now, preorderID)
		logActivity(s.db, "error", produk, preorderID, fmt.Sprintf("[webhook] Gagal: %s", ket))
	default:
		s.db.Exec(`UPDATE preorders SET provider_ref_id=?, last_raw_status=?, last_raw_message=?, updated_at=? WHERE id=?`,
			payload.RefID, payload.Status, ket, now, preorderID)
		logActivity(s.db, "info", produk, preorderID, fmt.Sprintf("[webhook] Update status: %s", payload.Status))
	}
	jsonResp(w, 200, map[string]bool{"ok": true})
}

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
