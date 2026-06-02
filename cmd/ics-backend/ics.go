package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ICSClient struct {
	secret  string
	baseURL string
	http    *http.Client
}

func NewICSClient(secret, baseURL string) *ICSClient {
	return &ICSClient{
		secret:  secret,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

type Product struct {
	Produk    string
	Nama      string
	Harga     float64
	Available bool
}

type icsProduct struct {
	Code   string  `json:"code"`
	Name   string  `json:"name"`
	Price  float64 `json:"price"`
	Status string  `json:"status"`
	Stock  int     `json:"stock"`
}

func (c *ICSClient) doJSON(method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parse error: %w — body: %.200s", err, raw)
	}
	return nil
}

func (c *ICSClient) ListProduct() ([]Product, error) {
	var resp struct {
		Success bool         `json:"success"`
		Data    []icsProduct `json:"data"`
	}
	if err := c.doJSON(http.MethodGet, "/products", nil, &resp); err != nil {
		return nil, err
	}
	products := make([]Product, 0, len(resp.Data))
	for _, p := range resp.Data {
		products = append(products, Product{
			Produk:    p.Code,
			Nama:      p.Name,
			Harga:     p.Price,
			Available: strings.EqualFold(p.Status, "available") && p.Stock > 0,
		})
	}
	return products, nil
}

type CreateTrxResp struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		RefID            string  `json:"refid"`
		Price            float64 `json:"price"`
		BalanceRemaining float64 `json:"balance_remaining"`
		Status           string  `json:"status"`
		Message          string  `json:"message"`
	} `json:"data"`
}

func (c *ICSClient) CreateTrx(produk, tujuan, reffID string) (*CreateTrxResp, error) {
	req := map[string]string{
		"product_code":  produk,
		"dest_number":   tujuan,
		"ref_id_custom": reffID,
	}
	var resp CreateTrxResp
	if err := c.doJSON(http.MethodPost, "/trx", req, &resp); err != nil {
		return nil, err
	}
	if resp.Message == "" {
		resp.Message = resp.Data.Message
	}
	return &resp, nil
}

type TransactionStatusResp struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		RefID      string  `json:"refid"`
		Product    string  `json:"product"`
		Dest       string  `json:"dest"`
		Status     string  `json:"status"`
		SN         *string `json:"sn"`
		Message    string  `json:"message"`
		Price      float64 `json:"price"`
		LastUpdate string  `json:"last_update"`
	} `json:"data"`
}

func (c *ICSClient) GetTransactionStatus(providerRefID string) (*TransactionStatusResp, error) {
	var resp TransactionStatusResp
	if err := c.doJSON(http.MethodGet, "/trx/"+providerRefID, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Message == "" {
		resp.Message = resp.Data.Message
	}
	return &resp, nil
}

type ProfileResp struct {
	Success bool `json:"success"`
	Data    struct {
		KodeReseller string  `json:"kodereseller"`
		Username     string  `json:"username"`
		Saldo        float64 `json:"saldo"`
		Status       string  `json:"status"`
		WebhookURL   string  `json:"webhook_url"`
	} `json:"data"`
}

func (c *ICSClient) GetProfile() (*ProfileResp, error) {
	var resp ProfileResp
	if err := c.doJSON(http.MethodGet, "/profile", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
