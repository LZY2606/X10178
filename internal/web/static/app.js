"use strict";

const state = {
  runs: [], refId: null, currentId: null,
  currentRun: null, refRun: null,
  peakSet: null, detection: null,
  mapTargetId: null, mapping: null, anchors: [], locks: [],
  suggestions: [], selectedPeaks: new Set(),
  alignPts: null, residual: null, consensus: null,
  drag: null,
};

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));

async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) { opts.headers["Content-Type"] = "application/json"; opts.body = JSON.stringify(body); }
  const res = await fetch(path, opts);
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch (e) { data = text; }
  if (!res.ok) throw Object.assign(new Error((data && data.error) || res.statusText), { data, status: res.status });
  return data;
}

function msg(el, text, kind) {
  el.textContent = text || "";
  el.className = "msg" + (kind ? " " + kind : "");
}

function shapeOf(p) {
  if (p.plateau) return "平顶";
  if (p.rightIdx - p.apexIdx > p.apexIdx - p.leftIdx + 1) return "拖尾";
  if (p.apexIdx - p.leftIdx > p.rightIdx - p.apexIdx + 1) return "前伸";
  return "对称";
}

async function loadRuns(selectCurrent) {
  const data = await api("GET", "/api/runs");
  state.runs = data.runs;
  const list = $("#runList");
  list.innerHTML = "";
  const refSel = $("#refSelect");
  const mapSel = $("#mapTarget");
  refSel.innerHTML = ""; mapSel.innerHTML = "";
  for (const r of state.runs) {
    const div = document.createElement("div");
    div.className = "run-item" + (r.id === state.currentId ? " active" : "");
    div.innerHTML = `<div>${r.name || r.id}</div><div class="sub">${r.sample || ""} · ${r.instrument || ""} · ${r.id}</div>`;
    div.onclick = () => selectRun(r.id);
    list.appendChild(div);
    refSel.add(new Option(`${r.name || r.id} (${r.id})`, r.id));
    if (r.id !== state.refId) mapSel.add(new Option(`${r.name || r.id} (${r.id})`, r.id));
  }
  if (!state.refId && state.runs.length) state.refId = "run-ref";
  if (state.refId) refSel.value = state.refId;
  if (!state.currentId && state.runs.length) state.currentId = state.runs.find(r => r.id !== state.refId)?.id || state.runs[0].id;
  if (state.currentId) mapSel.value = state.currentId;
  if (selectCurrent !== false) await selectRun(state.currentId);
}

async function selectRun(id) {
  if (!id) return;
  state.currentId = id;
  state.mapTargetId = id;
  state.selectedPeaks = new Set();
  await Promise.all([loadRunCurves(), loadPeaks(), loadMappingState()]);
  await loadConsensusState();
  renderAll();
}

async function loadRunCurves() {
  state.currentRun = await api("GET", `/api/runs/${state.currentId}`);
  state.refRun = await api("GET", `/api/runs/${state.refId}`);
  try {
    const a = await api("POST", `/api/mappings/${state.refId}/${state.currentId}/align`, { version: 0 });
    state.alignPts = a.aligned;
  } catch (e) { state.alignPts = null; }
}

async function loadPeaks() {
  try { state.peakSet = await api("GET", `/api/runs/${state.currentId}/peakset`); }
  catch (e) { state.peakSet = null; }
  $("#psVer").textContent = state.peakSet ? `峰集 v${state.peakSet.version}${state.peakSet.frozen ? " · 已冻结" : " · 草案"}` : "尚无峰集";
}

async function loadMappingState() {
  try {
    state.mapping = await api("GET", `/api/mappings/${state.refId}/${state.currentId}/head`);
    state.anchors = state.mapping.anchors.map(a => ({ ...a }));
    state.locks = (state.mapping.locks || []).map(l => ({ ...l }));
  } catch (e) {
    state.mapping = null; state.anchors = []; state.locks = [];
  }
  refreshMapVerLabel();
  try {
    const r = await api("POST", `/api/mappings/${state.refId}/${state.currentId}/residual`, { version: 0 });
    state.residual = r.residual;
  } catch (e) { state.residual = null; }
}

function refreshMapVerLabel() {
  if (!state.mapping) { $("#mapVer").textContent = "尚无映射"; return; }
  const suffix = state.mapping.frozen ? " · 已冻结（编辑将开新系列）" : " · 草案";
  $("#mapVer").textContent = `映射 v${state.mapping.version}${suffix}`;
}

// saveBaseVersion returns the base version for commits: -1 opens a new series
// on top of a frozen head so frozen history is preserved.
function saveBaseVersion() {
  return state.mapping && state.mapping.frozen ? -1 : (state.mapping ? state.mapping.version : 0);
}

// ---------- canvas helpers ----------
function fitCanvas(cv) {
  const dpr = window.devicePixelRatio || 1;
  const rect = cv.getBoundingClientRect();
  cv.width = rect.width * dpr;
  const ctx = cv.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  return { ctx, w: rect.width, h: cv.height };
}

function scales(minX, maxX, minY, maxY, w, h, pad) {
  return {
    x: (t) => pad + ((t - minX) / (maxX - minX || 1)) * (w - 2 * pad),
    y: (v) => h - pad - ((v - minY) / (maxY - minY || 1)) * (h - 2 * pad),
    minX, maxX, minY, maxY, w, h, pad,
    invX: (px) => minX + ((px - pad) / (w - 2 * pad)) * (maxX - minX),
    invY: (py) => minY + ((h - pad - py) / (h - 2 * pad)) * (maxY - minY),
  };
}

function drawSeries(ctx, sc, times, values, color, width, dashed) {
  ctx.save();
  ctx.strokeStyle = color; ctx.lineWidth = width || 1.5;
  if (dashed) ctx.setLineDash([5, 4]);
  ctx.beginPath();
  for (let i = 0; i < times.length; i++) {
    const px = sc.x(times[i]), py = sc.y(values[i]);
    i === 0 ? ctx.moveTo(px, py) : ctx.lineTo(px, py);
  }
  ctx.stroke();
  ctx.restore();
}

function bounds(runs) {
  let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
  for (const run of runs) {
    minX = Math.min(minX, run.times[0]); maxX = Math.max(maxX, run.times[run.times.length - 1]);
    for (const v of run.values) { minY = Math.min(minY, v); maxY = Math.max(maxY, v); }
  }
  return [minX, maxX, minY, maxY];
}

function drawAxes(ctx, sc) {
  ctx.strokeStyle = "#2b3650"; ctx.fillStyle = "#8a97ad"; ctx.lineWidth = 1; ctx.font = "10px sans-serif";
  for (let i = 0; i <= 5; i++) {
    const t = sc.minX + (sc.maxX - sc.minX) * i / 5;
    const px = sc.x(t);
    ctx.beginPath(); ctx.moveTo(px, sc.pad); ctx.lineTo(px, sc.h - sc.pad); ctx.stroke();
    ctx.fillText(t.toFixed(1), px - 10, sc.h - sc.pad + 13);
  }
}

function renderOverlay() {
  const cv = $("#overlayCanvas");
  if (!state.refRun || !state.currentRun) return;
  const { ctx, w, h } = fitCanvas(cv);
  const all = [state.refRun, state.currentRun];
  let [minX, maxX, minY, maxY] = bounds(all);
  if (state.alignPts) for (const p of state.alignPts) maxY = Math.max(maxY, p.value);
  const sc = scales(minX, maxX, minY, maxY * 1.05, w, h, 28);
  drawAxes(ctx, sc);
  if ($("#showRef").checked) drawSeries(ctx, sc, state.refRun.times, state.refRun.values, "#5ab0ff", 2);
  if ($("#showRaw").checked) drawSeries(ctx, sc, state.currentRun.times, state.currentRun.values, "#8a97ad", 1.2, true);
  if (state.alignPts && $("#showAligned").checked) {
    drawSeries(ctx, sc, state.alignPts.map(p => p.refTime), state.alignPts.map(p => p.value), "#3fd08a", 2);
  }
  // legend
  const legends = [["#5ab0ff", "参考(原始)"], ["#8a97ad", "目标(原始)"], ["#3fd08a", "对齐后(派生)"]];
  ctx.font = "11px sans-serif";
  legends.forEach((l, i) => {
    ctx.fillStyle = l[0]; ctx.fillRect(sc.w - 150, 12 + i * 16, 10, 3);
    ctx.fillStyle = "#e6ecf5"; ctx.fillText(l[1], sc.w - 134, 17 + i * 16);
  });
  $("#alignTitle").textContent = `${state.currentRun.name} → ${state.refRun.name}`;
  if (state.alignPts) {
    const p0 = state.alignPts[0];
    $("#alignMeta").textContent = `血缘：每点记录 srcIdx/srcTime（原始采样）+ mappingId ${p0.mappingId} v${p0.mapVer}；共 ${state.alignPts.length} 点`;
  } else {
    $("#alignMeta").textContent = "尚无已保存映射：到“锚点/映射”页创建后即可叠加。";
  }
}

function renderPeakCanvas() {
  const cv = $("#peakCanvas");
  if (!state.currentRun) return;
  const { ctx, w, h } = fitCanvas(cv);
  const r = state.currentRun;
  const maxV = Math.max(...r.values) * 1.08;
  const sc = scales(r.times[0], r.times[r.times.length - 1], 0, maxV, w, h, 24);
  drawAxes(ctx, sc);
  drawSeries(ctx, sc, r.times, r.values, "#e6ecf5", 1.6);
  if (!state.peakSet) return;
  for (const p of state.peakSet.peaks) {
    const color = p.status === "ignored" ? "#ffb347" : p.status === "merged" || p.status === "split" ? "#b58cff" : "#5ab0ff";
    const x1 = sc.x(r.times[p.leftIdx]), x2 = sc.x(r.times[p.rightIdx]);
    ctx.strokeStyle = color; ctx.globalAlpha = state.selectedPeaks.has(p.id) ? 1 : 0.55;
    ctx.beginPath(); ctx.moveTo(x1, sc.y(0)); ctx.lineTo(x1, sc.h - sc.pad - 30); ctx.stroke();
    ctx.beginPath(); ctx.moveTo(x2, sc.y(0)); ctx.lineTo(x2, sc.h - sc.pad - 30); ctx.stroke();
    ctx.fillStyle = color;
    ctx.beginPath(); ctx.arc(sc.x(p.apexTime), sc.y(p.height), p.plateau ? 5 : 3.5, 0, Math.PI * 2); ctx.fill();
    if (p.plateau && p.plateauEnd) {
      ctx.strokeStyle = color; ctx.beginPath();
      ctx.moveTo(sc.x(r.times[p.apexIdx]), sc.y(p.height));
      ctx.lineTo(sc.x(r.times[p.plateauEnd]), sc.y(p.height)); ctx.stroke();
    }
    ctx.globalAlpha = 1;
    ctx.fillStyle = color; ctx.font = "9px sans-serif";
    ctx.fillText(p.id.replace("pk-", ""), sc.x(p.apexTime) - 14, sc.y(p.height) - 7);
  }
}

// ---------- mapping canvas ----------
function mapDomain() {
  const xs = state.anchors.map(a => a.x), ys = state.anchors.map(a => a.y);
  const rt = state.refRun.times;
  const min = Math.min(rt[0], ...xs, ...ys);
  const max = Math.max(rt[rt.length - 1], ...xs, ...ys);
  return [min - 0.2, max + 0.2];
}

function renderMapCanvas() {
  const cv = $("#mapCanvas");
  if (!state.refRun) return;
  const { ctx, w, h } = fitCanvas(cv);
  const [lo, hi] = mapDomain();
  const sc = scales(lo, hi, lo, hi, w, h, 34);
  // identity line
  ctx.save(); ctx.strokeStyle = "#445066"; ctx.setLineDash([4, 4]);
  ctx.beginPath(); ctx.moveTo(sc.x(lo), sc.y(lo)); ctx.lineTo(sc.x(hi), sc.y(hi)); ctx.stroke(); ctx.restore();
  drawAxes(ctx, sc);
  // locks
  for (const lk of state.locks) {
    ctx.fillStyle = "rgba(255,179,71,.10)";
    ctx.fillRect(sc.x(lk.xStart), sc.pad, sc.x(lk.xEnd) - sc.x(lk.xStart), h - 2 * sc.pad);
    ctx.strokeStyle = "#ffb347"; ctx.strokeRect(sc.x(lk.xStart), sc.pad, sc.x(lk.xEnd) - sc.x(lk.xStart), h - 2 * sc.pad);
  }
  // piecewise map through anchors sorted by x
  const sorted = [...state.anchors].sort((a, b) => a.x - b.x);
  if (sorted.length >= 2) {
    ctx.strokeStyle = "#3fd08a"; ctx.lineWidth = 2; ctx.beginPath();
    sorted.forEach((a, i) => i === 0 ? ctx.moveTo(sc.x(a.x), sc.y(a.y)) : ctx.lineTo(sc.x(a.x), sc.y(a.y)));
    ctx.stroke();
  }
  for (const a of state.anchors) {
    ctx.fillStyle = a.kind === "manual" ? "#5ab0ff" : "#b58cff";
    ctx.beginPath(); ctx.arc(sc.x(a.x), sc.y(a.y), 6, 0, Math.PI * 2); ctx.fill();
    ctx.fillStyle = "#e6ecf5"; ctx.font = "9px sans-serif";
    ctx.fillText(a.id.replace("anc-", ""), sc.x(a.x) + 7, sc.y(a.y) - 7);
  }
  // crossing highlight: check inversions in sorted anchors
  for (let i = 1; i < sorted.length; i++) {
    if (sorted[i].y < sorted[i - 1].y || sorted[i].x === sorted[i - 1].x && sorted[i].y !== sorted[i - 1].y) {
      ctx.strokeStyle = "#ff6b6b"; ctx.lineWidth = 2;
      ctx.beginPath(); ctx.moveTo(sc.x(sorted[i - 1].x), sc.y(sorted[i - 1].y));
      ctx.lineTo(sc.x(sorted[i].x), sc.y(sorted[i].y)); ctx.stroke();
    }
  }
  cv._sc = sc;
}

function renderResidual() {
  const cv = $("#residCanvas");
  if (!state.residual) { const { ctx, w, h } = fitCanvas(cv); ctx.clearRect(0, 0, w, h); return; }
  const { ctx, w, h } = fitCanvas(cv);
  const xs = state.residual.map(p => p.refTime), ds = state.residual.map(p => p.delta);
  const maxAbs = Math.max(0.05, ...ds.map(Math.abs));
  const sc = scales(Math.min(...xs), Math.max(...xs), -maxAbs, maxAbs, w, h, 20);
  ctx.strokeStyle = "#445066"; ctx.beginPath();
  ctx.moveTo(sc.pad, sc.y(0)); ctx.lineTo(w - sc.pad, sc.y(0)); ctx.stroke();
  ctx.fillStyle = "#3fd08a";
  ctx.beginPath();
  state.residual.forEach((p, i) => i === 0 ? ctx.moveTo(sc.x(p.refTime), sc.y(0)) : ctx.lineTo(sc.x(p.refTime), sc.y(0)));
  state.residual.forEach((p, i) => i === 0 ? ctx.moveTo(sc.x(p.refTime), sc.y(p.delta)) : ctx.lineTo(sc.x(p.refTime), sc.y(p.delta)));
  ctx.strokeStyle = "#ffb347"; ctx.stroke();
}

function anchorAtEvent(ev) {
  const cv = $("#mapCanvas");
  const rect = cv.getBoundingClientRect();
  const mx = ev.clientX - rect.left, my = ev.clientY - rect.top;
  const sc = cv._sc; if (!sc) return null;
  for (const a of state.anchors) {
    const dx = sc.x(a.x) - mx, dy = sc.y(a.y) - my;
    if (dx * dx + dy * dy < 100) return a;
  }
  return null;
}

function bindMapDrag() {
  const cv = $("#mapCanvas");
  cv.addEventListener("mousedown", (ev) => {
    const a = anchorAtEvent(ev);
    if (a) state.drag = a;
  });
  window.addEventListener("mousemove", async (ev) => {
    if (!state.drag) return;
    const rect = cv.getBoundingClientRect();
    const sc = cv._sc;
    state.drag.x = clamp(sc.invX(ev.clientX - rect.left), sc.minX, sc.maxX);
    state.drag.y = clamp(sc.invY(ev.clientY - rect.top), sc.minY, sc.maxY);
    renderMapCanvas(); renderAnchorTable();
  });
  window.addEventListener("mouseup", async () => {
    if (!state.drag) return;
    const moved = state.drag; state.drag = null;
    // local recompute: server checks monotonicity + locked segments
    try {
      const body = {
        baseVersion: saveBaseVersion(),
        anchors: state.anchors, locks: state.locks,
        anchorId: moved.id, newX: moved.x, newY: moved.y,
      };
      const res = await api("POST", `/api/mappings/${state.refId}/${state.currentId}/recompute`, body);
      msg($("#mapMsg"), `局部重算：仅 [${res.range.lo.toFixed(2)}, ${res.range.hi.toFixed(2)}] 区段变化，锁区段逐点不变，候选 v${res.version}`, "good");
    } catch (e) {
      if (e.data && e.data.code === "locked") {
        msg($("#mapMsg"), `拒绝：调整与锁定区段 ${e.data.lockConflicts.map(l => l.lockId).join(",")} 冲突，范围 [${e.data.range.lo.toFixed(2)},${e.data.range.hi.toFixed(2)}]。已回退拖动。`, "bad");
        await loadMappingState(); renderAll();
      } else if (e.data && e.data.rejected) {
        msg($("#mapMsg"), "该位置破坏单调性，最小拒绝集：\n" + fmtRejected(e.data.rejected) + "\n已回退拖动。", "bad");
        await loadMappingState(); renderAll();
      } else {
        msg($("#mapMsg"), String(e.message || e), "bad");
      }
    }
  });
}

function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }

function fmtRejected(rs) {
  return rs.map(r => `• ${r.anchorId} (x=${r.x.toFixed(2)},y=${r.y.toFixed(2)}) ${r.reason}${r.with ? " ↔ " + r.with : ""}`).join("\n");
}

// ---------- peak table + operations ----------
function renderPeakTable() {
  const tb = $("#peakTable tbody");
  tb.innerHTML = "";
  if (!state.peakSet) return;
  for (const p of state.peakSet.peaks) {
    const tr = document.createElement("tr");
    if (state.selectedPeaks.has(p.id)) tr.className = "sel";
    const tie = p.tiedRank ? `<span class="tag tie">并列#${p.tiedRank}</span>` : "";
    tr.innerHTML = `
      <td>${p.id}${p.parentId ? ` <span class="muted">←${p.parentId}</span>` : ""}</td>
      <td>${p.apexTime.toFixed(3)}</td>
      <td>${p.height.toFixed(3)}</td>
      <td>${p.area.toFixed(3)}</td>
      <td>${p.prominence.toFixed(3)}</td>
      <td>${shapeOf(p)}${p.plateau ? ` [${p.apexIdx}..${p.plateauEnd}]` : ""}</td>
      <td><span class="tag ${p.status}">${p.status}</span></td>
      <td>${tie}</td>
      <td></td>`;
    tr.onclick = () => {
      if (state.selectedPeaks.has(p.id)) state.selectedPeaks.delete(p.id);
      else state.selectedPeaks.add(p.id);
      renderPeakCanvas(); renderPeakTable();
    };
    const act = tr.lastChild;
    if (p.status === "active") {
      const ig = document.createElement("button"); ig.textContent = "忽略";
      ig.onclick = (e) => { e.stopPropagation(); doEdit("ignore", { id: p.id }); };
      act.appendChild(ig);
    } else if (p.status === "ignored") {
      const rs = document.createElement("button"); rs.textContent = "恢复";
      rs.onclick = (e) => { e.stopPropagation(); doEdit("restore", { id: p.id }); };
      act.appendChild(rs);
    }
    tb.appendChild(tr);
  }
}

async function doEdit(kind, extra) {
  if (!state.peakSet) return;
  const body = Object.assign({ baseVersion: state.peakSet.version }, extra);
  try {
    const ps = await api("POST", `/api/runs/${state.currentId}/peakset/${kind}`, body);
    state.peakSet = ps; state.selectedPeaks = new Set();
    msg($("#migInfo"), `已生成可撤销的新版本 v${ps.version}（${kind}）`, "good");
    renderAll();
  } catch (e) {
    if (e.data && e.data.code === "stale_version") {
      msg($("#migInfo"), `并发冲突：服务端已到 v${e.data.serverVersion}，请刷新基线后重试。`, "bad");
      await loadPeaks(); renderAll();
    } else msg($("#migInfo"), String(e.message || e), "bad");
  }
}

function bindPeakActions() {
  $("#btnMerge").onclick = () => {
    const ids = [...state.selectedPeaks];
    if (ids.length < 2) return msg($("#migInfo"), "请至少选择两个峰再合并", "warn");
    doEdit("merge", { ids });
  };
  $("#btnIgnore").onclick = () => {
    const ids = [...state.selectedPeaks];
    if (!ids.length) return msg($("#migInfo"), "请选择峰", "warn");
    doEdit("ignore", { id: ids[0] });
  };
  $("#btnRestore").onclick = () => {
    const ign = state.peakSet.peaks.filter(p => p.status === "ignored").pop();
    if (ign) doEdit("restore", { id: ign.id });
  };
  $("#btnSplit").onclick = () => {
    const ids = [...state.selectedPeaks];
    const target = ids.map(id => state.peakSet.peaks.find(p => p.id === id)).find(p => p && p.plateau);
    const idx = parseInt($("#splitIdx").value, 10);
    if (!target) return msg($("#migInfo"), "请先选中一个平顶峰", "warn");
    if (Number.isNaN(idx)) return msg($("#migInfo"), "请输入 boundary sample idx", "warn");
    doEdit("split", { id: target.id, boundaryIdx: idx });
  };
  $("#btnUndo").onclick = async () => {
    const versions = await api("GET", `/api/runs/${state.currentId}/peakset/versions`);
    const vs = versions.versions;
    if (vs.length < 2) return msg($("#migInfo"), "没有更早的版本可撤销", "warn");
    const prev = vs[vs.length - 2];
    try {
      // Revert appends a fresh revision restoring the previous peaks; the act
      // itself is versioned and undoable, nothing is deleted.
      const reverted = await api("POST", `/api/runs/${state.currentId}/peakset/revert`, {
        baseVersion: state.peakSet.version, targetVersion: prev.version,
      });
      state.peakSet = reverted;
      state.selectedPeaks = new Set();
      msg($("#migInfo"), `已撤销：新修订 v${reverted.version} 恢复自 v${prev.version}（历史保留）`, "good");
      renderAll();
    } catch (e) {
      if (e.data && e.data.code === "stale_version") {
        msg($("#migInfo"), "并发冲突：峰集已被他人更新，请刷新后重试。", "bad");
        await loadPeaks(); renderAll();
      } else msg($("#migInfo"), String(e.message || e), "bad");
    }
  };
  $("#btnFreezePS").onclick = async () => {
    await api("POST", `/api/runs/${state.currentId}/peakset/freeze`, { version: state.peakSet.version });
    await loadPeaks(); renderAll();
  };
  $("#btnRedetect").onclick = async () => {
    const params = {
      noiseFloor: parseFloat($("#pNoise").value),
      prominence: parseFloat($("#pProm").value),
      minDistanceIdx: parseInt($("#pDist").value, 10),
      levelTolerance: 1e-9,
    };
    try {
      const res = await api("POST", `/api/runs/${state.currentId}/detect`, { params });
      state.peakSet = res.peakSet;
      const m = res.migration;
      if (m) {
        msg($("#migInfo"),
          `重新检测完成：稳定ID迁移 ${m.matched.length} 个；消失 [${m.disappeared.join(", ") || "无"}]；新增 [${m["new"].join(", ") || "无"}]（不依赖数组下标）`,
          m.disappeared.length || m["new"].length ? "warn" : "good");
      } else msg($("#migInfo"), "首次检测完成", "good");
      renderAll();
    } catch (e) { msg($("#migInfo"), String(e.message || e), "bad"); }
  };
}

// ---------- anchor table ----------
function renderAnchorTable() {
  const tb = $("#anchorTable tbody");
  tb.innerHTML = "";
  for (const a of state.anchors) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td>${a.id}</td><td>${a.x.toFixed(3)}</td><td>${a.y.toFixed(3)}</td>
      <td><span class="tag ${a.kind === "manual" ? "active" : "merged"}">${a.kind}</span></td>
      <td>${a.peakId || ""}</td><td></td>`;
    const del = document.createElement("button"); del.textContent = "×";
    del.onclick = () => { state.anchors = state.anchors.filter(x => x.id !== a.id); renderMapCanvas(); renderAnchorTable(); };
    tr.lastChild.appendChild(del);
    tb.appendChild(tr);
  }
}

// ---------- anchor operations ----------
function mappingBody() {
  return { baseVersion: saveBaseVersion(), anchors: state.anchors, locks: state.locks };
}

function bindMapActions() {
  $("#btnAddAnchor").onclick = () => {
    const x = parseFloat($("#newX").value), y = parseFloat($("#newY").value);
    if (Number.isNaN(x) || Number.isNaN(y)) return msg($("#mapMsg"), "请输入 x/y", "warn");
    state.anchors.push({ id: "anc-" + Math.random().toString(16).slice(2, 8), x, y, kind: "manual" });
    renderMapCanvas(); renderAnchorTable();
  };
  $("#btnValidate").onclick = async () => {
    try {
      const res = await api("POST", `/api/mappings/${state.refId}/${state.currentId}/validate`, mappingBody());
      state.mapping = res.mapping;
      msg($("#mapMsg"), `校验通过：${res.mapping.accepted.length} 个锚点构成单调映射，可保存。`, "good");
    } catch (e) {
      if (e.data && e.data.rejected) {
        state.mapping = e.data.mapping;
        msg($("#mapMsg"), "拒绝保存。破坏单调性的最小锚点集合（未做全局排序掩盖）：\n" + fmtRejected(e.data.rejected), "bad");
      } else msg($("#mapMsg"), String(e.message || e), "bad");
    }
    renderMapCanvas();
  };
  $("#btnSaveMap").onclick = async () => {
    try {
      const saved = await api("POST", `/api/mappings/${state.refId}/${state.currentId}/commit`, mappingBody());
      state.mapping = saved;
      msg($("#mapMsg"), `已保存映射 v${saved.version}`, "good");
      await loadMappingState(); renderAll();
    } catch (e) {
      if (e.data && e.data.code === "stale_version") msg($("#mapMsg"), "旧版本并发提交被拒绝，请刷新映射后重试。", "bad");
      else if (e.data && e.data.rejected) msg($("#mapMsg"), "拒绝保存：\n" + fmtRejected(e.data.rejected), "bad");
      else msg($("#mapMsg"), String(e.message || e), "bad");
    }
  };
  $("#btnFreezeMap").onclick = async () => {
    if (!state.mapping) return;
    await api("POST", `/api/mappings/${state.refId}/${state.currentId}/freeze`, { version: state.mapping.version });
    await loadMappingState(); renderAll();
    msg($("#mapMsg"), `映射 v${state.mapping.version} 已冻结`, "good");
  };
  $("#btnSuggest").onclick = async () => {
    try {
      const res = await api("POST", `/api/mappings/${state.refId}/${state.currentId}/suggest`, { topK: 3 });
      state.suggestions = res.suggestions;
      renderSuggestions();
      msg($("#mapMsg"), `自动建议保留每峰最多 3 个候选，含形状/面积比/邻峰关系说明。`, "good");
    } catch (e) { msg($("#mapMsg"), String(e.message || e), "bad"); }
  };
  let selRange = null;
  $("#mapCanvas").addEventListener("dblclick", (ev) => {
    const sc = $("#mapCanvas")._sc; if (!sc) return;
    const rect = $("#mapCanvas").getBoundingClientRect();
    const x = sc.invX(ev.clientX - rect.left);
    if (!selRange) { selRange = [x, null]; msg($("#mapMsg"), "再次双击设置锁区间终点", "warn"); }
    else {
      selRange[1] = x;
      const [a, b] = selRange.sort((p, q) => p - q);
      state.locks.push({ id: "lock-" + Math.random().toString(16).slice(2, 7), xStart: a, xEnd: b });
      selRange = null; renderMapCanvas();
      msg($("#mapMsg"), `已加锁 [${a.toFixed(2)}, ${b.toFixed(2)}]`, "good");
    }
  });
  $("#btnAddLock").onclick = () => msg($("#mapMsg"), "在锚点图上双击两次即可划定锁定区间。", "warn");
}

function renderSuggestions() {
  const box = $("#suggestBox");
  box.innerHTML = "";
  const byRef = {};
  for (const s of state.suggestions) (byRef[s.refPeakId] ||= []).push(s);
  for (const refId of Object.keys(byRef)) {
    const wrap = document.createElement("div");
    wrap.className = "s";
    const cands = byRef[refId];
    wrap.innerHTML = `<div><b>${refId}</b> @ ${cands[0].x.toFixed(2)}：</div>` +
      cands.map((c, i) => `<div class="s ${c.ambiguous ? "amb" : ""}">候选${i + 1} ${c.candidateId} → y=${c.y.toFixed(2)} 形状=${c.shape} 面积比=${c.areaRatio.toFixed(2)} ${c.neighbor}${c.ambiguous ? " ⚠ 近似并列" : ""} <button data-r="${refId}" data-c="${c.candidateId}" data-x="${c.x}" data-y="${c.y}">采用</button></div>`).join("");
    box.appendChild(wrap);
  }
  box.querySelectorAll("button").forEach(b => b.onclick = () => {
    state.anchors.push({ id: "anc-s" + Math.random().toString(16).slice(2, 7), x: parseFloat(b.dataset.x), y: parseFloat(b.dataset.y), kind: "auto", peakId: b.dataset.c });
    renderMapCanvas(); renderAnchorTable();
  });
}

// ---------- consensus ----------
async function loadConsensusState() {
  const batch = state.refRun ? (state.runs.find(r => r.id === state.refId)?.batch || "B1") : "B1";
  try { state.consensus = await api("GET", `/api/consensus/${batch}/head`); }
  catch (e) { state.consensus = null; }
  renderFamilies(); renderConsensusVersions();
}

function bindConsensus() {
  $("#btnBuildCons").onclick = async () => {
    const batch = state.runs.find(r => r.id === state.refId)?.batch || "B1";
    const runIds = state.runs.filter(r => r.id !== state.refId).map(r => r.id);
    const body = {
      baseVersion: state.consensus ? state.consensus.version : 0,
      refRunId: state.refId, runIds,
      normRule: $("#normRule").value, tol: parseFloat($("#tol").value),
    };
    try {
      const c = await api("POST", `/api/consensus/${batch}/build`, body);
      state.consensus = c;
      msg($("#mapMsg"), "", "");
      renderFamilies(); renderConsensusVersions();
      $("#consVer").textContent = `共识 v${c.version} · ${c.normRule}${c.frozen ? " · 冻结" : ""}`;
    } catch (e) { alert("构建共识失败：" + (e.message || e)); }
  };
  $("#btnFreezeCons").onclick = async () => {
    if (!state.consensus) return;
    const batch = state.consensus.batch;
    await api("POST", `/api/consensus/${batch}/freeze`, { version: state.consensus.version });
    await loadConsensusState();
  };
}

function renderFamilies() {
  const box = $("#familiesBox");
  box.innerHTML = "";
  if (!state.consensus) { box.innerHTML = '<div class="muted">尚无共识。先冻结各运行映射与峰集，再构建。</div>'; return; }
  $("#consVer").textContent = `共识 v${state.consensus.version} · ${state.consensus.normRule}${state.consensus.frozen ? " · 冻结" : " · 草案"}`;
  for (const f of state.consensus.peaks) {
    const div = document.createElement("div");
    div.className = "fam";
    div.innerHTML = `<h4>${f.id} <span class="pill">参考时间 ${f.refTime.toFixed(2)}</span>
        <span class="pill">归一化面积 ${f.area.toFixed(3)}</span>
        <span class="pill sup">支持 ${f.supporting.length}</span>
        ${f.missing.length ? `<span class="pill miss">缺失 ${f.missing.length}: ${f.missing.join(", ")}</span>` : ""}</h4>`;
    const tbl = document.createElement("table");
    tbl.className = "grid";
    tbl.innerHTML = "<thead><tr><th>运行</th><th>峰</th><th>映射后时间</th><th>原始面积</th><th>归一化面积</th></tr></thead>";
    const tb = document.createElement("tbody");
    for (const s of f.supporting) {
      tb.innerHTML += `<tr><td>${s.runId}</td><td>${s.peakId}</td><td>${s.refTime.toFixed(3)}</td><td>${s.rawArea.toFixed(3)}</td><td>${s.normArea.toFixed(3)}</td></tr>`;
    }
    tbl.appendChild(tb);
    div.appendChild(tbl);
    box.appendChild(div);
  }
}

// ---------- versions / compare ----------
async function renderVersionPanels() {
  if (!state.currentId) return;
  const psVers = await api("GET", `/api/runs/${state.currentId}/peakset/versions`).catch(() => ({ versions: [] }));
  fillVerTable($("#psVerTable tbody"), psVers.versions.map(v => [v.version, v.frozen ? "是" : "", v.note || ""], ));
  let mapVers = { versions: [] };
  try { mapVers = await api("GET", `/api/mappings/${state.refId}/${state.currentId}/versions`); } catch (e) {}
  const mtb = $("#mapVerTable tbody"); mtb.innerHTML = "";
  for (const m of mapVers.versions) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td>${m.version}</td><td>${m.accepted.length}</td><td>${m.rejected.length}</td><td>${m.frozen ? "是" : ""}</td>`;
    tr.style.cursor = "pointer";
    tr.onclick = () => diffMapping(m);
    mtb.appendChild(tr);
  }
  const batch = state.runs.find(r => r.id === state.currentId)?.batch || "B1";
  renderConsensusVersions(batch);
}

function fillVerTable(tb, rows) {
  tb.innerHTML = "";
  for (const r of rows) {
    const tr = document.createElement("tr");
    tr.innerHTML = r.map(c => `<td>${c ?? ""}</td>`).join("");
    tb.appendChild(tr);
  }
}

async function renderConsensusVersions(batch) {
  batch = batch || "B1";
  let vs = { versions: [] };
  try { vs = await api("GET", `/api/consensus/${batch}/versions`); } catch (e) {}
  const tb = $("#consVerTable tbody"); tb.innerHTML = "";
  for (const c of vs.versions) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td>${c.version}</td><td>${c.normRule}</td><td>${c.peaks.length}</td><td>${c.frozen ? "是" : ""}</td>`;
    tb.appendChild(tr);
  }
}

function diffMapping(m) {
  const cur = state.mapping;
  const lines = [];
  lines.push(`比较映射 v${m.version} 与当前 v${cur ? cur.version : "-"}`);
  const key = (a) => a.id;
  const curA = new Map((cur ? cur.accepted : []).map(a => [key(a), a]));
  for (const a of m.accepted) {
    const b = curA.get(a.id);
    if (!b) lines.push(`+ ${a.id} (${a.x.toFixed(2)}→${a.y.toFixed(2)}) 仅存在于 v${m.version}`);
    else if (Math.abs(b.x - a.x) > 1e-9 || Math.abs(b.y - a.y) > 1e-9)
      lines.push(`~ ${a.id}: (${a.x.toFixed(2)},${a.y.toFixed(2)}) → (${b.x.toFixed(2)},${b.y.toFixed(2)})`);
  }
  for (const [id, b] of curA) if (!m.accepted.find(a => a.id === id))
    lines.push(`- ${id} (${b.x.toFixed(2)}→${b.y.toFixed(2)}) 仅存在于当前版本`);
  if (m.rejected.length) lines.push("拒绝锚点：" + m.rejected.map(r => r.anchorId + ":" + r.reason).join(", "));
  $("#diffBox").textContent = lines.join("\n") || "两个版本锚点一致。";
}

// ---------- import / seed ----------
function bindImport() {
  $("#btnImport").onclick = async () => {
    const text = $("#impData").value.trim();
    const rows = text.split(/\r?\n/).map(l => l.split(",").map(parseFloat));
    if (rows.some(r => r.length !== 2 || r.some(Number.isNaN))) {
      return msg($("#impMsg"), "每行必须是 time,signal 两个数字", "bad");
    }
    const body = {
      name: $("#impName").value || "imported",
      sample: $("#impSample").value || "S-?",
      instrument: $("#impInstr").value || "HPLC-?",
      batch: $("#impBatch").value || "B1",
      times: rows.map(r => r[0]), values: rows.map(r => r[1]),
      meta: { importedAt: new Date().toISOString() },
    };
    try {
      const run = await api("POST", "/api/runs", body);
      msg($("#impMsg"), `已导入 ${run.id}（${rows.length} 个原始采样点，未改写）`, "good");
      await loadRuns(false);
      await selectRun(run.id);
    } catch (e) { msg($("#impMsg"), String(e.message || e), "bad"); }
  };
  $("#btnSeed").onclick = async () => {
    await api("POST", "/api/demo/seed", {});
    await loadRuns();
  };
}

// ---------- render dispatch ----------
function renderAll() {
  renderOverlay();
  renderPeakCanvas();
  renderPeakTable();
  renderMapCanvas();
  renderResidual();
  renderAnchorTable();
  renderVersionPanels();
}

function bindTabs() {
  $$(".tab").forEach(t => t.onclick = () => {
    $$(".tab").forEach(x => x.classList.remove("active"));
    $$(".tabpane").forEach(x => x.classList.remove("active"));
    t.classList.add("active");
    $("#tab-" + t.dataset.tab).classList.add("active");
    requestAnimationFrame(renderAll);
  });
}

function bindMisc() {
  $("#refSelect").onchange = async (e) => { state.refId = e.target.value; await loadRuns(); };
  $("#mapTarget").onchange = async (e) => { await selectRun(e.target.value); };
  for (const id of ["showRaw", "showAligned", "showRef"]) $("#" + id).onchange = renderOverlay;
}

window.addEventListener("resize", () => requestAnimationFrame(renderAll));

(async function init() {
  bindTabs(); bindMisc(); bindMapDrag(); bindPeakActions(); bindMapActions(); bindConsensus(); bindImport();
  try {
    await loadRuns();
  } catch (e) {
    console.error(e);
  }
})();
