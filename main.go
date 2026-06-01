package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"log"
	"os"
)

func main() {
	cfgPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Fatalf("Config file tidak ditemukan: %s\nBuat dari config.example.json", *cfgPath)
		}
		log.Fatalf("Load config: %v", err)
	}

	db, err := initDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Init DB: %v", err)
	}
	defer db.Close()
	log.Printf("[MAIN] DB: %s", cfg.DBPath)

	client, err := NewKhfyClient(cfg.KhfyAPIKey, cfg.ProxyAddr)
	if err != nil {
		log.Fatalf("Init khfy client: %v", err)
	}

	tg := NewTelegram(cfg.TelegramToken, cfg.TelegramChatID)
	if tg != nil {
		log.Printf("[MAIN] Telegram notifier aktif (chat_id=%s)", cfg.TelegramChatID)
	}

	monitor := NewStockMonitor(cfg, client, db)
	buyer := NewBuyer(cfg, client, db, tg, monitor)
	apiSrv := NewAPIServer(cfg, db, client)

	// Start goroutines
	go monitor.Run()
	go buyer.Run()

	log.Printf("[MAIN] Pre-order bot berjalan")
	if err := apiSrv.Serve(); err != nil {
		log.Fatalf("API server: %v", err)
	}
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:4]) + "-" +
		hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" +
		hex.EncodeToString(b[8:10]) + "-" +
		hex.EncodeToString(b[10:])
}
