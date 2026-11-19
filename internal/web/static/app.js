// Encorr web app. Plain JavaScript, no build step. Request the encore.
"use strict";

const $app = document.getElementById("app");
const IMG = "https://image.tmdb.org/t/p/";
const state = { status: null, user: null, pendingCount: 0, timers: [] };

// ---------- helpers ----------
const esc = (v) => String(v ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const img = (path, size = "w342") => (path && path.startsWith("/") ? IMG + size + path : "");
const qs = (sel, root = document) => root.querySelector(sel);
const qsa = (sel, root = document) => [...root.querySelectorAll(sel)];

async function api(path, opts = {}) {
  const init = { method: opts.method || "GET", headers: { Accept: "application/json" }, credentials: "same-origin" };
  if (init.method !== "GET") init.headers["X-Encorr"] = "1";
  if (opts.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(opts.body);
  }
  let res;
  try {
    res = await fetch("/api/v1" + path, init);
  } catch {
    throw new Error("Can't reach Encorr. Is it still running?");
  }
  let data = null;
  try { data = await res.json(); } catch { /* empty */ }
  if (res.status === 401 && !opts.allow401) {
    state.user = null;
    if (!location.hash.startsWith("#/login")) boot();
    throw new Error((data && data.error) || "Please sign in.");
  }
  if (!res.ok) throw new Error((data && data.error) || `Something went wrong (${res.status}).`);
  return data;
}

function toast(msg, kind = "") {
  const t = document.createElement("div");
  t.className = "toast " + kind;
  t.textContent = msg;
  qs("#toasts").appendChild(t);
  setTimeout(() => t.remove(), kind === "error" ? 7000 : 4000);
}

function modal(html, { wide = false, sheet = false } = {}) {
  const bg = document.createElement("div");
  bg.className = "modal-bg";
  bg.innerHTML = `<div class="modal glass ${wide ? "wide" : ""} ${sheet ? "sheet" : ""}" role="dialog" aria-modal="true">${html}</div>`;
  const close = () => { bg.remove(); document.removeEventListener("keydown", onKey); };
  const onKey = (e) => { if (e.key === "Escape") close(); };
  bg.addEventListener("click", (e) => { if (e.target === bg) close(); });
  document.addEventListener("keydown", onKey);
  document.body.appendChild(bg);
  qsa("[data-close]", bg).forEach((b) => b.addEventListener("click", close));
  return { el: qs(".modal", bg), close };
}

function busy(btn, on, label) {
  if (!btn) return;
  if (on) {
    btn.dataset.label = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = `<span class="spinner"></span> ${esc(label || "Working…")}`;
  } else {
    btn.disabled = false;
    if (btn.dataset.label) btn.innerHTML = btn.dataset.label;
  }
}

// Tooltip: data-tip shows a small bubble on hover and keyboard focus.
const tip = (text) => ` data-tip="${esc(text)}"`;
const exact = (iso) => { try { return new Date(iso).toLocaleString("en-GB", { dateStyle: "medium", timeStyle: "short" }); } catch (_) { return ""; } };
const when = (iso) => `<time datetime="${esc(iso)}"${tip(exact(iso))}>${esc(ago(iso))}</time>`;

function ago(iso) {
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (!isFinite(s)) return "";
  if (s < 60) return "just now";
  if (s < 3600) return Math.floor(s / 60) + " min ago";
  if (s < 86400) return Math.floor(s / 3600) + " h ago";
  if (s < 86400 * 30) return Math.floor(s / 86400) + " days ago";
  return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
}

// niceDate: "today", "tomorrow", "on Fri 21 Nov", "on 1 Dec 2027".
function niceDate(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return "";
  const day = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate());
  const diff = Math.round((day(d) - day(new Date())) / 86400000);
  if (diff === 0) return "today";
  if (diff === 1) return "tomorrow";
  if (diff > 1 && diff < 7) return "on " + d.toLocaleDateString(undefined, { weekday: "long" });
  const opts = { weekday: "short", day: "numeric", month: "short" };
  if (d.getFullYear() !== new Date().getFullYear()) { delete opts.weekday; opts.year = "numeric"; }
  return "on " + d.toLocaleDateString(undefined, opts);
}

function setBackdrop(path) {
  const b = qs("#backdrop");
  if (path) { b.style.backgroundImage = `url("${img(path, "w1280")}")`; b.classList.add("on"); }
  else b.classList.remove("on");
}

function clearTimers() { state.timers.forEach(clearInterval); state.timers = []; }
const isAdmin = () => state.user && state.user.role === "admin";
const seasonsText = (s) => (!s || !s.length ? "" : s.length === 1 ? `Season ${s[0]}` : `Seasons ${s.join(", ")}`);

const STATUS = {
  available: ["available", "In Plex"],
  completed: ["completed", "In Plex"],
  partial: ["partial", "Partly in Plex"],
  processing: ["processing", "Requested"],
  approved: ["approved", "On its way"],
  pending: ["pending", "Waiting for approval"],
  declined: ["declined", "Declined"],
  failed: ["failed", "Failed"],
};
const PILL_TIPS = { available: "Already in Plex: ready to watch", completed: "Arrived in Plex", partial: "Some of it is in Plex", processing: "Requested and on its way", approved: "Approved and on its way", pending: "Waiting for the owner to say yes", declined: "The owner said no", failed: "Something went wrong sending it to download" };
const pill = (s, text) => { const [cls, label] = STATUS[s] || ["off", s]; return `<span class="pill ${cls}"${PILL_TIPS[s] ? tip(PILL_TIPS[s]) : ""}><span class="dot"></span>${esc(text || label)}</span>`; };

// ---------- brand ----------
// The e-Loop: the "e" of encorr is also a replay loop. Drawn in the accent colour.
// The Encorr ribbon E. MARK is one colour (follows the accent, or any CSS
// colour); GMARK is the full gradient version used with the wordmark.
const E_OUTER = "M178 36 L160 62 H96 C78 62 66 74 66 92 V108 C66 126 78 138 96 138 H160 L142 164 H92 C60 164 40 142 40 110 V90 C40 58 60 36 92 36 Z";
const E_MID = "M162 87 L145 113 H90 C77 113 71 104 75 96 C78 90 84 87 92 87 Z";
const MARK = (size, cls = "") => `<svg class="mark ${cls}" width="${size}" height="${size}" viewBox="22 13 174 174" aria-hidden="true"><path d="${E_OUTER}" fill="currentColor"/><path d="${E_MID}" fill="currentColor" opacity=".82"/></svg>`;
let gmarkN = 0;
const GMARK = (size, cls = "") => {
  const id = "eg" + ++gmarkN;
  return `<svg class="mark gmark ${cls}" width="${size}" height="${size}" viewBox="22 13 174 174" aria-hidden="true"><defs>
    <linearGradient id="${id}a" x1="0.1" y1="0" x2="0.9" y2="1"><stop offset="0" stop-color="var(--accent-2)"/><stop offset=".55" stop-color="var(--accent)"/><stop offset="1" stop-color="var(--accent-pop)"/></linearGradient>
    <linearGradient id="${id}b" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="var(--accent)"/><stop offset="1" stop-color="var(--accent-2)"/></linearGradient></defs>
    <path d="${E_OUTER}" fill="url(#${id}a)"/><path d="${E_MID}" fill="url(#${id}b)"/></svg>`;
};
const LOCKUP = (size) => `<span class="lockup" style="font-size:${size}px" role="img" aria-label="Encorr">${GMARK(Math.round(size * 1.05), "lk")}<span>encorr</span></span>`;

// ---------- icons ----------
const RI = {
  flame: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><path d="M12 3c.5 3.5 4.5 5.2 4.5 9.5a4.5 4.5 0 0 1-9 0c0-2 1-3.3 2-4.3.2 1.6 1 2.6 2 2.8C11 9 11 6 12 3Z"/></svg>',
  recent: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><rect x="3.5" y="5" width="17" height="12" rx="2.5"/><path d="M8 20.5h8M10.5 9l4 2-4 2z"/></svg>',
  film: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="3.5" width="16" height="17" rx="2.5"/><path d="M8 3.5v17M16 3.5v17M4 8h4M4 12h4M4 16h4M16 8h4M16 12h4M16 16h4"/></svg>',
  tv: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><rect x="3" y="6" width="18" height="12.5" rx="2.5"/><path d="m8.5 2.5 3.5 3.5 3.5-3.5"/></svg>',
  ticket: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><path d="M3.5 7.5a2 2 0 0 1 2-2h13a2 2 0 0 1 2 2v2a2.5 2.5 0 0 0 0 5v2a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2v-2a2.5 2.5 0 0 0 0-5z"/><path d="M14 6v12" stroke-dasharray="2 2.2"/></svg>',
  star: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><path d="m12 3.5 2.6 5.3 5.9.9-4.3 4.1 1 5.8L12 16.8l-5.2 2.8 1-5.8-4.3-4.1 5.9-.9z"/></svg>',
  person: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="8.5" r="4"/><path d="M4.5 20.5c1.2-3.6 4-5.5 7.5-5.5s6.3 1.9 7.5 5.5"/></svg>',
  studio: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><path d="M4 20.5V9l8-5 8 5v11.5M9 20.5v-6h6v6M3 20.5h18"/></svg>',
  network: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="12" r="2"/><path d="M8 8a5.6 5.6 0 0 0 0 8M16 8a5.6 5.6 0 0 1 0 8M5.2 5.2a9.6 9.6 0 0 0 0 13.6M18.8 5.2a9.6 9.6 0 0 1 0 13.6"/></svg>',
  tag: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><path d="M3.5 12V4.5a1 1 0 0 1 1-1H12l8.5 8.5-8.5 8.5z"/><circle cx="8" cy="8" r="1.4" fill="currentColor"/></svg>',
  spark: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><path d="M12 3.5c.8 4.6 3.9 7.7 8.5 8.5-4.6.8-7.7 3.9-8.5 8.5-.8-4.6-3.9-7.7-8.5-8.5 4.6-.8 7.7-3.9 8.5-8.5Z"/></svg>',
};
function rowIcon(key) {
  const k = String(key || ""), pre = k.split(":")[0];
  const m = { trending: "flame", "plex-recent": "recent", movies: "film", tv: "tv", upcoming: "ticket", "now-playing": "ticket", "on-air": "tv", "top-movies": "star", "top-tv": "star", person: "person", company: "studio", network: "network", genre: "tag", keyword: "tag", decade: "spark", gems: "spark" };
  return RI[m[k] || m[pre] || "spark"];
}

const I = {
  clock: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"><circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/></svg>',
  calendar: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"><rect x="3.5" y="5" width="17" height="15" rx="2.5"/><path d="M3.5 10h17M8 3v4M16 3v4"/></svg>',
  list: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"><path d="M9 6.5h11M9 12h11M9 17.5h11"/><circle cx="4.5" cy="6.5" r="1" fill="currentColor"/><circle cx="4.5" cy="12" r="1" fill="currentColor"/><circle cx="4.5" cy="17.5" r="1" fill="currentColor"/></svg>',
  more: '<svg viewBox="0 0 24 24" fill="currentColor"><circle cx="5.5" cy="12" r="1.8"/><circle cx="12" cy="12" r="1.8"/><circle cx="18.5" cy="12" r="1.8"/></svg>',
  send: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linejoin="round"><path d="M20.5 3.5 10 14M20.5 3.5l-6.5 17-4-6.5-6.5-4z"/></svg>',
  warn: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3.5 21.5 20h-19z"/><path d="M12 10v4.5M12 17.5v.01"/></svg>',
  refresh: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M19.5 12a7.5 7.5 0 1 1-2.2-5.3M19.5 4v4.5H15"/></svg>',
  wrench: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linejoin="round"><path d="M14.5 4a4.5 4.5 0 0 0-4.2 6.1L4 16.4 7.6 20l6.3-6.3A4.5 4.5 0 0 0 20 9.5l-2.8 2.8-3-.5-.5-3L16.5 6A4.5 4.5 0 0 0 14.5 4Z"/></svg>',
  hand: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M8 11V5.5a1.5 1.5 0 0 1 3 0V10m0-5.5a1.5 1.5 0 0 1 3 0V10m0-4a1.5 1.5 0 0 1 3 0v7a7 7 0 0 1-7 7h-.5a6 6 0 0 1-4.6-2.2L3 14.5a1.6 1.6 0 0 1 2.4-2.1L8 14.5"/></svg>',
  download: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3.5v12M7 11l5 5 5-5M4.5 20.5h15"/></svg>',
  spark: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linejoin="round"><path d="M12 3.5c.8 4.6 3.9 7.7 8.5 8.5-4.6.8-7.7 3.9-8.5 8.5-.8-4.6-3.9-7.7-8.5-8.5 4.6-.8 7.7-3.9 8.5-8.5Z"/></svg>',
  pause: '<svg viewBox="0 0 24 24" fill="currentColor"><rect x="6.5" y="5" width="4" height="14" rx="1.2"/><rect x="13.5" y="5" width="4" height="14" rx="1.2"/></svg>',
  up: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20V7M6.5 12.5 12 7l5.5 5.5M5 3.5h14"/></svg>',
  stop: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9"><circle cx="12" cy="12" r="8.5"/><rect x="9" y="9" width="6" height="6" rx="1" fill="currentColor"/></svg>',
  x: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6.5 6.5l11 11M17.5 6.5l-11 11"/></svg>',
  trash: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 7h15M9.5 7V4.5h5V7M6.5 7l1 12.5h9l1-12.5"/></svg>',
  discover: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="9"/><path d="m15.5 8.5-2 5-5 2 2-5z" fill="currentColor" fill-opacity=".25"/></svg>',
  search: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="11" cy="11" r="6.5"/><path d="m20 20-4.2-4.2"/></svg>',
  requests: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="3.5" width="16" height="17" rx="3"/><path d="M8 8.5h8M8 12h8M8 15.5h5"/></svg>',
  users: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="9" cy="8.5" r="3.2"/><path d="M3.5 19c.7-3 3-4.6 5.5-4.6s4.8 1.6 5.5 4.6"/><circle cx="17" cy="9.5" r="2.5"/><path d="M16.5 14.6c2.1.2 3.5 1.6 4 4"/></svg>',
  settings: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="3"/><path d="M12 2.8v2.4M12 18.8v2.4M4.2 7.5l2.1 1.2M17.7 15.3l2.1 1.2M4.2 16.5l2.1-1.2M17.7 8.7l2.1-1.2"/><circle cx="12" cy="12" r="7"/></svg>',
  play: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M8 5.5v13l11-6.5z"/></svg>',
  plus: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M12 5v14M5 12h14"/></svg>',
  check: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"><path d="m5 12.5 4.5 4.5L19 7.5"/></svg>',
  plex: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M6 3h5.2l6.8 9-6.8 9H6l6.8-9z"/></svg>',
  chev: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" width="16" height="16"><path d="m9 5 7 7-7 7"/></svg>',
};

const ACCENTS = [["violet", "#b4b6ff", "Lavender"], ["blue", "#5b93f0", "Blue"], ["teal", "#2fbfae", "Teal"], ["green", "#52c47c", "Green"], ["yellow", "#e9bb4a", "Yellow"], ["coral", "#ef8256", "Orange"], ["red", "#e55a61", "Red"], ["pink", "#e46aa8", "Pink"]];

// The "+" on a poster: films are requested on the spot, shows open the
// season picker on their page.
async function quickRequest(el) {
  const [type, id] = el.dataset.qreq.split(":");
  if (type === "tv") { state.autoReq = true; location.hash = `#/title/tv/${id}`; return; }
  el.classList.add("busy");
  try {
    await api("/requests", { method: "POST", body: { type: "movie", tmdbId: Number(id) } });
    el.classList.remove("busy"); el.classList.add("done"); el.innerHTML = I.check; el.removeAttribute("data-qreq");
    toast("Requested", "ok");
    refreshPending();
  } catch (e) { el.classList.remove("busy"); toast(e.message, "error"); }
}
document.addEventListener("click", (e) => {
  const q = e.target.closest && e.target.closest("[data-qreq]");
  if (!q) return;
  e.preventDefault(); e.stopPropagation(); quickRequest(q);
}, true);
document.addEventListener("keydown", (e) => {
  if ((e.key === "Enter" || e.key === " ") && e.target.matches && e.target.matches("[data-qreq]")) { e.preventDefault(); quickRequest(e.target); }
}, true);

// ---------- boot & routing ----------
async function boot() {
  clearTimers();
  try {
    state.status = await api("/status");
  } catch (e) {
    $app.innerHTML = `<div class="center"><div class="glass wizard"><h1>Can't reach Encorr</h1><p class="muted">${esc(e.message)}</p><button class="btn primary" onclick="location.reload()">Try again</button></div></div>`;
    return;
  }
  state.user = state.status.user || null;
  document.documentElement.dataset.accent = state.status.accent || "violet";
  document.documentElement.dataset.theme = state.status.theme || "navy";
  const s = state.status;
  if (!s.hasAdmin || (!s.setupComplete && isAdmin())) return renderWizard();
  if (!state.user) return renderLogin();
  if (!s.setupComplete) return renderNotReady();
  if (state.user.mustChangePassword) return renderChoosePassword();
  renderShell();
  route();
}

// Keyboard: "/" jumps to search from anywhere.
document.addEventListener("keydown", (e) => {
  if (e.key !== "/" || e.ctrlKey || e.metaKey || e.altKey) return;
  const t = e.target;
  if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable)) return;
  e.preventDefault();
  const box = qs("#hq");
  if (box) box.focus(); else location.hash = "#/discover?focus=1";
});

window.addEventListener("hashchange", () => { if (state.user && state.status && state.status.setupComplete) route(); });

function route() {
  if (!qs("main")) renderShell();
  qsa(".modal-bg").forEach((m) => m.remove()); // a pop-up never outlives its page
  clearTimers();
  setBackdrop(null);
  window.scrollTo(0, 0);
  const h = location.hash.replace(/^#\/?/, "");
  const [path, query] = h.split("?");
  const parts = path.split("/").filter(Boolean);
  const params = new URLSearchParams(query || "");
  const page = parts[0] || "discover";
  qsa(".nav a.item").forEach((a) => a.classList.toggle("active", a.dataset.page === page || ((page === "title" || page === "browse" || page === "search" || page === "collection") && a.dataset.page === "discover")));
  const main = qs("main");
  const pages = {
    discover: () => pageDiscover(main, params.get("q") || "", params.get("focus") === "1"),
    search: () => pageDiscover(main, params.get("q") || "", true),
    browse: () => pageBrowse(main, decodeURIComponent(parts.slice(1).join("/"))),
    collection: () => pageCollection(main, parts[1]),
    title: () => pageTitle(main, parts[1], parts[2]),
    requests: () => pageRequests(main, params.get("filter")),
    users: () => (isAdmin() ? pageUsers(main) : pageDiscover(main)),
    settings: () => (isAdmin() ? pageSettings(main, parts[1] || "general") : pageDiscover(main)),
    profile: () => pageProfile(main),
  };
  (pages[page] || pages.discover)();
}

function renderShell() {
  const u = state.user;
  const admin = isAdmin();
  $app.innerHTML = `
  <div class="shell">
    <nav class="nav" aria-label="Main">
      <a href="#/discover" class="brand" aria-label="Encorr home">${LOCKUP(30)}</a>
      <a class="item" data-page="discover" href="#/discover"${tip("What's popular, trending and coming soon")}>${I.discover}<span class="label">Home</span></a>
      <a class="item" data-page="requests" href="#/requests"${tip("Everything asked for, and where it's up to")}>${I.requests}<span class="label">${admin ? "Requests" : "My requests"}</span><span class="badge" id="pendingBadge" hidden title="Waiting for your approval"></span></a>
      ${admin ? `<a class="item" data-page="users" href="#/users"${tip("Make accounts and choose what people can do")}>${I.users}<span class="label">People</span></a>
      <a class="item" data-page="settings" href="#/settings"${tip("Plex, Radarr, Sonarr, SABnzbd, Discord and more")}>${I.settings}<span class="label">Settings</span></a>` : ""}
      <div class="spacer"></div>
      <a href="#/profile" class="item me" data-page="profile">
        ${u.thumb ? `<img class="avatar" src="${esc(u.thumb)}" alt="">` : `<span class="avatar initial">${esc(u.username.slice(0, 1).toUpperCase())}</span>`}
        <span class="label"><span class="who">${esc(u.username)}</span><span class="role">${{ admin: "Owner", trusted: "Trusted", friend: "Friend" }[u.role] || ""}</span></span>
      </a>
    </nav>
    <main id="main"></main>
  </div>`;
  clearInterval(state.pendingTimer);
  refreshPending(); state.pendingTimer = setInterval(refreshPending, 60000);
}

// The badge on Requests: things needing the owner, or for friends, requests
// that have arrived since they last looked.
async function refreshPending() {
  try {
    const r = await api("/requests?filter=upcoming");
    const g = r.groups || {};
    const n = isAdmin() ? g.needs || 0 : r.newReady || 0;
    const b = qs("#pendingBadge");
    if (b) { b.hidden = n === 0; b.textContent = n; b.title = isAdmin() ? "Needs you" : "Ready since you last looked"; b.classList.toggle("good", !isAdmin()); }
  } catch { /* ignore */ }
}

// ---------- sign-in ----------
async function plexSignIn(btn, after) {
  const popup = window.open("about:blank", "plexauth", "width=620,height=720");
  busy(btn, true, "Waiting for Plex…");
  try {
    const pin = await api("/auth/plex/pin", { method: "POST", body: {} });
    if (popup) popup.location.href = pin.url;
    else window.open(pin.url, "_blank");
    const started = Date.now();
    while (Date.now() - started < 180000) {
      await new Promise((r) => setTimeout(r, 1500));
      const res = await api("/auth/plex/check", { method: "POST", body: { id: pin.id }, allow401: true });
      if (res.done) {
        if (popup && !popup.closed) popup.close();
        toast("Signed in with Plex", "ok");
        await after();
        return;
      }
      if (popup && popup.closed && Date.now() - started > 4000) {
        // One last check in case they approved just before closing.
        const last = await api("/auth/plex/check", { method: "POST", body: { id: pin.id }, allow401: true });
        if (last.done) { toast("Signed in with Plex", "ok"); await after(); return; }
        throw new Error("The Plex window was closed before signing in.");
      }
    }
    throw new Error("Plex sign-in timed out. Please try again.");
  } catch (e) {
    if (popup && !popup.closed) popup.close();
    toast(e.message, "error");
  } finally {
    busy(btn, false);
  }
}

function renderLogin() {
  setBackdrop(null);
  const s = state.status;
  $app.innerHTML = `
  <div class="center"><div class="glass wizard login">
    <h1 class="brandhead">${LOCKUP(64)}</h1>
    <div class="tag">Request the encore.</div>
    <form id="pwForm" class="signin-form" autocomplete="on">
      <label class="field"><span>Username</span><input type="text" id="lu" name="username" autocomplete="username" autocapitalize="none" spellcheck="false" required></label>
      <label class="field"><span>Password</span><input type="password" id="lp" name="password" autocomplete="current-password" required></label>
      <div id="lerr"></div>
      <button class="btn primary wide" type="submit" id="lgo">Sign in</button>
    </form>
    <div class="or"><span>or</span></div>
    <button class="btn plex wide" id="signin">${I.plex} Sign in with Plex</button>
    <p class="small dim" style="margin-top:18px">No account yet? Ask ${s.serverName ? `the owner of ${esc(s.serverName)}` : "the owner"} to make you one.</p>
  </div></div>`;
  qs("#signin").addEventListener("click", (e) => plexSignIn(e.currentTarget, boot));
  qs("#pwForm").addEventListener("submit", async (e) => {
    e.preventDefault();
    const btn = qs("#lgo");
    busy(btn, true, "Signing in…");
    qs("#lerr").innerHTML = "";
    try {
      await api("/auth/login", { method: "POST", body: { username: qs("#lu").value.trim(), password: qs("#lp").value }, allow401: true }).then((r) => { if (r && r.error) throw new Error(r.error); });
      await boot();
    } catch (err) {
      qs("#lerr").innerHTML = `<div class="notice error">${esc(err.message)}</div>`;
      busy(btn, false);
    }
  });
  qs("#lu").focus();
}

// After the owner makes or resets an account, the person picks their own password.
function renderChoosePassword() {
  setBackdrop(null);
  $app.innerHTML = `<div class="center"><div class="glass wizard login">
    <div class="brandhead">${LOCKUP(48)}</div>
    <h1>Welcome, ${esc(state.user.username)}</h1>
    <p class="muted">Choose your own password. Only you will know it.</p>
    <form id="cpForm" class="signin-form">
      <input type="text" name="username" autocomplete="username" value="${esc(state.user.username)}" hidden>
      <label class="field"><span>New password</span><input type="password" id="np1" autocomplete="new-password" minlength="8" required><small>At least 8 characters.</small></label>
      <label class="field"><span>Type it again</span><input type="password" id="np2" autocomplete="new-password" minlength="8" required></label>
      <div id="cperr"></div>
      <button class="btn primary wide" type="submit" id="cpgo">Save and continue</button>
    </form>
    <button class="btn sm" id="out" style="margin-top:14px">Sign out</button>
  </div></div>`;
  qs("#np1").focus();
  qs("#out").addEventListener("click", signOut);
  qs("#cpForm").addEventListener("submit", async (e) => {
    e.preventDefault();
    const a = qs("#np1").value, b = qs("#np2").value;
    if (a !== b) { qs("#cperr").innerHTML = `<div class="notice error">The two passwords don't match.</div>`; return; }
    busy(qs("#cpgo"), true, "Saving…");
    try { await api("/me/password", { method: "POST", body: { new: a } }); toast("Password saved", "ok"); await boot(); }
    catch (err) { qs("#cperr").innerHTML = `<div class="notice error">${esc(err.message)}</div>`; busy(qs("#cpgo"), false); }
  });
}

function renderNotReady() {
  $app.innerHTML = `<div class="center"><div class="glass wizard login"><div class="brandhead">${LOCKUP(48)}</div><h1>Almost there</h1><p class="muted">The server owner is still setting Encorr up. Check back soon.</p><button class="btn" id="out">Sign out</button></div></div>`;
  qs("#out").addEventListener("click", signOut);
}

async function signOut() {
  await api("/auth/logout", { method: "POST", body: {} }).catch(() => {});
  state.user = null;
  location.hash = "";
  boot();
}

// ---------- cards & rows ----------
function card(c) {
  const st = c.status && c.status !== "none" ? `<div class="flag">${pill(c.status === "available" ? "available" : c.status)}</div>` : "";
  const p = img(c.poster);
  const rating = c.rating ? `<span class="crate">${c.rating.toFixed(1)}</span>` : "";
  const quick = (!c.status || c.status === "none") ? `<span class="qadd" role="button" tabindex="0" data-qreq="${esc(c.type)}:${esc(c.id)}" aria-label="Request ${esc(c.title)}"${tip(c.type === "tv" ? "Request: pick the seasons" : "Request this film")}>${I.plus}</span>` : "";
  return `<a class="card" href="#/title/${esc(c.type)}/${esc(c.id)}" title="${esc(c.title)}">
    <div class="poster">${p ? `<img loading="lazy" src="${esc(p)}" alt="">` : `<div class="noimg">${esc(c.title)}</div>`}
      ${c.rating ? `<span class="rchip">${c.rating.toFixed(1)}</span>` : ""}${st}${quick}
      <div class="peek"><div class="pt">${esc(c.title)}</div><div class="pm">${esc(c.year || "")} ${rating}</div>${c.overview ? `<div class="po">${esc(c.overview)}</div>` : ""}<span class="pgo">${c.status === "available" ? "In your Plex" : "View"} ${I.chev}</span></div></div>
    <div class="meta"><div class="t">${esc(c.title)}</div><div class="y">${[c.year, c.type === "tv" ? "TV" : "Film"].filter(Boolean).map(esc).join(" · ")}</div></div></a>`;
}

const skeletonRow = () => `<div class="scroller">${Array.from({ length: 8 }, () => `<div><div class="skeleton" style="aspect-ratio:2/3"></div></div>`).join("")}</div>`;

async function pageDiscover(main, q = "", focus = false) {
  const hr = new Date().getHours();
  const hello = hr < 12 ? "Morning" : hr < 18 ? "Afternoon" : "Evening";
  let seenHow = false;
  try { seenHow = localStorage.getItem("encorr.how") === "1"; } catch (_) {}
  main.innerHTML = `
    <section class="welcome">
      <h1>${hello}, ${esc(state.user.username)}</h1><p class="muted lead">What do you fancy watching?</p>
      <form class="searchbar big" id="homeSearch" role="search">${I.search}<input type="search" id="hq" placeholder="Search for a film or TV show" autocomplete="off" aria-label="Search for a film or TV show" value="${esc(q)}"><button class="btn primary" type="submit">Search</button></form>
      ${seenHow ? "" : `<ol class="how glass" id="how">
        <li><span class="n">1</span><div><strong>Find it</strong><span>Search, or browse the rows below. Tap a heading to see everything in it.</span></div></li>
        <li><span class="n">2</span><div><strong>Request it</strong><span>Press Request. That's all you do.</span></div></li>
        <li><span class="n">3</span><div><strong>Watch it in Plex</strong><span>It downloads by itself and we tell you when it's ready.</span></div></li>
        <button class="x" id="howClose" aria-label="Hide these tips" title="Got it">${I.check} Got it</button>
      </ol>`}
    </section>
    <div id="libsync"></div>
    <div id="results" hidden></div>
    <div id="browse">
      <div id="hero" class="hero skeleton"></div>
      <div id="rows">${Array.from({ length: 4 }, () => `<div class="shelf"><h2 class="skeleton" style="width:220px;height:26px"></h2>${skeletonRow()}</div>`).join("")}</div>
    </div>`;
  const hc = qs("#howClose");
  if (hc) hc.addEventListener("click", () => { try { localStorage.setItem("encorr.how", "1"); } catch (_) {} qs("#how").remove(); });

  // --- search, right here on the home screen ---
  const input = qs("#hq");
  let timer, lastQ = null;
  const showSearch = async (val) => {
    val = val.trim();
    if (val === lastQ) return;
    lastQ = val;
    history.replaceState(null, "", "#/discover" + (val ? "?q=" + encodeURIComponent(val) : ""));
    const res = qs("#results"), browse = qs("#browse");
    if (!val) { res.hidden = true; res.innerHTML = ""; browse.hidden = false; return; }
    browse.hidden = true;
    res.hidden = false;
    const sk = `<div class="grid">${Array.from({ length: 6 }, () => `<div class="skeleton" style="aspect-ratio:2/3"></div>`).join("")}</div>`;
    res.innerHTML = `<h2>Films</h2>${sk}<h2>TV shows</h2>${sk}`;
    try {
      const r = await api("/search?q=" + encodeURIComponent(val));
      if (lastQ !== val) return;
      if (!r.movies.length && !r.tv.length) {
        res.innerHTML = `<div class="empty"><div class="big"></div>Nothing found for “${esc(val)}”. Check the spelling, or try fewer words.</div>`;
        return;
      }
      const block = (title, list, kind) => `<section class="results-block"><div class="results-head"><h2>${title} <span class="count">${list.length}${list.length >= 20 ? "+" : ""}</span></h2></div>
        ${list.length ? `<div class="grid">${list.map(card).join("")}</div>` : `<div class="empty small">No ${kind} called “${esc(val)}”.</div>`}</section>`;
      // Films first, unless only a show matches the title exactly.
      const exact = (l) => l.length && l[0].title.toLowerCase() === val.toLowerCase();
      const filmsFirst = r.movies.length && !(exact(r.tv) && !exact(r.movies));
      const films = block("Films", r.movies, "films"), tv = block("TV shows", r.tv, "shows");
      res.innerHTML = `<div class="search-jump">${r.movies.length ? `<a href="#" data-jump="0">Films (${r.movies.length})</a>` : ""}${r.tv.length ? `<a href="#" data-jump="1">TV shows (${r.tv.length})</a>` : ""}</div>` + (filmsFirst ? films + tv : tv + films);
      qsa("[data-jump]", res).forEach((a) => a.addEventListener("click", (e) => {
        e.preventDefault();
        const want = a.dataset.jump === "0" ? "Films" : "TV shows";
        const h = qsa(".results-block h2", res).find((x) => x.textContent.startsWith(want));
        if (h) h.scrollIntoView({ behavior: "smooth", block: "start" });
      }));
    } catch (e) {
      res.innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
    }
  };
  input.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(() => showSearch(input.value), 350); });
  qs("#homeSearch").addEventListener("submit", (e) => { e.preventDefault(); clearTimeout(timer); showSearch(input.value); });
  if (focus || q) input.focus();
  if (q) showSearch(q);

  // --- "reading your Plex library" banner (first sync, or a re-sync) ---
  const syncBanner = async () => {
    const box = qs("#libsync");
    if (!box) return;
    let st;
    try { st = await api("/status"); } catch (_) { return; }
    const L = st.library || {};
    if (!L.running && L.synced) { box.innerHTML = ""; return; }
    box.innerHTML = `<div class="glass libsync">${L.running ? `<span class="spinner sm"></span>` : ""}<div><strong>${L.running ? "Reading your Plex library…" : "Encorr hasn't read your whole Plex library yet"}</strong>
      <span class="small dim">${L.films || L.shows ? `Found ${L.films} films and ${L.shows} shows so far. ` : ""}${L.running ? "Anything you already have will show “In Plex” once it's done." : "It'll try again shortly."}</span></div></div>`;
    if (L.running) state.timers.push(setTimeout(syncBanner, 4000));
  };
  syncBanner();

  // --- rows ---
  let rows;
  try { rows = (await api("/discover-rows")).rows; } catch (e) { qs("#rows").innerHTML = `<div class="notice error">${esc(e.message)}</div>`; qs("#hero").remove(); return; }
  const box = qs("#rows");
  if (!box) return;
  box.innerHTML = rows.map((r, i) => `<section class="shelf">
      <a class="shelf-head" href="#/browse/${encodeURIComponent(r.key)}"${tip("See everything in " + r.title)}><h2><span class="ri" aria-hidden="true">${rowIcon(r.key)}</span>${esc(r.title)}</h2><span class="see">See all ${I.chev}</span></a>
      <div id="row-${i}">${skeletonRow()}</div></section>`).join("");
  const load = async (r, i) => {
    const el = qs("#row-" + i);
    try {
      const d = await api("/discover/" + encodeURIComponent(r.key));
      if (!el) return [];
      if (!d.results.length) { el.closest(".shelf").remove(); return []; }
      el.innerHTML = `<div class="scroller">${d.results.map(card).join("")}<a class="card more" href="#/browse/${encodeURIComponent(r.key)}"><div class="poster"><div class="noimg">See all<br>${I.chev}</div></div></a></div>`;
      return d.results;
    } catch (e) {
      if (el) el.innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
      return [];
    }
  };
  const [trending] = await Promise.all(rows.map(load));
  const slides = (trending || []).filter((t) => t.backdrop).slice(0, 5);
  const h = qs("#hero");
  if (!h) return;
  if (!slides.length) { h.remove(); return; }
  h.classList.remove("skeleton");
  h.classList.add("carousel");
  const action = (x) => x.status === "available" ? `<span class="have">${MARK(18)} In your Plex library</span>`
    : ["processing", "pending"].includes(x.status) ? `<span class="have req">${I.check} Requested</span>`
    : `<a class="btn primary" href="#/title/${esc(x.type)}/${esc(x.id)}">${I.plus} Request</a>`;
  h.innerHTML = slides.map((x, i) => `<div class="slide ${i === 0 ? "on" : ""}" style="background-image:url('${esc(img(x.backdrop, "w1280"))}')" aria-hidden="${i === 0 ? "false" : "true"}">
      <div class="inner">
        ${x.status === "available" ? `<div class="qchips"><span class="qchip plex">In your Plex</span></div>` : ""}<div class="eyebrow"><span class="ech">Trending this week</span><span>${x.type === "tv" ? "TV show" : "Film"}</span></div>
        <h1>${esc(x.title)}</h1>
        <div class="hmeta">${x.rating ? `<span class="crate">${x.rating.toFixed(1)}</span>` : ""}${x.year ? `<span>${esc(x.year)}</span>` : ""}</div>
        <p>${esc(x.overview)}</p>
        <div class="row">${action(x)}<a class="btn" href="#/title/${esc(x.type)}/${esc(x.id)}">More info</a></div>
      </div></div>`).join("") +
    (slides.length > 1 ? `<div class="dots" role="tablist">${slides.map((x, i) => `<button class="${i === 0 ? "on" : ""}" data-i="${i}" aria-label="Show ${esc(x.title)}"></button>`).join("")}</div>` : "");
  let at = 0;
  const show = (i) => {
    at = (i + slides.length) % slides.length;
    qsa(".slide", h).forEach((el, j) => { el.classList.toggle("on", j === at); el.setAttribute("aria-hidden", j === at ? "false" : "true"); });
    qsa(".dots button", h).forEach((el, j) => el.classList.toggle("on", j === at));
    if (qs("#results").hidden) setBackdrop(slides[at].backdrop);
  };
  qsa(".dots button", h).forEach((b) => b.addEventListener("click", () => { show(Number(b.dataset.i)); restart(); }));
  let paused = false;
  h.addEventListener("mouseenter", () => (paused = true));
  h.addEventListener("mouseleave", () => (paused = false));
  let spin;
  const restart = () => { clearInterval(spin); if (slides.length > 1 && !window.matchMedia("(prefers-reduced-motion: reduce)").matches) { spin = setInterval(() => { if (!paused && !document.hidden) show(at + 1); }, 8000); state.timers.push(spin); } };
  show(0);
  restart();
}

// ---------- "see all" for one row ----------
async function pageBrowse(main, key) {
  main.innerHTML = `
    <div class="page-head"><div><a class="back" href="#/discover">‹ Home</a><h1 id="btitle">&nbsp;</h1><div class="muted" id="bsub">Everything in this list. Keep scrolling for more.</div></div></div>
    <div class="grid" id="bgrid"></div>
    <div id="bmore" class="more-loader"><span class="spinner"></span></div>`;
  const grid = qs("#bgrid"), more = qs("#bmore");
  let page = 0, pages = 1, loading = false;
  const next = async () => {
    if (loading || page >= pages) return;
    loading = true;
    try {
      const d = await api(`/discover/${encodeURIComponent(key)}?page=${page + 1}`);
      if (!qs("#bgrid")) return;
      page++;
      pages = d.totalPages || 1;
      if (d.title) { qs("#btitle").textContent = d.title; document.title = d.title.replace(/^\S+\s/, "") + " · Encorr"; }
      grid.insertAdjacentHTML("beforeend", d.results.map(card).join(""));
      if (page >= pages || !d.results.length) more.innerHTML = page > 1 || d.results.length ? `<span class="dim">That's everything </span>` : `<div class="empty">Nothing here right now.</div>`;
    } catch (e) {
      more.innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
      pages = 0;
    } finally { loading = false; }
    // If the first page doesn't fill the screen, keep going.
    if (page < pages && more.getBoundingClientRect().top < window.innerHeight + 400) next();
  };
  const io = new IntersectionObserver((es) => { if (es.some((e) => e.isIntersecting)) next(); }, { rootMargin: "600px" });
  io.observe(more);
  const stop = () => { io.disconnect(); window.removeEventListener("hashchange", stop); };
  window.addEventListener("hashchange", stop);
  next();
}

// ---------- a film series ----------
async function pageCollection(main, id) {
  main.innerHTML = `<div class="hero skeleton"></div><div class="grid" style="margin-top:24px">${Array.from({ length: 6 }, () => `<div class="skeleton" style="aspect-ratio:2/3"></div>`).join("")}</div>`;
  let c;
  try { c = await api("/collection/" + encodeURIComponent(id)); } catch (e) { main.innerHTML = `<div class="notice error">${esc(e.message)}</div>`; return; }
  const have = c.parts.filter((p) => p.status === "available").length;
  const asked = c.parts.filter((p) => ["pending", "processing", "approved", "partial"].includes(p.status)).length;
  const upcoming = c.parts.filter((p) => !p.date).length;
  setBackdrop(c.backdrop);
  const summary = [`${c.parts.length} films`, have ? `${have} in your Plex` : "", asked ? `${asked} on the way` : "", upcoming ? `${upcoming} not dated yet` : ""].filter(Boolean).join(" · ");
  const canAsk = c.missing > 0;
  const who = isAdmin() || state.user.role === "trusted" ? "They'll start downloading straight away." : "They'll go to the owner to approve.";
  main.innerHTML = `
    <a class="back" href="#/discover">‹ Home</a>
    <section class="hero col-hero" style="background-image:url('${esc(img(c.backdrop, "w1280"))}')"><div class="inner">
      <div class="eyebrow">Film series</div>
      <h1>${esc(c.name)}</h1>
      <div class="facts">${esc(summary)}</div>
      ${c.overview ? `<p>${esc(c.overview)}</p>` : ""}
      <div class="row">
        ${canAsk ? `<button class="btn primary" id="reqAll"${tip(who)}>${I.plus} Request the ${c.missing === c.parts.length ? "whole series" : c.missing + " you haven't got"}</button>` : `<span class="have">${MARK(18)} ${have === c.parts.length ? "You've got the whole series" : "Everything that's out is in Plex or on its way"}</span>`}
      </div></div></section>
    <h2>In release order</h2>
    <div class="grid">${c.parts.map(card).join("")}</div>`;
  const b = qs("#reqAll");
  if (b) b.addEventListener("click", async () => {
    if (!confirmBox(`Request ${c.missing} film${c.missing === 1 ? "" : "s"} from ${c.name}? ${who}`)) return;
    busy(b, true, "Requesting…");
    try {
      const r = await api(`/collection/${encodeURIComponent(id)}/request`, { method: "POST", body: {} });
      toast(`Requested ${r.requested} film${r.requested === 1 ? "" : "s"}${r.failed ? ` (${r.failed} couldn't be added)` : ""}`, "ok");
      pageCollection(main, id);
    } catch (e) { toast(e.message, "error"); busy(b, false); }
  });
}

// ---------- title page ----------
// ---------- the journey ----------
// Every request walks the same four steps. Showing them as one line answers
// "where's my film?" at a glance, for anyone.
const JOURNEY = ["Requested", "Approved", "Downloading", "In Plex"];
const STAGE_TEXT = { Downloading: "Downloading", Queued: "Queued for download", Paused: "Download paused", Unpacking: "Unpacking", Importing: "Moving it into your library", "Waiting for Plex": "Downloaded. Waiting for Plex to add it", Stuck: "Download is stuck", Failed: "Download failed" };
const STEP_TIPS = ["You asked for it", "The owner said yes (trusted people and owners skip this)", "Radarr or Sonarr finds a copy and SABnzbd downloads it", "It's in Plex, ready to watch"];

// status: pending | approved | processing | declined | failed | completed | available
function journeyState(status, p, part) {
  switch (status) {
    case "pending": return { at: 1, text: "Waiting for the owner to approve it" };
    case "declined": return { at: 1, bad: true, text: "The owner declined this request" };
    case "failed": return { at: 2, bad: true, text: "Couldn't start the download. The owner can retry it" };
    case "completed": case "available": return { at: 4, text: "Ready to watch in Plex" };
  }
  const r = part || {};
  if (!p && r.state === "waiting") {
    const when = r.release ? `Out to watch at home ${niceDate(r.release)}` : r.nextAiring ? `Next episode ${niceDate(r.nextAiring)}` : "Not released yet";
    return { at: 2, text: `${when}`, bits: ["it'll download by itself when it's out"], fill: 0, wait: true };
  }
  if (!p && r.state === "finishing") return { at: 3, text: "All there. Waiting for Plex to confirm", busy: true };
  if (!p && r.want > 0 && r.have > 0) {
    const left = r.want - r.have;
    return { at: 2, text: `${r.have} of ${r.want} episodes in Plex`, bits: [`looking for the other ${left}`], fill: Math.round((r.have / r.want) * 100) };
  }
  if (!p) return { at: 2, text: "Looking for a copy to download", fill: 0 };
  const stage = p.stage || "Downloading";
  const text = STAGE_TEXT[stage] || stage;
  if (["Unpacking", "Importing", "Waiting for Plex"].includes(stage)) return { at: 3, text, busy: true };
  const bits = [];
  if (stage !== "Stuck" && stage !== "Failed") bits.push(`${p.percent}%`);
  if (p.timeLeft) bits.push(`${p.timeLeft} left`);
  if (p.speed) bits.push(p.speed);
  if (p.blocked) return { at: 3, text: "Downloaded, but needs a hand to import", bad: true, note: p.note };
  return { at: 2, text, bits, fill: p.percent, bad: stage === "Stuck" || stage === "Failed", note: p.note };
}

function journeyHTML(status, p, compact, part) {
  const j = journeyState(status, p, part);
  const steps = JOURNEY.map((label, i) => {
    const cls = i < j.at ? "done" : i === j.at ? (j.bad ? "now bad" : "now") : "";
    // The line after a step fills as that step finishes; while downloading it
    // fills with the download itself.
    const fill = i < j.at ? 100 : i === j.at && j.fill != null ? j.fill : 0;
    const node = i < j.at ? (i === 3 ? MARK(compact ? 14 : 20) : I.check) : i === j.at && j.bad ? "!" : "";
    return `<li class="${cls}" style="--f:${fill}%"${tip(STEP_TIPS[i])} tabindex="0"><span class="node">${node}</span><span class="lab">${label}</span></li>`;
  }).join("");
  const done = j.at >= 4;
  const detail = `<div class="jtext ${j.bad ? "bad" : done ? "good" : ""}"><strong>${esc(j.text)}</strong>${j.bits && j.bits.length ? `<span>${j.bits.map(esc).join(", ")}</span>` : ""}${j.busy ? `<span class="spinner sm" aria-hidden="true"></span>` : ""}</div>${j.note && !compact ? `<div class="small dim">${esc(j.note)}</div>` : ""}`;
  return `<div class="journey ${compact ? "compact" : "glass"} ${done ? "complete" : ""}" ${compact ? "" : 'id="journey"'} role="group" aria-label="Request progress: ${esc(j.text)}"><ol>${steps}</ol>${detail}</div>`;
}

// "4K HDR10" + "5.1" -> chips. Only shown when Plex told us what you have.
function qchipsHTML(P) {
  if (!P || !(P.quality || P.audio)) return "";
  const bits = String(P.quality || "").split(/\s+/).filter(Boolean);
  if (P.audio) bits.push(P.audio);
  return `<div class="qchips">${bits.map((b) => `<span class="qchip ${/^4k$/i.test(b) ? "k4" : ""}">${esc(b.toUpperCase() === "4K" ? "4K UHD" : b)}</span>`).join("")}</div>`;
}

async function pageTitle(main, type, id) {
  main.innerHTML = `<div class="title-top"><div class="poster skeleton"></div><div><div class="skeleton" style="height:40px;width:60%"></div><div class="skeleton" style="height:16px;width:40%;margin-top:14px"></div><div class="skeleton" style="height:90px;margin-top:20px"></div></div></div>`;
  let data;
  try {
    data = await api(`/title/${encodeURIComponent(type)}/${encodeURIComponent(id)}`);
  } catch (e) {
    main.innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
    return;
  }
  const d = data.details, s = data.status;
  setBackdrop(d.backdrop);
  const runtime = d.runtime ? (type === "movie" ? `${Math.floor(d.runtime / 60)}h ${d.runtime % 60}m` : `${d.runtime} min episodes`) : "";

  const seasons = d.seasons || [];
  const seasonState = (n) => (s.seasons || {})[n] || "";
  const requestable = type === "movie"
    ? !["available", "processing", "pending"].includes(s.status)
    : seasons.some((x) => !["available", "processing", "pending"].includes(seasonState(x.season_number)));

  let statusLine = "";
  if (s.status === "available") statusLine = `<span class="have">${MARK(18)} In your Plex library</span>`;
  else if (s.status === "partial") statusLine = pill("partial");

  const mine = s.myRequest;
  const cancelBtn = mine && mine.status === "pending" ? `<button class="btn sm danger" id="cancelReq">Cancel my request</button>` : "";
  // Show the journey while it's on its way (or once it's arrived, for whoever asked).
  const jStatus = (st) => (st.myRequest && st.myRequest.status) || (st.status === "processing" ? "approved" : st.status);
  const showJourney = (st) => st.myRequest || st.status === "processing" || st.status === "pending";
  const prog = showJourney(s) ? journeyHTML(jStatus(s), s.progress) : "";

  const P = data.plex || null;
  const money = (n) => n ? new Intl.NumberFormat("en-GB", { style: "currency", currency: "USD", maximumFractionDigits: 0 }).format(n) : "";
  const longDate = (x) => { const t = new Date(x); return isNaN(t) ? x : t.toLocaleDateString(undefined, { day: "numeric", month: "long", year: "numeric" }); };
  const genreKind = type === "tv" ? "tv" : "movie";
  const genres = (d.genreList && d.genreList.length ? d.genreList : (d.genres || []).map((g) => ({ name: g })))
    .map((g) => g.id ? `<a href="#/browse/${encodeURIComponent(`genre:${g.id}:${genreKind}`)}">${esc(g.name)}</a>` : esc(g.name)).join(", ");
  const genresHtml = genres ? `<span class="gl">${genres}</span>` : "";
  const runtimeTxt = d.runtime ? (type === "movie" ? `${Math.floor(d.runtime / 60) ? Math.floor(d.runtime / 60) + "h " : ""}${d.runtime % 60}m` : `${d.runtime} min episodes`) : "";
  const headFacts = [d.cert ? `<span class="cert">${esc(d.cert)}</span>` : "", runtimeTxt ? esc(runtimeTxt) : "", genresHtml].filter(Boolean).join(`<span class="sep">|</span>`);
  const personLink = (p) => `<a href="#/browse/person:${esc(p.id)}"${tip("Films with " + p.name)}>${esc(p.name)}</a>`;

  // Ratings: TMDB always, plus IMDb / Rotten Tomatoes from Plex for things you have.
  const ratings = [];
  if (d.rating) ratings.push(`<span class="rt tmdb"${tip(`TMDB users${d.voteCount ? ` (${d.voteCount.toLocaleString()} votes)` : ""}`)}><b>TMDB</b> ${Math.round(d.rating * 10)}%</span>`);
  for (const r of (P && P.ratings) || []) {
    if (r.source === "tmdb") continue;
    const ic = r.source === "imdb" ? `<b class="imdb">IMDb</b>` : r.source === "rt-critic" ? `<b class="rtb ${r.fresh ? "fresh" : "rotten"}">RT</b>` : `<b class="rtb aud ${r.fresh ? "fresh" : "rotten"}">Audience</b>`;
    ratings.push(`<span class="rt ${esc(r.source)}"${tip(r.label)}>${ic} ${esc(r.value)}</span>`);
  }

  const RELEASE = { cinema: "In cinemas", digital: "Digital", disc: "Disc", tv: "On TV" };
  const facts = [];
  const fact = (k, v) => { if (v) facts.push(`<div class="frow"><span class="k">${k}</span><span class="v">${v}</span></div>`); };
  fact("Status", esc({ "Returning Series": "Still running", "Ended": "Ended", "Canceled": "Cancelled", "In Production": "In production", "Planned": "Planned", "Released": "Released", "Post Production": "Post-production", "Rumored": "Rumoured" }[d.tmdbStatus] || d.tmdbStatus || ""));
  if (d.releases && d.releases.length) fact("Release dates", d.releases.map((r) => `<span class="rel">${RELEASE[r.kind] || esc(r.kind)} <span class="dim">${esc(longDate(r.date))}</span></span>`).join(""));
  else if (type === "movie" && d.date) fact("Released", esc(longDate(d.date)));
  if (type === "tv") {
    fact("First aired", d.firstAir ? esc(longDate(d.firstAir)) : "");
    fact("Last aired", d.lastAir && d.tmdbStatus !== "Returning Series" ? esc(longDate(d.lastAir)) : "");
    fact("Seasons", d.numSeasons ? `${d.numSeasons}${d.numEpisodes ? ` <span class="dim">(${d.numEpisodes} episodes)</span>` : ""}` : "");
    fact("Network", esc(d.network || ""));
  }
  fact("Revenue", money(d.revenue));
  fact("Budget", money(d.budget));
  fact("Original language", esc(d.language || ""));
  fact("Original title", esc(d.originalTitle || ""));
  fact("Country", esc((d.countries || []).join(", ")));
  fact("Studio", esc((d.studios || []).join(", ")));
  if (P && (P.quality || P.audio)) fact("In your Plex", [P.quality, P.audio].filter(Boolean).map((q) => `<span class="qual">${esc(q)}</span>`).join(" "));
  const providers = (d.providers || []).length ? `<div class="fblock"><div class="k">Currently streaming on</div><div class="provs">${d.providers.map((p) => `<a href="${esc(d.providersUrl || "#")}" target="_blank" rel="noopener" title="${esc(p.name)}"><img src="${esc(img(p.logo, "w92"))}" alt="${esc(p.name)}"></a>`).join("")}</div><div class="tiny dim">Streaming info from JustWatch</div></div>` : "";
  const links = [
    `<a href="https://www.themoviedb.org/${type}/${esc(d.id)}" target="_blank" rel="noopener">TMDB</a>`,
    d.imdbId ? `<a href="https://www.imdb.com/title/${esc(d.imdbId)}" target="_blank" rel="noopener">IMDb</a>` : "",
    d.imdbId ? `<a href="https://trakt.tv/search/imdb/${esc(d.imdbId)}" target="_blank" rel="noopener">Trakt</a>` : "",
    type === "movie" ? `<a href="https://letterboxd.com/tmdb/${esc(d.id)}" target="_blank" rel="noopener">Letterboxd</a>` : "",
    type === "tv" && d.tvdbId ? `<a href="https://thetvdb.com/dereferrer/series/${esc(d.tvdbId)}" target="_blank" rel="noopener">TheTVDB</a>` : "",
    d.homepage ? `<a href="${esc(d.homepage)}" target="_blank" rel="noopener">Official site</a>` : "",
  ].filter(Boolean).join("");

  const statusPill = s.status === "available" ? `<span class="tpill ok">${MARK(14)} In your Plex</span>`
    : s.status === "partial" ? `<span class="tpill part">Some seasons in Plex</span>`
    : s.status === "processing" ? `<span class="tpill busy">On its way</span>`
    : s.status === "pending" ? `<span class="tpill wait">Waiting for approval</span>` : "";

  main.innerHTML = `
  <section class="t-hero">
    <div class="t-bd" style="background-image:url('${esc(img(d.backdrop, "w1280"))}')"></div>
    <div class="t-head">
      <div class="poster">${d.poster ? `<img src="${esc(img(d.poster, "w500"))}" alt="">` : ""}</div>
      <div class="t-main">
        ${statusPill}
        <h1>${esc(d.title)} ${d.year ? `<span class="yr">(${esc(d.year)})</span>` : ""}</h1>
        <div class="t-facts">${headFacts}</div>
        ${qchipsHTML(P)}
        <div class="actions">
          ${P && P.url && ["available", "partial"].includes(s.status) ? `<a class="btn plex" href="${esc(P.url)}" target="_blank" rel="noopener"${tip("Open it in Plex")}>${I.play} Play on Plex</a>` : ""}
          ${requestable ? `<button class="btn primary cta" id="reqBtn"${tip(isAdmin() || state.user.role === "trusted" ? "Starts downloading straight away" : "Sends it to the owner to approve")}>${I.plus} ${s.status === "partial" || (type === "tv" && Object.keys(s.seasons || {}).length) ? "Request more" : "Request"}</button>` : ""}
          ${d.trailer ? `<button class="btn" id="trailer"${tip("Watch the trailer here")}>Trailer</button>` : ""}
          ${cancelBtn}
        </div>
      </div>
    </div>
  </section>
  ${prog}
  <div class="t-body">
    <div class="t-left">
      ${d.tagline ? `<p class="tagline">${esc(d.tagline)}</p>` : ""}
      <h2>Overview</h2>
      <p class="overview">${esc(d.overview || "No description yet.")}</p>
      ${(d.crew || []).length ? `<div class="crew">${d.crew.map((g) => `<div><div class="k">${esc(g.job)}</div><div class="v">${g.people.map(personLink).join(", ")}</div></div>`).join("")}</div>` : ""}
      ${mine ? `<div class="small muted">You requested this ${when(mine.created)}${mine.status === "pending" ? ": waiting for approval." : "."}</div>` : s.requestCount ? `<div class="small muted">Requested by ${s.requestCount} ${s.requestCount === 1 ? "person" : "people"}.</div>` : ""}
      ${(d.keywords || []).length ? `<div class="tags">${d.keywords.map((k) => `<a href="#/browse/${encodeURIComponent(`keyword:${k.id}:${genreKind}`)}"${tip("More like this: " + k.name)}>${esc(k.name)}</a>`).join("")}</div>` : ""}
      ${d.collection ? `<a class="glass colbanner" href="#/collection/${esc(d.collection.id)}"${tip("See every film in the series and request the ones you haven't got")}><span class="ic"></span><span><strong>Part of ${esc(d.collection.name)}</strong><span class="small dim">See all the films and request the rest in one go</span></span>${I.chev}</a>` : ""}
    </div>
    <aside class="t-side glass">
      ${ratings.length ? `<div class="ratings">${ratings.join("")}</div>` : ""}
      ${facts.join("")}
      ${providers}
      <div class="fblock"><div class="k">Find out more</div><div class="links">${links}</div></div>
    </aside>
  </div>
  ${(d.cast || []).length ? `<h2>Cast</h2><div class="scroller cast">${d.cast.map((c) => `<a class="person" href="#/browse/person:${esc(c.id)}"${tip("Films with " + c.name)}><div class="ph">${c.photo ? `<img loading="lazy" src="${esc(img(c.photo, "w185"))}" alt="">` : `<span>${esc((c.name || "?").split(" ").map((x) => x[0]).join("").slice(0, 2))}</span>`}</div><div class="n">${esc(c.name)}</div><div class="c">${esc(c.character || "")}</div></a>`).join("")}</div>` : ""}
  ${seasons.length ? `<h2>Seasons</h2><div class="seasons">${seasons.map((x) => {
    const st = seasonState(x.season_number);
    return `<div class="glass season"><div><strong>${esc(x.name)}</strong><div class="small dim">${x.episode_count} episodes${x.air_date ? " · " + esc(x.air_date.slice(0, 4)) : ""}</div></div>${st ? pill(st) : ""}</div>`;
  }).join("")}</div>` : ""}
  ${data.recommended && data.recommended.length ? `<h2>More like this</h2><div class="scroller">${data.recommended.map(card).join("")}</div>` : ""}`;

  const reqBtn = qs("#reqBtn");
  if (reqBtn) reqBtn.addEventListener("click", () => requestModal(type, d, s));
  if (state.autoReq) { state.autoReq = false; if (reqBtn) requestModal(type, d, s); }
  const tr = qs("#trailer");
  if (tr) tr.addEventListener("click", () => modal(`<iframe class="video" src="https://www.youtube-nocookie.com/embed/${encodeURIComponent(d.trailer)}?autoplay=1" allow="autoplay; encrypted-media; picture-in-picture" allowfullscreen title="Trailer"></iframe>`, { wide: true }));
  const cb = qs("#cancelReq");
  if (cb) cb.addEventListener("click", async () => {
    if (!confirmBox("Cancel your request?")) return;
    try { await api(`/requests/${mine.id}`, { method: "DELETE" }); toast("Request cancelled"); route(); } catch (e) { toast(e.message, "error"); }
  });
  // While it's on its way, update just the progress box every few seconds;
  // redraw the page once it lands in Plex.
  if (s.progress || s.status === "processing") {
    const here = location.hash;
    const t = setInterval(async () => {
      if (location.hash !== here) return clearInterval(t);
      try {
        const fresh = await api(`/title/${encodeURIComponent(type)}/${encodeURIComponent(id)}`);
        const fs = fresh.status || {};
        if (fs.status !== s.status) return pageTitle(main, type, id);
        const box = qs("#journey");
        if (box && showJourney(fs)) box.outerHTML = journeyHTML(jStatus(fs), fs.progress);
      } catch (_) {}
    }, 5000);
    state.timers.push(t);
  }
}

function confirmBox(msg) { return window.confirm(msg); }

function requestModal(type, d, s) {
  const seasons = d.seasons || [];
  const st = (n) => (s.seasons || {})[n] || "";
  const free = seasons.filter((x) => !["available", "processing", "pending"].includes(st(x.season_number)));
  const SEASON_STATUS = { available: "In Plex", partial: "Partly in Plex", processing: "On its way", pending: "Waiting for approval" };
  const sw = (attrs) => `<label class="switch sm"><input type="checkbox" ${attrs}><span class="slider"></span></label>`;
  const seasonHtml = type === "tv" ? `
    <div class="stable">
      <div class="shead">${sw(`id="allS" ${free.length ? "checked" : "disabled"}`)}<span>Season</span><span><span class="long">Episodes</span><span class="short">Eps</span></span><span>Status</span></div>
      <div id="sList">${seasons.map((x) => {
        const taken = !free.includes(x), status = st(x.season_number);
        const future = x.air_date && x.air_date > new Date().toISOString().slice(0, 10);
        return `<label class="srow ${taken ? "taken" : ""}">${sw(`value="${x.season_number}" ${taken ? "disabled" : "checked"}`)}<span class="sn">${esc(x.name)}${x.air_date ? ` <span class="dim">${esc(x.air_date.slice(0, 4))}</span>` : ""}</span><span>${x.episode_count || "–"}</span><span>${status ? `<span class="spill st-${esc(status)}">${esc(SEASON_STATUS[status] || status)}</span>` : future ? `<span class="spill future">Not out yet</span>` : `<span class="spill none">Not requested</span>`}</span></label>`;
      }).join("")}</div>
    </div>
    <div class="small dim" id="sCount" style="margin:8px 2px 0"></div>` : "";
  const role = state.user.role;
  const m = modal(`
    <div class="mhead">${d.poster ? `<img src="${esc(img(d.poster, "w185"))}" alt="">` : ""}<div><div class="eyebrow">${type === "tv" ? "Request a show" : "Request a film"}</div><h2>${esc(d.title)} ${d.year ? `<span class="dim" style="font-weight:500">(${esc(d.year)})</span>` : ""}</h2></div></div>
    <div class="notice ${role === "friend" ? "" : "ok"}" style="margin:4px 0 14px">${role === "friend" ? "ℹ️ The owner will approve it, then it downloads by itself." : "This request will be approved automatically."}</div>
    ${seasonHtml}
    <label class="field"><span>Note (optional)</span><input type="text" id="note" maxlength="300" placeholder="e.g. extended cut please"></label>
    <div class="foot"><button class="btn" data-close>Cancel</button><button class="btn primary" id="go">${I.plus} Request the encore</button></div>`);
  const all = qs("#allS", m.el);
  if (all) {
    const boxes = qsa("#sList input:not([disabled])", m.el);
    const eps = (b) => Number((seasons.find((x) => String(x.season_number) === b.value) || {}).episode_count || 0);
    const count = () => {
      const on = boxes.filter((b) => b.checked);
      qs("#sCount", m.el).textContent = on.length ? `${on.length} season${on.length === 1 ? "" : "s"}, ${on.reduce((n, b) => n + eps(b), 0)} episodes` : "Pick the seasons you want";
      qs("#go", m.el).disabled = !on.length;
    };
    all.addEventListener("change", () => { boxes.forEach((b) => (b.checked = all.checked)); count(); });
    boxes.forEach((b) => b.addEventListener("change", () => { all.checked = boxes.every((x) => x.checked); count(); }));
    count();
  }
  qs("#go", m.el).addEventListener("click", async (e) => {
    const body = { type, tmdbId: d.id, note: qs("#note", m.el).value };
    if (type === "tv") {
      body.seasons = qsa("#sList input:checked:not([disabled])", m.el).map((b) => Number(b.value));
      if (!body.seasons.length) { toast("Pick at least one season.", "error"); return; }
    }
    busy(e.currentTarget, true, "Requesting…");
    try {
      const r = await api("/requests", { method: "POST", body });
      m.close();
      toast(r.status === "approved" ? "Encore requested: it's on its way" : "Encore requested: waiting for approval", "ok");
      route();
      refreshPending();
    } catch (err) {
      toast(err.message, "error");
      busy(e.currentTarget, false);
    }
  });
}

// ---------- requests ----------
const STATES = {
  attention: { label: "Needs a hand", tip: "Stuck, failed, or waiting for someone to confirm an import" },
  pending: { label: "Waiting for approval", tip: "The owner hasn't said yes yet" },
  downloading: { label: "Downloading", tip: "Downloading right now" },
  finishing: { label: "Adding to Plex", tip: "Downloaded, being unpacked or added to Plex" },
  searching: { label: "Finding a copy", tip: "Radarr/Sonarr are looking for a copy to download" },
  waiting: { label: "Not out yet", tip: "Not released yet. It'll download by itself when it comes out" },
};

// The filters double as the stat tiles at the top of the page.
const REQ_GROUPS_ADMIN = [
  ["needs", "Needs you", "approvals and problems"],
  ["active", "Active", "finding and downloading"],
  ["upcoming", "Coming soon", "not released yet"],
  ["ready", "Ready", "last 30 days"],
];
const REQ_GROUPS_FRIEND = [
  ["active", "On the way", "waiting or downloading"],
  ["upcoming", "Coming soon", "not released yet"],
  ["ready", "Ready", "last 30 days"],
];
const GROUP_EMPTY = {
  needs: "Nothing needs you right now. Approvals and stuck downloads land here.",
  active: "Nothing on its way right now.",
  upcoming: "Nothing waiting for a release date.",
  ready: "Nothing has arrived in the last 30 days.",
  all: "No requests yet.",
  mine: "You haven't requested anything yet.",
};

// Every request walks these steps.
const STAGES = ["Requested", "Approved", "Finding", "Downloading", "Adding", "Ready"];
const STAGE_TIPS = ["Someone asked for it", "The owner said yes (trusted people and owners skip this)", "Radarr/Sonarr look for a copy to download", "SABnzbd downloads it", "Unpacking it and adding it to Plex", "In Plex, ready to watch"];

function stageOf(r) {
  const p = r.progress || {};
  switch (r.status) {
    case "pending": return { at: 1 };
    case "declined": return { at: 1, bad: true };
    case "failed": return { at: 2, bad: true };
    case "completed": return { at: 6 };
  }
  if (p.blocked) return { at: 4, bad: true };
  switch (r.state) {
    case "downloading": return { at: 3, live: true };
    case "finishing": return { at: 4, live: true };
    case "attention": return { at: 3, bad: true };
    default: return { at: 2, live: r.state === "searching" };
  }
}

function stagesHTML(r) {
  const s = stageOf(r);
  const done = s.at >= 6;
  return `<ol class="rsteps ${done ? "done" : ""}" aria-label="Progress">${STAGES.map((label, i) => {
    const cls = i < s.at ? "past" : i === s.at ? (s.bad ? "now bad" : s.live ? "now live" : "now") : "";
    return `<li class="${cls}"${tip(STAGE_TIPS[i])}><i></i><span>${label}</span></li>`;
  }).join("")}</ol>`;
}

// timeOfDay: "14:30" today, "Thu 14:30" this week, else a date.
function timeOfDay(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return "";
  const t = d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" });
  const days = (d - new Date()) / 86400000;
  if (d.toDateString() === new Date().toDateString()) return t;
  if (days < 6) return d.toLocaleDateString("en-GB", { weekday: "short" }) + " " + t;
  return d.toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

// The one box under the steps that says what's happening and what's next.
function reqInfoHTML(r) {
  const p = r.progress || {};
  const box = (cls, icon, main, sub = "") => `<div class="rinfo ${cls}"><span class="ri-ic">${icon}</span><div><strong>${main}</strong>${sub ? `<span>${sub}</span>` : ""}</div></div>`;
  if (r.status === "pending") return box("wait", I.clock, "Waiting for approval", isAdmin() ? "Approve it to start the download" : "The owner will have a look");
  if (r.status === "declined") return box("bad", I.x, "Declined", esc(r.error || ""));
  if (r.status === "failed") return box("bad", "!", "Couldn't send it to Radarr/Sonarr", esc(r.problem || r.error || ""));
  if (r.status === "completed") {
    const arrived = r.arrived ? `Arrived ${esc(ago(r.arrived))}` : "In Plex";
    return box("good", I.check, "Ready to watch", arrived);
  }
  if (p.blocked) return box("bad", "!", "Downloaded, but stuck before Plex", (r.problem ? esc(r.problem) + ". " : "") + (isAdmin() ? "Import it to finish. Auto-fix tries this by itself too" : "The owner has been told"));
  if (r.state === "attention") return box("bad", "!", p.stage === "Failed" ? "The download failed" : "The download is stuck", (r.problem ? esc(r.problem) + ". " : "") + "Auto-fix swaps it for another copy if it doesn't recover");
  if (r.state === "downloading" && p.percent != null) {
    const pct = Math.max(0, Math.min(100, p.percent));
    const label = p.stage === "Paused" ? "Paused" : p.stage === "Queued" ? "Queued" : `${pct}%${p.speed ? ` · ${esc(p.speed)}` : ""}`;
    return `<div class="rprog"><div class="pline"><span class="hi">${label}</span><span class="eta">${p.timeLeft ? `${esc(p.timeLeft)} left` : esc(STAGE_TEXT[p.stage] || "")}</span></div><div class="pbar"><i style="width:${pct}%;--w:${Math.max(pct, 1)}"></i></div></div>`;
  }
  if (r.state === "finishing") return box("live", `<span class="spinner sm"></span>`, STAGE_TEXT[p.stage] || "Adding it to Plex", "Almost there");
  if (r.state === "waiting") {
    const main = r.release ? `Out to watch at home ${esc(niceDate(r.release))}` : r.nextAiring ? `Next episode ${esc(niceDate(r.nextAiring))}` : "Not released yet";
    return box("wait", I.calendar, main, "It downloads by itself when it's out");
  }
  if (r.state === "searching") {
    const next = r.nextSearch ? `Looks again by itself at ${esc(timeOfDay(r.nextSearch))}` : "Looks again by itself every 12 hours";
    const last = r.lastSearch ? ` · last searched ${esc(ago(r.lastSearch))}` : "";
    const what = r.type === "tv" && r.want > 0 ? `${r.want - (r.have || 0)} episodes still to find` : "Looking for a copy to download";
    return box("wait", I.search, what, next + last);
  }
  return "";
}

// One strip per season: green in Plex, blue downloading, amber still missing.
function seasonStrips(r) {
  const list = r.seasonProgress || [];
  if (r.type !== "tv" || !list.length || r.status !== "approved") return "";
  return `<div class="sstrips">${list.map((x) => {
    if (!x.want) return `<button class="sstrip future" data-eps="${r.id}" data-season="${x.season}"${tip(`Season ${x.season}: not out yet`)}><span class="sl">S${x.season}</span><span class="sbar2"><i class="u" style="flex:1"></i></span><span class="sn">soon</span></button>`;
    const have = x.have, down = x.downloading || 0, miss = Math.max(0, x.want - have - down);
    const seg = (cls, n) => n > 0 ? `<i class="${cls}" style="flex:${n}"></i>` : "";
    return `<button class="sstrip ${have >= x.want ? "full" : ""}" data-eps="${r.id}" data-season="${x.season}"${tip(`Season ${x.season}: ${have} in Plex${down ? `, ${down} downloading` : ""}${miss ? `, ${miss} missing` : ""}. Tap for episodes`)}><span class="sl">S${x.season}</span><span class="sbar2">${seg("h", have)}${seg("d", down)}${seg("m", miss)}</span><span class="sn">${have}/${x.want}</span></button>`;
  }).join("")}</div>`;
}

function reqCard(r) {
  const admin = isAdmin();
  const p = img(r.poster, "w185");
  const pr = r.progress || {};
  const mine = r.requestedBy === state.user.id;
  const btns = [];
  if (admin && r.status === "pending") {
    btns.push(`<button class="btn primary grow" data-act="approve" data-id="${r.id}"${tip("Say yes: it goes straight to Radarr/Sonarr")}>${I.check} Approve</button>`);
    btns.push(`<button class="btn sq danger" data-act="decline" data-id="${r.id}" aria-label="Decline"${tip("Say no: they'll see it was declined")}>${I.x}</button>`);
  }
  if (admin && r.status === "failed") btns.push(`<button class="btn primary grow" data-act="retry" data-id="${r.id}"${tip("Send it to Radarr/Sonarr again")}>Retry</button>`);
  if (admin && pr.blocked) btns.push(`<button class="btn primary grow" data-act="import" data-id="${r.id}"${tip("Confirm the import Radarr/Sonarr are waiting on")}>Import it</button>`);
  if (admin && r.status === "approved" && ["searching", "attention"].includes(r.state) && !pr.blocked) btns.push(`<button class="btn grow" data-act="search" data-id="${r.id}"${tip("Ask Radarr/Sonarr to look for a copy right now")}>${I.search} Search now</button>`);
  if (r.status === "completed" && r.plexUrl) btns.push(`<a class="btn plex grow" href="${esc(r.plexUrl)}" target="_blank" rel="noopener">${I.play} Play on Plex</a>`);
  if (r.type === "tv" && ["approved", "completed"].includes(r.status)) btns.push(`<button class="btn grow" data-eps="${r.id}"${tip("Every episode: in Plex, downloading or missing")}>${I.list} Episodes</button>`);
  btns.push(`<button class="btn sq" data-timeline="${r.id}" aria-label="Timeline"${tip("Everything that's happened to this request")}>${I.clock}</button>`);
  if (admin && r.status !== "pending") btns.push(`<button class="btn sq" data-manage="${r.id}" aria-label="Manage"${tip("Download controls: pause, retry, pick a copy, remove")}>${I.more}</button>`);
  if (!admin && r.status === "pending" && mine) btns.push(`<button class="btn sq" data-act="delete" data-id="${r.id}" aria-label="Cancel"${tip("Cancel your request")}>${I.trash}</button>`);
  if (admin && r.status === "pending") btns.push(`<button class="btn sq" data-act="delete" data-id="${r.id}" aria-label="Delete"${tip("Delete this request")}>${I.trash}</button>`);

  const pill = { pending: ["pending", "Waiting for approval"], declined: ["declined", "Declined"], failed: ["failed", "Needs a hand"], completed: ["done", "Ready"] }[r.status]
    || (pr.blocked ? ["attention", "Needs a hand"] : STATES[r.state] ? [r.state, STATES[r.state].label] : ["approved", "Approved"]);
  const src = { watchlist: "from Plex watchlist", app: "from the app", discord: "from Discord" }[r.source] || "";
  const meta = [r.type === "tv" ? esc(seasonsText(r.seasons)) : "", `requested by ${esc(r.user || "someone")}`, when(r.created), esc(src)].filter(Boolean);
  return `<article class="req2 st-${esc(r.state || r.status)}" data-id="${r.id}">
    <a class="rposter" href="#/title/${esc(r.type)}/${esc(r.tmdbId)}">${p ? `<img src="${esc(p)}" alt="" loading="lazy">` : `<div class="ph"></div>`}<span class="rtype">${r.type === "tv" ? "TV" : "FILM"}</span>${r.new ? `<span class="rnew">New</span>` : ""}</a>
    <div class="rbody">
      <div class="rtop"><a class="t" href="#/title/${esc(r.type)}/${esc(r.tmdbId)}">${esc(r.title)} <span class="yr">${esc(r.year || "")}</span></a><span class="spill2 ${esc(pill[0])}">${esc(pill[1])}</span></div>
      <div class="s">${meta.join(" · ")}</div>
      ${r.note ? `<div class="s note">“${esc(r.note)}”</div>` : ""}
      ${stagesHTML(r)}
      ${reqInfoHTML(r)}
      ${seasonStrips(r)}
      <div class="racts">${btns.join("")}</div>
    </div>
  </article>`;
}

async function pageRequests(main, filter) {
  const admin = isAdmin();
  const groups = admin ? REQ_GROUPS_ADMIN : REQ_GROUPS_FRIEND;
  let chosen = filter;
  main.innerHTML = `
    <div class="page-head"><div><h1>${admin ? "Requests" : "My requests"}</h1><div class="muted">${admin ? "What needs you, what's on its way and what's arrived." : "What you've asked for, and where it's up to."}</div></div>
      <div class="row"><button class="btn sm ghost" data-f="${admin ? "all" : "mine"}">${admin ? "Show everything" : "All mine"}</button>${admin ? "" : `<button class="btn sm ghost" data-f="all">Everyone's</button>`}</div></div>
    <div class="rstats g${groups.length}" id="rstats">${groups.map(([k, l]) => `<button class="rstat ${k}" data-f="${k}"><span class="k"><i></i>${esc(l)}</span><div class="n">–</div><div class="d"></div></button>`).join("")}</div>
    <div id="rlist" class="list">${Array.from({ length: 3 }, () => `<div class="skeleton" style="height:180px;border-radius:24px"></div>`).join("")}</div>`;
  qsa("[data-f]", main).forEach((b) => b.addEventListener("click", () => (location.hash = "#/requests?filter=" + b.dataset.f)));

  const paint = (r) => {
    const g = r.groups || {};
    qsa(".rstat", main).forEach((b) => {
      const k = b.dataset.f, n = g[k] || 0;
      qs(".n", b).textContent = n;
      b.classList.toggle("on", k === chosen);
      b.classList.toggle("zero", n === 0);
      const sub = groups.find((x) => x[0] === k)[2];
      const extra = k === "active" && r.counts && chosen === "active" ? `${r.counts.downloading || 0} downloading` : k === "ready" && r.newReady ? `${r.newReady} new for you` : sub;
      qs(".d", b).textContent = extra;
    });
    qsa(".page-head [data-f]", main).forEach((b) => b.classList.toggle("on", b.dataset.f === chosen));
  };

  const load = async () => {
    const box = qs("#rlist");
    if (!box) return;
    try {
      let r = await api("/requests?filter=" + (chosen || (admin ? "needs" : "active")));
      if (!chosen) {
        chosen = admin ? "needs" : "active";
        if (!r.results.length) {
          const g = r.groups || {};
          const next = groups.map((x) => x[0]).find((k) => g[k] > 0);
          if (next && next !== chosen) { chosen = next; r = await api("/requests?filter=" + chosen); }
        }
      }
      paint(r);
      if (!r.results.length) {
        box.innerHTML = `<div class="empty">${esc(GROUP_EMPTY[chosen] || "Nothing here.")}<div style="margin-top:14px"><a class="btn primary" href="#/discover">Find something to request</a></div></div>`;
        return;
      }
      const older = chosen === "ready" ? `<div class="rmore"><button class="btn sm ghost" id="older">Show older</button></div>` : "";
      box.innerHTML = r.results.map(reqCard).join("") + (state.showOlder ? "" : older);
      const ob = qs("#older");
      if (ob) ob.addEventListener("click", async () => { state.showOlder = true; const all = await api("/requests?filter=ready&older=1"); box.innerHTML = all.results.map(reqCard).join(""); bindReqActions(box, load); });
      bindReqActions(box, load);
      const fast = r.results.some((x) => x.progress && x.state === "downloading");
      if (fast !== state.reqFast) { state.reqFast = fast; restart(); }
    } catch (e) {
      box.innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
    }
  };
  let timer;
  const restart = () => { clearInterval(timer); timer = setInterval(load, state.reqFast ? 5000 : 30000); state.timers.push(timer); };
  state.reqFast = false;
  state.showOlder = false;
  restart();
  await load();
}

function bindReqActions(box, reload) {
  qsa("[data-act]", box).forEach((b) => b.addEventListener("click", async () => {
    const id = b.dataset.id, act = b.dataset.act;
    try {
      if (act === "approve") { busy(b, true, "Approving…"); await api(`/requests/${id}/approve`, { method: "POST", body: {} }); toast("Approved: sent to download", "ok"); }
      if (act === "retry") { busy(b, true, "Retrying…"); await api(`/requests/${id}/retry`, { method: "POST", body: {} }); toast("Sent again"); }
      if (act === "search") { busy(b, true, "Asking…"); const r = await api(`/requests/${id}/search`, { method: "POST", body: {} }); toast(r.message || "Searching now", "ok"); }
      if (act === "import") { busy(b, true, "Importing…"); const r = await api(`/requests/${id}/import`, { method: "POST", body: {} }); toast(r.message || "Importing now", "ok"); }
      if (act === "decline") {
        const reason = window.prompt("Reason for declining (optional, shown to the requester):", "");
        if (reason === null) return;
        await api(`/requests/${id}/decline`, { method: "POST", body: { reason } });
        toast("Declined");
      }
      if (act === "delete") { if (!confirmBox("Remove this request?")) return; await api(`/requests/${id}`, { method: "DELETE" }); toast("Removed"); }
      await reload();
      refreshPending();
    } catch (e) { toast(e.message, "error"); busy(b, false); }
  }));
  qsa("[data-eps]", box).forEach((b) => b.addEventListener("click", () => episodesModal(Number(b.dataset.eps), Number(b.dataset.season || 0), reload)));
  qsa("[data-timeline]", box).forEach((b) => b.addEventListener("click", () => timelineModal(Number(b.dataset.timeline))));
  qsa("[data-manage]", box).forEach((b) => b.addEventListener("click", () => {
    const card = b.closest(".req2");
    manageModal(Number(b.dataset.manage), card ? qs(".t", card).textContent.trim() : "", reload);
  }));
}

// ---------- episodes ----------
const EP_STATUS = { have: ["In Plex", "ok"], downloading: ["Downloading", "dl"], missing: ["Missing", "miss"], unaired: ["Not aired", "un"], skipped: ["Not wanted", "un"] };

async function episodesModal(reqID, season, reload) {
  const m = modal(`<div class="mhead"><h2>Episodes</h2><button class="btn sq ghost" data-close aria-label="Close">${I.x}</button></div><div class="eps"><span class="spinner"></span></div>`, { wide: true, sheet: true });
  let data;
  try { data = await api(`/requests/${reqID}/episodes`); } catch (e) { qs(".eps", m.el).innerHTML = `<div class="notice error">${esc(e.message)}</div>`; return; }
  if (!data.seasons.length) { qs(".eps", m.el).innerHTML = `<p class="muted">${esc(data.message || "No episodes yet.")}</p>`; return; }
  let cur = data.seasons.find((s) => s.season === season) || data.seasons[0];
  const admin = isAdmin();
  const draw = () => {
    const s = cur;
    const tabs = data.seasons.length > 1 ? `<div class="etabs">${data.seasons.map((x) => `<button class="chip ${x.season === s.season ? "on" : ""}" data-s="${x.season}">Season ${x.season} <b>${x.have}/${x.aired || x.total}</b></button>`).join("")}</div>` : "";
    const sum = [`<b>${s.have}</b> in Plex`, s.downloading ? `<b>${s.downloading}</b> downloading` : "", s.missing ? `<b class="miss">${s.missing}</b> missing` : "", s.total > s.aired ? `${s.total - s.aired} still to air` : ""].filter(Boolean).join(" · ");
    const rows = s.episodes.map((e) => {
      const [label, cls] = EP_STATUS[e.status] || [e.status, ""];
      let right = `<span class="estat ${cls}">${label}</span>`;
      if (e.status === "have" && e.quality) right = `<span class="equal">${esc(e.quality)}${e.size ? ` · ${(e.size / 1e9).toFixed(1)} GB` : ""}</span>` + right;
      if (e.status === "downloading") right = `<span class="equal">${esc(e.stage || "")} ${e.percent || 0}%</span>` + right;
      if (e.status === "unaired" && e.air) right = `<span class="equal">${esc(niceDate(e.air))}</span>` + right;
      if (admin && e.status === "missing") right += `<button class="btn sm" data-search-ep="${e.id}"${tip("Search for just this episode")}>${I.search}</button>`;
      return `<div class="erow ${cls}"><span class="en">${e.number}</span><div class="et"><strong>${esc(e.title || "Episode " + e.number)}</strong>${e.air && e.status !== "unaired" ? `<span>${esc(new Date(e.air).toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" }))}</span>` : ""}</div><div class="er">${right}</div>${e.status === "downloading" ? `<span class="eprog" style="width:${e.percent || 0}%"></span>` : ""}</div>`;
    }).join("");
    qs(".eps", m.el).innerHTML = `${tabs}<div class="esum"><span>${sum}</span>${admin && s.missing ? `<button class="btn sm" id="searchSeason">${I.search} Search season ${s.season}</button>` : ""}</div><div class="elist">${rows}</div>`;
    qsa("[data-s]", m.el).forEach((b) => b.addEventListener("click", () => { cur = data.seasons.find((x) => x.season === Number(b.dataset.s)); draw(); }));
    const ss = qs("#searchSeason", m.el);
    if (ss) ss.addEventListener("click", async () => { busy(ss, true, "Asking…"); try { const r = await api(`/requests/${reqID}/search`, { method: "POST", body: { season: s.season } }); toast(r.message, "ok"); } catch (e) { toast(e.message, "error"); } busy(ss, false); });
    qsa("[data-search-ep]", m.el).forEach((b) => b.addEventListener("click", async () => { busy(b, true, ""); try { const r = await api(`/requests/${reqID}/search`, { method: "POST", body: { episodes: [Number(b.dataset.searchEp)] } }); toast(r.message, "ok"); } catch (e) { toast(e.message, "error"); } busy(b, false); }));
  };
  draw();
}

// ---------- timeline ----------
const TL_ICON = { requested: "plus", approved: "check", declined: "x", sent: "send", failed: "warn", ready: "check", retry: "refresh", search: "search", fix: "wrench", action: "hand", grabbed: "download", imported: "check", upgrade: "spark", removed: "trash", warn: "warn" };

async function timelineModal(reqID) {
  const m = modal(`<div class="mhead"><h2>Timeline</h2><button class="btn sq ghost" data-close aria-label="Close">${I.x}</button></div><div class="tl"><span class="spinner"></span></div>`, { wide: true, sheet: true });
  try {
    const r = await api(`/requests/${reqID}/timeline`);
    qs(".tl", m.el).innerHTML = r.items.length ? `<ol class="tlist">${r.items.map((it) => `<li class="k-${esc(it.kind)}"><span class="tdot">${I[TL_ICON[it.kind]] || ""}</span><div><strong>${esc(it.text)}</strong>${it.detail ? `<span class="td">${esc(it.detail)}</span>` : ""}<span class="tt">${esc(exact(it.at))}</span></div></li>`).join("")}</ol>` : `<p class="muted">Nothing yet.</p>`;
  } catch (e) { qs(".tl", m.el).innerHTML = `<div class="notice error">${esc(e.message)}</div>`; }
}

// ---------- manage (admin) ----------
async function manageModal(reqID, title, reload) {
  const r = await api("/requests?filter=all").then((x) => x.results.find((y) => y.id === reqID)).catch(() => null);
  if (!r) return toast("That request has gone", "error");
  const live = r.managed;
  const paused = r.progress && r.progress.stage === "Paused";
  const opt = (act, icon, label, sub, cls = "") => `<button class="mopt ${cls}" data-mact="${act}"><span class="mi">${I[icon]}</span><span><strong>${label}</strong><span>${sub}</span></span></button>`;
  const opts = [];
  if (live) {
    opts.push(paused ? opt("resume", "play", "Resume", "Carry on downloading") : opt("pause", "pause", "Pause", "Hold this download in SABnzbd"));
    opts.push(opt("top", "up", "Move to the front", "Download this before anything else"));
    opts.push(opt("retry", "refresh", "Try another copy", "Throw this one away, mark it as bad and find another"));
    opts.push(opt("stop", "stop", "Stop downloading", "Cancel the download but keep the request"));
  }
  if (r.status === "approved" || r.status === "completed") {
    opts.push(opt("pick", "list", "Pick a copy yourself", "See every copy the indexers have and choose one"));
    if (!live) opts.push(opt("search", "search", "Search now", "Ask Radarr/Sonarr to look again straight away"));
  }
  if (r.progress && r.progress.blocked) opts.push(opt("import", "check", "Import it", "Confirm the import Radarr/Sonarr are waiting on"));
  opts.push(opt("delete", "trash", "Remove request", r.source === "watchlist" ? "Won't be added again from the Plex watchlist. Files stay in Plex" : "Takes it off Encorr's list. Files stay in Plex", "danger"));
  const m = modal(`<div class="mhead"><div><h2>${esc(title || r.title)}</h2><div class="muted small">${esc(STATES[r.state] ? STATES[r.state].label : r.status)}</div></div><button class="btn sq ghost" data-close aria-label="Close">${I.x}</button></div><div class="mopts">${opts.join("")}</div>`);
  qsa("[data-mact]", m.el).forEach((b) => b.addEventListener("click", async () => {
    const act = b.dataset.mact;
    try {
      if (act === "pick") { m.close(); return releasesModal(reqID, r, reload); }
      if (act === "delete") { if (!confirmBox("Remove this request?")) return; await api(`/requests/${reqID}`, { method: "DELETE" }); toast("Removed"); }
      else if (act === "search") { const x = await api(`/requests/${reqID}/search`, { method: "POST", body: {} }); toast(x.message, "ok"); }
      else if (act === "import") { const x = await api(`/requests/${reqID}/import`, { method: "POST", body: {} }); toast(x.message, "ok"); }
      else {
        if ((act === "retry" || act === "stop") && !confirmBox(act === "retry" ? "Throw this download away and find another copy?" : "Stop this download? The request stays, and you can search again later.")) return;
        b.classList.add("busy");
        const x = await api(`/requests/${reqID}/download/${act}`, { method: "POST", body: {} });
        toast(x.message, "ok");
      }
      m.close();
      await reload();
    } catch (e) { toast(e.message, "error"); b.classList.remove("busy"); }
  }));
}

async function releasesModal(reqID, r, reload) {
  const seasons = r.type === "tv" ? (r.seasons || []) : [];
  let season = seasons[0] || 0;
  const m = modal(`<div class="mhead"><div><h2>Pick a copy</h2><div class="muted small">${esc(r.title)}</div></div><button class="btn sq ghost" data-close aria-label="Close">${I.x}</button></div>
    ${seasons.length > 1 ? `<div class="etabs">${seasons.map((s) => `<button class="chip ${s === season ? "on" : ""}" data-rs="${s}">Season ${s}</button>`).join("")}</div>` : ""}
    <div class="rels"></div>`, { wide: true, sheet: true });
  const load = async () => {
    const box = qs(".rels", m.el);
    box.innerHTML = `<div class="rwait"><span class="spinner"></span> Asking every indexer. This can take up to a minute…</div>`;
    try {
      const x = await api(`/requests/${reqID}/releases${season ? "?season=" + season : ""}`);
      if (!x.releases.length) { box.innerHTML = `<p class="muted">No copies found right now. Radarr/Sonarr keep looking by themselves.</p>`; return; }
      box.innerHTML = x.releases.map((rl, i) => `<div class="rel ${rl.approved ? "" : "rej"}">
        <div class="rl"><strong title="${esc(rl.title)}">${esc(rl.title)}</strong>
          <div class="rmeta"><span class="qchip">${esc(rl.quality || "?")}</span><span>${(rl.size / 1e9).toFixed(1)} GB</span><span>${esc(rl.age)} old</span><span>${esc(rl.indexer)}</span>${rl.fullSeason ? `<span>whole season</span>` : ""}${(rl.languages || []).length ? `<span>${esc(rl.languages.join(", "))}</span>` : ""}</div>
          ${rl.rejections && rl.rejections.length ? `<div class="rwhy">${esc(rl.rejections.join(" · "))}</div>` : ""}</div>
        <button class="btn sm ${rl.approved ? "primary" : ""}" data-grab="${i}">${I.download} Get</button></div>`).join("");
      qsa("[data-grab]", box).forEach((b) => b.addEventListener("click", async () => {
        const rl = x.releases[Number(b.dataset.grab)];
        if (!rl.approved && !confirmBox("Radarr/Sonarr wouldn't pick this one by themselves (" + (rl.rejections || []).join(", ") + "). Get it anyway?")) return;
        busy(b, true, "");
        try { const res = await api(`/requests/${reqID}/releases`, { method: "POST", body: { guid: rl.guid, indexerId: rl.indexerId, title: rl.title } }); toast(res.message, "ok"); m.close(); setTimeout(reload, 3000); } catch (e) { toast(e.message, "error"); busy(b, false); }
      }));
    } catch (e) { box.innerHTML = `<div class="notice error">${esc(e.message)}</div>`; }
  };
  qsa("[data-rs]", m.el).forEach((b) => b.addEventListener("click", () => { season = Number(b.dataset.rs); qsa("[data-rs]", m.el).forEach((c) => c.classList.toggle("on", c === b)); load(); }));
  load();
}

// ---------- users ----------
const ROLE_NAMES = { admin: "Owner", trusted: "Trusted", friend: "Friend" };
const ROLE_HELP = { admin: "Can do everything, including settings", trusted: "Requests start downloading straight away", friend: "Requests wait for your approval" };

async function pageUsers(main) {
  main.innerHTML = `
    <div class="page-head"><div><h1>People</h1><div class="muted">Everyone who can request. Make accounts for friends here.</div></div>
      <div class="row"><button class="btn" id="import">${I.plex} Import Plex friends</button><button class="btn primary" id="add">${I.plus} Add a person</button></div></div>
    <div class="people" id="ulist"><span class="spinner"></span></div>
    <div class="glass roles">${Object.keys(ROLE_NAMES).map((r) => `<div><strong>${ROLE_NAMES[r]}</strong><span>${ROLE_HELP[r]}</span></div>`).join("")}</div>`;
  const load = async () => {
    const r = await api("/users");
    const box = qs("#ulist");
    if (!box) return;
    box.innerHTML = r.results.map((u) => {
      const how = [u.hasPassword ? "a username and password" : "", u.plex ? "Plex" : ""].filter(Boolean).join(" or ");
      const me = u.id === state.user.id;
      return `<div class="glass person">
        ${u.thumb ? `<img class="pic" src="${esc(u.thumb)}" alt="">` : `<span class="pic initial">${esc(u.username.slice(0, 1).toUpperCase())}</span>`}
        <div class="who"><strong>${esc(u.username)}</strong>${me ? ' <span class="dim small">(you)</span>' : ""}
          <div class="small dim">Signs in with ${esc(how || "nothing yet")}${u.signedIn ? "" : ", hasn't signed in yet"}${u.mustChangePassword && !me ? ", needs to pick a password" : ""}</div>
          <div class="small dim">${u.requests || 0} request${u.requests === 1 ? "" : "s"}, joined ${when(u.created)}</div></div>
        <label class="rolepick"><span class="sr">Role for ${esc(u.username)}</span><select data-uid="${u.id}" ${me ? "disabled title='You can't change your own role'" : ""}>${Object.keys(ROLE_NAMES).map((k) => `<option value="${k}" ${k === u.role ? "selected" : ""}>${ROLE_NAMES[k]}</option>`).join("")}</select></label>
        <div class="row acts">
          <button class="btn sm" data-pw="${u.id}" data-name="${esc(u.username)}">${u.hasPassword ? "New password" : "Give a password"}</button>
          ${me ? "" : `<button class="btn sm danger" data-rm="${u.id}" data-name="${esc(u.username)}">Remove</button>`}
        </div></div>`;
    }).join("");
    qsa("select[data-uid]").forEach((sel) => sel.addEventListener("change", async () => {
      try { await api(`/users/${sel.dataset.uid}`, { method: "PATCH", body: { role: sel.value } }); toast("Role updated", "ok"); } catch (e) { toast(e.message, "error"); load(); }
    }));
    qsa("[data-pw]").forEach((b) => b.addEventListener("click", async () => {
      if (!confirmBox(`Make a new password for ${b.dataset.name}? Their old one stops working and they'll be signed out.`)) return;
      try { const r = await api(`/users/${b.dataset.pw}/password`, { method: "PUT", body: {} }); shareDetails(r.username, r.password, "New password ready"); load(); }
      catch (e) { toast(e.message, "error"); }
    }));
    qsa("[data-rm]").forEach((b) => b.addEventListener("click", async () => {
      if (!confirmBox(`Remove ${b.dataset.name}? They won't be able to sign in. Their past requests stay in the list.`)) return;
      try { await api(`/users/${b.dataset.rm}`, { method: "DELETE" }); toast(`${b.dataset.name} removed`); load(); }
      catch (e) { toast(e.message, "error"); }
    }));
  };
  qs("#add").addEventListener("click", () => addPersonModal(load));
  qs("#import").addEventListener("click", async (e) => {
    busy(e.currentTarget, true, "Importing…");
    try { const r = await api("/users/import", { method: "POST", body: {} }); toast(r.added ? `Added ${r.added} people from Plex` : "Everyone's already here", "ok"); await load(); }
    catch (err) { toast(err.message, "error"); }
    busy(e.currentTarget, false);
  });
  load().catch((e) => { const b = qs("#ulist"); if (b) b.innerHTML = `<div class="notice error">${esc(e.message)}</div>`; });
}

function addPersonModal(onDone) {
  const m = modal(`<h2>Add a person</h2>
    <p class="muted" style="margin-top:-6px">They'll sign in with this username and a password. Encorr makes the password for you; they choose their own the first time they sign in.</p>
    <form id="apForm">
      <label class="field"><span>Username</span><input type="text" id="apU" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="e.g. dave" required><small>Letters, numbers, dots, dashes or underscores. No spaces.</small></label>
      <div class="field"><span class="flabel">What can they do?</span>
        ${["friend", "trusted", "admin"].map((r) => `<label class="choice"><input type="radio" name="apR" value="${r}" ${r === "friend" ? "checked" : ""}><span><strong>${ROLE_NAMES[r]}</strong><small>${ROLE_HELP[r]}</small></span></label>`).join("")}
      </div>
      <div id="apErr"></div>
      <div class="foot"><button type="button" class="btn" data-close>Cancel</button><button class="btn primary" id="apGo" type="submit">Create account</button></div>
    </form>`);
  qs("#apU", m.el).focus();
  qs("#apForm", m.el).addEventListener("submit", async (e) => {
    e.preventDefault();
    busy(qs("#apGo", m.el), true, "Creating…");
    try {
      const r = await api("/users", { method: "POST", body: { username: qs("#apU", m.el).value.trim(), role: qs("input[name=apR]:checked", m.el).value } });
      m.close();
      shareDetails(r.user.username, r.password, "Account created");
      onDone();
    } catch (err) {
      qs("#apErr", m.el).innerHTML = `<div class="notice error">${esc(err.message)}</div>`;
      busy(qs("#apGo", m.el), false);
    }
  });
}

// Shows sign-in details once, ready to copy and send to the person.
function shareDetails(username, password, title) {
  const addr = (state.status && state.status.appUrl) || location.origin;
  const text = `Here's your Encorr account for requesting films and shows:\n${addr}\nUsername: ${username}\nPassword: ${password}\nYou'll pick your own password when you first sign in.`;
  const m = modal(`<h2>${esc(title)}</h2>
    <p class="muted" style="margin-top:-6px">Send these to ${esc(username)}. This is the only time the password is shown.</p>
    <div class="creds glass">
      <div><span>Address</span><strong>${esc(addr)}</strong></div>
      <div><span>Username</span><strong>${esc(username)}</strong></div>
      <div><span>Password</span><strong class="pw">${esc(password)}</strong></div>
    </div>
    ${/localhost|127\.0\.0\.1/.test(addr) ? `<div class="notice warn">That address only works on this computer. Set your public address in Settings → General so friends can reach Encorr.</div>` : ""}
    <div class="foot"><button class="btn" data-close>Done</button><button class="btn primary" id="cpy">Copy message</button></div>`);
  qs("#cpy", m.el).addEventListener("click", async () => {
    try { await navigator.clipboard.writeText(text); toast("Copied. Paste it into a message to them", "ok"); }
    catch (_) {
      const ta = document.createElement("textarea"); ta.value = text; document.body.appendChild(ta); ta.select();
      try { document.execCommand("copy"); toast("Copied", "ok"); } catch (__) { toast("Couldn't copy. Select the details and copy them by hand", "error"); }
      ta.remove();
    }
  });
}

// ---------- profile ----------
function pageProfile(main) {
  const u = state.user;
  main.innerHTML = `
    <div class="page-head"><div><h1>Your profile</h1></div></div>
    <div class="glass panel">
      <div class="row" style="gap:16px">
        ${u.thumb ? `<img src="${esc(u.thumb)}" alt="" style="width:64px;height:64px;border-radius:50%">` : ""}
        <div><h2 style="margin:0">${esc(u.username)}</h2><div class="muted small">${esc(u.email || "")}</div><div style="margin-top:6px">${pill(u.role === "admin" ? "approved" : u.role === "trusted" ? "available" : "partial", u.role[0].toUpperCase() + u.role.slice(1))}</div></div>
      </div>
      <h2>Password</h2>
      <p class="small muted" style="margin-top:-6px">${u.hasPassword ? "Change the password you sign in with." : "Set a password so you can also sign in with your username, without Plex."}</p>
      <form id="pwf" class="row" style="align-items:flex-end">
        <input type="text" name="username" autocomplete="username" value="${esc(u.username)}" hidden>
        ${u.hasPassword ? `<label class="field grow" style="margin:0"><span>Current password</span><input type="password" id="pcur" autocomplete="current-password"></label>` : ""}
        <label class="field grow" style="margin:0"><span>New password</span><input type="password" id="pnew" autocomplete="new-password" minlength="8" placeholder="At least 8 characters"></label>
        <button class="btn" type="submit" id="pgo">${u.hasPassword ? "Change password" : "Set password"}</button>
      </form>
      ${u.plex ? `<h2>Plex watchlist</h2>` : ""}
      ${u.plex ? `<div class="row between"><div class="grow"><strong>Request from my watchlist</strong><div class="small muted">Add a film or show to your Plex watchlist and Encorr requests it for you (checked every 10 minutes).</div></div>
        <label class="switch"><input type="checkbox" id="wl" ${u.watchlistSync ? "checked" : ""}><span class="slider"></span></label></div>` : ""}
      <h2>Discord</h2>
      <label class="field"><span>Your Discord user ID</span><input type="text" id="did" value="${esc(u.discordId || "")}" placeholder="e.g. 374591050192257027" inputmode="numeric">
        <small>Get @mentioned when your encore is ready. In Discord: Settings → Advanced → Developer Mode on, then right-click your name → Copy User ID.</small></label>
      <div class="row between" style="margin-top:20px"><button class="btn primary" id="save">Save</button><button class="btn danger" id="out">Sign out</button></div>
    </div>`;
  qs("#save").addEventListener("click", async (e) => {
    busy(e.currentTarget, true, "Saving…");
    try { state.user = await api("/me", { method: "PATCH", body: { discordId: qs("#did").value, watchlistSync: qs("#wl") ? qs("#wl").checked : false } }); toast("Saved", "ok"); }
    catch (err) { toast(err.message, "error"); }
    busy(e.currentTarget, false);
  });
  qs("#out").addEventListener("click", signOut);
  qs("#pwf").addEventListener("submit", async (e) => {
    e.preventDefault();
    busy(qs("#pgo"), true, "Saving…");
    try {
      state.user = await api("/me/password", { method: "POST", body: { current: qs("#pcur") ? qs("#pcur").value : "", new: qs("#pnew").value } });
      toast("Password saved", "ok");
      pageProfile(main);
    } catch (err) { toast(err.message, "error"); busy(qs("#pgo"), false); }
  });
}

// Running in the background (Windows service that starts with the computer).
async function loadBackgroundCard(box) {
  let b;
  try { b = await api("/system/background"); } catch (_) { return; }
  if (!b.supported) return;
  const on = b.installed;
  box.innerHTML = `<h2 style="margin-top:0">Run in the background</h2>
    <div class="row between"><div class="grow"><strong>${on ? "On: Encorr starts with Windows" : "Off: Encorr only runs while its window is open"}</strong>
      <div class="small muted">${on ? "No window needed. It starts when the computer does, even before anyone logs in, and restarts itself if it ever stops." : "Turn this on so Encorr is always available to your friends. Windows will ask you to allow it."}</div></div>
      <button class="btn ${on ? "" : "primary"}" id="bgBtn">${on ? "Turn off" : "Turn on"}</button></div>
    <div id="bgMsg"></div>`;
  qs("#bgBtn", box).addEventListener("click", async (e) => {
    if (on && !confirmBox("Stop running Encorr in the background? It will close, and you'll need to open encorr.exe to use it again.")) return;
    busy(e.currentTarget, true, on ? "Turning off…" : "Waiting for Windows…");
    try {
      await api("/system/background", { method: "POST", body: { on: !on } });
      qs("#bgMsg", box).innerHTML = on
        ? `<div class="notice ok">Done. Encorr is closing; open encorr.exe whenever you want it again.</div>`
        : `<div class="notice ok">Windows is asking "Do you want to allow this app to make changes?" on the server: press <strong>Yes</strong>. Encorr will move to the background and this page will reconnect in a few seconds.</div>`;
      if (!on) waitForBackground(box);
    } catch (err) { toast(err.message, "error"); busy(e.currentTarget, false); }
  });
}

// After switching on, wait for the background copy to answer, then reload.
function waitForBackground(box) {
  let tries = 0;
  const t = setInterval(async () => {
    tries++;
    try {
      const b = await api("/system/background");
      if (b.isService) { clearInterval(t); toast("Encorr is running in the background", "ok"); loadBackgroundCard(box); }
    } catch (_) { /* restarting */ }
    if (tries > 90) clearInterval(t);
  }, 2000);
}

// ---------- shared settings forms ----------
function arrForm(app, saved) {
  const isR = app === "radarr";
  const name = isR ? "Radarr" : "Sonarr";
  const fresh = !saved || !saved.url;
  const s = Object.assign({}, saved || {}, fresh ? { searchOnAdd: true, tagRequests: true, seasonFolders: true } : {});
  return `
    <div class="row between"><div><strong>Use ${name}</strong><div class="small muted">${isR ? "Approved film requests are sent here." : "Approved TV requests are sent here."}</div></div>
      <label class="switch"><input type="checkbox" data-f="enabled" ${s.enabled || fresh ? "checked" : ""}><span class="slider"></span></label></div>
    <div class="row" style="margin-top:14px"><button class="btn sm" data-a="detect">Find ${name} automatically</button><span class="small dim" data-out="detect"></span></div>
    <label class="field"><span>Address</span><input type="url" data-f="url" value="${esc(s.url || "")}" placeholder="http://localhost:${isR ? 7878 : 8989}"></label>
    <label class="field"><span>API key</span><input type="password" data-f="apiKey" value="" placeholder="${s.hasKey ? "Saved (" + esc(s.apiKeyHint) + "). Leave blank to keep it." : "Paste the API key"}" autocomplete="off">
      <small>In ${name}: Settings → General → Security → API Key.</small></label>
    <div class="row"><button class="btn" data-a="test">Test connection</button><span data-out="test" class="small"></span></div>
    <div data-out="opts" ${s.qualityProfileId ? "" : "hidden"}>
      <label class="field"><span>Quality profile</span><select data-f="qualityProfileId">${s.qualityProfileId ? `<option value="${s.qualityProfileId}">${esc(s.qualityProfileName)}</option>` : ""}</select></label>
      <label class="field"><span>Save to folder</span><select data-f="rootFolder">${s.rootFolder ? `<option>${esc(s.rootFolder)}</option>` : ""}</select></label>
      ${isR ? `<label class="field"><span>Grab films when they are</span><select data-f="minimumAvailability">${[["released", "Released (digital or disc)"], ["inCinemas", "In cinemas"], ["announced", "Announced"]].map(([v, l]) => `<option value="${v}" ${(s.minimumAvailability || "released") === v ? "selected" : ""}>${l}</option>`).join("")}</select></label>`
    : `<label class="field" data-out="lang" hidden><span>Language profile (Sonarr v3)</span><select data-f="languageProfileId"></select></label>
         <label class="check"><input type="checkbox" data-f="seasonFolders" ${s.seasonFolders ? "checked" : ""}> Put episodes in season folders</label>`}
      <label class="check"><input type="checkbox" data-f="searchOnAdd" ${s.searchOnAdd ? "checked" : ""}> Start searching as soon as a request is approved</label>
      <label class="check"><input type="checkbox" data-f="tagRequests" ${s.tagRequests ? "checked" : ""}> Tag each request with the requester's name in ${name}</label>
    </div>`;
}

function bindArrForm(root, app, saved, onSaved) {
  const name = app === "radarr" ? "Radarr" : "Sonarr";
  const f = (k) => qs(`[data-f="${k}"]`, root);
  const out = (k) => qs(`[data-out="${k}"]`, root);
  const fill = (res) => {
    const prevP = Number(f("qualityProfileId").value) || saved?.qualityProfileId;
    const prevR = f("rootFolder").value || saved?.rootFolder;
    const pick = res.profiles.find((p) => p.id === prevP) || res.profiles.find((p) => /1080|hd/i.test(p.name)) || res.profiles[0];
    f("qualityProfileId").innerHTML = res.profiles.map((p) => `<option value="${p.id}" ${pick && p.id === pick.id ? "selected" : ""}>${esc(p.name)}</option>`).join("");
    f("rootFolder").innerHTML = (res.rootFolders || []).map((r) => `<option value="${esc(r.path)}" ${r.path === prevR ? "selected" : ""}>${esc(r.path)}${r.freeSpace ? ` (${(r.freeSpace / 1e12).toFixed(1)} TB free)` : ""}</option>`).join("");
    if (app === "sonarr" && res.languageProfiles && res.languageProfiles.length) {
      out("lang").hidden = false;
      f("languageProfileId").innerHTML = res.languageProfiles.map((p) => `<option value="${p.id}" ${p.id === saved?.languageProfileId ? "selected" : ""}>${esc(p.name)}</option>`).join("");
    }
    out("opts").hidden = false;
  };
  const test = async (btn) => {
    busy(btn, true, "Testing…");
    const o = out("test");
    o.innerHTML = "";
    try {
      const res = await api(`/settings/${app}/test`, { method: "POST", body: { url: f("url").value, apiKey: f("apiKey").value } });
      if (res.ok) {
        o.innerHTML = `<span class="pill ok"><span class="dot"></span>Connected to ${name} ${esc(res.version)}</span>${res.error ? `<div class="notice warn">${esc(res.error)}</div>` : ""}`;
        fill(res);
        return true;
      }
      o.innerHTML = `<div class="notice error">${esc(res.error)}</div>`;
    } catch (e) {
      o.innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
    } finally { busy(btn, false); }
    return false;
  };
  qs('[data-a="test"]', root).addEventListener("click", (e) => test(e.currentTarget));
  qs('[data-a="detect"]', root).addEventListener("click", async (e) => {
    const btn = e.currentTarget;
    busy(btn, true, "Looking…");
    try {
      const d = await api(`/settings/detect/${app}`);
      if (d.found) {
        f("url").value = d.url;
        f("apiKey").value = d.apiKey;
        out("detect").textContent = d.note || `Found it (${d.configPath}).`;
        busy(btn, false);
        await test(qs('[data-a="test"]', root));
        return;
      }
      if (!f("url").value) f("url").value = d.url || "";
      out("detect").textContent = d.note || "Not found.";
    } catch (err) { out("detect").textContent = err.message; }
    busy(btn, false);
  });
  if (saved && saved.url) {
    // Load dropdown options quietly when a saved connection exists.
    api(`/settings/${app}/test`, { method: "POST", body: { url: saved.url, apiKey: "" } }).then((res) => res.ok && fill(res)).catch(() => {});
  }
  return async function save() {
    const sel = f("qualityProfileId");
    const body = {
      enabled: f("enabled").checked,
      url: f("url").value,
      apiKey: f("apiKey").value,
      qualityProfileId: Number(sel.value) || 0,
      qualityProfileName: sel.selectedOptions[0] ? sel.selectedOptions[0].textContent : "",
      rootFolder: f("rootFolder").value,
      searchOnAdd: f("searchOnAdd").checked,
      tagRequests: f("tagRequests").checked,
    };
    if (app === "radarr") body.minimumAvailability = f("minimumAvailability").value;
    else { body.seasonFolders = f("seasonFolders").checked; body.languageProfileId = Number(f("languageProfileId").value) || 0; }
    if (!body.enabled) { body.qualityProfileId = body.qualityProfileId || 0; }
    await api(`/settings/arr/${app}`, { method: "PUT", body });
    if (onSaved) onSaved();
  };
}

function discordForm(saved) {
  const s = saved || {};
  return `
    <div class="row between"><div><strong>Send notifications to Discord</strong><div class="small muted">New requests, approvals, and “Your encore is ready”.</div></div>
      <label class="switch"><input type="checkbox" id="dEnabled" ${s.enabled ? "checked" : ""}><span class="slider"></span></label></div>
    <label class="field"><span>Channel webhook address</span><input type="url" id="dHook" placeholder="${s.hasWebhook ? "Saved (" + esc(s.webhookHint) + "). Leave blank to keep it." : "https://discord.com/api/webhooks/…"}">
      <small>In Discord: open the channel's settings → Integrations → Webhooks → New Webhook → Copy Webhook URL.</small></label>
    <div class="row"><button class="btn" id="dTest">Send test message</button></div>`;
}

function bindDiscord(root) {
  qs("#dTest", root).addEventListener("click", async (e) => {
    busy(e.currentTarget, true, "Sending…");
    try { await api("/settings/discord/test", { method: "POST", body: { webhookUrl: qs("#dHook", root).value } }); toast("Test sent: check your Discord channel", "ok"); }
    catch (err) { toast(err.message, "error"); }
    busy(e.currentTarget, false);
  });
  return () => api("/settings/discord", { method: "PUT", body: { enabled: qs("#dEnabled", root).checked, webhookUrl: qs("#dHook", root).value } });
}

function tmdbForm(saved) {
  const builtIn = saved && saved.builtIn;
  return `<label class="field"><span>${builtIn ? "Your own TMDB key (optional)" : "TMDB API key"}</span><input type="password" id="tmdbKey" placeholder="${saved && saved.hasKey ? "Saved (" + esc(saved.keyHint) + "). Paste a new one to replace it." : builtIn ? "Leave blank to use Encorr's built-in key" : "Paste your TMDB API key"}" autocomplete="off">
    <small>${builtIn ? "Encorr has its own key built in, so you don't need one. Only add yours if you'd rather use your own." : `Free: sign up at <a href="https://www.themoviedb.org/signup" target="_blank" rel="noopener">themoviedb.org</a>, then Settings → API → create a key (either the “API Key” or the “Read Access Token” works).`}</small></label>
    ${builtIn && saved.hasKey ? `<button class="btn sm" id="tmdbReset" type="button">Use the built-in key instead</button>` : ""}`;
}

async function serverPicker(root, onPicked) {
  root.innerHTML = `<div class="row"><span class="spinner"></span> Looking for your Plex servers…</div>`;
  try {
    const r = await api("/settings/plex/servers");
    const owned = r.servers.filter((s) => s.owned);
    if (!owned.length) { root.innerHTML = `<div class="notice warn">No servers you own were found on this Plex account. Sign in with the account that owns the server.</div>`; return; }
    root.innerHTML = `<div class="list">${owned.map((s) => `<button class="glass lib" style="cursor:pointer;text-align:left;border:1px solid var(--glass-border)" data-mid="${esc(s.machineId)}"><span><strong>${esc(s.name)}</strong><div class="small dim">${s.local ? "On your network" : "Remote"}</div></span><span class="btn sm primary">Use this</span></button>`).join("")}</div>
      <details style="margin-top:14px"><summary class="small muted" style="cursor:pointer">Enter the address yourself</summary>
        <label class="field"><span>Plex address</span><input type="url" id="manualUrl" placeholder="http://localhost:32400"></label><small class="hint">Then press “Use this” on your server above.</small></details>
      <div id="pickOut"></div>`;
    qsa("[data-mid]", root).forEach((b) => b.addEventListener("click", async () => {
      const lbl = qs(".btn", b);
      busy(lbl, true, "Connecting…");
      try {
        const res = await api("/settings/plex/server", { method: "POST", body: { machineId: b.dataset.mid, url: (qs("#manualUrl", root) || {}).value || "" } });
        toast(`Connected to ${res.serverName}`, "ok");
        onPicked(res);
      } catch (e) {
        qs("#pickOut", root).innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
        busy(lbl, false);
      }
    }));
  } catch (e) {
    root.innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
  }
}

function librariesForm(libs) {
  if (!libs || !libs.length) return `<div class="notice warn">No film or TV libraries found on this server.</div>`;
  return `<div class="liblist">${libs.map((l) => `<label class="glass lib"><span><strong>${esc(l.title)}</strong> <span class="dim small">${l.type === "show" ? "TV" : "Films"}</span></span><label class="switch"><input type="checkbox" data-lib="${esc(l.key)}" ${l.enabled ? "checked" : ""}><span class="slider"></span></label></label>`).join("")}</div>`;
}
const saveLibraries = (root) => api("/settings/plex/libraries", { method: "PUT", body: { enabled: qsa("[data-lib]", root).filter((c) => c.checked).map((c) => c.dataset.lib) } });

// ---------- setup wizard ----------
const STEPS = ["signin", "server", "libraries", "tmdb", "radarr", "sonarr", "discord", "done"];
const STEPS_BACK = {};

async function renderWizard(stepName) {
  setBackdrop(null);
  let step = stepName || (state.user ? "server" : "signin");
  let settings = null;
  if (state.user) {
    try { settings = await api("/settings"); } catch { /* not admin */ }
  }
  const idx = STEPS.indexOf(step);
  const go = (s) => renderWizard(s);
  const shell = (title, lead, body, { back = true, next = "Continue", skip = false } = {}) => {
    $app.innerHTML = `<div class="center"><div class="glass wizard">
      <div class="logo"><div>${LOCKUP(34)}<div class="tag">Request the encore.</div></div></div>
      <div class="steps">${STEPS.map((_, i) => `<i class="${i < idx ? "done" : i === idx ? "now" : ""}"></i>`).join("")}</div>
      <h1>${esc(title)}</h1><p class="lead">${lead}</p>
      <div id="wbody">${body}</div>
      <div class="nav-btns">
        <div>${back && idx > 1 ? `<button class="btn" id="wBack">Back</button>` : ""}</div>
        <div class="row">${skip ? `<button class="btn" id="wSkip">Skip for now</button>` : ""}${next ? `<button class="btn primary" id="wNext">${esc(next)}</button>` : ""}</div>
      </div></div></div>`;
    const b = qs("#wBack"); if (b) b.addEventListener("click", () => go(STEPS_BACK[step] || STEPS[idx - 1]));
    const s = qs("#wSkip"); if (s) s.addEventListener("click", () => go(STEPS[idx + 1]));
    return qs("#wbody");
  };
  const onNext = (fn) => {
    const btn = qs("#wNext");
    btn.addEventListener("click", async () => {
      busy(btn, true, "Saving…");
      try { await fn(); } catch (e) { toast(e.message, "error"); busy(btn, false); }
    });
  };

  if (step === "signin") {
    shell("Welcome to Encorr", "Let's get you set up. It takes about five minutes. First, sign in with the Plex account that owns your server: you'll be the admin.",
      `<button class="btn plex" id="signin">${I.plex} Sign in with Plex</button><p class="hint" style="margin-top:14px">A Plex window opens. Approve it, and you'll come straight back here.</p>`, { back: false, next: "" });
    qs("#signin").addEventListener("click", (e) => plexSignIn(e.currentTarget, async () => { state.status = await api("/status"); state.user = state.status.user; go("server"); }));
    return;
  }
  if (step === "server") {
    const body = shell("Choose your Plex server", `Signed in as <strong>${esc(state.user.username)}</strong>. Pick the server whose libraries people will request for.`,
      `<div id="picker"></div>${settings && settings.plex.serverName ? `<div class="notice ok" style="margin-top:14px">Currently using <strong>${esc(settings.plex.serverName)}</strong>.</div>` : ""}`,
      { next: settings && settings.plex.serverName ? "Continue" : "" });
    serverPicker(qs("#picker", body), () => go("libraries"));
    if (qs("#wNext")) onNext(async () => go("libraries"));
    return;
  }
  if (step === "libraries") {
    const body = shell("Which libraries?", "Encorr watches these to know what's already in Plex. Most people leave them all on.", librariesForm(settings.plex.libraries));
    onNext(async () => { await saveLibraries(body); go(settings.tmdb.builtIn ? "radarr" : "tmdb"); });
    return;
  }
  if (step === "tmdb") {
    const has = settings.tmdb.hasKey;
    const body = shell("Film and TV information", "Encorr gets posters, trailers and details from TMDB, which is free. Paste your key below.", tmdbForm(settings.tmdb));
    onNext(async () => {
      const key = qs("#tmdbKey", body).value.trim();
      if (!key && has) return go("radarr");
      if (!key) throw new Error("Paste your TMDB key to continue.");
      await api("/settings/tmdb", { method: "PUT", body: { key } });
      toast("TMDB key works", "ok");
      go("radarr");
    });
    return;
  }
  if (step === "radarr" || step === "sonarr") {
    const name = step === "radarr" ? "Radarr" : "Sonarr";
    if (step === "radarr" && settings.tmdb.builtIn) STEPS_BACK.radarr = "libraries";
    const body = shell(`Connect ${name}`, `${step === "radarr" ? "Films" : "TV shows"} get downloaded through ${name}. If it's on this computer, Encorr can find it for you.`, arrForm(step, settings[step]), { skip: true });
    const save = bindArrForm(body, step, settings[step]);
    if (!settings[step].url) qs('[data-a="detect"]', body).click();
    onNext(async () => { await save(); toast(`${name} saved`, "ok"); go(step === "radarr" ? "sonarr" : "discord"); });
    return;
  }
  if (step === "discord") {
    const body = shell("Discord notifications", "Optional: post to a Discord channel when something is requested, approved, or ready to watch.", discordForm(settings.discord), { skip: true });
    const save = bindDiscord(body);
    onNext(async () => { await save(); go("done"); });
    return;
  }
  if (step === "done") {
    shell("You're all set", `Encorr will scan your Plex libraries now. Share this address with your friends: <span class="code">${esc(location.origin)}</span>. They sign in with their own Plex accounts.<br><br>Tip: on your phone, open Encorr in Chrome and choose <strong>Add to Home screen</strong> for an app icon.`, "", { next: "Start using Encorr" });
    onNext(async () => {
      await api("/settings/complete", { method: "POST", body: {} });
      state.status = await api("/status");
      location.hash = "#/discover";
      boot();
    });
  }
}

// ---------- settings pages ----------
const SETTINGS_MENU = [["general", "General"], ["plex", "Plex"], ["radarr", "Radarr"], ["sonarr", "Sonarr"], ["sabnzbd", "SABnzbd"], ["discord", "Discord"], ["health", "Health"], ["jobs", "Jobs & backup"]];

async function pageSettings(main, section) {
  main.innerHTML = `<div class="page-head"><div><h1>Settings</h1><div class="small dim">Encorr ${esc((state.status && state.status.version) || "")} · <a href="https://github.com/FiTH-g1gGLe/Encorr/releases/latest" target="_blank" rel="noopener">What's new ↗</a></div></div></div>
    <div class="settings"><nav class="glass menu">${SETTINGS_MENU.map(([k, l]) => `<a href="#/settings/${k}" class="${k === section ? "on" : ""}">${esc(l)}</a>`).join("")}</nav>
    <section class="glass panel" id="panel"><span class="spinner"></span></section></div>`;
  const panel = qs("#panel");
  let s;
  try { s = await api("/settings"); } catch (e) { panel.innerHTML = `<div class="notice error">${esc(e.message)}</div>`; return; }

  const saveBar = (label = "Save changes") => `<div class="row" style="margin-top:22px"><button class="btn primary" id="save">${label}</button></div>`;
  const onSave = (fn, msg = "Saved") => qs("#save").addEventListener("click", async (e) => {
    busy(e.currentTarget, true, "Saving…");
    try { await fn(); toast(msg, "ok"); } catch (err) { toast(err.message, "error"); }
    busy(e.currentTarget, false);
  });

  if (section === "general") {
    panel.innerHTML = `<div id="bgCard"></div><h2>Look</h2><div class="field-label">Accent colour</div>
      <div class="swatches">${ACCENTS.map(([k, c, l]) => `<button type="button" class="swatch ${(s.accent || "violet") === k ? "on" : ""}" data-acc="${k}"><span class="sw" style="background:${c}"></span>${l}</button>`).join("")}</div>
      <div class="field-label" style="margin-top:18px">Background</div>
      <div class="themes">${[["navy", "Midnight", "Deep navy with soft colour highlights"], ["black", "Graphite", "Neutral dark grey: lets the posters bring the colour"]].map(([k, l, d]) => `<button type="button" class="theme-opt ${(s.theme || "navy") === k ? "on" : ""}" data-theme-opt="${k}"><span class="tprev ${k}"><i></i><i></i><i></i></span><span><strong>${l}</strong><span class="small dim">${d}</span></span></button>`).join("")}</div>
      <h2>Requests</h2>
      <p class="lead">Limits for Friends. Trusted users and admins have no limit. Use 0 for no limit.</p>
      <div class="row"><label class="field grow"${tip("How many films each Friend can ask for in the period. 0 = no limit")}><span>Films per period</span><input type="number" min="0" id="qm" value="${s.quotas.friendMovieLimit}"></label>
        <label class="field grow"${tip("How many shows each Friend can ask for in the period. 0 = no limit")}><span>TV shows per period</span><input type="number" min="0" id="qt" value="${s.quotas.friendTvLimit}"></label>
        <label class="field grow"${tip("The limits reset after this many days")}><span>Period (days)</span><input type="number" min="1" id="qd" value="${s.quotas.days}"></label></div>
      <h2>Address and region</h2>
      <label class="field"><span>Public address (optional)</span><input type="url" id="appUrl" value="${esc(s.appUrl)}" placeholder="https://encorr.example.com"><small>Used for links in Discord messages, e.g. your Cloudflare Tunnel address.</small></label>
      <label class="field"><span>Region</span><select id="region">${[["GB", "United Kingdom"], ["IE", "Ireland"], ["US", "United States"], ["CA", "Canada"], ["AU", "Australia"], ["NZ", "New Zealand"]].map(([k, l]) => `<option value="${k}" ${s.region === k ? "selected" : ""}>${l}</option>`).join("")}</select><small>Sets release dates, age ratings and “coming soon”.</small></label>
      <h2>TMDB</h2>${tmdbForm(s.tmdb)}${saveBar()}`;
    loadBackgroundCard(qs("#bgCard"));
    let accent = s.accent || "violet";
    let theme = s.theme || "navy";
    qsa("[data-theme-opt]").forEach((el) => el.addEventListener("click", () => {
      theme = el.dataset.themeOpt;
      document.documentElement.dataset.theme = theme;
      qsa("[data-theme-opt]").forEach((x) => x.classList.toggle("on", x === el));
    }));
    qsa("[data-acc]").forEach((el) => el.addEventListener("click", () => {
      accent = el.dataset.acc;
      qsa("[data-acc]").forEach((x) => x.classList.toggle("on", x === el));
      document.documentElement.dataset.accent = accent;
    }));
    const rb = qs("#tmdbReset");
    if (rb) rb.addEventListener("click", async () => {
      try { await api("/settings/tmdb", { method: "PUT", body: { key: "" } }); toast("Using the built-in TMDB key", "ok"); route(); } catch (e) { toast(e.message, "error"); }
    });
    onSave(async () => {
      await api("/settings/general", { method: "PUT", body: { accent, theme, appUrl: qs("#appUrl").value, region: qs("#region").value, quotas: { friendMovieLimit: Number(qs("#qm").value), friendTvLimit: Number(qs("#qt").value), days: Number(qs("#qd").value) } } });
      const k = qs("#tmdbKey").value.trim();
      if (k) await api("/settings/tmdb", { method: "PUT", body: { key: k } });
    });
  }
  if (section === "plex") {
    panel.innerHTML = `<h2>Server</h2>
      <div class="row between"><div><strong>${esc(s.plex.serverName || "Not chosen")}</strong><div class="small dim">${esc(s.plex.url || "")}</div></div><button class="btn sm" id="change">Change server</button></div>
      <div id="picker" style="margin-top:14px"></div>
      <h2>Libraries</h2><p class="lead">Encorr watches these to know what's already in Plex.</p><div id="libs">${librariesForm(s.plex.libraries)}</div>${saveBar()}
      <h2>People</h2><p class="lead">Add everyone you share Plex with, so you can set their role before they sign in.</p><a class="btn" href="#/users">Go to Users</a>`;
    qs("#change").addEventListener("click", () => serverPicker(qs("#picker"), (res) => { qs("#libs").innerHTML = librariesForm(res.libraries); qs("#picker").innerHTML = ""; }));
    onSave(async () => { await saveLibraries(panel); await api("/jobs/plex-full/run", { method: "POST", body: {} }); }, "Saved: rescanning Plex");
  }
  if (section === "radarr" || section === "sonarr") {
    const name = section === "radarr" ? "Radarr" : "Sonarr";
    panel.innerHTML = `<h2>${name}</h2>${arrForm(section, s[section])}${saveBar()}`;
    const save = bindArrForm(panel, section, s[section]);
    onSave(save, `${name} saved`);
  }
  if (section === "sabnzbd") {
    const z = s.sabnzbd || {};
    panel.innerHTML = `<h2>SABnzbd <span class="dim small" style="font-weight:500">optional</span></h2>
      <p class="lead">Radarr and Sonarr only update their download list about once a minute. Connect SABnzbd and progress bars, speed and time left come straight from it, live.</p>
      ${z.detected ? `<div class="notice ok">Encorr found SABnzbd on this machine and connected it for you.</div>` : ""}
      <div class="row between"><div><strong>Live progress from SABnzbd</strong></div>
        <label class="switch"><input type="checkbox" id="zOn" ${z.enabled ? "checked" : ""}><span class="slider"></span></label></div>
      <label class="field"><span>Address</span><input type="url" id="zUrl" value="${esc(z.url || "")}" placeholder="http://localhost:8080/sabnzbd"></label>
      <label class="field"><span>API key</span><input type="text" id="zKey" autocomplete="off" placeholder="${z.hasKey ? "Saved (" + esc(z.keyHint) + "). Leave blank to keep it." : "SABnzbd → Config → General → API Key"}"></label>
      <div class="row"><button class="btn" id="zDetect">Find it automatically</button><button class="btn" id="zTest">Test</button></div>
      <div id="zMsg" style="margin-top:12px"></div>${saveBar()}`;
    const msg = (html) => (qs("#zMsg").innerHTML = html);
    qs("#zDetect").addEventListener("click", async (e) => {
      busy(e.currentTarget, true, "Looking…");
      try {
        const d = await api("/settings/detect/sabnzbd");
        if (d.found) { qs("#zUrl").value = d.url; qs("#zKey").value = d.apiKey; qs("#zOn").checked = true; }
        msg(d.found && !d.note ? `<div class="notice ok">Found it at ${esc(d.url)}. Press Save.</div>` : `<div class="notice warn">${esc(d.note || "Not found.")}</div>`);
      } catch (err) { msg(`<div class="notice error">${esc(err.message)}</div>`); }
      busy(e.currentTarget, false);
    });
    qs("#zTest").addEventListener("click", async (e) => {
      busy(e.currentTarget, true, "Testing…");
      try {
        const r = await api("/settings/sabnzbd/test", { method: "POST", body: { url: qs("#zUrl").value, apiKey: qs("#zKey").value } });
        msg(r.ok ? `<div class="notice ok">Connected to SABnzbd ${esc(r.version)}.</div>` : `<div class="notice error">${esc(r.error)}</div>`);
      } catch (err) { msg(`<div class="notice error">${esc(err.message)}</div>`); }
      busy(e.currentTarget, false);
    });
    onSave(() => api("/settings/sabnzbd", { method: "PUT", body: { enabled: qs("#zOn").checked, url: qs("#zUrl").value, apiKey: qs("#zKey").value } }), "SABnzbd saved");
  }
  if (section === "discord") {
    panel.innerHTML = `<h2>Discord</h2>${discordForm(s.discord)}<p class="hint">Each person can add their Discord user ID on their profile to get @mentioned when their encore is ready.</p>${saveBar()}`;
    onSave(bindDiscord(panel));
  }
  if (section === "health") {
    const ACTIONS = {
      "sab-pause": ["Pause downloads", "pause"], "sab-resume": ["Resume downloads", "play"], "sab-restart": ["Restart SABnzbd", "refresh"],
      "sab-orphans": ["Clear leftovers", "trash"], "sab-warnings": ["Clear warnings", "check"], autofix: ["Run auto-fix now", "wrench"],
    };
    const ACT_ICON = { fix: "wrench", warn: "warn", action: "hand" };
    const run = async () => {
      panel.innerHTML = `<div class="row between"><h2 style="margin:0">Health</h2><button class="btn sm" id="recheck">${I.refresh} Re-check</button></div>
        <p class="lead" style="margin-top:8px">Checking everything Encorr depends on…</p>
        <div class="glass hset"><div><strong>Auto-fix stuck downloads</strong><div class="small dim">Every 5 minutes: imports downloads stuck before Plex, swaps failed or stalled ones for another copy, and clears SABnzbd's leftovers. You'll see everything it does below.</div></div><label class="switch"><input type="checkbox" id="autofix" ${s.autoFix !== false ? "checked" : ""}><span class="slider"></span></label></div>
        <div class="hacts"><button class="btn" data-hact="search-missing"${tip("Ask Radarr and Sonarr to look for every missing film and episode now")}>${I.search} Search for everything missing</button></div>
        <div class="checks"><span class="spinner"></span></div>
        <h2>What Encorr has been doing</h2><div class="alog"><span class="spinner"></span></div>`;
      qs("#recheck").addEventListener("click", run);
      qs("#autofix").addEventListener("change", async (e) => {
        try { await api("/settings/general", { method: "PUT", body: { autoFix: e.target.checked } }); s.autoFix = e.target.checked; toast(e.target.checked ? "Auto-fix is on" : "Auto-fix is off", "ok"); } catch (err) { toast(err.message, "error"); e.target.checked = !e.target.checked; }
      });
      const bindActs = (root) => qsa("[data-hact]", root).forEach((b) => b.addEventListener("click", async () => {
        const act = b.dataset.hact;
        if (act === "sab-restart" && !confirmBox("Restart SABnzbd? Downloads pick up where they left off.")) return;
        if (act === "search-missing" && !confirmBox("Search for every missing film and episode now? With a big library this keeps the indexers busy for a while.")) return;
        busy(b, true, "Working…");
        try { const r = await api(`/health/${act}`, { method: "POST", body: {} }); toast(r.message, "ok"); setTimeout(run, act === "sab-restart" ? 8000 : 1200); } catch (e) { toast(e.message, "error"); busy(b, false); }
      }));
      bindActs(panel);
      try {
        const r = await api("/health");
        const sym = { ok: I.check, warn: "!", error: "!", off: "–" };
        const bad = r.checks.filter((c) => c.status === "error").length;
        const warn = r.checks.filter((c) => c.status === "warn").length;
        qs(".lead", panel).textContent = bad ? `${bad} thing${bad > 1 ? "s need" : " needs"} attention.` : warn ? `Working, with ${warn} thing${warn > 1 ? "s" : ""} to keep an eye on.` : "Everything's looking good.";
        qs(".checks", panel).innerHTML = r.checks.map((c) => `<div class="glass checkrow ${c.status}"><div class="ic ${c.status}">${sym[c.status] || "?"}</div><div class="cbody"><strong>${esc(c.name)}</strong><div class="small">${esc(c.message)}</div>${c.fix && c.status !== "ok" ? `<div class="fix">${esc(c.fix)}</div>` : ""}${(c.actions || []).length ? `<div class="cacts">${c.actions.map((a) => ACTIONS[a] ? `<button class="btn sm" data-hact="${a}">${I[ACTIONS[a][1]]} ${ACTIONS[a][0]}</button>` : "").join("")}</div>` : ""}</div></div>`).join("");
        bindActs(qs(".checks", panel));
      } catch (e) { qs(".checks", panel).innerHTML = `<div class="notice error">${esc(e.message)}</div>`; }
      try {
        const lg = await api("/activity");
        qs(".alog", panel).innerHTML = lg.items.length ? `<ol class="tlist">${lg.items.map((it) => `<li class="k-${esc(it.kind)}"><span class="tdot">${I[ACT_ICON[it.kind]] || ""}</span><div><strong>${esc(it.title)}</strong>${it.detail ? `<span class="td">${esc(it.detail)}</span>` : ""}<span class="tt">${esc(exact(it.at))}${it.requestId ? ` · <a href="#/requests?filter=all">request</a>` : ""}</span></div></li>`).join("")}</ol>` : `<p class="muted">Nothing yet. When Encorr fixes a download or something needs you, it's listed here.</p>`;
      } catch (e) { qs(".alog", panel).innerHTML = `<div class="notice error">${esc(e.message)}</div>`; }
    };
    run();
  }
  if (section === "jobs") {
    const load = async () => {
      const r = await api("/jobs");
      panel.innerHTML = `<h2>Background jobs</h2><p class="lead">These run on their own. Use Run now after changing something.</p>
        <div class="list">${r.jobs.map((j) => `<div class="glass lib"><div><strong>${esc(j.label)}</strong><div class="small dim">${esc(j.schedule)} · ${j.info.lastRun && !j.info.lastRun.startsWith("0001") ? "last run " + esc(ago(j.info.lastRun)) + (j.info.lastOk ? "" : " (failed)") : "not run yet"}</div>${j.info.message ? `<div class="small ${j.info.lastOk ? "muted" : ""}" style="${j.info.lastOk ? "" : "color:#fca5a5"}">${esc(j.info.message)}</div>` : ""}</div>
          <button class="btn sm" data-job="${esc(j.name)}" ${j.running ? "disabled" : ""}>${j.running ? "Running…" : "Run now"}</button></div>`).join("")}</div>
        <h2>Backup</h2><p class="lead">Saves a copy of Encorr's data and key to the backups folder in Encorr's data folder. Encorr also keeps the previous copy automatically.</p>
        <button class="btn" id="backup">Back up now</button>`;
      qsa("[data-job]").forEach((b) => b.addEventListener("click", async () => {
        try { await api(`/jobs/${b.dataset.job}/run`, { method: "POST", body: {} }); toast("Started"); setTimeout(load, 2500); } catch (e) { toast(e.message, "error"); }
      }));
      qs("#backup").addEventListener("click", async (e) => {
        busy(e.currentTarget, true, "Backing up…");
        try { const r2 = await api("/backup", { method: "POST", body: {} }); toast("Saved to " + r2.path, "ok"); } catch (err) { toast(err.message, "error"); }
        busy(e.currentTarget, false);
      });
    };
    load().catch((e) => (panel.innerHTML = `<div class="notice error">${esc(e.message)}</div>`));
    state.timers.push(setInterval(() => load().catch(() => {}), 15000));
  }
}

boot();
