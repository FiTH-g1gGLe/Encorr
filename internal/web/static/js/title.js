// Film and show pages, the request journey and the season picker.
"use strict";

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
    case "completed": case "available": return { at: 4, text: `Ready to watch in ${BOTH() ? "your library" : LIB()}` };
  }
  const r = part || {};
  if (!p && r.state === "waiting") {
    const when = r.release ? `Out to watch at home ${niceDate(r.release)}` : r.nextAiring ? `Next episode ${niceDate(r.nextAiring)}` : "Not released yet";
    return { at: 2, text: `${when}`, bits: ["it'll download by itself when it's out"], fill: 0, wait: true };
  }
  if (!p && r.state === "finishing") return { at: 3, text: `All there. Waiting for ${BOTH() ? "your library" : LIB()} to confirm`, busy: true };
  if (!p && r.want > 0 && r.have > 0) {
    const left = r.want - r.have;
    return { at: 2, text: `${r.have} of ${r.want} episodes in ${BOTH() ? "your library" : LIB()}`, bits: [`looking for the other ${left}`], fill: Math.round((r.have / r.want) * 100) };
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
  if (s.status === "available") statusLine = `<span class="have">${MARK(18)} In ${YOUR_LIB()}</span>`;
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
  if (d.rating) ratings.push(`<span class="rt tmdb"${tip(`TMDB users${d.voteCount ? ` (${d.voteCount.toLocaleString()} votes)` : ""}`)}>${appIcon("tmdb", 18)}<span class="sr-only">TMDB</span> ${Math.round(d.rating * 10)}%</span>`);
  for (const r of (P && P.ratings) || []) {
    if (r.source === "tmdb") continue;
    const ic = r.source === "imdb" ? `${appIcon("imdb", 18)}<span class="sr-only">IMDb</span>` : r.source === "rt-critic" ? `${appIcon("rotten-tomatoes", 18)}<span class="sr-only">Rotten Tomatoes</span>` : `${appIcon("rotten-tomatoes", 18)}<b class="rtb aud ${r.fresh ? "fresh" : "rotten"}">Audience</b>`;
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
  if (P && (P.quality || P.audio)) fact("In " + YOUR_LIB(), [P.quality, P.audio].filter(Boolean).map((q) => `<span class="qual">${esc(q)}</span>`).join(" "));
  const providers = (d.providers || []).length ? `<div class="fblock"><div class="k">Currently streaming on</div><div class="provs">${d.providers.map((p) => `<a href="${esc(d.providersUrl || "#")}" target="_blank" rel="noopener" title="${esc(p.name)}"><img src="${esc(img(p.logo, "w92"))}" alt="${esc(p.name)}"></a>`).join("")}</div><div class="tiny dim">Streaming info from JustWatch</div></div>` : "";
  const links = [
    `<a href="https://www.themoviedb.org/${type}/${esc(d.id)}" target="_blank" rel="noopener">TMDB</a>`,
    d.imdbId ? `<a href="https://www.imdb.com/title/${esc(d.imdbId)}" target="_blank" rel="noopener">IMDb</a>` : "",
    d.imdbId ? `<a href="https://trakt.tv/search/imdb/${esc(d.imdbId)}" target="_blank" rel="noopener">Trakt</a>` : "",
    type === "movie" ? `<a href="https://letterboxd.com/tmdb/${esc(d.id)}" target="_blank" rel="noopener">Letterboxd</a>` : "",
    type === "tv" && d.tvdbId ? `<a href="https://thetvdb.com/dereferrer/series/${esc(d.tvdbId)}" target="_blank" rel="noopener">TheTVDB</a>` : "",
    d.homepage ? `<a href="${esc(d.homepage)}" target="_blank" rel="noopener">Official site</a>` : "",
  ].filter(Boolean).join("");

  const statusPill = s.status === "available" ? `<span class="tpill ok">${MARK(14)} In ${YOUR_LIB()}</span>`
    : s.status === "partial" ? `<span class="tpill part">Some seasons in ${BOTH() ? "your library" : LIB()}</span>`
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
          ${data.jellyfin && data.jellyfin.url && ["available", "partial"].includes(s.status) ? `<a class="btn jf" href="${esc(data.jellyfin.url)}" target="_blank" rel="noopener"${tip("Open it in Jellyfin")}>${I.play} Play on Jellyfin</a>` : ""}
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
  const SEASON_STATUS = { available: IN_LIB(), partial: "Partly in " + (BOTH() ? "your library" : LIB()), processing: "On its way", pending: "Waiting for approval" };
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
