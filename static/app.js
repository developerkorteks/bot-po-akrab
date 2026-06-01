// State
let allProducts = [];
let allPreorders = [];
let productFilter = 'all';
let poFilter = 'all';
let modalProduct = null;

// ─── Init ────────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
  setupTabs();
  setupSSE();
  loadStats();
  loadSaldo();
  loadLogs();
  setInterval(loadStats, 5000);
  setInterval(loadSaldo, 30000);
  setInterval(() => {
    if (document.querySelector('[data-tab="products"]').classList.contains('active') ||
        document.getElementById('tab-products').classList.contains('active')) {
      loadProducts();
    }
    if (document.getElementById('tab-preorders').classList.contains('active')) {
      loadPreorders();
    }
    if (document.getElementById('tab-logs').classList.contains('active')) {
      loadLogs();
    }
    if (document.getElementById('tab-dashboard').classList.contains('active')) {
      loadLogs();
    }
  }, 8000);
});

// ─── Tabs ─────────────────────────────────────────────────────────────────────
function setupTabs() {
  document.querySelectorAll('.nav-item').forEach(item => {
    item.addEventListener('click', e => {
      e.preventDefault();
      const tab = item.dataset.tab;
      document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
      document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
      item.classList.add('active');
      document.getElementById('tab-' + tab).classList.add('active');
      document.getElementById('page-title').textContent = item.textContent.trim().replace(/^[^\w]+/, '');
      if (tab === 'products') loadProducts();
      if (tab === 'preorders') { loadPreorders(); if (!allProducts.length) loadProducts(); else populateProdukDropdown(); }
      if (tab === 'logs') loadLogs();
    });
  });
}

// ─── SSE ─────────────────────────────────────────────────────────────────────
function setupSSE() {
  const es = new EventSource('/api/events');
  const dot = document.getElementById('status-dot');
  const txt = document.getElementById('status-text');
  es.onopen = () => {
    dot.classList.remove('err');
    txt.textContent = 'Live';
  };
  es.onmessage = e => {
    const d = JSON.parse(e.data);
    updateStatCards(d);
  };
  es.onerror = () => {
    dot.classList.add('err');
    txt.textContent = 'Offline';
    setTimeout(setupSSE, 5000);
    es.close();
  };
}

// ─── Stats ───────────────────────────────────────────────────────────────────
async function loadStats() {
  const d = await api('/api/stats');
  if (d) updateStatCards(d);
}
function updateStatCards(d) {
  setEl('stat-pending',   d.pending   || 0);
  setEl('stat-buying',    d.buying    || 0);
  setEl('stat-success',   d.success   || 0);
  setEl('stat-failed',    d.failed    || 0);
  setEl('stat-retry',     d.retry     || 0);
  setEl('stat-available', d.products_available || 0);
}

async function loadSaldo() {
  const d = await api('/api/saldo');
  if (!d || !d.ok) return;
  const el = document.getElementById('saldo-info');
  if (el) el.textContent = `💰 Saldo: Rp${Number(d.saldo).toLocaleString('id-ID')} · Sukses: ${d.trx_sukses_hari_ini} · Pending: ${d.trx_pending_hari_ini} · Gagal: ${d.trx_gagal_hari_ini}`;
}

// ─── Products ─────────────────────────────────────────────────────────────────
async function loadProducts() {
  allProducts = await api('/api/products') || [];
  renderProducts();
  populateProdukDropdown();
}

function populateProdukDropdown() {
  const sel = document.getElementById('po-produk');
  if (!sel) return;
  const cur = sel.value;
  sel.innerHTML = '<option value="" disabled selected>\u2014 Pilih produk \u2014</option>';
  // Semua produk, tersedia di atas
  const sorted = [...allProducts].sort((a, b) => (b.available - a.available) || a.nama.localeCompare(b.nama));
  sorted.forEach(p => {
    const opt = document.createElement('option');
    opt.value = p.produk;
    opt.dataset.nama = p.nama;
    opt.dataset.harga = p.harga;
    opt.dataset.available = p.available;
    const label = p.available
      ? `✅ ${p.produk} — ${p.nama} (Rp${Number(p.harga).toLocaleString('id-ID')})`
      : `⛔ ${p.produk} — ${p.nama} (Rp${Number(p.harga).toLocaleString('id-ID')})`;
    opt.textContent = label;
    if (!p.available) opt.style.color = 'var(--text2)';
    sel.appendChild(opt);
  });
  if (cur) sel.value = cur;
}

function onProdukChange() {
  const sel = document.getElementById('po-produk');
  const opt = sel.options[sel.selectedIndex];
  if (!opt || !opt.value) return;
  const available = opt.dataset.available === 'True' || opt.dataset.available === 'true';
  const harga = Number(opt.dataset.harga);
  const info = document.getElementById('po-produk-info');
  if (info) {
    info.textContent = available
      ? `✅ Stok tersedia · Rp${harga.toLocaleString('id-ID')}`
      : `⛔ Stok kosong — pre-order akan menunggu restock`;
    info.style.color = available ? 'var(--success)' : 'var(--warn)';
  }
}
function setFilter(btn, f) {
  document.querySelectorAll('[data-filter]').forEach(b => {
    if (b.parentElement?.id !== 'tab-preorders' && !b.closest('.filter-bar')?.nextElementSibling?.id?.includes('preorders'))
      b.classList.remove('active');
  });
  btn.classList.add('active');
  productFilter = f;
  renderProducts();
}
function filterProducts() { renderProducts(); }
function renderProducts() {
  const q = (document.getElementById('product-search')?.value || '').toLowerCase();
  const tbody = document.getElementById('products-body');
  let items = allProducts.filter(p => {
    const matchQ = !q || p.produk.toLowerCase().includes(q) || p.nama.toLowerCase().includes(q);
    const matchF = productFilter === 'all' ||
      (productFilter === 'available' && p.available) ||
      (productFilter === 'unavailable' && !p.available);
    return matchQ && matchF;
  });
  if (!items.length) {
    tbody.innerHTML = `<tr><td colspan="6"><div class="empty-state"><div class="empty-icon">📦</div>Tidak ada produk</div></td></tr>`;
    return;
  }
  tbody.innerHTML = items.map(p => `
    <tr>
      <td><code>${p.produk}</code></td>
      <td>${p.nama}</td>
      <td>Rp${Number(p.harga).toLocaleString('id-ID')}</td>
      <td>${p.available
        ? '<span class="badge badge-available">Tersedia</span>'
        : '<span class="badge badge-empty">Kosong</span>'}</td>
      <td>${p.ghost_count > 0
        ? `<span class="badge badge-ghost">⚠ ${p.ghost_count}x</span>`
        : '<span style="color:var(--text2)">—</span>'}</td>
      <td>${p.available
        ? `<button class="btn-preorder" onclick='openModal(${JSON.stringify(p)})'>Pre-order</button>`
        : '<span style="color:var(--text2);font-size:12px">Tunggu restock</span>'}</td>
    </tr>`).join('');
}

// ─── Pre-orders ───────────────────────────────────────────────────────────────
async function loadPreorders() {
  allPreorders = await api('/api/preorders') || [];
  renderPreorders();
}
function setPOFilter(btn, f) {
  btn.closest('.filter-bar').querySelectorAll('.filter-btn').forEach(b => b.classList.remove('active'));
  btn.classList.add('active');
  poFilter = f;
  renderPreorders();
}
function renderPreorders() {
  const tbody = document.getElementById('preorders-body');
  const items = poFilter === 'all' ? allPreorders : allPreorders.filter(p => p.status === poFilter);
  if (!items.length) {
    tbody.innerHTML = `<tr><td colspan="6"><div class="empty-state"><div class="empty-icon">🛒</div>Tidak ada pre-order</div></td></tr>`;
    return;
  }
  tbody.innerHTML = items.map(p => `
    <tr>
      <td><code>${p.produk}</code><br><small style="color:var(--text2)">${p.produk_nama||''}</small></td>
      <td>${p.tujuan}</td>
      <td><span class="badge badge-${p.status}">${statusLabel(p.status)}</span>
          ${p.note ? `<br><small style="color:var(--text2);font-size:10px">${p.note}</small>` : ''}</td>
      <td>${p.attempts}/${p.max_attempts}</td>
      <td style="font-size:11px;color:var(--text2)">${fmtDate(p.created_at)}</td>
      <td>${['pending','retry'].includes(p.status)
        ? `<button class="btn-danger" onclick="cancelPreorder('${p.id}')">Batalkan</button>`
        : ''}</td>
    </tr>`).join('');
}

async function createPreorder(e) {
  e.preventDefault();
  const sel    = document.getElementById('po-produk');
  const produk = sel.value;
  const opt    = sel.options[sel.selectedIndex];
  const nama   = opt ? opt.dataset.nama || '' : '';
  const tujuan = document.getElementById('po-tujuan').value.trim();
  if (!produk) { toast('Pilih produk dulu', 'error'); return; }
  const res = await api('/api/preorders', 'POST', { produk, produk_nama: nama, tujuan });
  if (res?.id) {
    toast(`Pre-order ${produk} → ${tujuan} ditambahkan!`, 'success');
    e.target.reset();
    const info = document.getElementById('po-produk-info');
    if (info) info.textContent = '';
    loadPreorders();
  }
}

async function cancelPreorder(id) {
  if (!confirm('Batalkan pre-order ini?')) return;
  const res = await api(`/api/preorders/${id}`, 'DELETE');
  if (res?.ok) {
    toast('Pre-order dibatalkan', 'success');
    loadPreorders();
  }
}

// ─── Modal ───────────────────────────────────────────────────────────────────
function openModal(product) {
  modalProduct = product;
  document.getElementById('modal-produk-info').textContent =
    `📦 ${product.produk} — ${product.nama} (Rp${Number(product.harga).toLocaleString('id-ID')})`;
  document.getElementById('modal-tujuan').value = '';
  document.getElementById('modal-overlay').classList.add('show');
}
function closeModal() {
  document.getElementById('modal-overlay').classList.remove('show');
  modalProduct = null;
}
async function submitModalPreorder() {
  if (!modalProduct) return;
  const tujuan = document.getElementById('modal-tujuan').value.trim();
  if (!tujuan) { toast('Nomor tujuan wajib diisi', 'error'); return; }
  const res = await api('/api/preorders', 'POST', {
    produk: modalProduct.produk,
    produk_nama: modalProduct.nama,
    tujuan
  });
  if (res?.id) {
    toast(`Pre-order ${modalProduct.produk} → ${tujuan} ditambahkan!`, 'success');
    closeModal();
    document.querySelector('[data-tab="preorders"]').click();
  }
}

// ─── Logs ─────────────────────────────────────────────────────────────────────
async function loadLogs() {
  const logs = await api('/api/logs') || [];
  const html = logs.length === 0
    ? '<div class="empty-state"><div class="empty-icon">📋</div>Belum ada aktivitas</div>'
    : logs.map(l => `
      <div class="log-item">
        <span class="log-level ${l.level}">${l.level.toUpperCase()}</span>
        ${l.produk ? `<span class="log-produk">${l.produk}</span>` : '<span class="log-produk" style="color:var(--text2)">—</span>'}
        <span class="log-msg">${l.message}</span>
        <span class="log-time">${fmtDate(l.created_at)}</span>
      </div>`).join('');
  const el1 = document.getElementById('dash-logs');
  const el2 = document.getElementById('logs-list');
  if (el1) el1.innerHTML = html;
  if (el2) el2.innerHTML = html;
}

// ─── Helpers ─────────────────────────────────────────────────────────────────
async function api(url, method = 'GET', body = null) {
  try {
    const opts = { method, headers: { 'Content-Type': 'application/json' } };
    if (body) opts.body = JSON.stringify(body);
    const r = await fetch(url, opts);
    const d = await r.json();
    if (!r.ok) { toast(d.error || 'Error', 'error'); return null; }
    return d;
  } catch (e) {
    toast('Network error: ' + e.message, 'error');
    return null;
  }
}

function setEl(id, val) {
  const el = document.getElementById(id);
  if (el) el.textContent = val;
}

function statusLabel(s) {
  const map = { pending:'Menunggu', buying:'Beli', success:'Sukses', failed:'Gagal', retry:'Retry', cancelled:'Batal' };
  return map[s] || s;
}

function fmtDate(str) {
  if (!str) return '—';
  const d = new Date(str);
  const now = new Date();
  const diff = (now - d) / 1000;
  if (diff < 60)   return Math.floor(diff) + 'd lalu';
  if (diff < 3600) return Math.floor(diff/60) + 'm lalu';
  if (diff < 86400)return Math.floor(diff/3600) + 'j lalu';
  return d.toLocaleDateString('id-ID', { day:'2-digit', month:'short', hour:'2-digit', minute:'2-digit' });
}

let toastContainer;
function toast(msg, type = 'info') {
  if (!toastContainer) {
    toastContainer = document.createElement('div');
    toastContainer.className = 'toast-container';
    document.body.appendChild(toastContainer);
  }
  const el = document.createElement('div');
  el.className = `toast ${type}`;
  el.textContent = msg;
  toastContainer.appendChild(el);
  setTimeout(() => el.remove(), 3500);
}
