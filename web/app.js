// balloon — browser side.
//
// The PDF never leaves the machine: pdf.js renders it here and only the
// extracted text runs (a string and a box each) is posted to the local server,
// which does the parsing, numbering and balloon placement.

import * as pdfjsLib from '/vendor/pdf.mjs';

pdfjsLib.GlobalWorkerOptions.workerSrc = '/vendor/pdf.worker.mjs';

const SVG_NS = 'http://www.w3.org/2000/svg';
const $ = (id) => document.getElementById(id);

const state = {
  pdf: null,        // the loaded PDFDocumentProxy, null in demo mode
  drawing: null,    // the model.Drawing the server built
  texts: [],        // every extracted run, all pages
  page: 0,          // zero-based page index
  zoom: 1.3,
  selected: null,   // item id
  busy: false,     // serialize drawing edits so stale responses cannot restore deletions
};

// ---------------------------------------------------------------- extraction

// PDF text layers split a callout across runs almost every time: "⌀12.50" and
// " ±0.05" commonly arrive as separate items, and a parser handed the halves
// separately produces two useless characteristics instead of one correct one.
// Runs sitting on the same baseline with only a small gap between them are
// therefore stitched back together before anything else looks at them.
function mergeRuns(items) {
  const sorted = [...items].sort((a, b) =>
    Math.abs(a.box.y - b.box.y) > 2 ? a.box.y - b.box.y : a.box.x - b.box.x);

  const out = [];
  for (const it of sorted) {
    const prev = out[out.length - 1];
    if (prev && sameLine(prev, it) && gap(prev, it) < Math.max(prev.box.h, 4) * 1.2) {
      const right = Math.max(prev.box.x + prev.box.w, it.box.x + it.box.w);
      // Re-insert a space only when the glyphs were actually separated.
      const sep = gap(prev, it) > prev.box.h * 0.18 ? ' ' : '';
      prev.text += sep + it.text;
      prev.box.w = right - prev.box.x;
      prev.box.h = Math.max(prev.box.h, it.box.h);
      continue;
    }
    out.push({ text: it.text, page: it.page, box: { ...it.box } });
  }
  return out.filter((i) => i.text.trim() !== '');
}

const sameLine = (a, b) =>
  Math.abs((a.box.y + a.box.h / 2) - (b.box.y + b.box.h / 2)) < Math.max(a.box.h, b.box.h) * 0.55;

const gap = (a, b) => b.box.x - (a.box.x + a.box.w);

async function extractPage(page, index) {
  const vp = page.getViewport({ scale: 1 });
  const content = await page.getTextContent();

  const runs = [];
  for (const item of content.items) {
    if (!item.str || !item.str.trim()) continue;
    // Map the run's text-space transform into viewport space, which is already
    // top-left origin — the coordinate system the Go side works in.
    const tx = pdfjsLib.Util.transform(vp.transform, item.transform);
    const h = Math.hypot(tx[2], tx[3]) || 8;
    runs.push({
      text: item.str,
      page: index,
      box: { x: tx[4], y: tx[5] - h, w: item.width || 1, h },
    });
  }
  return { runs: mergeRuns(runs), width: vp.width, height: vp.height };
}

// ------------------------------------------------------------------- loading

async function loadPDF(file) {
  if (state.busy) return;
  setDrawingBusy(true);
  try {
    toast(`Reading ${file.name}…`);
    const buf = await file.arrayBuffer();
    state.pdf = await pdfjsLib.getDocument({ data: buf }).promise;

    const pages = [];
    let texts = [];
    for (let n = 1; n <= state.pdf.numPages; n++) {
      const page = await state.pdf.getPage(n);
      const { runs, width, height } = await extractPage(page, n - 1);
      pages.push({ index: n - 1, width, height });
      texts = texts.concat(runs);
    }

    state.texts = texts;
    state.page = 0;
    state.selected = null;

    const guessed = file.name.replace(/\.pdf$/i, '');
    if (!$('partNumber').value) $('partNumber').value = guessed;

    await build({
      id: 'local',
      name: $('partName').value,
      part_number: $('partNumber').value || guessed,
      revision: $('revision').value,
      pages,
    });

    await renderPage();
    toast(`${state.drawing.items.length} characteristics found`);
  } finally {
    setDrawingBusy(false);
  }
}

async function loadDemo() {
  if (state.busy) return;
  setDrawingBusy(true);
  try {
    const res = await fetch('/api/demo');
    if (!res.ok) return toast('Could not load the demo', true);
    const { drawing, texts } = await res.json();

    state.pdf = null;
    state.texts = texts;
    state.page = 0;
    state.selected = null;
    state.drawing = drawing;

    $('partNumber').value = drawing.part_number || '';
    $('partName').value = drawing.name || '';
    $('revision').value = drawing.revision || '';

    await renderPage();
    toast(`Demo part loaded — ${drawing.items.length} characteristics`);
  } finally {
    setDrawingBusy(false);
  }
}

async function build(meta) {
  const body = {
    drawing: meta,
    texts: state.texts,
    ...parserOptions(),
  };
  const res = await post('/api/build', body);
  if (res) {
    state.drawing = res;
    state.selected = null;
  }
}

// ------------------------------------------------------------------ rendering

async function renderPage() {
  const d = state.drawing;
  if (!d) return;

  $('empty').hidden = true;
  $('sheet').hidden = false;

  const page = d.pages.find((p) => p.index === state.page) || d.pages[0];
  const canvas = $('canvas');
  const overlay = $('overlay');

  const cssW = page.width * state.zoom;
  const cssH = page.height * state.zoom;

  // The sheet is sized independently of the canvas so it still occupies the
  // page area in demo mode, where there is no PDF to render behind it.
  $('sheet').style.width = `${cssW}px`;
  $('sheet').style.height = `${cssH}px`;

  if (state.pdf) {
    const dpr = window.devicePixelRatio || 1;
    const pdfPage = await state.pdf.getPage(state.page + 1);
    const vp = pdfPage.getViewport({ scale: state.zoom * dpr });
    canvas.width = vp.width;
    canvas.height = vp.height;
    canvas.style.width = `${cssW}px`;
    canvas.style.height = `${cssH}px`;
    canvas.hidden = false;
    await pdfPage.render({ canvasContext: canvas.getContext('2d'), viewport: vp }).promise;
  } else {
    // Demo mode has no PDF behind it: hide the canvas so the sheet's own white
    // background shows through as paper, and let the overlay draw the callouts
    // alongside the balloons.
    canvas.hidden = true;
  }

  overlay.setAttribute('width', cssW);
  overlay.setAttribute('height', cssH);
  overlay.setAttribute('viewBox', `0 0 ${page.width} ${page.height}`);

  drawOverlay(page);
  drawTable();
  updateChrome(d);
}

function drawOverlay(page) {
  const overlay = $('overlay');
  overlay.textContent = '';

  if (!state.pdf) {
    for (const t of state.texts.filter((t) => t.page === state.page)) {
      const el = document.createElementNS(SVG_NS, 'text');
      el.setAttribute('x', t.box.x);
      el.setAttribute('y', t.box.y + t.box.h * 0.78);
      el.setAttribute('font-size', t.box.h * 0.85);
      el.setAttribute('fill', '#16181d');
      el.setAttribute('font-family', 'system-ui, sans-serif');
      el.textContent = t.text;
      overlay.appendChild(el);
    }
  }

  for (const item of itemsOnPage()) overlay.appendChild(balloonNode(item));
}

function balloonNode(item) {
  const g = document.createElementNS(SVG_NS, 'g');
  g.classList.add('balloon');
  if (!item.clean) g.classList.add('unclean');
  if (!item.include) g.classList.add('excluded');
  if (item.id === state.selected) g.classList.add('selected');
  g.dataset.id = item.id;

  const leader = document.createElementNS(SVG_NS, 'line');
  leader.classList.add('leader');
  leader.setAttribute('x1', item.leader.a.x);
  leader.setAttribute('y1', item.leader.a.y);
  leader.setAttribute('x2', item.leader.b.x);
  leader.setAttribute('y2', item.leader.b.y);
  g.appendChild(leader);

  const tip = document.createElementNS(SVG_NS, 'circle');
  tip.classList.add('tip');
  tip.setAttribute('cx', item.leader.a.x);
  tip.setAttribute('cy', item.leader.a.y);
  tip.setAttribute('r', 1.6);
  g.appendChild(tip);

  const circle = document.createElementNS(SVG_NS, 'circle');
  circle.setAttribute('cx', item.balloon.c.x);
  circle.setAttribute('cy', item.balloon.c.y);
  circle.setAttribute('r', item.balloon.r);
  g.appendChild(circle);

  const label = document.createElementNS(SVG_NS, 'text');
  label.setAttribute('x', item.balloon.c.x);
  label.setAttribute('y', item.balloon.c.y + item.balloon.r * 0.36);
  label.setAttribute('font-size', item.balloon.r * 1.05);
  label.textContent = item.number;
  g.appendChild(label);

  const title = document.createElementNS(SVG_NS, 'title');
  title.textContent = item.issues?.length
    ? `${item.requirement}\n⚠ ${item.issues.join('\n⚠ ')}`
    : item.requirement;
  g.appendChild(title);

  return g;
}

function drawTable() {
  const tbody = $('tbody');
  tbody.textContent = '';
  const items = itemsOnPage();

  if (!items.length) {
    tbody.innerHTML = '<tr class="placeholder"><td colspan="4">No characteristics on this sheet.</td></tr>';
    return;
  }

  for (const item of items) {
    const tr = document.createElement('tr');
    tr.dataset.id = item.id;
    if (item.id === state.selected) tr.classList.add('selected');
    if (!item.include) tr.classList.add('excluded');
    if (!item.clean || item.characteristic.warnings?.length) tr.classList.add('flagged');

    const num = cell('td', item.number, 'c num');

    const req = cell('td', item.requirement, 'req editable');
    req.contentEditable = String(!state.busy);
    req.spellcheck = false;
    req.title = `as extracted: ${item.source.text}`;
    req.addEventListener('blur', () => reparse(item.id, req.textContent.trim()));
    req.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); req.blur(); }
      if (e.key === 'Escape') { req.textContent = item.requirement; req.blur(); }
    });

    const lim = cell('td', item.limits || '—', 'lim');

    const box = document.createElement('input');
    box.type = 'checkbox';
    box.checked = item.include;
    box.disabled = state.busy;
    box.title = 'Include on the inspection report';
    box.addEventListener('change', () => {
      item.include = box.checked;
      tr.classList.toggle('excluded', !box.checked);
      drawOverlay(currentPage());
      updateChrome(state.drawing);
    });
    const inc = document.createElement('td');
    inc.className = 'c';
    inc.appendChild(box);

    tr.append(num, req, lim, inc);
    tr.addEventListener('click', (e) => {
      if (e.target === box || e.target === req) return;
      select(item.id);
    });
    tbody.appendChild(tr);
  }
}

function cell(tag, text, cls) {
  const el = document.createElement(tag);
  if (cls) el.className = cls;
  el.textContent = text;
  return el;
}

function updateChrome(d) {
  const total = d.items.length;
  const inspectable = d.items.filter((i) => i.include).length;
  $('count').textContent = `${inspectable} of ${total} inspectable`;
  updateDeleteButton();

  const warnings = [];
  for (const it of d.items) {
    for (const w of it.characteristic.warnings || []) warnings.push(`#${it.number} ${w}`);
    for (const w of it.issues || []) warnings.push(`#${it.number} ${w}`);
  }
  const box = $('warnings');
  if (warnings.length) {
    box.hidden = false;
    box.innerHTML = `<strong>Review before sign-off</strong><ul>${
      warnings.map((w) => `<li>${escapeHTML(w)}</li>`).join('')}</ul>`;
  } else {
    box.hidden = true;
  }

  const pager = document.querySelector('.pager');
  pager.hidden = d.pages.length < 2;
  $('pageLabel').textContent = `${state.page + 1} / ${d.pages.length}`;
  $('prev').disabled = state.busy || state.page === 0;
  $('next').disabled = state.busy || state.page >= d.pages.length - 1;
}

// ----------------------------------------------------------------- selection

function select(id) {
  state.selected = id;
  updateDeleteButton();
  for (const el of document.querySelectorAll('.balloon')) {
    el.classList.toggle('selected', el.dataset.id === id);
  }
  for (const tr of document.querySelectorAll('#tbody tr')) {
    const on = tr.dataset.id === id;
    tr.classList.toggle('selected', on);
    if (on) tr.scrollIntoView({ block: 'nearest' });
  }
}

function updateDeleteButton() {
  const item = findItem(state.selected);
  $('deleteBalloon').disabled = state.busy || !item || item.page !== state.page;
  $('undoDelete').disabled = state.busy || !(state.drawing?.deleted?.length);
}

function setDrawingBusy(busy) {
  state.busy = busy;
  for (const id of ['tidy', 'reparse', 'exportSvg', 'exportXlsx', 'file',
                    'loadDemo', 'prev', 'next', 'zoomIn', 'zoomOut',
                    'tol1', 'tol2', 'tol3', 'tolAng']) {
    $(id).disabled = busy;
  }
  for (const el of document.querySelectorAll('#tbody .req')) el.contentEditable = String(!busy);
  for (const el of document.querySelectorAll('#tbody input')) el.disabled = busy;
  if (state.drawing) updateChrome(state.drawing);
  else updateDeleteButton();
}

// ------------------------------------------------------------------ dragging

// A balloon the solver could not place cleanly still has to end up somewhere
// sensible, and the fastest fix is a human dragging it. The leader is recomputed
// as it moves so it keeps terminating on the circle's edge rather than its
// centre — the same rule the solver uses.
function installDrag() {
  const overlay = $('overlay');
  let drag = null;

  overlay.addEventListener('pointerdown', (e) => {
    if (state.busy) return;
    const g = e.target.closest('.balloon');
    if (!g) return;
    const item = findItem(g.dataset.id);
    if (!item) return;

    const p = toSVG(e);
    drag = { g, item, dx: item.balloon.c.x - p.x, dy: item.balloon.c.y - p.y, moved: false };
    overlay.setPointerCapture(e.pointerId);
    select(item.id);
    e.preventDefault();
  });

  overlay.addEventListener('pointermove', (e) => {
    if (!drag) return;
    // Deletion or re-reading may have removed the item during this drag.
    if (state.busy || findItem(drag.item.id) !== drag.item) {
      try { overlay.releasePointerCapture(e.pointerId); } catch { /* already gone */ }
      drag = null;
      return;
    }
    const p = toSVG(e);
    drag.item.balloon.c.x = p.x + drag.dx;
    drag.item.balloon.c.y = p.y + drag.dy;
    drag.item.leader = leaderFor(drag.item.source.box, drag.item.balloon);
    drag.moved = true;

    // Once a human has placed it, the solver's complaint no longer applies.
    drag.item.clean = true;
    drag.item.issues = [];

    drag.g.replaceWith(balloonNode(drag.item));
    drag.g = overlay.querySelector(`.balloon[data-id="${CSS.escape(drag.item.id)}"]`);
  });

  const end = (e) => {
    if (!drag) return;
    if (drag.moved) updateChrome(state.drawing);
    try { overlay.releasePointerCapture(e.pointerId); } catch { /* already gone */ }
    drag = null;
  };
  overlay.addEventListener('pointerup', end);
  overlay.addEventListener('pointercancel', end);
}

function toSVG(e) {
  const svg = $('overlay');
  const pt = svg.createSVGPoint();
  pt.x = e.clientX;
  pt.y = e.clientY;
  return pt.matrixTransform(svg.getScreenCTM().inverse());
}

// Mirrors layout.leaderFor on the Go side.
function leaderFor(box, circle) {
  const a = { x: box.x + box.w / 2, y: box.y + box.h / 2 };
  const dx = circle.c.x - a.x;
  const dy = circle.c.y - a.y;
  const len = Math.hypot(dx, dy) || 1;
  return {
    a,
    b: { x: circle.c.x - (dx / len) * circle.r, y: circle.c.y - (dy / len) * circle.r },
  };
}

// -------------------------------------------------------------------- actions

// Drawing edits go through the Go model. The complete deletion history travels
// with the drawing, so layout, rebuild and undo share the same rules.
async function applyDrawingAction(path, extra, onSuccess) {
  const drawing = state.drawing;
  if (!drawing || state.busy) return false;
  const page = state.page;
  setDrawingBusy(true);
  try {
    const res = await post(path, { drawing, ...extra });
    if (!res || state.drawing !== drawing) return false;
    state.drawing = res;
    // Metadata fields can be edited while the local request is in flight.
    // Keep their current values rather than the older request snapshot.
    syncMeta();
    state.selected = null;
    onSuccess?.(res);
    if (page !== state.page) {
      await renderPage();
    } else {
      drawOverlay(currentPage());
      drawTable();
      updateChrome(res);
    }
    return true;
  } finally {
    setDrawingBusy(false);
  }
}

async function deleteSelectedBalloon() {
  const item = findItem(state.selected);
  if (!item || item.page !== state.page) return;
  await applyDrawingAction('/api/delete', { id: item.id }, () => {
    toast(`Balloon #${item.number} deleted — remaining balloons renumbered`);
  });
}

async function undoDeletion() {
  const deleted = state.drawing?.deleted || [];
  const item = deleted[deleted.length - 1];
  if (!item) return;
  await applyDrawingAction('/api/undo-delete', {}, (drawing) => {
    const restored = drawing.items.find(candidate => candidate.id === item.id);
    if (restored) {
      state.page = restored.page;
      state.selected = restored.id;
      toast(`Balloon #${restored.number} restored`);
    }
  });
}

function parserOptions() {
  return {
    tolerances: {
      1: num($('tol1').value, 0.2),
      2: num($('tol2').value, 0.1),
      3: num($('tol3').value, 0.05),
    },
    angular: num($('tolAng').value, 0.5),
    unit: 'mm',
  };
}

async function reparse(id, text) {
  const item = findItem(id);
  if (state.busy || !item || !text || text === item.requirement) return;
  setDrawingBusy(true);
  try {
    const res = await post('/api/parse', { text, ...parserOptions() });
    if (!res || findItem(id) !== item) return;
    item.characteristic = res.characteristic;
    item.requirement = res.requirement;
    item.limits = res.limits;
    item.designator = res.designator;
    item.include = res.inspectable;
    drawTable();
    drawOverlay(currentPage());
    updateChrome(state.drawing);
    toast(`#${item.number} re-read as ${res.requirement}`);
  } finally {
    setDrawingBusy(false);
  }
}

async function tidy() {
  await applyDrawingAction('/api/layout', { page: state.page }, () => toast('Balloons re-placed'));
}

async function download(path, fallbackName) {
  if (!state.drawing) return toast('Open a drawing first', true);

  syncMeta();
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      drawing: state.drawing,
      page: state.page,
      meta: {
        PartNumber: $('partNumber').value,
        PartName: $('partName').value,
        SerialNumber: $('serial').value,
        Revision: $('revision').value,
        DrawingNo: $('partNumber').value,
      },
    }),
  });
  if (!res.ok) return toast(`Export failed (${res.status})`, true);

  const name = filenameFrom(res.headers.get('Content-Disposition')) || fallbackName;
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
  toast(`Saved ${name}`);
}

function filenameFrom(header) {
  const m = /filename="([^"]+)"/.exec(header || '');
  return m ? m[1] : null;
}

function syncMeta() {
  if (!state.drawing) return;
  state.drawing.part_number = $('partNumber').value;
  state.drawing.name = $('partName').value;
  state.drawing.revision = $('revision').value;
}

// --------------------------------------------------------------------- utils

const itemsOnPage = () => (state.drawing?.items || []).filter((i) => i.page === state.page);
const currentPage = () => state.drawing.pages.find((p) => p.index === state.page) || state.drawing.pages[0];
const findItem = (id) => (state.drawing?.items || []).find((i) => i.id === id);
const num = (v, fallback) => (Number.isFinite(parseFloat(v)) ? parseFloat(v) : fallback);

function escapeHTML(s) {
  const d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}

async function post(path, body) {
  try {
    const res = await fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      toast(err.error || `Request failed (${res.status})`, true);
      return null;
    }
    return res.json();
  } catch (e) {
    toast(String(e), true);
    return null;
  }
}

let toastTimer;
function toast(msg, isError = false) {
  const el = $('toast');
  el.textContent = msg;
  el.classList.toggle('err', isError);
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, isError ? 6000 : 2600);
}

// ---------------------------------------------------------------------- wire

function init() {
  installDrag();

  $('file').addEventListener('change', (e) => {
    if (e.target.files[0]) loadPDF(e.target.files[0]).catch((err) => toast(String(err), true));
  });
  $('loadDemo').addEventListener('click', () => loadDemo());

  const stage = $('stage');
  stage.addEventListener('dragover', (e) => { e.preventDefault(); stage.classList.add('dragover'); });
  stage.addEventListener('dragleave', () => stage.classList.remove('dragover'));
  stage.addEventListener('drop', (e) => {
    e.preventDefault();
    stage.classList.remove('dragover');
    const f = e.dataTransfer.files[0];
    if (f && f.type === 'application/pdf') loadPDF(f).catch((err) => toast(String(err), true));
    else toast('That is not a PDF', true);
  });

  $('prev').addEventListener('click', () => { state.page--; renderPage(); });
  $('next').addEventListener('click', () => { state.page++; renderPage(); });
  $('zoomIn').addEventListener('click', () => { state.zoom = Math.min(4, state.zoom * 1.25); renderPage(); });
  $('zoomOut').addEventListener('click', () => { state.zoom = Math.max(0.3, state.zoom / 1.25); renderPage(); });
  $('tidy').addEventListener('click', tidy);
  $('deleteBalloon').addEventListener('click', deleteSelectedBalloon);
  $('undoDelete').addEventListener('click', undoDeletion);
  $('exportXlsx').addEventListener('click', () => download('/api/export.xlsx', 'inspection.xlsx'));
  $('exportSvg').addEventListener('click', () => download('/api/export.svg', 'drawing.svg'));

  $('reparse').addEventListener('click', async () => {
    if (!state.drawing) return toast('Open a drawing first', true);
    syncMeta();
    await applyDrawingAction('/api/build', { texts: state.texts, ...parserOptions() }, () => {
      toast('Drawing re-read with the new defaults');
    });
  });

  for (const id of ['partNumber', 'partName', 'revision']) {
    $(id).addEventListener('change', syncMeta);
  }

  document.addEventListener('keydown', (e) => {
    if (e.target.isContentEditable || e.target.closest('input, textarea, select')) return;
    if (state.busy) return;
    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && !e.repeat &&
        e.key.toLowerCase() === 'z' && state.drawing?.deleted?.length) {
      e.preventDefault();
      undoDeletion();
      return;
    }
    if (!e.ctrlKey && !e.metaKey && !e.altKey && !e.repeat &&
        (e.key === 'Delete' || e.key === 'Backspace')) {
      const item = findItem(state.selected);
      if (item && item.page === state.page) {
        e.preventDefault();
        deleteSelectedBalloon();
      }
      return;
    }
    if (e.key === 'ArrowRight' && !$('next').disabled) { state.page++; renderPage(); }
    if (e.key === 'ArrowLeft' && !$('prev').disabled) { state.page--; renderPage(); }
  });
}

init();
