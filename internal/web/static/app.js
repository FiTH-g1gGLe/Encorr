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

function modal(html, { wide = false } = {}) {
  const bg = document.createElement("div");
  bg.className = "modal-bg";
  bg.innerHTML = `<div class="modal glass ${wide ? "wide" : ""}" role="dialog" aria-modal="true">${html}</div>`;
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

function ago(iso) {
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (!isFinite(s)) return "";
  if (s < 60) return "just now";
  if (s < 3600) return Math.floor(s / 60) + " min ago";
  if (s < 86400) return Math.floor(s / 3600) + " h ago";
  if (s < 86400 * 30) return Math.floor(s / 86400) + " days ago";
  return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
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
const pill = (s, text) => { const [cls, label] = STATUS[s] || ["off", s]; return `<span class="pill ${cls}"><span class="dot"></span>${esc(text || label)}</span>`; };

// ---------- brand ----------
// The e-Loop: the "e" of encorr is also a replay loop. Drawn in the accent colour.
const MARK = (size, cls = "") => `<svg class="mark ${cls}" width="${size}" height="${size}" viewBox="0 0 64 64" aria-hidden="true"><path d="M14.0 32H50.0" stroke="currentColor" stroke-width="6.5" stroke-linecap="round"/><path d="M50.0 32.0A18 18 0 1 0 43.1 46.2" fill="none" stroke="currentColor" stroke-width="6.5" stroke-linecap="round"/><path d="M49.0 41.6L45.7 52.0L38.1 42.2Z" fill="currentColor" stroke="currentColor" stroke-width="2.5" stroke-linejoin="round"/></svg>`;
const LOCKUP = (size) => `<span class="lockup" style="font-size:${size}px" role="img" aria-label="Encorr">${MARK(Math.round(size), "lk")}<span>ncorr</span></span>`;

// ---------- icons ----------
const I = {
  discover: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="9"/><path d="m15.5 8.5-2 5-5 2 2-5z" fill="currentColor" fill-opacity=".25"/></svg>',
  search: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="11" cy="11" r="6.5"/><path d="m20 20-4.2-4.2"/></svg>',
  requests: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="3.5" width="16" height="17" rx="3"/><path d="M8 8.5h8M8 12h8M8 15.5h5"/></svg>',
  users: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="9" cy="8.5" r="3.2"/><path d="M3.5 19c.7-3 3-4.6 5.5-4.6s4.8 1.6 5.5 4.6"/><circle cx="17" cy="9.5" r="2.5"/><path d="M16.5 14.6c2.1.2 3.5 1.6 4 4"/></svg>',
  settings: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="3"/><path d="M12 2.8v2.4M12 18.8v2.4M4.2 7.5l2.1 1.2M17.7 15.3l2.1 1.2M4.2 16.5l2.1-1.2M17.7 8.7l2.1-1.2"/><circle cx="12" cy="12" r="7"/></svg>',
  play: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M8 5.5v13l11-6.5z"/></svg>',
  plus: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M12 5v14M5 12h14"/></svg>',
  check: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"><path d="m5 12.5 4.5 4.5L19 7.5"/></svg>',
  plex: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M6 3h5.2l6.8 9-6.8 9H6l6.8-9z"/></svg>',
};

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
  const s = state.status;
  if (!s.hasAdmin || (!s.setupComplete && isAdmin())) return renderWizard();
  if (!state.user) return renderLogin();
  if (!s.setupComplete) return renderNotReady();
  if (state.user.mustChangePassword) return renderChoosePassword();
  renderShell();
  route();
}

window.addEventListener("hashchange", () => { if (state.user && state.status && state.status.setupComplete) route(); });

function route() {
  if (!qs("main")) renderShell();
  clearTimers();
  setBackdrop(null);
  window.scrollTo(0, 0);
  const h = location.hash.replace(/^#\/?/, "");
  const [path, query] = h.split("?");
  const parts = path.split("/").filter(Boolean);
  const params = new URLSearchParams(query || "");
  const page = parts[0] || "discover";
  qsa(".nav a.item").forEach((a) => a.classList.toggle("active", a.dataset.page === page || (page === "title" && a.dataset.page === "discover")));
  const main = qs("main");
  const pages = {
    discover: () => pageDiscover(main),
    search: () => pageSearch(main, params.get("q") || ""),
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
      <a class="item" data-page="discover" href="#/discover">${I.discover}<span class="label">Home</span></a>
      <a class="item" data-page="search" href="#/search">${I.search}<span class="label">Search</span></a>
      <a class="item" data-page="requests" href="#/requests">${I.requests}<span class="label">${admin ? "Requests" : "My requests"}</span><span class="badge" id="pendingBadge" hidden title="Waiting for your approval"></span></a>
      ${admin ? `<a class="item" data-page="users" href="#/users">${I.users}<span class="label">People</span></a>
      <a class="item" data-page="settings" href="#/settings">${I.settings}<span class="label">Settings</span></a>` : ""}
      <div class="spacer"></div>
      <a href="#/profile" class="item me" data-page="profile">
        ${u.thumb ? `<img class="avatar" src="${esc(u.thumb)}" alt="">` : `<span class="avatar initial">${esc(u.username.slice(0, 1).toUpperCase())}</span>`}
        <span class="label"><span class="who">${esc(u.username)}</span><span class="role">${{ admin: "Owner", trusted: "Trusted", friend: "Friend" }[u.role] || ""}</span></span>
      </a>
    </nav>
    <main id="main"></main>
  </div>`;
  clearInterval(state.pendingTimer);
  if (admin) { refreshPending(); state.pendingTimer = setInterval(refreshPending, 60000); }
}

async function refreshPending() {
  try {
    const r = await api("/requests?filter=pending");
    const n = r.results.length;
    const b = qs("#pendingBadge");
    if (b) { b.hidden = n === 0; b.textContent = n; }
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
  return `<a class="card" href="#/title/${esc(c.type)}/${esc(c.id)}" title="${esc(c.title)}">
    <div class="poster">${p ? `<img loading="lazy" src="${esc(p)}" alt="">` : `<div class="noimg">${esc(c.title)}</div>`}
      <span class="type">${c.type === "tv" ? "TV" : "Film"}</span>${st}</div>
    <div class="meta"><div class="t">${esc(c.title)}</div><div class="y">${esc(c.year || "")}</div></div></a>`;
}

const skeletonRow = () => `<div class="scroller">${Array.from({ length: 8 }, () => `<div><div class="skeleton" style="aspect-ratio:2/3"></div></div>`).join("")}</div>`;

async function pageDiscover(main) {
  const hr = new Date().getHours();
  const hello = hr < 12 ? "Morning" : hr < 18 ? "Afternoon" : "Evening";
  let seenHow = false;
  try { seenHow = localStorage.getItem("encorr.how") === "1"; } catch (_) {}
  main.innerHTML = `
    <section class="welcome">
      <h1>${hello}, ${esc(state.user.username)}. What do you fancy watching?</h1>
      <form class="searchbar big" id="homeSearch" role="search">${I.search}<input type="search" id="hq" placeholder="Search for a film or TV show" autocomplete="off" aria-label="Search for a film or TV show"><button class="btn primary" type="submit">Search</button></form>
      ${seenHow ? "" : `<ol class="how glass" id="how">
        <li><span class="n">1</span><div><strong>Find it</strong><span>Search, or browse what's popular below.</span></div></li>
        <li><span class="n">2</span><div><strong>Request it</strong><span>Press Request. That's all you do.</span></div></li>
        <li><span class="n">3</span><div><strong>Watch it in Plex</strong><span>It downloads by itself and we tell you when it's ready.</span></div></li>
        <button class="x" id="howClose" aria-label="Hide these tips" title="Got it">${I.check} Got it</button>
      </ol>`}
    </section>
    <div id="hero" class="hero skeleton"></div>
    <h2>Trending this week</h2><div id="row-trending">${skeletonRow()}</div>
    <h2>Popular films</h2><div id="row-movies">${skeletonRow()}</div>
    <h2>Popular TV</h2><div id="row-tv">${skeletonRow()}</div>
    <h2>Coming soon to UK cinemas</h2><div id="row-upcoming">${skeletonRow()}</div>`;
  qs("#homeSearch").addEventListener("submit", (e) => {
    e.preventDefault();
    const v = qs("#hq").value.trim();
    location.hash = "#/search" + (v ? "?q=" + encodeURIComponent(v) : "");
  });
  const hc = qs("#howClose");
  if (hc) hc.addEventListener("click", () => { try { localStorage.setItem("encorr.how", "1"); } catch (_) {} qs("#how").remove(); });
  const load = async (kind) => {
    const el = qs("#row-" + kind);
    try {
      const r = await api("/discover/" + kind);
      el.innerHTML = `<div class="scroller">${r.results.map(card).join("")}</div>`;
      return r.results;
    } catch (e) {
      el.innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
      return [];
    }
  };
  const [trending] = await Promise.all(["trending", "movies", "tv", "upcoming"].map(load));
  const hero = (trending || []).find((t) => t.backdrop) || null;
  const h = qs("#hero");
  if (!h) return;
  if (!hero) { h.remove(); return; }
  setBackdrop(hero.backdrop);
  h.classList.remove("skeleton");
  h.style.backgroundImage = `url("${img(hero.backdrop, "w1280")}")`;
  h.innerHTML = `<div class="inner">
      <div class="eyebrow">Trending this week</div>
      <h1>${esc(hero.title)}</h1>
      <p>${esc(hero.overview)}</p>
      <div class="row">
        ${hero.status === "available" ? `<span class="have">${MARK(18)} In your Plex library</span>` : ["processing", "pending"].includes(hero.status) ? `<span class="have req">${I.check} Requested</span>` : `<a class="btn primary" href="#/title/${esc(hero.type)}/${esc(hero.id)}">${I.plus} Request</a>`}
        <a class="btn" href="#/title/${esc(hero.type)}/${esc(hero.id)}">More info</a>
      </div></div>`;
}

// ---------- search ----------
function pageSearch(main, q) {
  main.innerHTML = `
    <div class="page-head"><div><h1>Search</h1><div class="muted">Find a film or show to request.</div></div></div>
    <div class="searchbar">${I.search}<input type="search" id="q" placeholder="Search films and TV…" value="${esc(q)}" autocomplete="off"></div>
    <div id="results" style="margin-top:24px"></div>`;
  const input = qs("#q");
  input.focus();
  let t;
  const run = async (val) => {
    const box = qs("#results");
    if (!val.trim()) { box.innerHTML = `<div class="empty"><div class="big">🎬</div>Type a title to get started.</div>`; return; }
    box.innerHTML = `<div class="grid">${Array.from({ length: 12 }, () => `<div class="skeleton" style="aspect-ratio:2/3"></div>`).join("")}</div>`;
    try {
      const r = await api("/search?q=" + encodeURIComponent(val));
      if (input.value !== val) return;
      box.innerHTML = r.results.length ? `<div class="grid">${r.results.map(card).join("")}</div>` : `<div class="empty"><div class="big">🔍</div>Nothing found for “${esc(val)}”.</div>`;
    } catch (e) {
      box.innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
    }
  };
  input.addEventListener("input", () => {
    clearTimeout(t);
    t = setTimeout(() => {
      history.replaceState(null, "", "#/search?q=" + encodeURIComponent(input.value));
      run(input.value);
    }, 350);
  });
  run(q);
}

// ---------- title page ----------
// ---------- the journey ----------
// Every request walks the same four steps. Showing them as one line answers
// "where's my film?" at a glance, for anyone.
const JOURNEY = ["Requested", "Approved", "Downloading", "In Plex"];
const STAGE_TEXT = { Downloading: "Downloading", Queued: "Queued for download", Paused: "Download paused", Unpacking: "Unpacking", Importing: "Moving it into your library", "Waiting for Plex": "Downloaded. Waiting for Plex to add it", Stuck: "Download is stuck", Failed: "Download failed" };

// status: pending | approved | processing | declined | failed | completed | available
function journeyState(status, p) {
  switch (status) {
    case "pending": return { at: 1, text: "Waiting for the owner to approve it" };
    case "declined": return { at: 1, bad: true, text: "The owner declined this request" };
    case "failed": return { at: 2, bad: true, text: "Couldn't start the download. The owner can retry it" };
    case "completed": case "available": return { at: 4, text: "Ready to watch in Plex" };
  }
  if (!p) return { at: 2, text: "Looking for a copy to download", fill: 0 };
  const stage = p.stage || "Downloading";
  const text = STAGE_TEXT[stage] || stage;
  if (["Unpacking", "Importing", "Waiting for Plex"].includes(stage)) return { at: 3, text, busy: true };
  const bits = [];
  if (stage !== "Stuck" && stage !== "Failed") bits.push(`${p.percent}%`);
  if (p.timeLeft) bits.push(`${p.timeLeft} left`);
  if (p.speed) bits.push(p.speed);
  return { at: 2, text, bits, fill: p.percent, bad: stage === "Stuck" || stage === "Failed", note: p.note };
}

function journeyHTML(status, p, compact) {
  const j = journeyState(status, p);
  const steps = JOURNEY.map((label, i) => {
    const cls = i < j.at ? "done" : i === j.at ? (j.bad ? "now bad" : "now") : "";
    // The line after a step fills as that step finishes; while downloading it
    // fills with the download itself.
    const fill = i < j.at ? 100 : i === j.at && j.fill != null ? j.fill : 0;
    const node = i < j.at ? (i === 3 ? MARK(compact ? 14 : 20) : I.check) : i === j.at && j.bad ? "!" : "";
    return `<li class="${cls}" style="--f:${fill}%"><span class="node">${node}</span><span class="lab">${label}</span></li>`;
  }).join("");
  const done = j.at >= 4;
  const detail = `<div class="jtext ${j.bad ? "bad" : done ? "good" : ""}"><strong>${esc(j.text)}</strong>${j.bits && j.bits.length ? `<span>${j.bits.map(esc).join(", ")}</span>` : ""}${j.busy ? `<span class="spinner sm" aria-hidden="true"></span>` : ""}</div>${j.note && !compact ? `<div class="small dim">${esc(j.note)}</div>` : ""}`;
  return `<div class="journey ${compact ? "compact" : "glass"} ${done ? "complete" : ""}" ${compact ? "" : 'id="journey"'} role="group" aria-label="Request progress: ${esc(j.text)}"><ol>${steps}</ol>${detail}</div>`;
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
  const facts = [d.year, d.cert ? `<span class="cert">${esc(d.cert)}</span>` : "", runtime, d.network, d.rating ? `★ ${d.rating.toFixed(1)}` : ""]
    .filter(Boolean).map((f) => (String(f).startsWith("<span") ? f : esc(f))).join("<span class='dim'>•</span>");

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

  main.innerHTML = `
  <div class="title-top">
    <div class="poster">${d.poster ? `<img src="${esc(img(d.poster, "w500"))}" alt="">` : ""}</div>
    <div>
      <div class="dim small" style="font-weight:600">${type === "tv" ? "TV show" : "Film"}</div>
      <h1>${esc(d.title)}</h1>
      <div class="facts">${facts}</div>
      ${d.tagline ? `<p class="tagline">${esc(d.tagline)}</p>` : ""}
      <div class="genres">${(d.genres || []).map((g) => `<span>${esc(g)}</span>`).join("")}</div>
      <div class="actions">
        ${requestable ? `<button class="btn primary" id="reqBtn">${I.plus} ${s.status === "partial" || (type === "tv" && Object.keys(s.seasons || {}).length) ? "Request more" : "Request"}</button>` : ""}
        ${statusLine}
        ${d.trailer ? `<button class="btn" id="trailer">${I.play} Trailer</button>` : ""}
        ${cancelBtn}
      </div>
      ${prog}
      ${mine ? `<div class="small muted">You requested this ${esc(ago(mine.created))}${mine.status === "pending" ? ": waiting for an admin." : "."}</div>` : s.requestCount ? `<div class="small muted">Requested by ${s.requestCount} ${s.requestCount === 1 ? "person" : "people"}.</div>` : ""}
      <p class="overview">${esc(d.overview)}</p>
    </div>
  </div>
  ${seasons.length ? `<h2>Seasons</h2><div class="seasons">${seasons.map((x) => {
    const st = seasonState(x.season_number);
    return `<div class="glass season"><div><strong>${esc(x.name)}</strong><div class="small dim">${x.episode_count} episodes${x.air_date ? " · " + esc(x.air_date.slice(0, 4)) : ""}</div></div>${st ? pill(st) : ""}</div>`;
  }).join("")}</div>` : ""}
  ${data.recommended && data.recommended.length ? `<h2>More like this</h2><div class="scroller">${data.recommended.map(card).join("")}</div>` : ""}`;

  const reqBtn = qs("#reqBtn");
  if (reqBtn) reqBtn.addEventListener("click", () => requestModal(type, d, s));
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
  const seasonHtml = type === "tv" ? `
    <label class="check" style="border-bottom:1px solid var(--glass-border);margin-bottom:6px"><input type="checkbox" id="allS" checked> <strong>All available seasons</strong></label>
    <div id="sList" style="max-height:280px;overflow:auto">${seasons.map((x) => {
      const taken = !free.includes(x);
      return `<label class="check"><input type="checkbox" value="${x.season_number}" ${taken ? "disabled" : "checked"}> <span class="grow">${esc(x.name)} <span class="dim small">(${x.episode_count} episodes)</span></span>${st(x.season_number) ? pill(st(x.season_number)) : ""}</label>`;
    }).join("")}</div>` : "";
  const role = state.user.role;
  const m = modal(`
    <h2>Request ${esc(d.title)}</h2>
    <p class="muted" style="margin-top:-6px">${role === "friend" ? "An admin will approve it, then it downloads automatically." : "Approved automatically: it'll start downloading straight away."}</p>
    ${seasonHtml}
    <label class="field"><span>Note (optional)</span><input type="text" id="note" maxlength="300" placeholder="e.g. extended cut please"></label>
    <div class="foot"><button class="btn" data-close>Cancel</button><button class="btn primary" id="go">${I.plus} Request the encore</button></div>`);
  const all = qs("#allS", m.el);
  if (all) {
    const boxes = qsa("#sList input:not([disabled])", m.el);
    all.addEventListener("change", () => boxes.forEach((b) => (b.checked = all.checked)));
    boxes.forEach((b) => b.addEventListener("change", () => (all.checked = boxes.every((x) => x.checked))));
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
      if (isAdmin()) refreshPending();
    } catch (err) {
      toast(err.message, "error");
      busy(e.currentTarget, false);
    }
  });
}

// ---------- requests ----------
async function pageRequests(main, filter) {
  const admin = isAdmin();
  const tabs = admin
    ? [["pending", "Waiting for approval"], ["active", "In progress"], ["completed", "Completed"], ["all", "All"]]
    : [["active", "In progress"], ["completed", "Completed"], ["mine", "All mine"], ["all", "Everyone"]];
  filter = filter || tabs[0][0];
  main.innerHTML = `
    <div class="page-head"><div><h1>Requests</h1><div class="muted">${admin ? "Approve, decline and keep an eye on downloads." : "What you've asked for, and where it's up to."}</div></div>
      <div class="tabs">${tabs.map(([k, l]) => `<button data-f="${k}" class="${k === filter ? "on" : ""}">${esc(l)}</button>`).join("")}</div></div>
    <div id="rlist" class="list">${Array.from({ length: 4 }, () => `<div class="skeleton" style="height:105px"></div>`).join("")}</div>`;
  qsa(".tabs button", main).forEach((b) => b.addEventListener("click", () => (location.hash = "#/requests?filter=" + b.dataset.f)));
  const load = async () => {
    const box = qs("#rlist");
    if (!box) return;
    try {
      const r = await api("/requests?filter=" + filter);
      if (!r.results.length) {
        const msg = { pending: "Nothing waiting for approval.", active: "Nothing on its way right now.", completed: "Nothing completed yet. Finished requests land here." }[filter] || "No requests here yet.";
        box.innerHTML = `<div class="empty"><div class="big">🎟️</div>${msg}<div style="margin-top:14px"><a class="btn primary" href="#/discover">Find something to request</a></div></div>`;
        return;
      }
      box.innerHTML = r.results.map(reqRow).join("");
      bindReqActions(box, load);
      // Quick refresh while something is downloading, slow otherwise.
      const fast = r.results.some((x) => x.progress);
      if (fast !== state.reqFast) { state.reqFast = fast; restart(); }
    } catch (e) {
      box.innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
    }
  };
  let timer;
  const restart = () => { clearInterval(timer); timer = setInterval(load, state.reqFast ? 5000 : 30000); state.timers.push(timer); };
  state.reqFast = false;
  restart();
  await load();
}

function reqRow(r) {
  const admin = isAdmin();
  const p = img(r.poster, "w185");
  const prog = journeyHTML(r.status, r.progress, true);
  let actions = "";
  if (admin && r.status === "pending") actions = `<button class="btn sm ok" data-act="approve" data-id="${r.id}">${I.check} Approve</button><button class="btn sm danger" data-act="decline" data-id="${r.id}">Decline</button>`;
  else if (admin && r.status === "failed") actions = `<button class="btn sm" data-act="retry" data-id="${r.id}">Retry</button>`;
  if (r.status === "pending" && (admin || r.requestedBy === state.user.id)) actions += `<button class="btn sm" data-act="delete" data-id="${r.id}">${admin ? "Delete" : "Cancel"}</button>`;
  else if (admin && r.status !== "pending") actions += `<button class="btn sm" data-act="delete" data-id="${r.id}" title="Remove from the list">Remove</button>`;
  const src = { watchlist: "from Plex watchlist", app: "from the app", discord: "from Discord" }[r.source] || "";
  return `<div class="glass req">
    <a href="#/title/${esc(r.type)}/${esc(r.tmdbId)}">${p ? `<img src="${esc(p)}" alt="">` : `<div class="ph"></div>`}</a>
    <div class="info">
      <a class="t" href="#/title/${esc(r.type)}/${esc(r.tmdbId)}" style="color:inherit">${esc(r.title)} <span class="dim" style="font-weight:500">${esc(r.year || "")}</span></a>
      <div class="s">${r.type === "tv" ? esc(seasonsText(r.seasons)) + " · " : "Film · "}${esc(r.user || "Someone")} · ${esc(ago(r.created))}${src ? " · " + esc(src) : ""}</div>
      ${r.note ? `<div class="s">“${esc(r.note)}”</div>` : ""}
      ${r.error ? `<div class="err">${esc(r.error)}</div>` : ""}
    </div>
    <div class="right"><div class="row">${actions}</div></div>
    <div class="track">${prog}</div>
  </div>`;
}

function bindReqActions(box, reload) {
  qsa("[data-act]", box).forEach((b) => b.addEventListener("click", async () => {
    const id = b.dataset.id, act = b.dataset.act;
    try {
      if (act === "approve") { busy(b, true, "Approving…"); await api(`/requests/${id}/approve`, { method: "POST", body: {} }); toast("Approved: sent to download", "ok"); }
      if (act === "retry") { busy(b, true, "Retrying…"); await api(`/requests/${id}/retry`, { method: "POST", body: {} }); toast("Sent again"); }
      if (act === "decline") {
        const reason = window.prompt("Reason for declining (optional, shown to the requester):", "");
        if (reason === null) return;
        await api(`/requests/${id}/decline`, { method: "POST", body: { reason } });
        toast("Declined");
      }
      if (act === "delete") { if (!confirmBox("Remove this request?")) return; await api(`/requests/${id}`, { method: "DELETE" }); toast("Removed"); }
      await reload();
      if (isAdmin()) refreshPending();
    } catch (e) { toast(e.message, "error"); busy(b, false); }
  }));
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
          <div class="small dim">${u.requests || 0} request${u.requests === 1 ? "" : "s"}, joined ${esc(ago(u.created))}</div></div>
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
    <div class="row" style="margin-top:14px"><button class="btn sm" data-a="detect">🔎 Find ${name} automatically</button><span class="small dim" data-out="detect"></span></div>
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
  main.innerHTML = `<div class="page-head"><div><h1>Settings</h1></div></div>
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
    panel.innerHTML = `<div id="bgCard"></div><h2>Look</h2>
      <div class="swatches">${[["violet", "#9b7bff", "Soft violet"], ["coral", "#ff7a66", "Coral"]].map(([k, c, l]) => `<div class="swatch ${s.accent === k ? "on" : ""}" data-acc="${k}"><div class="chip" style="background:${c}"></div>${l}</div>`).join("")}</div>
      <h2>Requests</h2>
      <p class="lead">Limits for Friends. Trusted users and admins have no limit. Use 0 for no limit.</p>
      <div class="row"><label class="field grow"><span>Films per period</span><input type="number" min="0" id="qm" value="${s.quotas.friendMovieLimit}"></label>
        <label class="field grow"><span>TV shows per period</span><input type="number" min="0" id="qt" value="${s.quotas.friendTvLimit}"></label>
        <label class="field grow"><span>Period (days)</span><input type="number" min="1" id="qd" value="${s.quotas.days}"></label></div>
      <h2>Address and region</h2>
      <label class="field"><span>Public address (optional)</span><input type="url" id="appUrl" value="${esc(s.appUrl)}" placeholder="https://encorr.example.com"><small>Used for links in Discord messages, e.g. your Cloudflare Tunnel address.</small></label>
      <label class="field"><span>Region</span><select id="region">${[["GB", "United Kingdom"], ["IE", "Ireland"], ["US", "United States"], ["CA", "Canada"], ["AU", "Australia"], ["NZ", "New Zealand"]].map(([k, l]) => `<option value="${k}" ${s.region === k ? "selected" : ""}>${l}</option>`).join("")}</select><small>Sets release dates, age ratings and “coming soon”.</small></label>
      <h2>TMDB</h2>${tmdbForm(s.tmdb)}${saveBar()}`;
    loadBackgroundCard(qs("#bgCard"));
    let accent = s.accent;
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
      await api("/settings/general", { method: "PUT", body: { accent, appUrl: qs("#appUrl").value, region: qs("#region").value, quotas: { friendMovieLimit: Number(qs("#qm").value), friendTvLimit: Number(qs("#qt").value), days: Number(qs("#qd").value) } } });
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
    const run = async () => {
      panel.innerHTML = `<div class="row between"><h2 style="margin:0">Health</h2><button class="btn sm" id="recheck">Re-check</button></div><p class="lead" style="margin-top:8px">Checking everything Encorr depends on…</p><div class="checks"><span class="spinner"></span></div>`;
      qs("#recheck").addEventListener("click", run);
      try {
        const r = await api("/health");
        const sym = { ok: "✓", warn: "!", error: "✕", off: "–" };
        const bad = r.checks.filter((c) => c.status === "error").length;
        qs(".lead", panel).textContent = bad ? `${bad} thing${bad > 1 ? "s need" : " needs"} attention.` : "Everything's looking good.";
        qs(".checks", panel).innerHTML = r.checks.map((c) => `<div class="glass checkrow"><div class="ic ${c.status}">${sym[c.status] || "?"}</div><div><strong>${esc(c.name)}</strong><div class="small">${esc(c.message)}</div>${c.fix && c.status !== "ok" ? `<div class="fix">→ ${esc(c.fix)}</div>` : ""}</div></div>`).join("");
      } catch (e) { qs(".checks", panel).innerHTML = `<div class="notice error">${esc(e.message)}</div>`; }
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
