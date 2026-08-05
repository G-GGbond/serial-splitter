// 串口分线器 前端逻辑

let units = [];          // 分线器列表
let ports = { real: [], virt: [], pairs: [] };
let sources = [];        // 源串口下拉选项
let manualPairs = [];    // 可选端口对
let creating = new Set(); // 正在创建端口对的分线器 id

const $ = (sel) => document.querySelector(sel);

// ---- API ----
async function api(url, opts = {}) {
  const res = await fetch(url, {
    headers: { "Content-Type": "application/json" },
    ...opts,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text);
  }
  return res.json();
}

// ---- 端口刷新 ----
async function refreshPorts() {
  try {
    ports = await api("/api/ports");
    sources = [...ports.real, ...ports.virt];
    manualPairs = ports.pairs;
    updatePortStat();
    renderUnits();
  } catch (e) {
    console.error("刷新端口失败", e);
  }
}

function updatePortStat() {
  const el = $("#portStat");
  if (el) {
    const real = ports.real.length;
    const pairs = ports.pairs.length;
    el.innerHTML = `真实串口 <b>${real}</b> · 虚拟端口对 <b>${pairs}</b>`;
  }
}

// ---- 渲染 ----
// 草稿：保留用户未保存的源串口/波特率输入（避免 SSE 重建时丢失）
const drafts = {}; // id -> {source, baud}

function collectDrafts() {
  document.querySelectorAll(".unit").forEach((card) => {
    const id = parseInt(card.dataset.id);
    const sel = card.querySelector('select');
    const baud = card.querySelector('input[type="text"]');
    if (id && sel && baud) {
      const u = units.find((x) => x.id === id);
      // 只有未运行且用户改动过才保存
      if (u && !u.running && !u.source) {
        drafts[id] = { source: sel.value, baud: baud.value };
      }
    }
  });
}

function renderUnits() {
  collectDrafts();
  const wrap = $("#units");
  wrap.innerHTML = "";
  if (units.length === 0) {
    wrap.innerHTML = `
      <div class="empty-state">
        <div class="empty-icon">🔌</div>
        <div class="empty-text">还没有分线器</div>
        <div class="empty-sub">点击左上角「＋ 新建分线器」开始</div>
      </div>`;
  } else {
    units.forEach((u) => wrap.appendChild(unitCard(u)));
  }

  const running = units.filter((u) => u.running).length;
  const gs = $("#globalStatus");
  if (running > 0) {
    gs.textContent = `● ${running} 个分线器运行中（共 ${units.length} 个）`;
    gs.className = "status-dot running";
  } else {
    gs.textContent = `○ ${units.length} 个分线器，均未启动`;
    gs.className = "status-dot idle";
  }
}

function unitCard(u) {
  const card = document.createElement("div");
  card.className = "unit" + (u.running ? " running" : "");
  card.dataset.id = u.id;
  if (!u.pairs) u.pairs = [];
  if (!u.startedAt) u.startedAt = 0;

  // 头部
  const head = document.createElement("div");
  head.className = "unit-head";
  const title = document.createElement("span");
  title.className = "unit-title";
  title.textContent = `分线器 ${u.id}`;
  head.appendChild(title);

  const badge = document.createElement("span");
  badge.className = "unit-badge" + (u.running ? " on" : "");
  badge.textContent = u.running ? "● 运行中" : "○ 未启动";
  head.appendChild(badge);

  const state = document.createElement("span");
  state.className = "unit-state " + (u.running ? "running" : "stopped");
  state.textContent = u.running ? `${u.source} @ ${u.baud}` : "";
  head.appendChild(state);

  if (u.running && u.startedAt) {
    const t = document.createElement("span");
    t.className = "unit-time";
    t.textContent = `已运行 ${fmtDuration(Date.now() - u.startedAt)}`;
    t.dataset.role = "uptime";
    head.appendChild(t);
  }
  card.appendChild(head);

  // 配置行
  const config = document.createElement("div");
  config.className = "unit-config";

  const srcField = document.createElement("div");
  srcField.className = "field";
  srcField.innerHTML = '<label>源串口</label>';
  const sel = document.createElement("select");
  sources.forEach((s) => {
    const opt = document.createElement("option");
    opt.value = s;
    opt.textContent = s;
    if (s === u.source) opt.selected = true;
    sel.appendChild(opt);
  });
  // 优先使用用户草稿（未保存的改动）
  const draft = drafts[u.id];
  if (draft && draft.source && !u.source) {
    sel.value = draft.source;
  } else if (!u.source && sources.length > 0) {
    const real = ports.real.find((x) => x.includes("COM143")) || ports.real[0];
    if (real) sel.value = real;
  }
  srcField.appendChild(sel);
  config.appendChild(srcField);

  const baudField = document.createElement("div");
  baudField.className = "field";
  baudField.innerHTML = '<label>波特率</label>';
  const baud = document.createElement("input");
  baud.type = "text";
  baud.value = (draft && draft.baud && !u.source) ? draft.baud : (u.baud || 115200);
  baud.style.width = "90px";
  baudField.appendChild(baud);
  config.appendChild(baudField);

  if (u.running) {
    sel.disabled = true;
    baud.disabled = true;
  }
  card.appendChild(config);

  // 错误提示
  if (u.lastError && !u.running) {
    const err = document.createElement("div");
    err.style.cssText = "color:var(--red);font-size:12.5px;background:var(--red-soft);border:1px solid var(--red);border-radius:6px;padding:8px 12px;margin-bottom:10px";
    err.textContent = "⚠ " + u.lastError;
    card.appendChild(err);
  }

  // 端口对列表
  const list = document.createElement("div");
  list.className = "pair-list";
  if (u.pairs.length === 0 && !creating.has(u.id)) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = "（暂无终端端口，点下方按钮添加）";
    list.appendChild(empty);
  } else if (creating.has(u.id)) {
    const loading = document.createElement("div");
    loading.className = "loading-row";
    loading.innerHTML = `<div class="spinner"></div> 正在创建虚拟端口对…`;
    list.appendChild(loading);
  } else {
    u.pairs.forEach((p, i) => {
      const row = document.createElement("div");
      row.className = "pair-row";
      row.innerHTML = `
        <span class="term">终端 ${i + 1}</span>
        <span class="conn">请连 ${p.terminal}</span>
        <span class="hint">SecureCRT / MobaXterm</span>
      `;
      if (!u.running) {
        const del = document.createElement("button");
        del.className = "del";
        del.textContent = "✕";
        del.title = "移除该终端端口";
        del.onclick = () => delPair(u.id, p.terminal);
        row.appendChild(del);
      }
      list.appendChild(row);
    });
  }
  card.appendChild(list);

  // 按钮
  const btns = document.createElement("div");
  btns.className = "unit-btns";

  if (u.running) {
    const stop = document.createElement("button");
    stop.className = "btn danger";
    stop.textContent = "■ 停止";
    stop.title = "停止该分线器";
    stop.onclick = () => stopUnit(u.id);
    btns.appendChild(stop);
  } else {
    const addPair = document.createElement("button");
    addPair.className = "btn";
    addPair.textContent = "＋ 新建端口对";
    addPair.title = "自动创建一对虚拟串口并接管";
    addPair.disabled = creating.has(u.id);
    addPair.onclick = () => newPair(u.id);
    btns.appendChild(addPair);

    const addManual = document.createElement("button");
    addManual.className = "btn";
    addManual.textContent = "＋ 选已有端口";
    addManual.title = "从已有虚拟端口对中选择";
    addManual.disabled = creating.has(u.id);
    addManual.onclick = () => manualPair(u.id);
    btns.appendChild(addManual);

    const start = document.createElement("button");
    start.className = "btn primary";
    start.textContent = "▶ 启动";
    start.title = "开始双向转发";
    start.disabled = creating.has(u.id) || u.pairs.length === 0;
    start.onclick = () => startUnit(u.id);
    btns.appendChild(start);

    const del = document.createElement("button");
    del.className = "btn danger ghost";
    del.textContent = "删除";
    del.title = "删除此分线器";
    del.onclick = () => delUnit(u.id);
    btns.appendChild(del);
  }
  card.appendChild(btns);

  return card;
}

// 运行时长格式化
function fmtDuration(ms) {
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s} 秒`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} 分 ${s % 60} 秒`;
  const h = Math.floor(m / 60);
  return `${h} 时 ${m % 60} 分`;
}

// ---- 单元操作 ----
async function startUnit(id) {
  const card = document.querySelector(`.unit[data-id="${id}"]`);
  const sel = card.querySelector('select');
  const baud = card.querySelector('input[type="text"]');
  try {
    await api(`/api/units/${id}/start`, {
      method: "POST",
      body: JSON.stringify({ source: sel.value, baud: parseInt(baud.value) }),
    });
    // 记录启动时间
    const u = units.find((x) => x.id === id);
    if (u) u.startedAt = Date.now();
    await refreshUnits();
  } catch (e) {
    alert("启动失败：\n" + e.message);
  }
}

async function stopUnit(id) {
  await api(`/api/units/${id}/stop`, { method: "POST" });
  const u = units.find((x) => x.id === id);
  if (u) { u.startedAt = 0; u.running = false; }
  await refreshUnits();
}

async function newPair(id) {
  const before = (units.find((u) => u.id === id)?.pairs || []).length;
  creating.add(id);
  renderUnits();
  try {
    await api(`/api/units/${id}/addpair`, { method: "POST" });
    // 异步创建，轮询等待端口对出现或报错（最多 40 秒）
    for (let i = 0; i < 80; i++) {
      await new Promise((res) => setTimeout(res, 500));
      const u = units.find((x) => x.id === id);
      if (u && u.pairs && u.pairs.length > before) {
        const added = u.pairs[u.pairs.length - 1];
        creating.delete(id);
        renderUnits();
        alert(`已新建端口对：\n终端连接端 ${added.terminal}（SecureCRT 连这个）\n分线器接管端 ${added.takeover}`);
        return;
      }
      if (u && u.lastError) {
        creating.delete(id);
        renderUnits();
        alert("新建端口对失败：\n" + u.lastError);
        return;
      }
    }
    creating.delete(id);
    renderUnits();
    alert("新建端口对超时，请重新扫描端口后查看。");
  } catch (e) {
    creating.delete(id);
    renderUnits();
    alert("新建端口对失败：\n" + e.message);
  }
}

async function manualPair(id) {
  if (manualPairs.length === 0) {
    alert("没有可用端口对，先点「＋ 新建端口对」");
    return;
  }
  const opts = manualPairs.map((p) => `${p.takeover}（终端连 ${p.terminal}）`);
  const sel = document.createElement("select");
  opts.forEach((o, i) => {
    const opt = document.createElement("option");
    opt.value = i;
    opt.textContent = o;
    sel.appendChild(opt);
  });
  const modal = document.createElement("div");
  modal.style.cssText = `position:fixed;inset:0;background:rgba(0,0,0,.65);display:flex;align-items:center;justify-content:center;z-index:100;backdrop-filter:blur(2px)`;
  const box = document.createElement("div");
  box.style.cssText = `background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:22px;min-width:380px;box-shadow:var(--shadow)`;
  box.innerHTML = `<h3 style="margin-bottom:14px;font-size:15px">选择接管端口</h3>`;
  box.appendChild(sel);
  const btns = document.createElement("div");
  btns.style.cssText = "margin-top:18px;display:flex;gap:10px;justify-content:flex-end";
  const ok = document.createElement("button");
  ok.className = "btn primary";
  ok.textContent = "确定";
  const cancel = document.createElement("button");
  cancel.className = "btn";
  cancel.textContent = "取消";
  btns.appendChild(cancel);
  btns.appendChild(ok);
  box.appendChild(btns);
  modal.appendChild(box);
  document.body.appendChild(modal);

  cancel.onclick = () => modal.remove();
  ok.onclick = async () => {
    const idx = parseInt(sel.value);
    const p = manualPairs[idx];
    modal.remove();
    try {
      await api(`/api/units/${id}/addmanual`, {
        method: "POST",
        body: JSON.stringify({ takeover: p.takeover, terminal: p.terminal }),
      });
      await refreshUnits();
    } catch (e) {
      alert("添加失败：" + e.message);
    }
  };
}

async function delPair(id, terminal) {
  if (!confirm(`移除终端端口 ${terminal}？`)) return;
  try {
    await api(`/api/units/${id}/delpair`, {
      method: "POST",
      body: JSON.stringify({ terminal }),
    });
    await refreshUnits();
  } catch (e) {
    alert("移除失败：" + e.message);
  }
}

async function delUnit(id) {
  if (!confirm("删除此分线器？")) return;
  try {
    await api(`/api/units/${id}`, { method: "DELETE" });
    await refreshUnits();
  } catch (e) {
    alert("删除失败：" + e.message);
  }
}

// ---- 刷新分线器列表 ----
async function refreshUnits() {
  try {
    units = await api("/api/units/all");
    renderUnits();
  } catch (e) {
    console.error(e);
  }
}

// ---- SSE 实时更新 ----
function connectSSE() {
  const es = new EventSource("/api/stream");
  es.onmessage = (ev) => {
    try {
      const next = JSON.parse(ev.data);
      // 保留前端本地状态（启动时间）
      next.forEach((nu) => {
        const old = units.find((x) => x.id === nu.id);
        if (old && old.startedAt && nu.running) nu.startedAt = old.startedAt;
        else if (nu.running && !nu.startedAt) nu.startedAt = Date.now();
        else if (!nu.running) nu.startedAt = 0;
      });
      units = next;
      renderUnits();
    } catch (e) {
      console.error("SSE 解析失败", e);
    }
  };
  es.onerror = () => {
    setTimeout(connectSSE, 2000);
  };
}

// 每秒刷新运行时长显示
setInterval(() => {
  document.querySelectorAll('[data-role="uptime"]').forEach((el) => {
    const card = el.closest(".unit");
    if (card) {
      const id = parseInt(card.dataset.id);
      const u = units.find((x) => x.id === id);
      if (u && u.running && u.startedAt) {
        el.textContent = `已运行 ${fmtDuration(Date.now() - u.startedAt)}`;
      }
    }
  });
}, 1000);

// ---- 初始化 ----
async function init() {
  $("#btnNew").onclick = async () => {
    try {
      await api("/api/units/new", { method: "POST" });
      await refreshUnits();
    } catch (e) {
      alert("新建分线器失败：" + e.message);
    }
  };
  $("#btnRefresh").onclick = refreshPorts;

  connectSSE();
  await refreshPorts();
}

init();
