// 串口分线器 前端逻辑

let units = [];          // 分线器列表
let ports = { real: [], virt: [], pairs: [] };
let sources = [];        // 源串口下拉选项
let manualPairs = [];    // 可选端口对

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
    renderUnits();
  } catch (e) {
    console.error("刷新端口失败", e);
  }
}

// ---- 渲染 ----
function renderUnits() {
  const wrap = $("#units");
  wrap.innerHTML = "";
  if (units.length === 0) {
    wrap.innerHTML = '<div class="empty">还没有分线器，点左上角「＋ 新建分线器」</div>';
  }
  units.forEach((u) => wrap.appendChild(unitCard(u)));

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

  // 头部
  const head = document.createElement("div");
  head.className = "unit-head";
  const title = document.createElement("span");
  title.className = "unit-title";
  title.textContent = `分线器 ${u.id}`;
  const state = document.createElement("span");
  state.className = "unit-state " + (u.running ? "running" : "stopped");
  state.textContent = u.running ? `● 运行中  ${u.source} @ ${u.baud}` : "○ 未启动";
  head.appendChild(title);
  head.appendChild(state);
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
  if (!u.source && sources.length > 0) {
    // 默认选真实串口
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
  baud.value = u.baud || 115200;
  baud.style.width = "90px";
  baudField.appendChild(baud);
  config.appendChild(baudField);

  if (u.running) {
    sel.disabled = true;
    baud.disabled = true;
  }
  card.appendChild(config);

  // 端口对列表
  const list = document.createElement("div");
  list.className = "pair-list";
  if (u.pairs.length === 0) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = "（无终端端口，点下方按钮添加）";
    list.appendChild(empty);
  } else {
    u.pairs.forEach((p, i) => {
      const row = document.createElement("div");
      row.className = "pair-row";
      row.innerHTML = `
        <span class="term">终端 ${i + 1}</span>
        <span class="conn">请连 ${p.terminal}</span>
      `;
      if (!u.running) {
        const del = document.createElement("button");
        del.className = "del";
        del.textContent = "✕";
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
    stop.onclick = () => stopUnit(u.id);
    btns.appendChild(stop);
  } else {
    const addPair = document.createElement("button");
    addPair.className = "btn";
    addPair.textContent = "＋ 新建端口对";
    addPair.onclick = () => newPair(u.id);
    btns.appendChild(addPair);

    const addManual = document.createElement("button");
    addManual.className = "btn";
    addManual.textContent = "＋ 选已有端口";
    addManual.onclick = () => manualPair(u.id);
    btns.appendChild(addManual);

    const start = document.createElement("button");
    start.className = "btn primary";
    start.textContent = "▶ 启动";
    start.onclick = () => startUnit(u.id);
    btns.appendChild(start);

    const del = document.createElement("button");
    del.className = "btn danger";
    del.textContent = "删除";
    del.onclick = () => delUnit(u.id);
    btns.appendChild(del);
  }
  card.appendChild(btns);

  return card;
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
    await refreshUnits();
  } catch (e) {
    alert("启动失败：\n" + e.message);
  }
}

async function stopUnit(id) {
  await api(`/api/units/${id}/stop`, { method: "POST" });
  await refreshUnits();
}

async function newPair(id) {
  const before = units.find((u) => u.id === id)?.pairs?.length || 0;
  try {
    await api(`/api/units/${id}/addpair`, { method: "POST" });
    // 异步创建，轮询等待端口对出现（最多 30 秒）
    for (let i = 0; i < 60; i++) {
      await new Promise((res) => setTimeout(res, 500));
      const u = units.find((x) => x.id === id);
      if (u && u.pairs && u.pairs.length > before) {
        const added = u.pairs[u.pairs.length - 1];
        alert(`已新建端口对：\n终端连接端 ${added.terminal}（SecureCRT 连这个）\n分线器接管端 ${added.takeover}`);
        return;
      }
    }
    alert("新建端口对超时，请查看状态或重新扫描。");
  } catch (e) {
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
  // 简单弹窗
  const modal = document.createElement("div");
  modal.style.cssText = `position:fixed;inset:0;background:rgba(0,0,0,.6);display:flex;align-items:center;justify-content:center;z-index:100`;
  const box = document.createElement("div");
  box.style.cssText = `background:var(--card);border:1px solid var(--border);border-radius:12px;padding:20px;min-width:360px`;
  box.innerHTML = `<h3 style="margin-bottom:12px">选择接管端口</h3>`;
  box.appendChild(sel);
  const btns = document.createElement("div");
  btns.style.cssText = "margin-top:16px;display:flex;gap:10px;justify-content:flex-end";
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
    await api(`/api/units/${id}/addmanual`, {
      method: "POST",
      body: JSON.stringify({ takeover: p.takeover, terminal: p.terminal }),
    });
    await refreshUnits();
  };
}

async function delPair(id, terminal) {
  await api(`/api/units/${id}/delpair`, {
    method: "POST",
    body: JSON.stringify({ terminal }),
  });
  await refreshUnits();
}

async function delUnit(id) {
  if (!confirm("删除此分线器？")) return;
  await api(`/api/units/${id}`, { method: "DELETE" });
  await refreshUnits();
}

// ---- 刷新分线器列表 ----
async function refreshUnits() {
  try {
    units = await api("/api/units/all");
    renderUnits();
  } catch (e) {
    // /api/units/all 可能不存在，用 SSE 兜底
    console.error(e);
  }
}

// ---- SSE 实时更新 ----
function connectSSE() {
  const es = new EventSource("/api/stream");
  es.onmessage = (ev) => {
    try {
      units = JSON.parse(ev.data);
      renderUnits();
    } catch (e) {
      console.error("SSE 解析失败", e);
    }
  };
  es.onerror = () => {
    // 自动重连
    setTimeout(connectSSE, 2000);
  };
}

// ---- 初始化 ----
async function init() {
  $("#btnNew").onclick = async () => {
    await api("/api/units/new", { method: "POST" });
    await refreshPorts();
  };
  $("#btnRefresh").onclick = refreshPorts;

  connectSSE();
  await refreshPorts();
}

init();
