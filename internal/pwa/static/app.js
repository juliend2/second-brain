"use strict";

const $ = (sel) => document.querySelector(sel);

async function apiGet(path) {
  const res = await fetch(path);
  if (!res.ok) {
    let msg = "HTTP " + res.status;
    try { msg = (await res.json()).error || msg; } catch (_) {}
    throw new Error(msg);
  }
  return res.json();
}

const api = {
  search: (q, limit = 20) => apiGet(`/api/search?q=${encodeURIComponent(q)}&limit=${limit}`),
  node: (id) => apiGet(`/api/nodes/${encodeURIComponent(id)}`),
  neighbors: (id) => apiGet(`/api/graph/neighbors/${encodeURIComponent(id)}`),
  related: (id, k = 5) => apiGet(`/api/graph/related/${encodeURIComponent(id)}?k=${k}`),
  resolve: (title) => apiGet(`/api/graph/resolve?title=${encodeURIComponent(title)}`),
};

const view = $("#view");

function setTitle(t) { $("#topbar-title").textContent = t; }
function showBack(show) { $("#back").hidden = !show; }

/* ---------- routing ---------- */

function route() {
  const hash = location.hash || "#/";
  const parts = hash.split("/");
  if (parts[1] === "node") {
    showNode(decodeURIComponent(parts[2]));
  } else {
    showHome();
  }
}

window.addEventListener("hashchange", route);

/* ---------- home: search ---------- */

function showHome() {
  setTitle("Second brain");
  showBack(false);
  view.innerHTML = `
    <div class="card">
      <input type="search" id="q" placeholder="Search notes…" autocomplete="off" enterkeyhint="search">
    </div>
    <div id="results"></div>`;
  const box = $("#q");
  let timer = null;
  box.addEventListener("input", () => {
    clearTimeout(timer);
    const q = box.value.trim();
    timer = setTimeout(() => (q ? doSearch(q) : ($("#results").innerHTML = "")), 250);
  });
  box.focus();
}

async function doSearch(q) {
  const box = $("#results");
  box.innerHTML = `<p class="muted">Searching&#8230;</p>`;
  try {
    const { items } = await api.search(q);
    if (!items.length) {
      box.innerHTML = `<p class="empty">No matches for “${escapeHtml(q)}”.</p>`;
      return;
    }
    box.innerHTML = items
      .map((h) => `
        <div class="card" style="padding:0">
          <a class="result" href="#/node/${encodeURIComponent(h.id)}">
            <h3>${escapeHtml(h.title)}</h3>
            ${h.summary ? `<p>${escapeHtml(h.summary)}</p>` : ""}
            ${h.snippet ? `<p class="snippet">${escapeHtml(h.snippet)}</p>` : ""}
          </a>
        </div>`)
      .join("");
  } catch (err) {
    box.innerHTML = `<p class="error">${escapeHtml(String(err.message || err))}</p>`;
  }
}

/* ---------- node view: read-with-related ---------- */

async function showNode(id) {
  setTitle("Note");
  showBack(true);
  view.innerHTML = `<p class="muted">Loading note&#8230;</p>`;
  try {
    const [node, neigh, rel] = await Promise.all([
      api.node(id),
      api.neighbors(id).catch(() => null),
      api.related(id).catch(() => null),
    ]);
    renderNode(node, neigh, rel);
  } catch (err) {
    view.innerHTML = `<div class="card"><p class="error">${escapeHtml(String(err.message || err))}</p></div>`;
  }
}

function renderNode(node, neigh, rel) {
  setTitle(node.title || node.id);
  view.innerHTML = `
    <div class="card">
      <h1 class="node-title">${escapeHtml(node.title)}</h1>
      <div class="node-meta">${escapeHtml(node.id)} &middot; ${escapeHtml(node.status)}</div>
      ${node.tags && node.tags.length ? `<div class="tags">${node.tags.map((t) => `<span class="tag">${escapeHtml(t)}</span>`).join("")}</div>` : ""}
      ${node.summary ? `<p class="summary">${escapeHtml(node.summary)}</p>` : ""}
      <div class="markdown">${renderMarkdown(node.markdown || "")}</div>
    </div>

    <h2 class="section-title">Related by similarity</h2>
    <div class="card" style="padding:4px 12px">${relItems(rel)}</div>

    <h2 class="section-title">Links</h2>
    <div class="card" style="padding:4px 12px">${edgeItems(neigh, node.id)}</div>`;

  view.querySelectorAll(".wikilink").forEach((a) => {
    a.addEventListener("click", (ev) => {
      ev.preventDefault();
      resolveThenGo(decodeURIComponent(a.dataset.title), a);
    });
  });
}

function relItems(rel) {
  if (!rel) return `<p class="error">Could not load related notes.</p>`;
  if (!rel.items || !rel.items.length) return `<p class="empty">No related notes yet. Run <code>enrich embed</code> to enable similarity.</p>`;
  return `<ul class="rel-list">${rel.items
    .map((r) => `<li><a href="#/node/${encodeURIComponent(r.id)}">${escapeHtml(r.title || r.id)}
        <span class="score">${(r.score * 100).toFixed(0)}%</span></a></li>`)
    .join("")}</ul>`;
}

function edgeItems(neigh, currentId) {
  if (!neigh) return `<p class="error">Could not load links.</p>`;
  if (!neigh.edges || !neigh.edges.length) return `<p class="empty">No links. [[wikilinks]] and LLM “connects to” edges appear here.</p>`;
  const label = (e) => {
    const other = e.from === currentId ? e.to : e.from;
    return other.split(":").slice(1).join(":") || other;
  };
  return neigh.edges
    .map((e) => `
      <a class="edge" href="#/node/${encodeURIComponent(e.from === currentId ? e.to : e.from)}">
        <span class="kind">${escapeHtml(e.kind)}</span>${escapeHtml(label(e))}
        ${e.reason ? `<div class="why">${escapeHtml(e.reason)}</div>` : ""}
      </a>`)
    .join("");
}

async function resolveThenGo(title, anchor) {
  try {
    const { id } = await api.resolve(title);
    location.hash = "#/node/" + encodeURIComponent(id);
  } catch (_) {
    location.hash = "#/";
    setTimeout(() => {
      const box = $("#q");
      if (box) { box.value = title; doSearch(title); }
    }, 0);
  }
}

/* ---------- tiny markdown renderer (safe subset) ---------- */

function renderMarkdown(md) {
  const lines = String(md).split("\n");
  let html = "", inCode = false, codeBuf = [], inList = false;
  const closeList = () => { if (inList) { html += "</ul>"; inList = false; } };
  for (const raw of lines) {
    if (raw.trim().startsWith("```")) {
      if (inCode) {
        closeList();
        html += `<pre><code>${escapeHtml(codeBuf.join("\n"))}</code></pre>`;
        codeBuf = [];
        inCode = false;
      } else {
        closeList();
        inCode = true;
      }
      continue;
    }
    if (inCode) { codeBuf.push(raw); continue; }
    const h = raw.match(/^(#{1,6})\s(.*)$/);
    if (h) { closeList(); html += `<h${h[1].length}>${inline(h[2])}</h${h[1].length}>`; continue; }
    const b = raw.match(/^>\s?(.*)$/);
    if (b) { closeList(); html += `<blockquote>${inline(b[1])}</blockquote>`; continue; }
    const li = raw.match(/^-\s+(.*)$/);
    if (li) {
      if (!inList) { html += "<ul>"; inList = true; }
      html += `<li>${inline(li[1])}</li>`;
      continue;
    }
    closeList();
    if (raw.trim() === "") continue;
    html += `<p>${inline(raw)}</p>`;
  }
  closeList();
  if (inCode) html += `<pre><code>${escapeHtml(codeBuf.join("\n"))}</code></pre>`;
  return html;
}

function inline(s) {
  s = escapeHtml(s);
  s = s.replace(/`([^`]+)`/g, "<code>$1</code>");
  s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  s = s.replace(/\*([^*\s][^*]*)\*/g, "<em>$1</em>");
  s = s.replace(/\[([^\]]+)\]\((https?:\/\/[^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
  s = s.replace(/\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g,
    (_, title, alias) => `<a href="#/node/search" class="wikilink" data-title="${escapeAttr(title)}">${escapeHtml(alias || title)}</a>`);
  return s;
}

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}
function escapeAttr(s) {
  return escapeHtml(s).replace(/"/g, "&quot;");
}

/* ---------- service worker ---------- */

if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("/sw.js").catch(() => {});
}

route();
