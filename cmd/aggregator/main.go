package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Config struct {
	ListenAddr string `json:"listen_addr"`
	KHFYURL    string `json:"khfy_base_url"`
	ICSURL     string `json:"ics_base_url"`
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg := &Config{}
	if err := json.NewDecoder(f).Decode(cfg); err != nil {
		return nil, err
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8090"
	}
	if cfg.KHFYURL == "" {
		cfg.KHFYURL = "http://127.0.0.1:8089"
	}
	if cfg.ICSURL == "" {
		cfg.ICSURL = "http://127.0.0.1:8091"
	}
	return cfg, nil
}

type backendClient struct {
	provider string
	baseURL  string
	http     *http.Client
}

func (c *backendClient) get(path string, out any) error {
	resp, err := c.http.Get(c.baseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return errors.New(strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *backendClient) send(method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, c.baseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return errors.New(strings.TrimSpace(string(body)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

type server struct {
	khfy   *backendClient
	ics    *backendClient
	static http.Handler
}

func main() {
	cfgPath := flag.String("config", "config.aggregator.json", "path to config")
	flag.Parse()

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("Load config: %v", err)
	}

	base := http.Client{Timeout: 10 * time.Second}
	s := &server{
		khfy: &backendClient{provider: "khfy", baseURL: strings.TrimRight(cfg.KHFYURL, "/"), http: &base},
		ics:  &backendClient{provider: "ics", baseURL: strings.TrimRight(cfg.ICSURL, "/"), http: &base},
		static: http.FileServer(http.Dir(filepath.Join("cmd", "aggregator", "static"))),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/providers", s.handleProviders)
	mux.HandleFunc("/api/products", s.handleProducts)
	mux.HandleFunc("/api/preorders", s.handlePreorders)
	mux.HandleFunc("/api/preorders/", s.handlePreorderDelete)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/saldo", s.handleSaldo)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.Handle("/", s.static)

	log.Printf("[AGG] listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, withCORS(mux)))
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *server) providers() []*backendClient { return []*backendClient{s.khfy, s.ics} }

func (s *server) backendFor(name string) *backendClient {
	switch name {
	case "khfy":
		return s.khfy
	case "ics":
		return s.ics
	default:
		return nil
	}
}

func (s *server) handleProviders(w http.ResponseWriter, r *http.Request) {
	type provider struct {
		Provider string `json:"provider"`
		Online   bool   `json:"online"`
	}
	var list []provider
	for _, p := range s.providers() {
		var health map[string]any
		err := p.get("/api/health", &health)
		list = append(list, provider{Provider: p.provider, Online: err == nil})
	}
	writeJSON(w, 200, list)
}

type aggProduct struct {
	Provider   string  `json:"provider"`
	Produk     string  `json:"produk"`
	Nama       string  `json:"nama"`
	Harga      float64 `json:"harga"`
	Available  bool    `json:"available"`
	GhostCount int     `json:"ghost_count"`
	LockCount  int     `json:"lock_count"`
	UpdatedAt  string  `json:"updated_at"`
}

func (s *server) handleProducts(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("provider")
	var products []aggProduct
	offline := []string{}
	for _, p := range s.providers() {
		if filter != "" && filter != "all" && filter != p.provider {
			continue
		}
		var resp []aggProduct
		if err := p.get("/api/products", &resp); err != nil {
			offline = append(offline, p.provider)
			continue
		}
		for i := range resp {
			resp[i].Provider = p.provider
		}
		products = append(products, resp...)
	}
	sort.Slice(products, func(i, j int) bool {
		if products[i].Available != products[j].Available {
			return products[i].Available
		}
		if products[i].Provider != products[j].Provider {
			return products[i].Provider < products[j].Provider
		}
		return products[i].Nama < products[j].Nama
	})
	writeJSON(w, 200, map[string]any{"items": products, "offline": offline})
}

type aggPreorder struct {
	Provider       string `json:"provider"`
	ID             string `json:"id"`
	Produk         string `json:"produk"`
	ProdukNama     string `json:"produk_nama"`
	Tujuan         string `json:"tujuan"`
	ReffID         string `json:"reff_id"`
	ProviderRefID  string `json:"provider_ref_id,omitempty"`
	Status         string `json:"status"`
	Attempts       int    `json:"attempts"`
	MaxAttempts    int    `json:"max_attempts"`
	GhostFailCount int    `json:"ghost_fail_count"`
	LockFailCount  int    `json:"lock_fail_count"`
	Note           string `json:"note"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func (s *server) handlePreorders(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		filter := r.URL.Query().Get("provider")
		var items []aggPreorder
		offline := []string{}
		for _, p := range s.providers() {
			if filter != "" && filter != "all" && filter != p.provider {
				continue
			}
			var resp []aggPreorder
			if err := p.get("/api/preorders", &resp); err != nil {
				offline = append(offline, p.provider)
				continue
			}
			for i := range resp {
				resp[i].Provider = p.provider
			}
			items = append(items, resp...)
		}
		if items == nil {
			items = []aggPreorder{}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
		writeJSON(w, 200, map[string]any{"items": items, "offline": offline})
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Provider   string `json:"provider"`
			Produk     string `json:"produk"`
			ProdukNama string `json:"produk_nama"`
			Tujuan     string `json:"tujuan"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Provider == "" {
			writeJSON(w, 400, map[string]string{"error": "provider, produk, dan tujuan wajib diisi"})
			return
		}
		backend := s.backendFor(req.Provider)
		if backend == nil {
			writeJSON(w, 400, map[string]string{"error": "provider tidak dikenal"})
			return
		}
		var resp map[string]any
		err := backend.send(http.MethodPost, "/api/preorders", map[string]string{
			"produk":      req.Produk,
			"produk_nama": req.ProdukNama,
			"tujuan":      req.Tujuan,
		}, &resp)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": "provider " + req.Provider + " offline atau gagal memproses request"})
			return
		}
		resp["provider"] = req.Provider
		writeJSON(w, 201, resp)
		return
	}
	writeJSON(w, 405, map[string]string{"error": "method not allowed"})
}

func (s *server) handlePreorderDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/preorders/"), "/")
	if len(parts) != 2 {
		writeJSON(w, 400, map[string]string{"error": "gunakan format /api/preorders/{provider}/{id}"})
		return
	}
	backend := s.backendFor(parts[0])
	if backend == nil {
		writeJSON(w, 400, map[string]string{"error": "provider tidak dikenal"})
		return
	}
	var resp map[string]any
	if err := backend.send(http.MethodDelete, "/api/preorders/"+parts[1], nil, &resp); err != nil {
		writeJSON(w, 502, map[string]string{"error": "gagal membatalkan preorder di provider " + parts[0]})
		return
	}
	writeJSON(w, 200, resp)
}

type aggLog struct {
	Provider   string `json:"provider"`
	ID         int    `json:"id"`
	Level      string `json:"level"`
	Produk     string `json:"produk"`
	PreorderID string `json:"preorder_id"`
	Message    string `json:"message"`
	CreatedAt  string `json:"created_at"`
}

func (s *server) handleLogs(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("provider")
	var items []aggLog
	offline := []string{}
	for _, p := range s.providers() {
		if filter != "" && filter != "all" && filter != p.provider {
			continue
		}
		var resp []aggLog
		if err := p.get("/api/logs", &resp); err != nil {
			offline = append(offline, p.provider)
			continue
		}
		for i := range resp {
			resp[i].Provider = p.provider
		}
		items = append(items, resp...)
	}
	if items == nil {
		items = []aggLog{}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	if len(items) > 200 {
		items = items[:200]
	}
	writeJSON(w, 200, map[string]any{"items": items, "offline": offline})
}

func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	type statResp map[string]int
	out := map[string]any{
		"totals":   map[string]int{},
		"providers": map[string]any{},
		"offline":  []string{},
	}
	totals := out["totals"].(map[string]int)
	providers := out["providers"].(map[string]any)
	offline := []string{}
	for _, p := range s.providers() {
		var resp statResp
		if err := p.get("/api/stats", &resp); err != nil {
			offline = append(offline, p.provider)
			providers[p.provider] = map[string]any{"online": false}
			continue
		}
		providers[p.provider] = map[string]any{"online": true, "stats": resp}
		for k, v := range resp {
			totals[k] += v
		}
	}
	out["offline"] = offline
	writeJSON(w, 200, out)
}

func (s *server) handleSaldo(w http.ResponseWriter, r *http.Request) {
	providers := map[string]any{}
	offline := []string{}
	for _, p := range s.providers() {
		var resp map[string]any
		if err := p.get("/api/saldo", &resp); err != nil {
			offline = append(offline, p.provider)
			providers[p.provider] = map[string]any{"online": false, "ok": false}
			continue
		}
		resp["online"] = true
		resp["provider"] = p.provider
		providers[p.provider] = resp
	}
	writeJSON(w, 200, map[string]any{"providers": providers, "offline": offline})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "service": "aggregator"})
}
