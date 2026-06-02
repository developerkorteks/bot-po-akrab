package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

type Buyer struct {
	cfg     *Config
	client  *ICSClient
	db      *sql.DB
	tg      *Telegram
	monitor *StockMonitor
}

func NewBuyer(cfg *Config, client *ICSClient, db *sql.DB, tg *Telegram, mon *StockMonitor) *Buyer {
	return &Buyer{cfg: cfg, client: client, db: db, tg: tg, monitor: mon}
}

func (b *Buyer) Run() {
	go func() {
		for ev := range b.monitor.Events() {
			b.handleRestock(ev)
		}
	}()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		b.processRetryQueue()
		b.processPendingAvailable()
	}
}

func (b *Buyer) handleRestock(ev StockEvent) {
	rows, err := b.db.Query(`
		SELECT id, produk, produk_nama, tujuan, reff_id, attempts
		FROM preorders
		WHERE produk=? AND status='pending'
		ORDER BY created_at ASC
	`, ev.Produk)
	if err != nil {
		log.Printf("[BUYER] query preorders error: %v", err)
		return
	}
	defer rows.Close()

	type po struct {
		id, produk, produkNama, tujuan, reffID string
		attempts                               int
	}
	var orders []po
	for rows.Next() {
		var o po
		rows.Scan(&o.id, &o.produk, &o.produkNama, &o.tujuan, &o.reffID, &o.attempts)
		orders = append(orders, o)
	}

	if len(orders) == 0 {
		return
	}
	logActivity(b.db, "info", ev.Produk, "", fmt.Sprintf("%d pre-order diproses saat restock", len(orders)))
	for _, o := range orders {
		o := o
		go b.fireTrx(o.id, o.produk, o.produkNama, o.tujuan, o.reffID, o.attempts+1)
		time.Sleep(300 * time.Millisecond)
	}
}

func (b *Buyer) processRetryQueue() {
	rows, err := b.db.Query(`
		SELECT id, produk, produk_nama, tujuan, reff_id, attempts
		FROM preorders
		WHERE status='retry' AND (next_retry_at IS NULL OR next_retry_at <= ?)
		ORDER BY next_retry_at ASC
		LIMIT 10
	`, time.Now().UTC())
	if err != nil {
		return
	}

	type po struct {
		id, produk, produkNama, tujuan, reffID string
		attempts                               int
	}
	var orders []po
	for rows.Next() {
		var o po
		rows.Scan(&o.id, &o.produk, &o.produkNama, &o.tujuan, &o.reffID, &o.attempts)
		orders = append(orders, o)
	}
	rows.Close()

	for _, o := range orders {
		o := o
		var avail int
		b.db.QueryRow(`SELECT available FROM product_cache WHERE produk=?`, o.produk).Scan(&avail)
		if avail == 0 {
			b.db.Exec(`UPDATE preorders SET status='pending', updated_at=? WHERE id=?`, time.Now().UTC(), o.id)
			continue
		}
		go b.fireTrx(o.id, o.produk, o.produkNama, o.tujuan, o.reffID, o.attempts+1)
	}
}

func (b *Buyer) processPendingAvailable() {
	rows, err := b.db.Query(`
		SELECT p.id, p.produk, p.produk_nama, p.tujuan, p.reff_id, p.attempts
		FROM preorders p
		JOIN product_cache c ON c.produk = p.produk
		WHERE p.status='pending' AND c.available=1 AND c.suspect_ghost=0
		ORDER BY p.created_at ASC
		LIMIT 10
	`)
	if err != nil {
		return
	}
	type po struct {
		id, produk, produkNama, tujuan, reffID string
		attempts                               int
	}
	var orders []po
	for rows.Next() {
		var o po
		rows.Scan(&o.id, &o.produk, &o.produkNama, &o.tujuan, &o.reffID, &o.attempts)
		orders = append(orders, o)
	}
	rows.Close()

	for _, o := range orders {
		o := o
		go b.fireTrx(o.id, o.produk, o.produkNama, o.tujuan, o.reffID, o.attempts+1)
		time.Sleep(300 * time.Millisecond)
	}
}

func (b *Buyer) fireTrx(preorderID, produk, produkNama, tujuan, reffID string, attemptNum int) {
	b.db.Exec(`UPDATE preorders SET status='buying', attempts=?, updated_at=? WHERE id=?`,
		attemptNum, time.Now().UTC(), preorderID)

	txAttemptID := newUUID()
	b.db.Exec(`INSERT INTO tx_attempts(id,preorder_id,reff_id,attempt_num,status,created_at) VALUES(?,?,?,?,?,?)`,
		txAttemptID, preorderID, reffID, attemptNum, "pending", time.Now().UTC())

	resp, err := b.client.CreateTrx(produk, tujuan, reffID)
	if err != nil {
		logActivity(b.db, "error", produk, preorderID, "network error: "+err.Error())
		b.scheduleRetry(preorderID, attemptNum)
		return
	}

	if resp.Data.RefID != "" {
		b.db.Exec(`UPDATE preorders SET provider_ref_id=?, last_raw_status=?, last_raw_message=?, updated_at=? WHERE id=?`,
			resp.Data.RefID, resp.Data.Status, firstNonEmpty(resp.Data.Message, resp.Message), time.Now().UTC(), preorderID)
		b.db.Exec(`UPDATE tx_attempts SET provider_ref_id=?, raw_status=? WHERE id=?`,
			resp.Data.RefID, resp.Data.Status, txAttemptID)
	}

	go b.trackTransaction(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, resp)
}

func (b *Buyer) trackTransaction(preorderID, produk, produkNama, tujuan, reffID, txAttemptID string, trxResp *CreateTrxResp) {
	if !trxResp.Success {
		b.onFailed(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, firstNonEmpty(trxResp.Message, trxResp.Data.Message), trxResp.Data.Status)
		return
	}

	providerRefID := trxResp.Data.RefID
	timeout := time.NewTimer(3 * time.Minute)
	defer timeout.Stop()
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout.C:
			b.onFailed(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, "timeout tracking 3 menit", "timeout")
			return
		case <-ticker.C:
			if providerRefID == "" {
				b.db.QueryRow(`SELECT provider_ref_id FROM preorders WHERE id=?`, preorderID).Scan(&providerRefID)
			}
			if providerRefID == "" {
				continue
			}
			stat, err := b.client.GetTransactionStatus(providerRefID)
			if err != nil {
				continue
			}
			status := strings.ToLower(stat.Data.Status)
			ket := firstNonEmpty(valueOrEmpty(stat.Data.SN), stat.Data.Message, stat.Message)
			b.db.Exec(`UPDATE preorders SET last_raw_status=?, last_raw_message=?, updated_at=? WHERE id=?`,
				stat.Data.Status, ket, time.Now().UTC(), preorderID)
			if status == "success" {
				b.onSuccess(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, ket, providerRefID)
				return
			}
			if status == "failed" {
				b.onFailed(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, ket, stat.Data.Status)
				return
			}
		}
	}
}

func (b *Buyer) onSuccess(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, ket, providerRefID string) {
	now := time.Now().UTC()
	b.db.Exec(`UPDATE preorders SET status='success', note=?, updated_at=? WHERE id=?`, ket, now, preorderID)
	b.db.Exec(`UPDATE tx_attempts SET status='success', keterangan=?, provider_ref_id=?, resolved_at=? WHERE id=?`,
		ket, providerRefID, now, txAttemptID)
	logActivity(b.db, "success", produk, preorderID, fmt.Sprintf("Transaksi sukses: %s → %s", produkNama, tujuan))
	if b.tg != nil {
		b.tg.Send(fmt.Sprintf("✅ *Pre-order ICS Sukses!*\n\n📦 Produk: `%s` (%s)\n📱 Tujuan: `%s`\n🔑 ReffID: `%s`\n🧾 ProviderRef: `%s`\n📝 %s",
			produk, produkNama, tujuan, reffID, providerRefID, ket))
	}
}

func (b *Buyer) onFailed(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, ket, rawStatus string) {
	var attempts, maxAttempts int
	b.db.QueryRow(`SELECT attempts, max_attempts FROM preorders WHERE id=?`, preorderID).Scan(&attempts, &maxAttempts)
	now := time.Now().UTC()
	b.db.Exec(`UPDATE tx_attempts SET status='failed', keterangan=?, raw_status=?, resolved_at=? WHERE id=?`,
		ket, rawStatus, now, txAttemptID)

	failType := classifyFailure(ket)
	logActivity(b.db, "warn", produk, preorderID, fmt.Sprintf("Gagal attempt %d/%d (%s): %s", attempts, maxAttempts, failType, ket))

	if attempts >= maxAttempts {
		b.db.Exec(`UPDATE preorders SET status='failed', note=?, last_raw_status=?, last_raw_message=?, updated_at=? WHERE id=?`,
			fmt.Sprintf("Maks retry tercapai. Terakhir: %s", ket), rawStatus, ket, now, preorderID)
		if b.tg != nil {
			b.tg.Send(fmt.Sprintf("❌ *Pre-order ICS Gagal Permanen*\n\n📦 Produk: `%s` (%s)\n📱 Tujuan: `%s`\n🔄 Sudah dicoba %d kali\n📝 %s",
				produk, produkNama, tujuan, attempts, ket))
		}
		return
	}

	switch failType {
	case "saldo":
		b.db.Exec(`UPDATE preorders SET status='failed', note=?, last_raw_status=?, last_raw_message=?, updated_at=? WHERE id=?`,
			"Saldo tidak mencukupi. Top up dulu.", rawStatus, ket, now, preorderID)
		if b.tg != nil {
			b.tg.Send("⚠️ *SALDO ICS TIDAK CUKUP!*\n\nBot tidak dapat melanjutkan pre-order ICS.")
		}
	case "ghost":
		b.db.Exec(`UPDATE product_cache SET ghost_count=ghost_count+1 WHERE produk=?`, produk)
		var ghostFails int
		b.db.QueryRow(`UPDATE preorders SET ghost_fail_count=ghost_fail_count+1, last_raw_status=?, last_raw_message=?, updated_at=? WHERE id=? RETURNING ghost_fail_count`,
			rawStatus, ket, now, preorderID).Scan(&ghostFails)
		if ghostFails >= 3 {
			b.db.Exec(`UPDATE product_cache SET suspect_ghost=1, available=0, updated_at=? WHERE produk=?`, now, produk)
			b.db.Exec(`UPDATE preorders SET status='failed', note=?, updated_at=? WHERE id=?`,
				fmt.Sprintf("SUSPECT_GHOST: gagal %dx berturut-turut", ghostFails), now, preorderID)
		} else {
			next := now.Add(time.Duration(ghostFails) * 15 * time.Second)
			b.db.Exec(`UPDATE preorders SET status='retry', next_retry_at=?, updated_at=? WHERE id=?`, next, now, preorderID)
		}
	case "lock":
		b.db.Exec(`UPDATE product_cache SET lock_count=lock_count+1 WHERE produk=?`, produk)
		var lockFails int
		b.db.QueryRow(`UPDATE preorders SET lock_fail_count=lock_fail_count+1, last_raw_status=?, last_raw_message=?, updated_at=? WHERE id=? RETURNING lock_fail_count`,
			rawStatus, ket, now, preorderID).Scan(&lockFails)
		lockDelays := []time.Duration{10, 30, 60, 120, 300}
		idx := lockFails - 1
		if idx >= len(lockDelays) {
			idx = len(lockDelays) - 1
		}
		if lockFails >= 8 {
			b.db.Exec(`UPDATE product_cache SET suspect_ghost=1, updated_at=? WHERE produk=?`, now, produk)
			b.db.Exec(`UPDATE preorders SET status='failed', note=?, updated_at=? WHERE id=?`,
				fmt.Sprintf("LOCK_ESCALATED: locked %dx berturut-turut", lockFails), now, preorderID)
		} else {
			next := now.Add(lockDelays[idx] * time.Second)
			b.db.Exec(`UPDATE preorders SET status='retry', next_retry_at=?, updated_at=? WHERE id=?`, next, now, preorderID)
		}
	default:
		delay := retryDelay(attempts)
		next := now.Add(delay)
		b.db.Exec(`UPDATE preorders SET status='retry', next_retry_at=?, last_raw_status=?, last_raw_message=?, updated_at=? WHERE id=?`,
			next, rawStatus, ket, now, preorderID)
	}
}

func (b *Buyer) scheduleRetry(preorderID string, attempts int) {
	delay := retryDelay(attempts)
	next := time.Now().UTC().Add(delay)
	b.db.Exec(`UPDATE preorders SET status='retry', next_retry_at=?, updated_at=? WHERE id=?`,
		next, time.Now().UTC(), preorderID)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
