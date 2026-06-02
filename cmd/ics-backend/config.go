package main

import (
	"encoding/json"
	"os"
)

type Config struct {
	ICSSecretKey    string `json:"ics_secret_key"`
	TelegramToken   string `json:"telegram_token"`
	TelegramChatID  string `json:"telegram_chat_id"`
	PollIntervalSec int    `json:"poll_interval_sec"`
	ListenAddr      string `json:"listen_addr"`
	DBPath          string `json:"db_path"`
	MaxRetries      int    `json:"max_retries"`
	BaseURL         string `json:"base_url"`
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
		cfg.ListenAddr = ":8091"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "ics-preorder.db"
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 6
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.ics-store.my.id/api/reseller"
	}
	return cfg, nil
}
