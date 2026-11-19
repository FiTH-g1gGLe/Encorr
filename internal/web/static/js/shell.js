// The app frame: start-up, menu, page routing and signing in.
"use strict";

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
  applyLibName();
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

// applyLibName rewrites the fixed labels for this server ("In Plex",
// "In Jellyfin" or "In your library"). Labels are set once at start-up.
function applyLibName() {
  const L = LIB(), IN = IN_LIB(), YL = YOUR_LIB(), to = BOTH() ? "your library" : L;
  STATUS.available[1] = IN; STATUS.completed[1] = IN; STATUS.partial[1] = "Partly in " + to;
  PILL_TIPS.available = `Already in ${YL}: ready to watch`; PILL_TIPS.completed = `Arrived in ${to}`; PILL_TIPS.partial = `Some of it is in ${to}`;
  STATES.finishing.label = `Adding to ${to}`; STATES.finishing.tip = `Downloaded, being unpacked or added to ${to}`;
  STAGE_TIPS[4] = `Unpacking it and adding it to ${to}`; STAGE_TIPS[5] = `${IN}, ready to watch`;
  EP_STATUS.have[0] = IN;
  JOURNEY[3] = IN;
  STAGE_TEXT["Waiting for Plex"] = `Downloaded. Waiting for ${to} to add it`;
  STEP_TIPS[3] = `It's in ${to}, ready to watch`;
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
      <a class="item" data-page="settings" href="#/settings"${tip("Plex, Jellyfin, Radarr, Sonarr, SABnzbd, Discord and more")}>${I.settings}<span class="label">Settings</span></a>` : ""}
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
    let issues = 0;
    if (isAdmin()) { try { issues = (await api("/issues")).open || 0; } catch { /* ignore */ } }
    const n = isAdmin() ? (g.needs || 0) + issues : r.newReady || 0;
    const b = qs("#pendingBadge");
    if (b) { b.hidden = n === 0; b.textContent = n; b.title = isAdmin() ? "Needs you" : "Ready since you last looked"; b.classList.toggle("good", !isAdmin()); }
  } catch { /* ignore */ }
}

// ---------- sign-in ----------
async function plexSignIn(btn, after, checkPath = "/auth/plex/check") {
  const popup = window.open("about:blank", "plexauth", "width=620,height=720");
  busy(btn, true, "Waiting for Plex…");
  try {
    const pin = await api("/auth/plex/pin", { method: "POST", body: {} });
    if (popup) popup.location.href = pin.url;
    else window.open(pin.url, "_blank");
    const started = Date.now();
    while (Date.now() - started < 180000) {
      await new Promise((r) => setTimeout(r, 1500));
      const res = await api(checkPath, { method: "POST", body: { id: pin.id }, allow401: true });
      if (res.done) {
        if (popup && !popup.closed) popup.close();
        toast("Signed in with Plex", "ok");
        await after();
        return;
      }
      if (popup && popup.closed && Date.now() - started > 4000) {
        // One last check in case they approved just before closing.
        const last = await api(checkPath, { method: "POST", body: { id: pin.id }, allow401: true });
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

// jellyfinSignIn offers Quick Connect (a code typed into a Jellyfin app
// you're already signed in to) and, as the other option, a Jellyfin
// username and password. Encorr passes them to Jellyfin and keeps neither.
function jellyfinSignIn(after) {
  const s = state.status || {};
  const name = s.jellyfinName ? esc(s.jellyfinName) : "Jellyfin";
  const m = modal(`<div class="jfsign">
    <div class="jfhead"><span class="jfmark">${I.play}</span><div><h2>Sign in with Jellyfin</h2><div class="small dim">${name}</div></div></div>
    ${s.quickConnect ? `<div class="jftabs" role="tablist"><button class="on" data-tab="qc" role="tab">Quick Connect</button><button data-tab="pw" role="tab">Password</button></div>` : ""}
    <div data-pane="qc" ${s.quickConnect ? "" : "hidden"}>
      <p class="small muted">Open Jellyfin on a phone, TV or computer where you're already signed in. Go to your profile, choose <strong>Quick Connect</strong> and type this code:</p>
      <div class="jfcode" id="jfcode" aria-live="polite"><span class="spinner"></span></div>
      <div class="jfwait small dim" id="jfwait">Getting a code…</div>
    </div>
    <form data-pane="pw" ${s.quickConnect ? "hidden" : ""} autocomplete="on" class="signin-form">
      <label class="field"><span>Jellyfin username</span><input type="text" id="jfu" autocomplete="username" autocapitalize="none" spellcheck="false" required></label>
      <label class="field"><span>Password</span><input type="password" id="jfp" autocomplete="current-password"></label>
      <div id="jferr"></div>
      <button class="btn jf wide" type="submit" id="jfgo">Sign in</button>
    </form>
    <button class="btn wide ghost" data-close style="margin-top:12px">Cancel</button>
  </div>`);
  let alive = true, secret = "";
  const stop = () => { alive = false; };
  qsa("[data-close]", m.el).forEach((b) => b.addEventListener("click", stop));
  m.el.parentElement.addEventListener("click", (e) => { if (e.target === m.el.parentElement) stop(); });
  const done = async () => { stop(); m.close(); toast("Signed in with Jellyfin", "ok"); await after(); };
  const show = (tab) => {
    qsa("[data-tab]", m.el).forEach((b) => b.classList.toggle("on", b.dataset.tab === tab));
    qsa("[data-pane]", m.el).forEach((p) => { p.hidden = p.dataset.pane !== tab; });
    if (tab === "pw") qs("#jfu", m.el).focus();
  };
  qsa("[data-tab]", m.el).forEach((b) => b.addEventListener("click", () => show(b.dataset.tab)));
  qs("form[data-pane=pw]", m.el).addEventListener("submit", async (e) => {
    e.preventDefault();
    const btn = qs("#jfgo", m.el);
    busy(btn, true, "Signing in…");
    qs("#jferr", m.el).innerHTML = "";
    try {
      await api("/auth/jellyfin/login", { method: "POST", body: { username: qs("#jfu", m.el).value.trim(), password: qs("#jfp", m.el).value }, allow401: true });
      await done();
    } catch (err) {
      qs("#jferr", m.el).innerHTML = `<div class="notice error">${esc(err.message)}</div>`;
      busy(btn, false);
    }
  });
  const startQuick = async () => {
    const code = qs("#jfcode", m.el), wait = qs("#jfwait", m.el);
    try {
      const r = await api("/auth/jellyfin/quick", { method: "POST", body: {} });
      secret = r.secret;
      code.innerHTML = r.code.split("").map((c) => `<span>${esc(c)}</span>`).join("");
      wait.innerHTML = `<span class="spinner sm"></span> Waiting for you to approve it…`;
    } catch (err) {
      code.innerHTML = "";
      wait.innerHTML = `<div class="notice error">${esc(err.message)}</div>`;
      show("pw");
      return;
    }
    const started = Date.now();
    while (alive && Date.now() - started < 9 * 60000) {
      await new Promise((r) => setTimeout(r, 2000));
      if (!alive || !m.el.isConnected) return; // closed (Esc, Cancel or outside click)
      try {
        const res = await api("/auth/jellyfin/quick/check", { method: "POST", body: { secret }, allow401: true });
        if (res.done) return done();
      } catch (err) {
        if (!alive) return;
        wait.innerHTML = `<div class="notice error">${esc(err.message)}</div><button class="btn sm" id="jfagain" style="margin-top:10px">Get a new code</button>`;
        qs("#jfagain", m.el).addEventListener("click", () => { qs("#jfcode", m.el).innerHTML = `<span class="spinner"></span>`; startQuick(); });
        return;
      }
    }
    if (alive) {
      wait.innerHTML = `The code ran out. <button class="btn sm" id="jfagain">Get a new code</button>`;
      qs("#jfagain", m.el).addEventListener("click", startQuick);
    }
  };
  if (s.quickConnect) startQuick(); else qs("#jfu", m.el).focus();
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
    <div class="signin-alt">
      ${s.plex !== false ? `<button class="btn plex wide" id="signin">${I.plex} Sign in with Plex</button>` : ""}
      ${s.jellyfin ? `<button class="btn jf wide" id="jfsignin">${I.play} Sign in with Jellyfin</button>` : ""}
    </div>
    <p class="small dim" style="margin-top:18px">No account yet? Ask ${s.serverName ? `the owner of ${esc(s.serverName)}` : "the owner"} to make you one.</p>
  </div></div>`;
  const pb = qs("#signin"); if (pb) pb.addEventListener("click", (e) => plexSignIn(e.currentTarget, boot));
  const jb = qs("#jfsignin"); if (jb) jb.addEventListener("click", () => jellyfinSignIn(boot));
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
