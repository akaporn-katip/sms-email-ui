'use strict';

const state = {
  view: 'email',   // 'email' | 'sms'
  kind: '',        // '' | 'otp' | 'scheduled'
  query: '',
  selected: null,
  emails: [],
  messages: [],
  stats: null,
  htmlTab: 'text',
};

const $ = (id) => document.getElementById(id);

function escapeHtml(value) {
  return String(value ?? '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  })[c]);
}

function formatBytes(n) {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function formatTime(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  const time = d.toLocaleTimeString('th-TH', { hour: '2-digit', minute: '2-digit' });
  return sameDay ? time : `${d.toLocaleDateString('th-TH', { day: '2-digit', month: 'short' })} ${time}`;
}

async function api(path, options) {
  const res = await fetch(path, options);
  const text = await res.text();
  let body = null;
  try { body = text ? JSON.parse(text) : null; } catch { body = text; }
  if (!res.ok) {
    const message = body && body.error ? body.error : `${res.status} ${res.statusText}`;
    throw new Error(message);
  }
  return body;
}

// ---------------------------------------------------------------- rendering

function renderStats() {
  const s = state.stats;
  if (!s) return;
  const today = s.today || { total: 0, delivered: 0, failed: 0 };
  $('stats').innerHTML = [
    `<span class="pill">Email <b>${s.emails}</b></span>`,
    `<span class="pill">SMS <b>${s.messages}</b></span>`,
    `<span class="pill">OTP <b>${s.otps}</b></span>`,
    `<span class="pill">วันนี้ <b>${today.total}</b> (✓${today.delivered} ✗${today.failed})</span>`,
    `<span class="pill">เครดิต <b>${s.sms_remaining}</b></span>`,
    `<span class="pill">rate limit <b>${s.rateLimit ? 'on' : 'off'}</b></span>`,
  ].join('');
  $('count-email').textContent = s.emails;
  $('count-sms').textContent = s.messages;
  $('count-otp').textContent = s.otps;
  $('count-sched').textContent = state.messages.filter((m) => m.kind === 'scheduled').length;
}

function renderNav() {
  for (const el of document.querySelectorAll('.nav[data-view]')) {
    const active = el.dataset.view === state.view &&
      (el.dataset.kind || '') === (state.view === 'sms' ? state.kind : '');
    el.classList.toggle('active', active);
  }
}

function smsTitle(m) {
  if (m.kind === 'otp') return `OTP → ${m.recipient}`;
  if (m.kind === 'batch') return `Batch → ${m.recipient}`;
  if (m.kind === 'scheduled') return `Scheduled → ${m.recipient}`;
  return `SMS → ${m.recipient}`;
}

function renderList() {
  const list = $('list');
  if (state.view === 'email') {
    if (!state.emails.length) {
      list.innerHTML = '<div class="empty">ไม่มีอีเมล</div>';
      return;
    }
    list.innerHTML = state.emails.map((e) => `
      <div class="item ${e.read ? '' : 'unread'} ${state.selected === e.id ? 'selected' : ''}" data-id="${escapeHtml(e.id)}">
        <div class="row">
          <span class="who">${escapeHtml(e.from || '(unknown sender)')}</span>
          <span class="when">${escapeHtml(formatTime(e.date))}</span>
        </div>
        <div class="subject">${escapeHtml(e.subject || '(no subject)')}</div>
        <div class="preview">${escapeHtml(e.preview || '')}</div>
      </div>`).join('');
  } else {
    if (!state.messages.length) {
      list.innerHTML = '<div class="empty">ไม่มีข้อความ SMS</div>';
      return;
    }
    list.innerHTML = state.messages.map((m) => `
      <div class="item ${state.selected === m.id ? 'selected' : ''}" data-id="${escapeHtml(m.id)}">
        <div class="row">
          <span class="who">${escapeHtml(smsTitle(m))}</span>
          <span class="when">${escapeHtml(formatTime(m.createdAt))}</span>
        </div>
        <div class="subject">
          <span class="badge ${escapeHtml(m.status)}">${escapeHtml(m.statusDetail || m.status)}</span>
          ${m.kind === 'otp' ? `<span class="badge otp">${escapeHtml(m.otpCode || 'otp')}</span>` : ''}
        </div>
        <div class="preview">${escapeHtml(m.message)}</div>
      </div>`).join('');
  }
}

function kv(rows) {
  return `<table class="kv">${rows
    .filter(([, v]) => v !== undefined && v !== null && v !== '')
    .map(([k, v]) => `<tr><td>${escapeHtml(k)}</td><td>${v}</td></tr>`)
    .join('')}</table>`;
}

function renderEmailDetail(e) {
  const attachments = (e.attachments || []).map((a, i) => `
    <div class="attach">
      <span>📎 ${escapeHtml(a.filename || 'attachment')} <span class="muted">${escapeHtml(a.contentType)} · ${formatBytes(a.size)}</span></span>
      <a href="/api/emails/${encodeURIComponent(e.id)}/attachments/${i}">ดาวน์โหลด</a>
    </div>`).join('');

  const headers = Object.entries(e.headers || {})
    .map(([k, v]) => [k, escapeHtml(Array.isArray(v) ? v.join('<br>') : v)]);

  let body;
  if (e.html && state.htmlTab === 'html') {
    body = `<iframe class="html-body" sandbox srcdoc="${escapeHtml(e.html)}"></iframe>`;
  } else {
    body = `<div class="msgbox">${escapeHtml(e.text || '(ไม่มีเนื้อหาแบบข้อความ)')}</div>`;
  }

  $('detail').innerHTML = `
    <h1>${escapeHtml(e.subject || '(no subject)')}</h1>
    <div class="meta">
      <div><b>From:</b> ${escapeHtml(e.from || '')}</div>
      <div><b>To:</b> ${escapeHtml((e.to || []).join(', '))}${e.cc && e.cc.length ? ` · <b>Cc:</b> ${escapeHtml(e.cc.join(', '))}` : ''}</div>
      <div><b>Date:</b> ${escapeHtml(new Date(e.date).toLocaleString('th-TH'))} · ${formatBytes(e.size)}</div>
    </div>
    <div class="actions">
      <button class="btn" id="toggle-read">${e.read ? 'ทำเป็นยังไม่อ่าน' : 'ทำเป็นอ่านแล้ว'}</button>
      <a class="btn" style="text-decoration:none" href="/api/emails/${encodeURIComponent(e.id)}/raw">ดาวน์โหลด .eml</a>
      <button class="btn" id="delete-item">ลบ</button>
    </div>
    ${attachments ? `<div style="margin-bottom:14px">${attachments}</div>` : ''}
    ${e.html ? `<div class="tabs">
        <button class="tab ${state.htmlTab === 'text' ? 'active' : ''}" data-tab="text">Text</button>
        <button class="tab ${state.htmlTab === 'html' ? 'active' : ''}" data-tab="html">HTML</button>
      </div>` : ''}
    ${body}
    <h3 style="font-size:13px;color:var(--muted);margin:20px 0 6px">Headers</h3>
    ${kv(headers)}
  `;
}

function renderSMSDetail(m) {
  const rows = [
    ['id', escapeHtml(m.id)],
    ['kind', escapeHtml(m.kind)],
    ['sender', escapeHtml(m.sender)],
    ['recipient', escapeHtml(m.recipient)],
    ['status', `<span class="badge ${escapeHtml(m.status)}">${escapeHtml(m.status)}</span>`],
    ['statusDetail', escapeHtml(m.statusDetail || '-')],
    ['detail', m.detail ? escapeHtml(m.detail) : '-'],
    ['detailCategory', m.detailCategory ? `<code>${escapeHtml(m.detailCategory)}</code>` : '-'],
    ['creditCost', m.creditCost],
    ['createdAt', escapeHtml(new Date(m.createdAt).toLocaleString('th-TH'))],
    ['sentAt', m.sentAt ? escapeHtml(new Date(m.sentAt).toLocaleString('th-TH')) : '-'],
    ['deliveredAt', m.deliveredAt ? escapeHtml(new Date(m.deliveredAt).toLocaleString('th-TH')) : '-'],
    ['scheduledAt', m.scheduledAt ? escapeHtml(new Date(m.scheduledAt).toLocaleString('th-TH')) : '-'],
    ['ref', m.ref ? `<code>${escapeHtml(m.ref)}</code>` : '-'],
    ['batchId', m.batchId ? escapeHtml(m.batchId) : '-'],
  ];

  $('detail').innerHTML = `
    <h1>${escapeHtml(smsTitle(m))}</h1>
    <div class="meta"><div>${escapeHtml(formatTime(m.createdAt))} · ${escapeHtml(m.kind)}</div></div>
    <div class="actions">
      <button class="btn" id="delete-item">ลบ</button>
    </div>
    ${m.otpCode ? `<div class="otp-code">${escapeHtml(m.otpCode)}</div>` : ''}
    <div class="msgbox">${escapeHtml(m.message)}</div>
    <h3 style="font-size:13px;color:var(--muted);margin:20px 0 6px">รายละเอียด</h3>
    ${kv(rows)}
  `;
}

function renderDetail() {
  const detail = $('detail');
  if (state.view === 'email') {
    const e = state.emails.find((x) => x.id === state.selected);
    if (!e) { detail.innerHTML = '<div class="empty">เลือกข้อความเพื่อดูรายละเอียด</div>'; return; }
    renderEmailDetail(e);
    return;
  }
  const m = state.messages.find((x) => x.id === state.selected);
  if (!m) { detail.innerHTML = '<div class="empty">เลือกข้อความเพื่อดูรายละเอียด</div>'; return; }
  renderSMSDetail(m);
}

// ------------------------------------------------------------------ loading

async function loadStats() {
  state.stats = await api('/api/stats');
  renderStats();
}

async function loadList() {
  const params = new URLSearchParams();
  if (state.query) params.set('q', state.query);

  if (state.view === 'email') {
    const data = await api(`/api/emails?${params}`);
    state.emails = data.emails || [];
  } else {
    if (state.kind) params.set('kind', state.kind);
    const data = await api(`/api/sms?${params}`);
    state.messages = data.messages || [];
  }
  if (state.selected && !isSelectedStillPresent()) {
    state.selected = null;
  }
  renderList();
  renderDetail();
  renderNav();
}

function isSelectedStillPresent() {
  if (state.view === 'email') return state.emails.some((e) => e.id === state.selected);
  return state.messages.some((m) => m.id === state.selected);
}

async function refresh() {
  try {
    await Promise.all([loadStats(), loadList()]);
  } catch (err) {
    console.error(err);
  }
}

async function selectItem(id) {
  state.selected = id;
  state.htmlTab = 'text';
  if (state.view === 'email') {
    const e = state.emails.find((x) => x.id === id);
    if (e && !e.read) {
      e.read = true;
      api(`/api/emails/${encodeURIComponent(id)}/read`, { method: 'POST' })
        .then(loadStats)
        .catch(console.error);
    }
  }
  renderList();
  renderDetail();
}

// ------------------------------------------------------------------- events

$('list').addEventListener('click', (ev) => {
  const item = ev.target.closest('.item');
  if (item) selectItem(item.dataset.id);
});

$('detail').addEventListener('click', async (ev) => {
  const target = ev.target;
  if (target.id === 'delete-item') {
    const base = state.view === 'email' ? '/api/emails/' : '/api/sms/';
    await api(base + encodeURIComponent(state.selected), { method: 'DELETE' });
    state.selected = null;
    refresh();
    return;
  }
  if (target.id === 'toggle-read') {
    const e = state.emails.find((x) => x.id === state.selected);
    const read = !e.read;
    await api(`/api/emails/${encodeURIComponent(state.selected)}/read?read=${read}`, { method: 'POST' });
    e.read = read;
    renderList();
    renderDetail();
    return;
  }
  if (target.dataset.tab) {
    state.htmlTab = target.dataset.tab;
    renderDetail();
  }
});

for (const el of document.querySelectorAll('.nav[data-view]')) {
  el.addEventListener('click', () => {
    state.view = el.dataset.view;
    state.kind = el.dataset.kind || '';
    state.selected = null;
    refresh();
  });
}

$('search').addEventListener('input', (ev) => {
  state.query = ev.target.value.trim();
  loadList().catch(console.error);
});

$('refresh').addEventListener('click', refresh);

$('clear-view').addEventListener('click', async () => {
  if (!confirm('ล้างข้อความในกล่องปัจจุบัน?')) return;
  if (state.view === 'email') {
    await api('/api/emails', { method: 'DELETE' });
  } else if (state.kind === 'otp') {
    await api('/api/otps', { method: 'DELETE' });
  } else {
    await api('/api/sms', { method: 'DELETE' });
  }
  state.selected = null;
  refresh();
});

$('clear-all').addEventListener('click', async () => {
  if (!confirm('ล้างอีเมลและ SMS ทั้งหมด?')) return;
  await api('/api/emails', { method: 'DELETE' });
  await api('/api/sms', { method: 'DELETE' });
  await api('/api/otps', { method: 'DELETE' });
  state.selected = null;
  refresh();
});

document.addEventListener('keydown', (ev) => {
  if (ev.key === 'Escape') { state.selected = null; renderList(); renderDetail(); }
});

refresh();
setInterval(refresh, 3000);
