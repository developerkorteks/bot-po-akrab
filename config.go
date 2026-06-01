package main

import (
	"encoding/json"
	"os"
)

type Config struct {
	KhfyAPIKey      string `json:"khfy_api_key"`
	TelegramToken   string `json:"telegram_token"`
	TelegramChatID  string `json:"telegram_chat_id"`
	ProxyAddr       string `json:"proxy_addr"`        // e.g. http://admin:pass@127.0.0.1:62080
	PollIntervalSec int    `json:"poll_interval_sec"` // default 5
	ListenAddr      string `json:"listen_addr"`       // default :8090
	DBPath          string `json:"db_path"`           // default preorder.db
	MaxRetries      int    `json:"max_retries"`       // default 6
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg := &Config{}
	if err := json.NewDecoder(f).Decode(cfg); err != nil {
		return nil, err
	}
	if cfg.PollIntervalSec <= 0 {
		cfg.PollIntervalSec = 5
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8090"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "preorder.db"
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 6
	}
	if cfg.ProxyAddr == "" {
		cfg.ProxyAddr = "http://admin:password_kamu@127.0.0.1:62080"
	}
	return cfg, nil
}
