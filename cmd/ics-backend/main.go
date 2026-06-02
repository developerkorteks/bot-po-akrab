package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"log"
	"os"
)

func main() {
	cfgPath := flag.String("config", "config.ics.json", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Fatalf("Config file tidak ditemukan: %s", *cfgPath)
		}
		log.Fatalf("Load config: %v", err)
	}

	db, err := initDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Init DB: %v", err)
	}
	defer db.Close()

	client := NewICSClient(cfg.ICSSecretKey, cfg.BaseURL)
	tg := NewTelegram(cfg.TelegramToken, cfg.TelegramChatID)

	monitor := NewStockMonitor(cfg, client, db)
	buyer := NewBuyer(cfg, client, db, tg, monitor)
	apiSrv := NewAPIServer(cfg, db, client)

	go monitor.Run()
	go buyer.Run()

	log.Printf("[MAIN] ICS backend berjalan on %s", cfg.ListenAddr)
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
