package main

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

func initDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
	PRAGMA journal_mode=WAL;
	PRAGMA foreign_keys=ON;

	CREATE TABLE IF NOT EXISTS product_cache (
		produk        TEXT PRIMARY KEY,
		nama          TEXT,
		harga         REAL,
		available     INTEGER DEFAULT 0,
		ghost_count   INTEGER DEFAULT 0,
		lock_count    INTEGER DEFAULT 0,
		suspect_ghost INTEGER DEFAULT 0,
		updated_at    DATETIME
	);

	CREATE TABLE IF NOT EXISTS preorders (
		id               TEXT PRIMARY KEY,
		produk           TEXT NOT NULL,
		produk_nama      TEXT,
		tujuan           TEXT NOT NULL,
		reff_id          TEXT UNIQUE NOT NULL,
		provider_ref_id  TEXT UNIQUE,
		status           TEXT DEFAULT 'pending',
		attempts         INTEGER DEFAULT 0,
		max_attempts     INTEGER DEFAULT 6,
		ghost_fail_count INTEGER DEFAULT 0,
		lock_fail_count  INTEGER DEFAULT 0,
		next_retry_at    DATETIME,
		note             TEXT,
		last_raw_status  TEXT,
		last_raw_message TEXT,
		created_at       DATETIME,
		updated_at       DATETIME,
		expires_at       DATETIME
	);

	CREATE TABLE IF NOT EXISTS tx_attempts (
		id               TEXT PRIMARY KEY,
		preorder_id      TEXT NOT NULL,
		reff_id          TEXT NOT NULL,
		provider_ref_id  TEXT,
		attempt_num      INTEGER,
		status           TEXT DEFAULT 'pending',
		keterangan       TEXT,
		raw_status       TEXT,
		created_at       DATETIME,
		resolved_at      DATETIME,
		FOREIGN KEY(preorder_id) REFERENCES preorders(id)
	);

	CREATE TABLE IF NOT EXISTS activity_logs (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		level       TEXT DEFAULT 'info',
		produk      TEXT,
		preorder_id TEXT,
		message     TEXT,
		created_at  DATETIME
	);
	`)
	if err != nil {
		return err
	}
	for _, alter := range []string{
		`ALTER TABLE product_cache ADD COLUMN suspect_ghost INTEGER DEFAULT 0`,
		`ALTER TABLE preorders ADD COLUMN ghost_fail_count INTEGER DEFAULT 0`,
		`ALTER TABLE preorders ADD COLUMN lock_fail_count INTEGER DEFAULT 0`,
		`ALTER TABLE preorders ADD COLUMN provider_ref_id TEXT`,
		`ALTER TABLE preorders ADD COLUMN last_raw_status TEXT`,
		`ALTER TABLE preorders ADD COLUMN last_raw_message TEXT`,
		`ALTER TABLE tx_attempts ADD COLUMN provider_ref_id TEXT`,
		`ALTER TABLE tx_attempts ADD COLUMN raw_status TEXT`,
	} {
		db.Exec(alter)
	}
	return nil
}

func logActivity(db *sql.DB, level, produk, preorderID, msg string) {
	db.Exec(`INSERT INTO activity_logs(level,produk,preorder_id,message,created_at) VALUES(?,?,?,?,?)`,
		level, produk, preorderID, msg, time.Now().UTC())
}
