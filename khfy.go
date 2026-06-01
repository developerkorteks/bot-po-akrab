package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const khfyBase = "https://panel.khfy-store.com"

type KhfyClient struct {
	apiKey string
	http   *http.Client
}

func NewKhfyClient(apiKey, proxyAddr string) (*KhfyClient, error) {
	transport := &http.Transport{}
	if proxyAddr != "" {
		pu, err := url.Parse(proxyAddr)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy addr: %w", err)
		}
		transport.Proxy = http.ProxyURL(pu)
	}
	return &KhfyClient{
		apiKey: apiKey,
		http: &http.Client{
			Timeout:   15 * time.Second,
			Transport: transport,
		},
	}, nil
}

type rawProduct struct {
	KodeProduk   string  `json:"kode_produk"`
	NamaProduk   string  `json:"nama_produk"`
	KodeProvider string  `json:"kode_provider"`
	Gangguan     int     `json:"gangguan"`
	Kosong       int     `json:"kosong"`
	HargaFinal   float64 `json:"harga_final"`
}

type Product struct {
	Produk    string
	Nama      string
	Harga     float64
	Available bool
}

type ListProductResp struct {
	Ok    bool         `json:"ok"`
	Count int          `json:"count"`
	Data  []rawProduct `json:"data"`
}

func (c *KhfyClient) ListProduct() ([]Product, error) {
	resp, err := c.http.Get(fmt.Sprintf("%s/api_v2/list_product?api_key=%s", khfyBase, c.apiKey))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r ListProductResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse error: %w — body: %.200s", err, body)
	}
	products := make([]Product, 0, len(r.Data))
	for _, rp := range r.Data {
		products = append(products, Product{
			Produk:    rp.KodeProduk,
			Nama:      rp.NamaProduk,
			Harga:     rp.HargaFinal,
			Available: rp.Kosong == 0 && rp.Gangguan == 0,
		})
	}
	return products, nil
}

type TrxResp struct {
	Ok   bool   `json:"ok"`
	Msg  string `json:"msg"`
	Data struct {
		TrxID  string `json:"trxid"`
		ReffID string `json:"reffid"`
		Tujuan string `json:"tujuan"`
		Status string `json:"status"`
	} `json:"data"`
}

func (c *KhfyClient) CreateTrx(produk, tujuan, reffID string) (*TrxResp, error) {
	u := fmt.Sprintf("%s/api_v2/trx?produk=%s&tujuan=%s&reff_id=%s&api_key=%s",
		khfyBase, url.QueryEscape(produk), url.QueryEscape(tujuan),
		url.QueryEscape(reffID), c.apiKey)
	resp, err := c.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r TrxResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse error: %w — body: %.200s", err, body)
	}
	return &r, nil
}

type HistoryItem struct {
	Kode       int     `json:"kode"`        // trxid numeric
	KodeProduk string  `json:"kode_produk"` // product code
	Tujuan     string  `json:"tujuan"`
	Harga      float64 `json:"harga"`
	Status2    int     `json:"status2"`     // 0=success?,1=failed?,2=pending
	StatusText string  `json:"status_text"` // SUKSES,GAGAL,PENDING,BATAL
	SN         *string `json:"sn"`          // serial number (null saat pending)
	Keterangan *string `json:"keterangan"`  // keterangan (nullable)
	TglEntri   string  `json:"tgl_entri"`
	TglStatus  string  `json:"tgl_status"`
}

func (h HistoryItem) GetKeterangan() string {
	if h.Keterangan != nil {
		return *h.Keterangan
	}
	if h.SN != nil {
		return *h.SN
	}
	return ""
}

type HistoryResp struct {
	Ok    bool          `json:"ok"`
	Count int           `json:"count"`
	Data  []HistoryItem `json:"data"`
}

func (c *KhfyClient) GetHistory(reffID string) (*HistoryResp, error) {
	u := fmt.Sprintf("%s/api_v2/history?api_key=%s&refid=%s", khfyBase, c.apiKey, url.QueryEscape(reffID))
	resp, err := c.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r HistoryResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse error: %w — body: %.200s", err, body)
	}
	return &r, nil
}

type AkrabStock struct {
	Type     string `json:"type"`
	Nama     string `json:"nama"`
	SisaSlot int    `json:"sisa_slot"`
}

type AkrabStockResp struct {
	Ok   bool         `json:"ok"`
	Data []AkrabStock `json:"data"`
}

type ResellerInfo struct {
	KodeReseller      string  `json:"kode_reseller"`
	Saldo             float64 `json:"saldo"`
	Bonus             float64 `json:"bonus"`
	TrxTotalHariIni   int     `json:"trx_total_hari_ini"`
	TrxSuksesHariIni  int     `json:"trx_sukses_hari_ini"`
	TrxGagalHariIni   int     `json:"trx_gagal_hari_ini"`
	TrxPendingHariIni int     `json:"trx_pending_hari_ini"`
}

type DashboardResp struct {
	Ok     bool           `json:"ok"`
	Count  int            `json:"count"`
	Data   []ResellerInfo `json:"data"`
	NewTrx []HistoryItem  `json:"new_trx"`
}

func (c *KhfyClient) GetDashboard() (*DashboardResp, error) {
	u := fmt.Sprintf("%s/api_v2/dashboard?api_key=%s", khfyBase, c.apiKey)
	resp, err := c.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r DashboardResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse dashboard: %w — body: %.200s", err, body)
	}
	return &r, nil
}

func (c *KhfyClient) CekStockAkrab() ([]AkrabStock, error) {
	resp, err := c.http.Get(khfyBase + "/api_v3/cek_stock_akrab")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r AkrabStockResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse akrab: %w — body: %.200s", err, body)
	}
	return r.Data, nil
}
