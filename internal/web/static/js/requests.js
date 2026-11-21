// Requests: lists, cards, episodes, timeline, download controls.
"use strict";

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
    const arrived = r.arrived ? `Arrived ${esc(ago(r.arrived))}` : IN_LIB();
    return box("good", I.check, "Ready to watch", arrived);
  }
  if (p.blocked) return box("bad", "!", `Downloaded, but stuck before ${BOTH() ? "your library" : LIB()}`, (r.problem ? esc(r.problem) + ". " : "") + (isAdmin() ? "Import it to finish. Auto-fix tries this by itself too" : "The owner has been told"));
  if (r.state === "attention") return box("bad", "!", p.stage === "Failed" ? "The download failed" : "The download is stuck", (r.problem ? esc(r.problem) + ". " : "") + "Auto-fix swaps it for another copy if it doesn't recover");
  if (r.state === "downloading" && p.percent != null) {
    const pct = Math.max(0, Math.min(100, p.percent));
    const label = p.stage === "Paused" ? "Paused" : p.stage === "Queued" ? "Queued" : `${pct}%${p.speed ? ` · ${esc(p.speed)}` : ""}`;
    return `<div class="rprog"><div class="pline"><span class="hi">${label}</span><span class="eta">${p.timeLeft ? `${esc(p.timeLeft)} left` : esc(STAGE_TEXT[p.stage] || "")}</span></div><div class="pbar"><i style="width:${pct}%;--w:${Math.max(pct, 1)}"></i></div></div>`;
  }
  if (r.state === "finishing") return box("live", `<span class="spinner sm"></span>`, STAGE_TEXT[p.stage] || `Adding it to ${BOTH() ? "your library" : LIB()}`, "Almost there");
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

// One strip per season: green in the library, blue downloading, amber still missing.
function seasonStrips(r) {
  const list = r.seasonProgress || [];
  if (r.type !== "tv" || !list.length || r.status !== "approved") return "";
  return `<div class="sstrips">${list.map((x) => {
    if (!x.want) return `<button class="sstrip future" data-eps="${r.id}" data-season="${x.season}"${tip(`Season ${x.season}: not out yet`)}><span class="sl">S${x.season}</span><span class="sbar2"><i class="u" style="flex:1"></i></span><span class="sn">soon</span></button>`;
    const have = x.have, down = x.downloading || 0, miss = Math.max(0, x.want - have - down);
    const seg = (cls, n) => n > 0 ? `<i class="${cls}" style="flex:${n}"></i>` : "";
    return `<button class="sstrip ${have >= x.want ? "full" : ""}" data-eps="${r.id}" data-season="${x.season}"${tip(`Season ${x.season}: ${have} in ${BOTH() ? "your library" : LIB()}${down ? `, ${down} downloading` : ""}${miss ? `, ${miss} missing` : ""}. Tap for episodes`)}><span class="sl">S${x.season}</span><span class="sbar2">${seg("h", have)}${seg("d", down)}${seg("m", miss)}</span><span class="sn">${have}/${x.want}</span></button>`;
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
  if (r.status === "completed" && r.jellyfinUrl) btns.push(`<a class="btn jf grow" href="${esc(r.jellyfinUrl)}" target="_blank" rel="noopener">${I.play} Play on Jellyfin</a>`);
  if (r.type === "tv" && ["approved", "completed"].includes(r.status)) btns.push(`<button class="btn grow" data-eps="${r.id}"${tip(`Every episode: in ${BOTH() ? "your library" : LIB()}, downloading or missing`)}>${I.list} Episodes</button>`);
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
    <div id="issues"></div>
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

  const issuesReady = loadIssues();
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
      const openIssues = await issuesReady;
      if (admin && openIssues) {
        const t = qs(".rstat.needs .n", main);
        if (t) { t.textContent = (r.groups.needs || 0) + openIssues; qs(".rstat.needs", main).classList.remove("zero"); }
      }
      if (!r.results.length && admin && openIssues && chosen === "needs") { box.innerHTML = ""; return; }
      if (!r.results.length) {
        box.innerHTML = `<div class="empty">${esc(GROUP_EMPTY[chosen] || "Nothing here.")}<div style="margin-top:14px"><a class="btn primary" href="#/discover">Find something to request</a></div></div>`;
        return;
      }
      const older = chosen === "ready" ? `<div class="rmore"><button class="btn sm ghost" id="older">Show older</button></div>` : "";
      box.innerHTML = r.results.map(reqCard).join("") + (state.showOlder ? "" : older);
      const ob = qs("#older");
      if (ob) ob.addEventListener("click", async () => { state.showOlder = true; const all = await api("/requests?filter=ready&older=1"); box.innerHTML = all.results.map(reqCard).join(""); bindReqActions(box, load); });
      bindReqActions(box, load);
      // Check every few seconds while anything is moving (or was just asked
      // for), so cards update live.
      const fast = r.results.some((x) => ["downloading", "finishing", "searching"].includes(x.state) || (x.created && Date.now() - new Date(x.created) < 180000 && x.group !== "ready"));
      if (fast !== state.reqFast) { state.reqFast = fast; restart(); }
    } catch (e) {
      box.innerHTML = `<div class="notice error">${esc(e.message)}</div>`;
    }
  };
  let timer;
  const tick = () => { if (!document.hidden && document.body.contains(box0)) load(); };
  const restart = () => { clearInterval(timer); timer = setInterval(tick, state.reqFast ? 3000 : 15000); state.timers.push(timer); };
  // Coming back to the app (or tab) refreshes straight away.
  const onBack = () => {
    if (!document.body.contains(box0)) { document.removeEventListener("visibilitychange", onBack); window.removeEventListener("focus", onBack); return; }
    if (!document.hidden) load();
  };
  const box0 = qs("#rlist");
  document.addEventListener("visibilitychange", onBack);
  window.addEventListener("focus", onBack);
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
    const sum = [`<b>${s.have}</b> in ${BOTH() ? "your library" : LIB()}`, s.downloading ? `<b>${s.downloading}</b> downloading` : "", s.missing ? `<b class="miss">${s.missing}</b> missing` : "", s.total > s.aired ? `${s.total - s.aired} still to air` : ""].filter(Boolean).join(" · ");
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
  let episode = 0; // 0 = the whole season
  let episodes = {}; // season -> [{id, number, title, status}]
  const m = modal(`<div class="mhead"><div><h2>Pick a copy</h2><div class="muted small">${esc(r.title)}</div></div><button class="btn sq ghost" data-close aria-label="Close">${I.x}</button></div>
    ${seasons.length > 1 ? `<div class="etabs">${seasons.map((s) => `<button class="chip ${s === season ? "on" : ""}" data-rs="${s}">Season ${s}</button>`).join("")}</div>` : ""}
    ${r.type === "tv" ? `<div class="eppick" hidden></div>` : ""}
    <div class="rels"></div>`, { wide: true, sheet: true });
  const STATE = { have: "In your library", downloading: "Downloading", missing: "Missing", unaired: "Not out yet", skipped: "Not wanted" };
  const paintEpisodes = () => {
    const box = qs(".eppick", m.el);
    const list = episodes[season] || [];
    if (!box || !list.length) { if (box) box.hidden = true; return; }
    box.hidden = false;
    box.innerHTML = `<span class="small dim">Episode</span>
      <button class="chip ${episode === 0 ? "on" : ""}" data-re="0">Whole season</button>
      ${list.map((e) => `<button class="chip ep ${e.status || ""} ${episode === e.id ? "on" : ""}" data-re="${e.id}"${tip(`E${e.number}${e.title ? ": " + e.title : ""} · ${STATE[e.status] || ""}`)}>E${e.number}</button>`).join("")}`;
    qsa("[data-re]", box).forEach((b) => b.addEventListener("click", () => { episode = Number(b.dataset.re); paintEpisodes(); load(); }));
  };
  if (r.type === "tv") {
    api(`/requests/${reqID}/episodes`).then((x) => {
      for (const s of x.seasons || []) episodes[s.season] = (s.episodes || []).filter((e) => e.number > 0);
      paintEpisodes();
    }).catch(() => {});
  }
  const load = async () => {
    const box = qs(".rels", m.el);
    box.innerHTML = `<div class="rwait"><span class="spinner"></span> <span class="rwt">Asking every indexer. Big shows can take a minute or two…</span></div>`;
    const mine = `${season}:${episode}`;
    const stale = () => mine !== `${season}:${episode}` || !document.body.contains(box);
    try {
      // Start the search, then check back: a slow search can't time out on the way.
      const { job } = await api(`/requests/${reqID}/releases/search`, { method: "POST", body: { season, episode } });
      let x;
      for (;;) {
        await new Promise((res) => setTimeout(res, 2500));
        if (stale()) return; // they picked something else, or closed it
        x = await api(`/releases/search/${job}`);
        if (x.done) break;
        const t = qs(".rwt", box);
        if (t && x.seconds >= 10) t.textContent = `Still asking every indexer (${x.seconds}s). Big shows can take a minute or two…`;
      }
      if (stale()) return;
      if (!x.releases.length) { box.innerHTML = `<p class="muted">No copies found right now. ${episode ? "Try the whole season: episodes often come in season packs." : "Radarr/Sonarr keep looking by themselves."}</p>`; return; }
      box.innerHTML = x.releases.map((rl, i) => `<div class="rel ${rl.approved ? "" : "rej"}">
        <div class="rl"><strong class="rname">${esc(rl.title)}</strong>
          <div class="rmeta"><span class="qchip">${esc(rl.quality || "?")}</span><span>${(rl.size / 1e9).toFixed(1)} GB</span><span>${esc(rl.age)} old</span><span>${esc(rl.indexer)}</span>${rl.fullSeason ? `<span>whole season</span>` : ""}</div>
          ${(rl.languages || []).length ? `<div class="rlang small dim">Also: ${esc(rl.languages.join(", "))}</div>` : ""}
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
  qsa("[data-rs]", m.el).forEach((b) => b.addEventListener("click", () => { season = Number(b.dataset.rs); episode = 0; qsa("[data-rs]", m.el).forEach((c) => c.classList.toggle("on", c === b)); paintEpisodes(); load(); }));
  load();
}

// ---------- problem reports ----------
async function loadIssues() {
  const box = qs("#issues");
  if (!box) return;
  let r;
  try { r = await api("/issues"); } catch { return 0; }
  if (!r.results.length) { box.innerHTML = ""; return 0; }
  const admin = isAdmin();
  const STATUS = { open: ["Waiting for the owner", "open"], replacing: ["Getting a new copy", "replacing"], resolved: ["Sorted", "resolved"] };
  const scope = (i) => i.type !== "tv" || !i.season ? "" : i.episode ? `S${String(i.season).padStart(2, "0")}E${String(i.episode).padStart(2, "0")}` : `Season ${i.season}`;
  box.innerHTML = `<section class="glass issues">
    <div class="ihead"><h2>${I.flag} ${admin ? "Problems reported" : "Problems you reported"}</h2><span class="small dim">${r.open ? `${r.open} waiting` : "All sorted"}</span></div>
    ${r.results.map((i) => {
      const [label, cls] = STATUS[i.status] || [i.status, ""];
      const last = (i.events || []).slice(-1)[0];
      const acts = i.status === "resolved" ? "" : `<div class="row iacts">
        ${i.canReplace ? `<button class="btn sm primary" data-irep="${i.id}"${tip("Deletes this copy, blocklists it and downloads a different one")}>${I.refresh} Get a new copy</button>` : ""}
        ${admin || i.mine ? `<button class="btn sm" data-ires="${i.id}" data-mine="${i.mine && !admin ? 1 : ""}">${I.check} ${admin ? "Mark as sorted" : "It's fine now"}</button>` : ""}
      </div>`;
      return `<div class="issue ${cls}">
        <a href="#/title/${i.type === "tv" ? "tv" : "movie"}/${i.tmdbId}" class="ipost">${i.poster ? `<img src="${esc(img(i.poster, "w92"))}" alt="">` : ""}</a>
        <div class="ibody">
          <div class="ititle"><a href="#/title/${i.type === "tv" ? "tv" : "movie"}/${i.tmdbId}">${esc(i.title)}</a>${scope(i) ? ` <span class="dim">${esc(scope(i))}</span>` : ""}</div>
          <div class="imeta"><span class="ikindpill">${esc(i.kindName)}</span><span class="istat ${cls}">${esc(label)}</span><span class="small dim">${admin ? `${esc(i.reporter || "Someone")}, ` : ""}${when(i.created)}</span></div>
          ${i.note ? `<div class="inote">"${esc(i.note)}"</div>` : ""}
          ${i.status !== "open" && last ? `<div class="small dim ilast">${esc(last.text)}</div>` : ""}
        </div>
        ${acts}
      </div>`;
    }).join("")}
  </section>`;
  qsa("[data-irep]", box).forEach((b) => b.addEventListener("click", async () => {
    if (!confirmBox("Delete this copy and download a different one? It'll be missing from your library until the new one arrives, usually a few minutes.")) return;
    busy(b, true, "Asking…");
    try { const res = await api(`/issues/${b.dataset.irep}/replace`, { method: "POST", body: {} }); toast(res.message || "Getting a new copy", "ok"); loadIssues(); refreshPending(); }
    catch (e) { toast(e.message, "error"); busy(b, false); }
  }));
  qsa("[data-ires]", box).forEach((b) => b.addEventListener("click", async () => {
    let reply = "";
    if (!b.dataset.mine) {
      reply = window.prompt("Add a note for them? (optional, e.g. \"Turn on subtitles in the player\")", "");
      if (reply === null) return;
    }
    busy(b, true, "Saving…");
    try { await api(`/issues/${b.dataset.ires}/resolve`, { method: "POST", body: { reply } }); toast("Marked as sorted", "ok"); loadIssues(); refreshPending(); }
    catch (e) { toast(e.message, "error"); busy(b, false); }
  }));
  return r.open || 0;
}
