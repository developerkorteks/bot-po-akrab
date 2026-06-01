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
	client  *KhfyClient
	db      *sql.DB
	tg      *Telegram
	monitor *StockMonitor
}

func NewBuyer(cfg *Config, client *KhfyClient, db *sql.DB, tg *Telegram, mon *StockMonitor) *Buyer {
	return &Buyer{cfg: cfg, client: client, db: db, tg: tg, monitor: mon}
}

func (b *Buyer) Run() {
	// Process restock events
	go func() {
		for ev := range b.monitor.Events() {
			b.handleRestock(ev)
		}
	}()

	// Loop: setiap 10s proses retry queue DAN pending yang produknya sudah tersedia
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

	var orders []struct {
		id, produk, produkNama, tujuan, reffID string
		attempts                               int
	}
	for rows.Next() {
		var o struct {
			id, produk, produkNama, tujuan, reffID string
			attempts                               int
		}
		rows.Scan(&o.id, &o.produk, &o.produkNama, &o.tujuan, &o.reffID, &o.attempts)
		orders = append(orders, o)
	}

	if len(orders) == 0 {
		return
	}

	log.Printf("[BUYER] %d pre-order akan diproses untuk %s", len(orders), ev.Produk)
	logActivity(b.db, "info", ev.Produk, "", fmt.Sprintf("%d pre-order diproses saat restock", len(orders)))

	for _, o := range orders {
		o := o // capture
		go b.fireTrx(o.id, o.produk, o.produkNama, o.tujuan, o.reffID, o.attempts+1)
		time.Sleep(300 * time.Millisecond) // jeda kecil antar transaksi
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
	rows.Close() // tutup sebelum fireTrx!

	for _, o := range orders {
		o := o // capture
		var avail int
		b.db.QueryRow(`SELECT available FROM product_cache WHERE produk=?`, o.produk).Scan(&avail)
		if avail == 0 {
			b.db.Exec(`UPDATE preorders SET status='pending', updated_at=? WHERE id=?`,
				time.Now().UTC(), o.id)
			continue
		}
		go b.fireTrx(o.id, o.produk, o.produkNama, o.tujuan, o.reffID, o.attempts+1)
	}
}

// processPendingAvailable memproses pre-order berstatus 'pending' yang produknya
// sudah tersedia (available=1 di product_cache). Dipanggil tiap 10s agar
// pre-order yang dibuat saat stok sudah ada langsung dieksekusi.
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

	// Collect dulu ke slice — JANGAN biarkan rows cursor open saat fireTrx (HTTP call)
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
	rows.Close() // tutup sebelum fireTrx!

	for _, o := range orders {
		o := o // capture
		log.Printf("[BUYER] pending+available: fire preorder=%s produk=%s", o.id, o.produk)
		go b.fireTrx(o.id, o.produk, o.produkNama, o.tujuan, o.reffID, o.attempts+1)
		time.Sleep(300 * time.Millisecond)
	}
}

func (b *Buyer) fireTrx(preorderID, produk, produkNama, tujuan, reffID string, attemptNum int) {
	log.Printf("[BUYER] fire /trx preorder=%s produk=%s tujuan=%s attempt=%d",
		preorderID, produk, tujuan, attemptNum)

	b.db.Exec(`UPDATE preorders SET status='buying', attempts=?, updated_at=? WHERE id=?`,
		attemptNum, time.Now().UTC(), preorderID)

	txAttemptID := newUUID()
	b.db.Exec(`INSERT INTO tx_attempts(id,preorder_id,reff_id,attempt_num,status,created_at) VALUES(?,?,?,?,?,?)`,
		txAttemptID, preorderID, reffID, attemptNum, "pending", time.Now().UTC())

	resp, err := b.client.CreateTrx(produk, tujuan, reffID)
	if err != nil {
		log.Printf("[BUYER] /trx network error: %v", err)
		logActivity(b.db, "error", produk, preorderID, "network error: "+err.Error())
		b.scheduleRetry(preorderID, attemptNum)
		return
	}

	// Langsung poll history untuk konfirmasi
	go b.trackTransaction(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, resp)
}

func (b *Buyer) trackTransaction(preorderID, produk, produkNama, tujuan, reffID, txAttemptID string, trxResp *TrxResp) {
	// Jika trx langsung gagal (ok=false), handle sekarang
	if !trxResp.Ok {
		log.Printf("[BUYER] trx rejected: %s", trxResp.Msg)
		b.onFailed(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, trxResp.Msg)
		return
	}

	// Deadline 3 menit — cukup untuk transaksi normal
	timeout := time.NewTimer(3 * time.Minute)
	defer timeout.Stop()
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()

	log.Printf("[TRACKER] mulai tracking preorder=%s reffID=%s", preorderID, reffID)

	for {
		select {
		case <-timeout.C:
			log.Printf("[TRACKER] TIMEOUT preorder=%s", preorderID)
			b.onFailed(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, "timeout tracking 3 menit")
			return

		case <-ticker.C:
			hist, err := b.client.GetHistory(reffID)
			if err != nil {
				log.Printf("[TRACKER] history error: %v", err)
				continue
			}

			if hist.Count == 0 || len(hist.Data) == 0 {
				log.Printf("[TRACKER] preorder=%s belum ada di history, tunggu...", preorderID)
				continue
			}

			item := hist.Data[0]
			status := strings.ToLower(item.StatusText)
			ket := item.GetKeterangan()
			log.Printf("[TRACKER] preorder=%s status=%s ket=%s", preorderID, item.StatusText, ket)

			if strings.Contains(status, "sukses") || strings.Contains(status, "success") {
				b.onSuccess(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, ket)
				return
			}

			if strings.Contains(status, "gagal") || strings.Contains(status, "batal") ||
				strings.Contains(status, "failed") || strings.Contains(status, "cancel") {
				b.onFailed(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, ket)
				return
			}
			// PENDING / lainnya: lanjut polling
		}
	}
}

func (b *Buyer) onSuccess(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, ket string) {
	log.Printf("[BUYER] SUCCESS preorder=%s produk=%s tujuan=%s", preorderID, produk, tujuan)
	now := time.Now().UTC()
	b.db.Exec(`UPDATE preorders SET status='success', updated_at=? WHERE id=?`, now, preorderID)
	b.db.Exec(`UPDATE tx_attempts SET status='success', keterangan=?, resolved_at=? WHERE id=?`, ket, now, txAttemptID)
	logActivity(b.db, "success", produk, preorderID, fmt.Sprintf("Transaksi sukses: %s → %s", produkNama, tujuan))

	if b.tg != nil {
		b.tg.Send(fmt.Sprintf("✅ *Pre-order Sukses!*\n\n📦 Produk: `%s` (%s)\n📱 Tujuan: `%s`\n🔑 ReffID: `%s`\n📝 %s",
			produk, produkNama, tujuan, reffID, ket))
	}
}

func (b *Buyer) onFailed(preorderID, produk, produkNama, tujuan, reffID, txAttemptID, ket string) {
	var attempts, maxAttempts int
	b.db.QueryRow(`SELECT attempts, max_attempts FROM preorders WHERE id=?`, preorderID).Scan(&attempts, &maxAttempts)

	now := time.Now().UTC()
	b.db.Exec(`UPDATE tx_attempts SET status='failed', keterangan=?, resolved_at=? WHERE id=?`, ket, now, txAttemptID)

	failType := classifyFailure(ket)
	log.Printf("[BUYER] FAILED preorder=%s type=%s ket=%s attempts=%d/%d", preorderID, failType, ket, attempts, maxAttempts)
	logActivity(b.db, "warn", produk, preorderID, fmt.Sprintf("Gagal attempt %d/%d (%s): %s", attempts, maxAttempts, failType, ket))

	if attempts >= maxAttempts {
		b.db.Exec(`UPDATE preorders SET status='failed', note=?, updated_at=? WHERE id=?`,
			fmt.Sprintf("Maks retry tercapai. Terakhir: %s", ket), now, preorderID)
		logActivity(b.db, "error", produk, preorderID, "Pre-order gagal permanen setelah "+fmt.Sprint(attempts)+" percobaan")
		if b.tg != nil {
			b.tg.Send(fmt.Sprintf("❌ *Pre-order Gagal Permanen*\n\n📦 Produk: `%s` (%s)\n📱 Tujuan: `%s`\n🔄 Sudah dicoba %d kali\n📝 %s\n\nSilakan order manual.",
				produk, produkNama, tujuan, attempts, ket))
		}
		return
	}

	switch failType {
	case "saldo":
		// Saldo tidak cukup — gagal permanen, notif admin
		b.db.Exec(`UPDATE preorders SET status='failed', note=?, updated_at=? WHERE id=?`,
			"Saldo tidak mencukupi. Top up dulu.", now, preorderID)
		logActivity(b.db, "error", produk, preorderID, "⚠️ SALDO TIDAK CUKUP — semua pre-order dihentikan")
		if b.tg != nil {
			b.tg.Send("⚠️ *SALDO TIDAK CUKUP!*\n\nBot tidak dapat melanjutkan pre-order.\nSilakan top up saldo KhfyPay segera.")
		}

	case "ghost":
		// Tambah ghost_fail_count per preorder
		b.db.Exec(`UPDATE product_cache SET ghost_count=ghost_count+1 WHERE produk=?`, produk)
		var ghostFails int
		b.db.QueryRow(`UPDATE preorders SET ghost_fail_count=ghost_fail_count+1, updated_at=? WHERE id=? RETURNING ghost_fail_count`,
			now, preorderID).Scan(&ghostFails)

		const maxGhostFails = 3
		if ghostFails >= maxGhostFails {
			// Tandai produk sebagai SUSPECT_GHOST
			b.db.Exec(`UPDATE product_cache SET suspect_ghost=1, available=0, updated_at=? WHERE produk=?`, now, produk)
			b.db.Exec(`UPDATE preorders SET status='failed', note=?, updated_at=? WHERE id=?`,
				fmt.Sprintf("SUSPECT_GHOST: gagal %dx berturut-turut (ghost stock), stok tidak bisa dibeli", ghostFails), now, preorderID)
			logActivity(b.db, "error", produk, preorderID,
				fmt.Sprintf("🚫 SUSPECT_GHOST setelah %d gagal — produk ditandai, pre-order dibatalkan", ghostFails))
			if b.tg != nil {
				b.tg.Send(fmt.Sprintf("🚫 *SUSPECT GHOST STOCK*\n\n📦 `%s` (%s)\n📱 Tujuan: `%s`\n\nGagal dibeli %dx berturut-turut meski stok 'tersedia'.\nProduk ditandai ghost. Pre-order dibatalkan.",
					produk, produkNama, tujuan, ghostFails))
			}
		} else {
			// Kembali ke pending dengan delay kecil (15s per ghost fail)
			delay := time.Duration(ghostFails) * 15 * time.Second
			next := now.Add(delay)
			b.db.Exec(`UPDATE preorders SET status='retry', next_retry_at=?, updated_at=? WHERE id=?`, next, now, preorderID)
			logActivity(b.db, "warn", produk, preorderID,
				fmt.Sprintf("Ghost stock #%d/%d, retry dalam %.0fs", ghostFails, maxGhostFails, delay.Seconds()))
		}

	case "lock":
		// Escalating delay berdasarkan lock_fail_count per preorder
		b.db.Exec(`UPDATE product_cache SET lock_count=lock_count+1 WHERE produk=?`, produk)
		var lockFails int
		b.db.QueryRow(`UPDATE preorders SET lock_fail_count=lock_fail_count+1, updated_at=? WHERE id=? RETURNING lock_fail_count`,
			now, preorderID).Scan(&lockFails)

		// Escalate: 10s, 30s, 60s, 120s, 300s (5 menit max)
		lockDelays := []time.Duration{10, 30, 60, 120, 300}
		idx := lockFails - 1
		if idx >= len(lockDelays) {
			idx = len(lockDelays) - 1
		}
		lockDelay := time.Duration(lockDelays[idx]) * time.Second

		// Setelah 8x lock berturut-turut → treat as ghost (produk mungkin memang tidak bisa dibeli)
		if lockFails >= 8 {
			b.db.Exec(`UPDATE product_cache SET suspect_ghost=1, updated_at=? WHERE produk=?`, now, produk)
			b.db.Exec(`UPDATE preorders SET status='failed', note=?, updated_at=? WHERE id=?`,
				fmt.Sprintf("LOCK_ESCALATED: locked %dx berturut-turut, kemungkinan ghost/maintenance panjang", lockFails), now, preorderID)
			logActivity(b.db, "error", produk, preorderID,
				fmt.Sprintf("🔒 Lock %dx — eskalasi ke SUSPECT_GHOST, pre-order gagal permanen", lockFails))
			if b.tg != nil {
				b.tg.Send(fmt.Sprintf("🔒 *LOCK BERKEPANJANGAN*\n\n📦 `%s`\n📱 `%s`\nLocked %dx berturut-turut.\nPre-order dibatalkan, cek kondisi provider.",
					produk, tujuan, lockFails))
			}
		} else {
			next := now.Add(lockDelay)
			b.db.Exec(`UPDATE preorders SET status='retry', next_retry_at=?, updated_at=? WHERE id=?`, next, now, preorderID)
			logActivity(b.db, "warn", produk, preorderID,
				fmt.Sprintf("🔒 Lock #%d, retry dalam %.0fs", lockFails, lockDelay.Seconds()))
		}

	default:
		// HTTP_CLIENT_RESPONSE_BODY_ERR dan error teknis → backoff normal
		delay := retryDelay(attempts)
		next := now.Add(delay)
		b.db.Exec(`UPDATE preorders SET status='retry', next_retry_at=?, updated_at=? WHERE id=?`, next, now, preorderID)
		logActivity(b.db, "warn", produk, preorderID, fmt.Sprintf("Retry dalam %.0fs (err: %s)", delay.Seconds(), ket))
	}
}

func (b *Buyer) scheduleRetry(preorderID string, attempts int) {
	delay := retryDelay(attempts)
	next := time.Now().UTC().Add(delay)
	b.db.Exec(`UPDATE preorders SET status='retry', next_retry_at=?, updated_at=? WHERE id=?`,
		next, time.Now().UTC(), preorderID)
}

// extractExistingReffID parses RC=<uuid> dari pesan "Tunggu Transaksi selesai RC=xxx"
func extractExistingReffID(msg string) string {
	idx := strings.Index(msg, "RC=")
	if idx < 0 {
		return ""
	}
	rest := msg[idx+3:]
	// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx = 36 chars
	if len(rest) >= 36 {
		return rest[:36]
	}
	return ""
}
