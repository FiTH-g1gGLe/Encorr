// Finding things: home page rows, posters, see-all pages and collections.
"use strict";

// ---------- cards & rows ----------
function card(c) {
  const st = c.status && c.status !== "none" ? `<div class="flag">${pill(c.status === "available" ? "available" : c.status)}</div>` : "";
  const p = img(c.poster);
  const rating = c.rating ? `<span class="crate">${c.rating.toFixed(1)}</span>` : "";
  const quick = (!c.status || c.status === "none") ? `<span class="qadd" role="button" tabindex="0" data-qreq="${esc(c.type)}:${esc(c.id)}" aria-label="Request ${esc(c.title)}"${tip(c.type === "tv" ? "Request: pick the seasons" : "Request this film")}>${I.plus}</span>` : "";
  return `<a class="card" href="#/title/${esc(c.type)}/${esc(c.id)}" title="${esc(c.title)}">
    <div class="poster">${p ? `<img loading="lazy" src="${esc(p)}" alt="">` : `<div class="noimg">${esc(c.title)}</div>`}
      ${c.rating ? `<span class="rchip">${c.rating.toFixed(1)}</span>` : ""}${st}${quick}
      <div class="peek"><div class="pt">${esc(c.title)}</div><div class="pm">${esc(c.year || "")} ${rating}</div>${c.overview ? `<div class="po">${esc(c.overview)}</div>` : ""}<span class="pgo">${c.status === "available" ? "In " + YOUR_LIB() : "View"} ${I.chev}</span></div></div>
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
        <li><span class="n">3</span><div><strong>Watch it in ${BOTH() ? "Plex or Jellyfin" : LIB()}</strong><span>It downloads by itself and we tell you when it's ready.</span></div></li>
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
    box.innerHTML = `<div class="glass libsync">${L.running ? `<span class="spinner sm"></span>` : ""}<div><strong>${L.running ? `Reading ${YOUR_LIB()}…` : `Encorr hasn't read all of ${YOUR_LIB()} yet`}</strong>
      <span class="small dim">${L.films || L.shows ? `Found ${L.films} films and ${L.shows} shows so far. ` : ""}${L.running ? `Anything you already have will show “${IN_LIB()}” once it's done.` : "It'll try again shortly."}</span></div></div>`;
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
  const action = (x) => x.status === "available" ? `<span class="have">${MARK(18)} In ${YOUR_LIB()}</span>`
    : ["processing", "pending"].includes(x.status) ? `<span class="have req">${I.check} Requested</span>`
    : `<a class="btn primary" href="#/title/${esc(x.type)}/${esc(x.id)}">${I.plus} Request</a>`;
  h.innerHTML = slides.map((x, i) => `<div class="slide ${i === 0 ? "on" : ""}" style="background-image:url('${esc(img(x.backdrop, "w1280"))}')" aria-hidden="${i === 0 ? "false" : "true"}">
      <div class="inner">
        ${x.status === "available" ? `<div class="qchips"><span class="qchip plex">In ${YOUR_LIB()}</span></div>` : ""}<div class="eyebrow"><span class="ech">Trending this week</span><span>${x.type === "tv" ? "TV show" : "Film"}</span></div>
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
  const summary = [`${c.parts.length} films`, have ? `${have} in ${YOUR_LIB()}` : "", asked ? `${asked} on the way` : "", upcoming ? `${upcoming} not dated yet` : ""].filter(Boolean).join(" · ");
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
        ${canAsk ? `<button class="btn primary" id="reqAll"${tip(who)}>${I.plus} Request the ${c.missing === c.parts.length ? "whole series" : c.missing + " you haven't got"}</button>` : `<span class="have">${MARK(18)} ${have === c.parts.length ? "You've got the whole series" : `Everything that's out is in ${BOTH() ? "your library" : LIB()} or on its way`}</span>`}
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
