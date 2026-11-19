// Encorr web app. Plain JavaScript, no build step. Request the encore.
// The files in js/ load in order and share one scope: core, shell, browse,
// title, requests, people, settings.
// Shared basics: helpers, the API call, pop-ups, icons and the logo.
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
// What the media library is called here: "Plex", "Jellyfin", or "your
// library" when both are connected.
const LIB = () => (state.status && state.status.libraryName) || "Plex";
const BOTH = () => LIB() === "your library";
const IN_LIB = () => (BOTH() ? "In your library" : "In " + LIB());
const YOUR_LIB = () => (BOTH() ? "your library" : "your " + LIB());
// ---------- address boxes in Docker ----------
// Inside Docker, "localhost" is Encorr itself, so address boxes (marked
// data-addr="port") get the server's real address, a note, and a warning
// the moment someone types localhost.
const isLoop = (h) => !h || h === "localhost" || h.startsWith("127.") || h === "::1" || h === "[::1]";
const serverHost = () => (isLoop(location.hostname) ? "192.168.1.50" : location.hostname);
function addrHints(root) {
  if (!state.status || !state.status.docker) return;
  qsa("input[data-addr]:not([data-dh])", root || document).forEach((inp) => {
    inp.dataset.dh = "1";
    const guess = `http://${serverHost()}:${inp.dataset.addr}${inp.dataset.path || ""}`;
    inp.placeholder = guess;
    if (!inp.value && !isLoop(location.hostname)) { inp.value = guess; inp.dataset.auto = "1"; }
    const pill = document.createElement("div");
    pill.className = "addrpill";
    pill.setAttribute("role", "status");
    const draw = () => {
      const bad = /\/\/(localhost|127\.|\[::1\]|0\.0\.0\.0)/i.test(inp.value);
      pill.classList.toggle("bad", bad);
      pill.innerHTML = bad
        ? `${I.warn}<span><b>Not localhost.</b> Encorr runs in Docker, where localhost means Encorr itself. Use your server's address (${esc(serverHost())}) or the app's container name.</span>`
        : `<span class="dk">Docker</span><span>Use your server's address or the app's container name, not localhost.</span>`;
    };
    draw();
    inp.addEventListener("input", () => { delete inp.dataset.auto; draw(); });
    (inp.closest(".field") || inp).insertAdjacentElement("afterend", pill);
  });
}
new MutationObserver(() => addrHints()).observe(document.documentElement, { childList: true, subtree: true });

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
