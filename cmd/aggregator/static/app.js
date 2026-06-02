let providerFilter = 'all';
let allProducts = [];
let allPreorders = [];
let allLogs = [];
let providerStates = [];
let saldoStates = {};

document.addEventListener('DOMContentLoaded', () => {
  setupTabs();
  loadAll();
  setInterval(loadAll, 8000);
});

function setupTabs() {
  document.querySelectorAll('.nav-item').forEach(item => {
    item.addEventListener('click', e => {
      e.preventDefault();
      const tab = item.dataset.tab;
      document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
      document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
      item.classList.add('active');
      document.getElementById('tab-' + tab).classList.add('active');
      document.getElementById('page-title').textContent = item.textContent.trim();
    });
  });
}

async function loadAll() {
  await Promise.all([loadProviders(), loadStats(), loadProducts(), loadPreorders(), loadLogs(), loadSaldo()]);
}

async function loadProviders() {
  providerStates = await api('/api/providers') || [];
  renderStatusSummary();
}

function onProviderChange() {
  providerFilter = document.getElementById('provider-filter').value;
  renderProducts();
  renderPreorders();
  renderLogs();
}

async function loadStats() {
  const data = await api('/api/stats');
  if (!data) return;
  const stats = providerFilter === 'all'
    ? data.totals
    : ((data.providers?.[providerFilter] || {}).stats || {});
  setEl('stat-pending', stats.pending || 0);
  setEl('stat-buying', stats.buying || 0);
  setEl('stat-success', stats.success || 0);
  setEl('stat-failed', stats.failed || 0);
  setEl('stat-retry', stats.retry || 0);
  setEl('stat-available', stats.products_available || 0);

  const cards = document.getElementById('provider-cards');
  const entries = Object.entries(data.providers || {});
  cards.innerHTML = entries.map(([provider, info]) => {
    const stats = info.stats || {};
    const online = !!info.online;
    return `<div class="provider-card ${online ? 'online' : 'offline'}">
      <div><span class="badge badge-${provider}">${provider.toUpperCase()}</span></div>
      <div class="muted">${online ? 'Online' : 'Offline'}</div>
      <div class="muted">Pending ${stats.pending || 0} · Success ${stats.success || 0}</div>
      <div class="muted">Produk ${stats.products_available || 0}/${stats.products_total || 0}</div>
    </div>`;
  }).join('');
}

async function loadSaldo() {
  const data = await api('/api/saldo');
  if (!data) return;
  saldoStates = data.providers || {};
  renderStatusSummary();
}

async function loadProducts() {
  const data = await api('/api/products');
  allProducts = data?.items || [];
  renderProducts();
  populateProdukDropdown();
}

function renderProducts() {
  const tbody = document.getElementById('products-body');
  const q = (document.getElementById('product-search')?.value || '').toLowerCase();
  const items = allProducts.filter(p =>
    (providerFilter === 'all' || p.provider === providerFilter) &&
    (!q || p.produk.toLowerCase().includes(q) || p.nama.toLowerCase().includes(q))
  );
  if (!items.length) {
    tbody.innerHTML = `<tr><td colspan="6"><div class="empty-state">Tidak ada produk</div></td></tr>`;
    return;
  }
  tbody.innerHTML = items.map(p => `
    <tr>
      <td><span class="badge badge-${p.provider}">${p.provider.toUpperCase()}</span></td>
      <td><code>${p.produk}</code></td>
      <td>${p.nama}</td>
      <td>Rp${Number(p.harga).toLocaleString('id-ID')}</td>
      <td>${p.available ? '<span class="badge badge-available">Tersedia</span>' : '<span class="badge badge-empty">Kosong</span>'}</td>
      <td>${p.available ? '<button class="btn-sm" onclick="prefillPreorder(\'' + p.provider + '\', \'' + p.produk + '\')">Pre-order</button>' : '-'}</td>
    </tr>
  `).join('');
}

function prefillPreorder(provider, produk) {
  document.querySelector('[data-tab="preorders"]').click();
  document.getElementById('po-provider').value = provider;
  populateProdukDropdown();
  document.getElementById('po-produk').value = produk;
}

function populateProdukDropdown() {
  const provider = document.getElementById('po-provider').value;
  const sel = document.getElementById('po-produk');
  const items = allProducts.filter(p => p.provider === provider);
  sel.innerHTML = items.map(p => `<option value="${p.produk}" data-nama="${escapeHtml(p.nama)}">${p.produk} — ${escapeHtml(p.nama)}${p.available ? '' : ' (Kosong)'}</option>`).join('');
}

async function loadPreorders() {
  const data = await api('/api/preorders');
  allPreorders = data?.items || [];
  renderPreorders();
}

function renderPreorders() {
  const tbody = document.getElementById('preorders-body');
  const items = allPreorders.filter(p => providerFilter === 'all' || p.provider === providerFilter);
  if (!items.length) {
    tbody.innerHTML = `<tr><td colspan="6"><div class="empty-state">Tidak ada pre-order</div></td></tr>`;
    return;
  }
  tbody.innerHTML = items.map(p => `
    <tr>
      <td><span class="badge badge-${p.provider}">${p.provider.toUpperCase()}</span></td>
      <td><code>${p.produk}</code><br><small class="muted">${p.produk_nama || ''}</small></td>
      <td>${p.tujuan}</td>
      <td><span class="badge badge-${p.status}">${statusLabel(p.status)}</span>${p.note ? `<br><small class="muted">${escapeHtml(p.note)}</small>` : ''}</td>
      <td>${p.attempts}/${p.max_attempts}</td>
      <td>${['pending', 'retry'].includes(p.status) ? `<button class="btn-danger" onclick="cancelPreorder('${p.provider}','${p.id}')">Batalkan</button>` : ''}</td>
    </tr>
  `).join('');
}

async function createPreorder(e) {
  e.preventDefault();
  const provider = document.getElementById('po-provider').value;
  const sel = document.getElementById('po-produk');
  const produk = sel.value;
  const nama = sel.options[sel.selectedIndex]?.dataset.nama || '';
  const tujuan = document.getElementById('po-tujuan').value.trim();
  const res = await api('/api/preorders', 'POST', { provider, produk, produk_nama: nama, tujuan });
  if (res?.id) {
    toast(`Pre-order ${provider.toUpperCase()} ditambahkan`, 'success');
    e.target.reset();
    document.getElementById('po-provider').value = provider;
    populateProdukDropdown();
    loadPreorders();
  }
}

async function cancelPreorder(provider, id) {
  if (!confirm('Batalkan pre-order ini?')) return;
  const res = await api(`/api/preorders/${provider}/${id}`, 'DELETE');
  if (res?.ok) {
    toast('Pre-order dibatalkan', 'success');
    loadPreorders();
  }
}

async function loadLogs() {
  const data = await api('/api/logs');
  allLogs = data?.items || [];
  renderLogs();
}

function renderLogs() {
  const items = allLogs.filter(l => providerFilter === 'all' || l.provider === providerFilter);
  const html = !items.length
    ? '<div class="empty-state">Belum ada aktivitas</div>'
    : items.map(l => `<div class="log-item">
      <span class="badge badge-${l.provider}">${l.provider.toUpperCase()}</span>
      <span class="badge badge-${l.level}">${l.level.toUpperCase()}</span>
      <span>${escapeHtml(l.message)}</span>
      <span class="muted">${fmtDate(l.created_at)}</span>
    </div>`).join('');
  document.getElementById('dash-logs').innerHTML = html;
  document.getElementById('logs-list').innerHTML = html;
}

async function api(url, method = 'GET', body = null) {
  try {
    const opts = { method, headers: { 'Content-Type': 'application/json' } };
    if (body) opts.body = JSON.stringify(body);
    const res = await fetch(url, opts);
    const data = await res.json();
    if (!res.ok) {
      toast(data.error || 'Error', 'error');
      return null;
    }
    return data;
  } catch (err) {
    toast('Network error: ' + err.message, 'error');
    return null;
  }
}

function setEl(id, val) {
  const el = document.getElementById(id);
  if (el) el.textContent = val;
}

function statusLabel(s) {
  return ({ pending: 'Pending', buying: 'Buying', success: 'Sukses', failed: 'Gagal', retry: 'Retry', cancelled: 'Batal' })[s] || s;
}

function fmtDate(str) {
  if (!str) return '—';
  const d = new Date(str);
  return d.toLocaleString('id-ID');
}

function escapeHtml(value) {
  return String(value).replace(/[&<>"]/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[ch]));
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

function renderStatusSummary() {
  const offline = providerStates.filter(p => !p.online).map(p => p.provider.toUpperCase());
  const healthText = offline.length ? `Offline: ${offline.join(', ')}` : 'Semua provider online';
  const saldoText = Object.values(saldoStates).filter(Boolean).map(p => {
    if (!p.online) return `${(p.provider || 'unknown').toUpperCase()}: offline`;
    return `${p.provider.toUpperCase()}: Rp${Number(p.saldo || 0).toLocaleString('id-ID')}`;
  }).join(' · ');
  document.getElementById('status-summary').textContent = saldoText ? `${healthText} · ${saldoText}` : healthText;
}
