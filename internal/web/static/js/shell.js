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
