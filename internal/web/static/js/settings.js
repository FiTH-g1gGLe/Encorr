// Settings, the setup wizard and the health page.
"use strict";

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
    <label class="field"><span>Address</span><input type="url" data-f="url" data-addr="${isR ? 7878 : 8989}" value="${esc(s.url || "")}" placeholder="http://localhost:${isR ? 7878 : 8989}"></label>
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
        f("url").value = d.url; delete f("url").dataset.auto; f("url").dispatchEvent(new Event("input"));
        f("apiKey").value = d.apiKey;
        out("detect").textContent = d.note || `Found it (${d.configPath}).`;
        busy(btn, false);
        await test(qs('[data-a="test"]', root));
        return;
      }
      if (d.url && (!f("url").value || f("url").dataset.auto)) { f("url").value = d.url; delete f("url").dataset.auto; f("url").dispatchEvent(new Event("input")); }
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
        <label class="field"><span>Plex address</span><input type="url" id="manualUrl" data-addr="32400" placeholder="http://localhost:32400"></label><small class="hint">Then press “Use this” on your server above.</small></details>
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

function librariesForm(libs, attr = "lib") {
  if (!libs || !libs.length) return `<div class="notice warn">No film or TV libraries found on this server.</div>`;
  return `<div class="liblist">${libs.map((l) => `<label class="glass lib"><span><strong>${esc(l.title)}</strong> <span class="dim small">${l.type === "show" ? "TV" : "Films"}</span></span><label class="switch"><input type="checkbox" data-${attr}="${esc(l.key)}" ${l.enabled ? "checked" : ""}><span class="slider"></span></label></label>`).join("")}</div>`;
}
const saveLibraries = (root) => api("/settings/plex/libraries", { method: "PUT", body: { enabled: qsa("[data-lib]", root).filter((c) => c.checked).map((c) => c.dataset.lib) } });
const saveJfLibraries = (root) => api("/settings/jellyfin/libraries", { method: "PUT", body: { enabled: qsa("[data-jflib]", root).filter((c) => c.checked).map((c) => c.dataset.jflib) } });

// jellyfinForm asks for the server address and an administrator's sign-in.
// The password is used once, to make Encorr its own API key, and not kept.
function jellyfinForm(j = {}) {
  return `<div class="jfform">
    <label class="field"><span>Jellyfin address</span><input type="url" id="jfUrl" data-addr="8096" value="${esc(j.url || "")}" placeholder="http://localhost:8096" autocomplete="off"></label>
    <div class="grid2">
      <label class="field"><span>Administrator username</span><input type="text" id="jfUser" autocomplete="username" autocapitalize="none" spellcheck="false"></label>
      <label class="field"><span>Password</span><input type="password" id="jfPass" autocomplete="current-password"></label>
    </div>
    <small class="hint">Used once to give Encorr its own API key (you'll see it as “Encorr” under Dashboard → API Keys). Your password isn't saved.</small>
    <div id="jfOut"></div>
  </div>`;
}
const jfDefault = () => ({ url: state.status && state.status.docker ? "" : "http://localhost:8096" });
// findJellyfin fills the address box when Encorr can see Jellyfin (its
// container name inside Docker, or this machine), unless someone typed one.
async function findJellyfin(root) {
  const inp = qs("#jfUrl", root);
  if (!inp) return;
  try {
    const r = await api("/find/jellyfin", { allow401: true });
    if (r && r.found && (inp.dataset.auto || !inp.value || inp.value === "http://localhost:8096")) {
      inp.value = r.url; delete inp.dataset.auto; inp.dispatchEvent(new Event("input"));
      const out = qs("#jfOut", root); if (out) out.innerHTML = `<div class="small dim" style="margin-top:6px">Found Jellyfin at ${esc(r.url)}.</div>`;
    }
  } catch { /* not found: the box keeps its guess */ }
}
const jfFormBody = (root) => ({ url: qs("#jfUrl", root).value.trim(), username: qs("#jfUser", root).value.trim(), password: qs("#jfPass", root).value });

// ---------- setup wizard ----------
const STEPS = ["signin", "server", "libraries", "jellyfin", "tmdb", "radarr", "sonarr", "discord", "done"];
const STEPS_BACK = {};

async function renderWizard(stepName) {
  setBackdrop(null);
  let settings = null;
  if (state.user) {
    try { settings = await api("/settings"); } catch { /* not admin */ }
  }
  const jfOnly = settings && settings.jellyfin.connected && !settings.plex.url && !state.user.plex;
  let step = stepName || (state.user ? (jfOnly ? "libraries" : "server") : "signin");
  if (step === "server" && jfOnly) step = "libraries"; // set up with Jellyfin: Plex can come later
  if (step === "jellyfin" && settings && settings.jellyfin.connected) step = settings.tmdb.builtIn ? "radarr" : "tmdb";
  if (step === "tmdb" && settings && settings.tmdb.builtIn && !settings.tmdb.hasKey) step = "radarr"; // Encorr's own key works
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
    const body = shell("Welcome to Encorr", "Let's get you set up. It takes about five minutes. First, which media server do you use? You'll be the admin. If you run both, start with either: you can add the other in a moment.",
      `<div class="pickserver">
        <button class="glass srvopt" id="signin"><span class="srvic plex">${I.plex}</span><span><strong>Plex</strong><span class="small dim">Sign in with the Plex account that owns your server</span></span></button>
        <button class="glass srvopt" id="usejf"><span class="srvic jf">${I.play}</span><span><strong>Jellyfin</strong><span class="small dim">Sign in as a Jellyfin administrator</span></span></button>
      </div>
      <div id="jfsetup" hidden style="margin-top:18px">${jellyfinForm(jfDefault())}<button class="btn jf" id="jfConnect" style="margin-top:14px">Connect Jellyfin</button></div>`, { back: false, next: "" });
    qs("#signin").addEventListener("click", (e) => plexSignIn(e.currentTarget, async () => { state.status = await api("/status"); state.user = state.status.user; go("server"); }));
    qs("#usejf").addEventListener("click", () => { qs("#jfsetup", body).hidden = false; qs("#usejf").classList.add("on"); qs("#jfUser", body).focus(); findJellyfin(body); });
    qs("#jfConnect", body).addEventListener("click", async (e) => {
      const btn = e.currentTarget;
      busy(btn, true, "Connecting…");
      qs("#jfOut", body).innerHTML = "";
      try {
        const r = await api("/auth/jellyfin/setup", { method: "POST", body: jfFormBody(body), allow401: true });
        toast(`Connected to ${r.jellyfin.serverName}`, "ok");
        state.status = await api("/status"); state.user = state.status.user;
        go("libraries");
      } catch (err) {
        qs("#jfOut", body).innerHTML = `<div class="notice error">${esc(err.message)}</div>`;
        busy(btn, false);
      }
    });
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
    const P = settings.plex.url, J = settings.jellyfin.connected;
    const lead = P && J ? "Encorr watches these to know what's already in your library. Most people leave them all on."
      : `Encorr watches these to know what's already in ${J ? "Jellyfin" : "Plex"}. Most people leave them all on.`;
    const body = shell("Which libraries?", lead,
      `${P ? `${J ? `<h3 class="libhead">${I.plex} Plex</h3>` : ""}${librariesForm(settings.plex.libraries)}` : ""}
       ${J ? `${P ? `<h3 class="libhead">${I.play} Jellyfin</h3>` : ""}${librariesForm(settings.jellyfin.libraries, "jflib")}` : ""}`);
    if (jfOnly) STEPS_BACK.libraries = "libraries";
    const b = qs("#wBack"); if (b && jfOnly) b.remove();
    onNext(async () => {
      if (P) await saveLibraries(body);
      if (J) await saveJfLibraries(body);
      go(J ? (settings.tmdb.builtIn ? "radarr" : "tmdb") : "jellyfin");
    });
    return;
  }
  if (step === "jellyfin") {
    const body = shell("Also use Jellyfin?", "Optional. If you run Jellyfin as well as Plex, connect it and friends can sign in with either. Encorr checks both to see what you already have.",
      jellyfinForm(jfDefault()), { skip: true, next: "Connect Jellyfin" });
    findJellyfin(body);
    onNext(async () => {
      qs("#jfOut", body).innerHTML = "";
      try {
        const r = await api("/settings/jellyfin", { method: "POST", body: jfFormBody(body) });
        toast(`Connected to ${r.serverName}`, "ok");
        go("libraries");
      } catch (err) {
        qs("#jfOut", body).innerHTML = `<div class="notice error">${esc(err.message)}</div>`;
        busy(qs("#wNext"), false);
      }
    });
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
    if (step === "radarr" && settings.tmdb.builtIn) STEPS_BACK.radarr = settings.jellyfin.connected && !settings.plex.url ? "libraries" : "jellyfin";
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
    const P = settings.plex.url, J = settings.jellyfin.connected;
    const servers = P && J ? "Plex and Jellyfin" : J ? "Jellyfin" : "Plex";
    shell("You're all set", `Encorr will read your ${servers} libraries now. Share this address with your friends: <span class="code">${esc(location.origin)}</span>. They sign in with their own ${servers} accounts${J ? ", or with a Jellyfin Quick Connect code" : ""}.<br><br>Tip: on your phone, open Encorr in Chrome and choose <strong>Add to Home screen</strong> for an app icon.`, "", { next: "Start using Encorr" });
    onNext(async () => {
      await api("/settings/complete", { method: "POST", body: {} });
      state.status = await api("/status");
      location.hash = "#/discover";
      boot();
    });
  }
}

// ---------- settings pages ----------
// Settings menu icons: apps show their own logo; Encorr's own pages use
// thin line icons in the theme colour.
const MENU_ICONS = {
  general: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><path d="M21 4h-7M10 4H3M21 12h-9M8 12H3M21 20h-5M12 20H3M14 2v4M8 10v4M16 18v4"/></svg>',
  health: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M19.5 13.6 12 21l-7.5-7.4A5 5 0 0 1 12 6.6a5 5 0 0 1 7.5 7z"/><path d="M3.5 12h4l1.5-3 3 6 1.5-3h7"/></svg>',
  tune: '<svg viewBox="0 0 512 512" fill="currentColor" aria-hidden="true"><path d="m412.3 227.1c-1.6-3.1-4.8-5.1-8.2-5.1H267.5l46.8-210.8c1-4.3-1.2-8.6-5.2-10.4s-8.7-.6-11.3 3L100.5 275.2c-2 2.8-2.3 6.5-.8 9.6 1.6 3.1 4.8 5.1 8.2 5.1h136.5l-46.8 210.8c-1 4.3 1.2 8.6 5.2 10.4 1.2.6 2.5.8 3.8.8 2.9 0 5.7-1.4 7.5-3.8l197.4-271.4c2.1-2.7 2.4-6.5.8-9.6"/></svg>',
};
const menuIcon = (k) => MENU_ICONS[k] || (k === "subscriptions" ? I.calendar : k === "jobs" ? I.refresh : appIcon(k, 16));
const SETTINGS_MENU = [["general", "General"], ["plex", "Plex"], ["jellyfin", "Jellyfin"], ["radarr", "Radarr"], ["sonarr", "Sonarr"], ["sabnzbd", "SABnzbd"], ["discord", "Discord"], ["health", "Health"], ["tune", "Tune-up"], ["subscriptions", "Subscriptions"], ["jobs", "Jobs & backup"]];

async function pageSettings(main, section) {
  main.innerHTML = `<div class="page-head"><div><h1>Settings</h1><div class="small dim">Encorr ${esc((state.status && state.status.version) || "")} · <a href="https://github.com/FiTH-g1gGLe/Encorr/releases/latest" target="_blank" rel="noopener" class="ghlink"><img src="/icons/github.png" alt="" width="13" height="13">What's new ↗</a></div></div><div id="updbox"></div></div>
    <div class="settings"><nav class="glass menu">${SETTINGS_MENU.map(([k, l]) => `<a href="#/settings/${k}" class="${k === section ? "on" : ""}"><span class="mi">${menuIcon(k)}</span><span class="ml">${esc(l)}</span></a>`).join("")}</nav>
    <section class="glass panel" id="panel"><span class="spinner"></span></section></div>`;
  updateBox();
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
  if (section === "plex" && !s.plex.url && !(state.user && state.user.plex)) {
    // Set up with Jellyfin (or a password): link a Plex account first.
    panel.innerHTML = `<h2>Plex <span class="dim small" style="font-weight:500">optional</span></h2>
      <p class="lead">${s.jellyfin.connected ? "Encorr is using Jellyfin. If you also run Plex, connect it and friends can sign in with either. Encorr checks both to see what you already have." : "Connect the Plex server people will request for."}</p>
      <button class="btn plex" id="plink">${I.plex} Sign in with Plex</button>
      <p class="hint" style="margin-top:12px">Use the Plex account that owns the server. It's linked to you, so you can sign in with it too.</p>
      <div id="picker" style="margin-top:14px"></div>`;
    qs("#plink").addEventListener("click", (e) => plexSignIn(e.currentTarget, async () => {
      state.status = await api("/status"); state.user = state.status.user;
      qs("#plink").remove();
      serverPicker(qs("#picker"), () => pageSettings(main, "plex"));
    }, "/settings/plex/link"));
    return;
  }
  if (section === "jellyfin") {
    const j = s.jellyfin;
    if (!j.connected) {
      panel.innerHTML = `<h2>Jellyfin <span class="dim small" style="font-weight:500">optional</span></h2>
        <p class="lead">${s.plex.url ? "Run Jellyfin as well as Plex? Connect it and friends can sign in with either, and Encorr checks both to see what you already have." : "Connect your Jellyfin server."}</p>
        ${jellyfinForm(jfDefault())}
        <div class="row" style="margin-top:16px"><button class="btn jf" id="jfConnect">Connect Jellyfin</button></div>`;
      findJellyfin(panel);
      qs("#jfConnect").addEventListener("click", async (e) => {
        busy(e.currentTarget, true, "Connecting…");
        qs("#jfOut").innerHTML = "";
        try {
          const r = await api("/settings/jellyfin", { method: "POST", body: jfFormBody(panel) });
          toast(`Connected to ${r.serverName}: reading its library`, "ok");
          state.status = await api("/status"); applyLibName();
          pageSettings(main, "jellyfin");
        } catch (err) { qs("#jfOut").innerHTML = `<div class="notice error">${esc(err.message)}</div>`; busy(e.currentTarget, false); }
      });
      return;
    }
    panel.innerHTML = `<h2>Server</h2>
      <div class="row between"><div><strong>${esc(j.serverName)}</strong><div class="small dim">${esc(j.url)}${j.version ? ` · Jellyfin ${esc(j.version)}` : ""}</div></div><button class="btn sm" id="jfRe">Reconnect</button></div>
      <div id="jfReBox" hidden style="margin-top:14px">${jellyfinForm(j)}<div class="row" style="margin-top:12px"><button class="btn jf sm" id="jfReGo">Reconnect</button></div></div>
      <h2>Libraries</h2><p class="lead">Encorr watches these to know what's already in Jellyfin.</p><div id="jflibs">${librariesForm(j.libraries, "jflib")}</div>
      <h2>Play buttons</h2><p class="lead">Play buttons open Jellyfin at the address above. If friends reach Jellyfin at a different address (from outside your home, say), put it here.</p>
      <label class="field"><span>Address friends use <span class="dim">optional</span></span><input type="url" id="jfPub" value="${esc(j.publicUrl || "")}" placeholder="https://jellyfin.example.com"></label>
      ${saveBar()}
      <h2>Signing in</h2><p class="lead">Anyone with an account on ${esc(j.serverName)} can sign in to Encorr with their Jellyfin username and password, or with a Quick Connect code. New people start as Friends: change roles under <a href="#/users">Users</a>.</p>
      <div class="danger-zone"><button class="btn sm danger" id="jfOff">Disconnect Jellyfin</button></div>`;
    qs("#jfRe").addEventListener("click", () => { qs("#jfReBox").hidden = !qs("#jfReBox").hidden; });
    qs("#jfReGo").addEventListener("click", async (e) => {
      busy(e.currentTarget, true, "Connecting…");
      qs("#jfOut").innerHTML = "";
      try { await api("/settings/jellyfin", { method: "POST", body: jfFormBody(panel) }); toast("Reconnected", "ok"); pageSettings(main, "jellyfin"); }
      catch (err) { qs("#jfOut").innerHTML = `<div class="notice error">${esc(err.message)}</div>`; busy(e.currentTarget, false); }
    });
    qs("#jfOff").addEventListener("click", async () => {
      if (!confirm(`Disconnect ${j.serverName}? Encorr forgets what's in it. Friends who only use Jellyfin can't sign in until you reconnect.`)) return;
      try { await api("/settings/jellyfin", { method: "DELETE" }); toast("Jellyfin disconnected", "ok"); state.status = await api("/status"); applyLibName(); pageSettings(main, "jellyfin"); }
      catch (err) { toast(err.message, "error"); }
    });
    onSave(async () => {
      await api("/settings/jellyfin/public", { method: "PUT", body: { publicUrl: qs("#jfPub").value.trim() } });
      await saveJfLibraries(panel);
    }, "Saved: rereading Jellyfin");
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
      <label class="field"><span>Address</span><input type="url" id="zUrl" data-addr="8080" data-path="/sabnzbd" value="${esc(z.url || "")}" placeholder="http://localhost:8080/sabnzbd"></label>
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
        <div class="hacts"><a class="btn" href="#/settings/tune"${tip("Check your apps' settings against recommended ones")}>${I.wrench} Open Tune-up</a><button class="btn" data-hact="search-missing"${tip("Ask Radarr and Sonarr to look for every missing film and episode now")}>${I.search} Search for everything missing</button></div>
        <div class="checks"><span class="spinner"></span></div>
        <div class="row between" style="align-items:center"><h2>What Encorr has been doing</h2><button class="btn sm" id="alogClear">${I.trash || ""} Clear</button></div><div class="alog"><span class="spinner"></span></div>`;
      qs("#alogClear", panel).addEventListener("click", async (e) => {
        if (!confirmBox("Clear Encorr's activity list and log file? Settings, requests and history aren't touched.")) return;
        busy(e.currentTarget, true, "Clearing…");
        try { await api("/activity", { method: "DELETE" }); toast("Logs cleared", "ok"); run(); }
        catch (err) { toast(err.message, "error"); }
        busy(e.currentTarget, false);
      });
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
        qs(".checks", panel).innerHTML = r.checks.map((c) => `<div class="glass checkrow ${c.status}"><div class="ic ${c.status}">${sym[c.status] || "?"}</div><div class="cbody"><strong class="cname">${appIcon(c.name.replace(/^Indexers?\b.*/i, "prowlarr"), 18)}${esc(c.name)}</strong><div class="small">${esc(c.message)}</div>${c.fix && c.status !== "ok" ? `<div class="fix">${esc(c.fix)}</div>` : ""}${(c.actions || []).length ? `<div class="cacts">${c.actions.map((a) => ACTIONS[a] ? `<button class="btn sm" data-hact="${a}">${I[ACTIONS[a][1]]} ${ACTIONS[a][0]}</button>` : "").join("")}</div>` : ""}</div></div>`).join("");
        bindActs(qs(".checks", panel));
      } catch (e) { qs(".checks", panel).innerHTML = `<div class="notice error">${esc(e.message)}</div>`; }
      try {
        const lg = await api("/activity");
        qs(".alog", panel).innerHTML = lg.items.length ? `<ol class="tlist">${lg.items.map((it) => `<li class="k-${esc(it.kind)}"><span class="tdot">${I[ACT_ICON[it.kind]] || ""}</span><div><strong>${esc(it.title)}</strong>${it.detail ? `<span class="td">${esc(it.detail)}</span>` : ""}<span class="tt">${esc(exact(it.at))}${it.requestId ? ` · <a href="#/requests?filter=all">request</a>` : ""}</span></div></li>`).join("")}</ol>` : `<p class="muted">Nothing yet. When Encorr fixes a download or something needs you, it's listed here.</p>`;
      } catch (e) { qs(".alog", panel).innerHTML = `<div class="notice error">${esc(e.message)}</div>`; }
    };
    run();
  }
  if (section === "tune") {
    const APPS = { sabnzbd: "SABnzbd", radarr: "Radarr", sonarr: "Sonarr", prowlarr: "Prowlarr", plex: "Plex", jellyfin: "Jellyfin" };
    const LEVEL = { safe: ["Safe", "Standard advice for every setup"], review: ["Your call", "Good for most setups, but worth a read first"], warn: ["Needs a look", ""], info: ["Advice", ""] };
    const BOTTLE = { internet: "Your internet line", provider: "Your news server", disk: "The download folder's drive", cpu: "The CPU", settings: "Settings or the provider", unknown: "Not sure yet" };
    const mbs = (v) => (v ? `${Math.round(v)} MB/s` : "–");
    const speedCard = (t) => {
      if (!t) return `<div class="glass tspeed empty"><div><strong>Speed test</strong><div class="small dim">Measures your internet line, the download folders and the CPU, and how much your news server actually has. Takes about 20 seconds. The results tune the recommendations below to this computer.</div></div><button class="btn primary" id="speedRun">${I.refresh} Run speed test</button></div>`;
      const srv = (t.servers || []).filter((x) => x.tried > 0);
      const tile = (k, v, sub, bad) => `<div class="tst ${bad ? "bad" : ""}"><span>${k}</span><b>${v}</b><i>${sub || ""}</i></div>`;
      return `<div class="glass tspeed">
        <div class="tshead"><div><strong>Speed test</strong><div class="small dim">${esc(ago(t.at))}${t.internetBy ? ` · internet measured by ${esc(t.internetBy)}` : ""}</div></div><button class="btn sm" id="speedRun">${I.refresh} Run again</button></div>
        <div class="tstiles">
          ${tile("Internet", mbs(t.internetMBs), `${Math.round((t.internetMBs || 0) * 8)} Mb/s`, t.bottleneck === "internet")}
          ${tile("Download folder", mbs(t.downloadDirMBs), "writing speed", t.bottleneck === "disk")}
          ${tile("Complete folder", mbs(t.completeDirMBs), "writing speed", t.completeDirMBs && t.completeDirMBs < 60)}
          ${tile("CPU score", t.cpu ? Math.round(t.cpu).toLocaleString() : "–", "100,000+ is ideal", t.bottleneck === "cpu")}
          ${srv.map((x) => tile(esc(x.label || x.name), `${Math.round(x.rate)}%`, x.backup ? "found · backup, only fills gaps" : "of articles found", !x.backup && x.rate < 80 && x.tried > 50000)).join("")}
        </div>
        <div class="tsverdict b-${esc(t.bottleneck)}"><span class="ri-ic">${t.bottleneck === "settings" ? I.check : "!"}</span><div><strong>Slowest part: ${esc(BOTTLE[t.bottleneck] || t.bottleneck)}</strong>${(t.advice || []).map((a) => `<span>${esc(a)}</span>`).join("")}</div></div>
      </div>`;
    };
    const row = (f) => {
      const [lv, lvTip] = LEVEL[f.level] || ["", ""];
      const change = f.status === "change";
      return `<div class="tfind ${esc(f.status)} l-${esc(f.level)}">
        <div class="tfmain"><div class="tftop">${appBadge(f.app, APPS[f.app])}<strong>${esc(f.title)}</strong>${change || f.level === "warn" ? `<span class="tlv l-${esc(f.level)}"${lvTip ? tip(lvTip) : ""}>${lv}</span>` : ""}</div>
          <div class="tfwhy">${esc(f.why)}</div>
          ${f.current || f.recommended ? `<div class="tfvals">${f.current ? `<span class="cur">${esc(f.current)}</span>` : ""}${f.recommended ? `<span class="arrow">→</span><span class="rec">${esc(f.recommended)}</span>` : ""}</div>` : ""}
          ${f.fix ? `<div class="tffix">${I.hand} ${esc(f.fix)}</div>` : ""}
          ${f.failed ? `<div class="tffail">${I.warn}<span><b>Last try didn't work:</b> ${esc(f.failed)}</span></div>` : ""}</div>
        ${f.canApply ? `<button class="btn sm ${f.level === "safe" ? "primary" : ""}" data-apply="${esc(f.id)}">${f.failed ? "Try again" : "Apply"}</button>` : f.status === "ok" ? `<span class="tok">${I.check}</span>` : ""}
      </div>`;
    };
    const load = async () => {
      panel.innerHTML = `<div class="row between"><h2 style="margin:0">Tune-up</h2><button class="btn sm" id="recheck">${I.refresh} Check again</button></div>
        <p class="lead" style="margin-top:8px">Checks your apps' settings against recommended ones and explains each in plain English. Nothing changes until you press Apply, and every change can be undone.</p>
        <div class="tbody"><div class="rwait"><span class="spinner"></span> Reading settings from your apps…</div></div>`;
      qs("#recheck").addEventListener("click", load);
      let r;
      try { r = await api("/tune"); } catch (e) { qs(".tbody", panel).innerHTML = `<div class="notice error">${esc(e.message)}</div>`; return; }
      const fs = r.findings;
      const todo = fs.filter((f) => f.status === "change");
      const safe = todo.filter((f) => f.level === "safe" && f.canApply);
      const warn = fs.filter((f) => f.level === "warn");
      const advice = fs.filter((f) => f.status === "notice" && f.level !== "warn");
      const good = fs.filter((f) => f.status === "ok");
      const appsHtml = r.apps.map((a) => `<span class="tappchip ${a.connected ? "on" : a.error ? "bad" : ""}"${tip(a.error || (a.connected ? "Connected" : "Not connected"))}><i></i>${appIcon(a.app)}${esc(APPS[a.app])}${a.version ? ` <em>${esc(a.version)}</em>` : ""}</span>`).join("");
      const prow = r.apps.find((a) => a.app === "prowlarr");
      const prowForm = prow && !prow.connected ? `<details class="glass tprow"><summary><strong>Connect Prowlarr</strong> <span class="small dim">to check your indexers too (optional)</span></summary>
        <div class="grid2"><label class="field"><span>Address</span><input id="pUrl" type="url" data-addr="9696" placeholder="http://localhost:9696" value="${esc((s.prowlarr || {}).url || "")}"></label>
        <label class="field"><span>API key</span><input id="pKey" type="password" placeholder="${(s.prowlarr || {}).hasKey ? "Saved" : "Prowlarr → Settings → General"}"></label></div>
        <button class="btn" id="pSave">Connect</button></details>` : "";
      const section = (title, list, extra = "") => list.length ? `<h3 class="tsec">${title}${extra}</h3><div class="tlist2">${list.map(row).join("")}</div>` : "";
      const changes = (r.changes || []).slice(0, 12);
      const sabOn = r.apps.some((a) => a.app === "sabnzbd" && a.connected);
      qs(".tbody", panel).innerHTML = `
        ${speedCard(r.speed)}
        ${sabOn ? `<div class="glass tconn" id="tconn"></div>` : ""}
        <div class="tapps">${appsHtml}</div>
        ${prowForm}
        <div class="glass tsum"><div><b>${todo.length}</b> recommended change${todo.length === 1 ? "" : "s"}${warn.length ? ` · <b class="w">${warn.length}</b> to look at` : ""} · <b class="g">${good.length}</b> already good</div>
          ${safe.length ? `<button class="btn primary" id="applySafe"${tip("Only the ones marked Safe. The rest are your call")}>${I.check} Apply ${safe.length} safe change${safe.length === 1 ? "" : "s"}</button>` : ""}</div>
        ${section("Needs a look", warn)}
        ${section("Recommended", todo.filter((f) => f.level !== "warn"))}
        ${section("Advice", advice)}
        ${good.length ? `<details class="tgood"><summary>${good.length} setting${good.length === 1 ? " is" : "s are"} already as recommended</summary><div class="tlist2">${good.map(row).join("")}</div></details>` : ""}
        ${changes.length ? `<h3 class="tsec">Changes made by Tune-up</h3><div class="tlist2">${changes.map((c) => `<div class="tfind chg ${c.undone ? "undone" : ""}"><div class="tfmain"><div class="tftop">${appBadge(c.app, APPS[c.app])}<strong>${esc(c.title)}</strong></div><div class="tfwhy">${esc(exact(c.at))} by ${esc(c.by || "someone")}${c.undone ? " · undone" : ""}</div></div>${c.canUndo && !c.undone ? `<button class="btn sm" data-undo="${c.id}">${I.refresh} Undo</button>` : ""}</div>`).join("")}</div>` : ""}`;
      const apply = async (ids, btn) => {
        busy(btn, true, "Applying…");
        try {
          const res = await api("/tune/apply", { method: "POST", body: { ids } });
          const bad = res.results.filter((x) => !x.ok);
          if (res.changed) toast(`Changed ${res.changed} setting${res.changed === 1 ? "" : "s"}`, "ok");
          bad.forEach((b) => toast(b.error, "error"));
          load();
        } catch (e) { toast(e.message, "error"); busy(btn, false); }
      };
      qsa("[data-apply]", panel).forEach((b) => b.addEventListener("click", () => apply([b.dataset.apply], b)));
      const as = qs("#applySafe", panel);
      if (as) as.addEventListener("click", () => apply(safe.map((f) => f.id), as));
      qsa("[data-undo]", panel).forEach((b) => b.addEventListener("click", async () => {
        busy(b, true, "Undoing…");
        try { await api(`/tune/undo/${b.dataset.undo}`, { method: "POST", body: {} }); toast("Undone", "ok"); load(); }
        catch (e) {
          if (/changed again/.test(e.message) && confirmBox(e.message)) {
            try { await api(`/tune/undo/${b.dataset.undo}`, { method: "POST", body: { force: true } }); toast("Undone", "ok"); load(); return; } catch (e2) { toast(e2.message, "error"); }
          } else if (!/changed again/.test(e.message)) toast(e.message, "error");
          busy(b, false);
        }
      }));
      // Connection helper: tries a few connection counts on a live download.
      const drawConn = (c) => {
        const box = qs("#tconn", panel);
        if (!box) return;
        const top = Math.max(1, ...c.steps.map((x) => x.mbs));
        const bars = c.steps.length ? `<div class="cbars">${c.steps.map((x) => `<div class="cbar ${x.connections === c.best - 2 || x.connections === c.best ? "best" : ""}"><i style="height:${Math.max(6, (x.mbs / top) * 100)}%"></i><b>${Math.round(x.mbs)}</b><span>${x.connections}</span></div>`).join("")}${c.running && c.trying ? `<div class="cbar now"><i style="height:6%"></i><b>…</b><span>${c.trying}</span></div>` : ""}</div><div class="small dim">MB/s at each number of connections</div>` : "";
        const head = c.running
          ? `<div><strong>Finding the best connection count…</strong><div class="small dim">Trying ${c.trying || "…"} connections on ${esc(c.label || "your news server")}. Takes about 5 minutes; keep something downloading.</div></div><button class="btn sm" id="connStop">${I.stop} Stop</button>`
          : `<div><strong>Connection helper</strong><div class="small dim">${c.message ? esc(c.message) : c.error ? esc(c.error) : "Tries a few connection counts while something downloads, measures the real speed of each, and keeps the smallest number that reaches top speed (plus two spare). Start a big download first."}</div></div><button class="btn sm" id="connRun">${I.refresh} ${c.steps.length ? "Run again" : "Find the best number"}</button>`;
        box.innerHTML = `<div class="tshead">${head}</div>${bars}`;
        const run = qs("#connRun", box), stop = qs("#connStop", box);
        if (run) run.addEventListener("click", async () => { busy(run, true, "Starting…"); try { await api("/tune/connections/start", { method: "POST", body: {} }); pollConn(); } catch (e) { toast(e.message, "error"); busy(run, false); } });
        if (stop) stop.addEventListener("click", async () => { busy(stop, true, "Stopping…"); try { await api("/tune/connections/stop", { method: "POST", body: {} }); } catch (e) { toast(e.message, "error"); } });
      };
      const pollConn = async () => {
        try {
          const c = await api("/tune/connections");
          drawConn(c);
          if (c.running) { const t = setTimeout(pollConn, 2500); state.timers.push(t); }
          else if (c.best && c.finished && Date.now() - new Date(c.finished).getTime() < 15000) { toast("Connection helper finished", "ok"); }
        } catch { /* ignore */ }
      };
      if (sabOn) pollConn();
      const sr = qs("#speedRun", panel);
      if (sr) sr.addEventListener("click", async () => {
        busy(sr, true, "Testing… about 20 seconds");
        try { await api("/tune/speedtest", { method: "POST", body: {} }); toast("Speed test done", "ok"); load(); } catch (e) { toast(e.message, "error"); busy(sr, false); }
      });
      const pd = qs(".tprow", panel);
      if (pd) pd.addEventListener("toggle", async () => {
        if (!pd.open || pd.dataset.looked) return;
        pd.dataset.looked = "1";
        const u = qs("#pUrl", panel), k = qs("#pKey", panel);
        try {
          const d = await api("/settings/detect/prowlarr");
          if (d.url && (!u.value || u.dataset.auto)) { u.value = d.url; delete u.dataset.auto; u.dispatchEvent(new Event("input")); }
          if (d.found && d.apiKey && !k.value) k.value = d.apiKey;
        } catch { /* type it in */ }
      });
      const ps = qs("#pSave", panel);
      if (ps) ps.addEventListener("click", async () => {
        busy(ps, true, "Connecting…");
        try { await api("/settings/prowlarr", { method: "PUT", body: { enabled: true, url: qs("#pUrl").value, apiKey: qs("#pKey").value } }); toast("Prowlarr connected", "ok"); s = await api("/settings"); load(); } catch (e) { toast(e.message, "error"); busy(ps, false); }
      });
    };
    load();
  }
  if (section === "subscriptions") {
    const CUR = { GBP: "£", USD: "$", EUR: "€" };
    const PLAN = { monthly: "Monthly", yearly: "Yearly", lifetime: "Lifetime", block: "Block", other: "One-off" };
    const KIND = { provider: "News server", indexer: "Indexer", other: "Other" };
    const ST = { urgent: "Renew now", soon: "Renewing soon", expired: "Expired", ok: "Active", lifetime: "Lifetime", unknown: "No date yet", off: "Turned off" };
    const money = (v, c) => `${CUR[c] || ""}${Number(v).toFixed(v % 1 ? 2 : 0)}${CUR[c] ? "" : " " + esc(c || "")}`;
    const bytes = (b) => (b >= 1e12 ? (b / 1e12).toFixed(b >= 1e13 ? 0 : 1) + " TB" : b >= 1e9 ? Math.round(b / 1e9) + " GB" : Math.round(b / 1e6) + " MB");
    const SVG = (d) => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${d}</svg>`;
    const KICON = {
      provider: SVG('<rect x="3" y="4" width="18" height="6" rx="2"/><rect x="3" y="14" width="18" height="6" rx="2"/><path d="M7 7h.01M7 17h.01"/>'),
      indexer: SVG('<circle cx="11" cy="11" r="6"/><path d="m20 20-4.5-4.5"/>'),
      other: SVG('<path d="M4 7a3 3 0 0 1 3-3h10a3 3 0 0 1 3 3v10a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3z"/><path d="M9 12h6M12 9v6"/>'),
    };
    const PENCIL = SVG('<path d="M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16z"/>');
    const LINK = SVG('<path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>');
    const fmtDate = (d) => new Date(d).toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
    const when = (v, i) => {
      if (v.plan === "block" && v.blockSize) {
        const used = v.blockUsed || 0, left = Math.max(0, v.blockSize - used), pct = Math.min(100, Math.round((used / v.blockSize) * 100));
        return `<div class="swhen"><b>${bytes(left)}</b><span>left of ${bytes(v.blockSize)}</span>
          <div class="subtime" role="img" aria-label="${pct}% used"><i style="width:${100 - pct}%"></i></div>
          ${v.renews && v.status !== "lifetime" ? `<span class="sdate">${I.calendar} Ends ${esc(fmtDate(v.renews))}</span>` : ""}</div>`;
      }
      if (v.status === "lifetime") return `<div class="swhen"><b>Lifetime</b><span>never renews</span><div class="subtime full"><i style="width:100%"></i></div></div>`;
      if (v.daysLeft != null) {
        const d = v.daysLeft, span = v.plan === "monthly" ? 31 : 365, pct = d < 0 ? 0 : Math.min(100, Math.round((d / span) * 100));
        const big = d < 0 ? "Ended" : d === 0 ? "Today" : d > 999 ? "999+ days" : `${d} day${d === 1 ? "" : "s"}`;
        const verb = d < 0 ? "Ended" : v.plan === "monthly" || v.plan === "yearly" ? "Renews" : "Ends";
        return `<div class="swhen"><b>${big}</b><span>${verb} ${esc(fmtDate(v.renews))}</span><div class="subtime"><i style="width:${pct}%"></i></div></div>`;
      }
      return `<div class="swhen none"><button class="btn sm ghost" data-edit="${i}">${I.calendar} Add renewal date</button></div>`;
    };
    const card = (v, i) => {
      const planTxt = PLAN[v.plan] || (v.source === "Prowlarr" && v.renews ? "VIP account" : "");
      const bits = [esc(KIND[v.kind] || v.kind), planTxt ? esc(planTxt) : ""].filter(Boolean).join(" · ");
      const cost = v.cost ? ` · <b class="scost">${money(v.cost, v.currency)}</b>${v.plan === "monthly" ? "/month" : v.plan === "yearly" ? "/year" : ""}` : "";
      return `<article class="subrow s-${esc(v.status)}" style="--i:${i}">
        <div class="sico"${tip(ST[v.status] || "")}>${KICON[v.kind] || KICON.other}</div>
        <div class="sbody">
          <div class="sname"><b>${esc(v.name)}</b>${v.source === "You" ? `<span class="schip">Added by you</span>` : appBadge(v.source)}<span class="sstate">${esc(ST[v.status] || v.status)}</span></div>
          <div class="ssub">${bits}${cost}</div>
          ${v.note ? `<div class="snote">${I.warn} ${esc(v.note)}</div>` : ""}${v.notes ? `<div class="snotes">${esc(v.notes)}</div>` : ""}
        </div>
        ${when(v, i)}
        <div class="sact">
          <button class="btn sm sq" data-edit="${i}" aria-label="Edit ${esc(v.name)}"${tip(v.id ? "Edit" : "Add details")}>${PENCIL}</button>
          ${v.url ? `<a class="btn sm sq" href="${esc(v.url)}" target="_blank" rel="noopener" aria-label="Open account page"${tip("Open account page")}>${LINK}</a>` : ""}
          ${v.source === "You" ? `<button class="btn sm sq" data-del="${v.id}" aria-label="Remove"${tip("Remove")}>${I.trash}</button>` : ""}
        </div>
      </article>`;
    };
    const GROUPS = [
      ["Coming up", "Renews or runs out within 30 days", (v) => ["urgent", "soon", "expired"].includes(v.status)],
      ["Active", "", (v) => v.status === "ok"],
      ["Lifetime", "Paid once, never renews", (v) => v.status === "lifetime"],
      ["Needs a date", "Add a renewal date and Encorr will remind you before it runs out", (v) => v.status === "unknown" || v.status === "off"],
    ];
    const listHTML = (items) => {
      if (!items.length) return `<div class="subempty">${I.calendar}<strong>No subscriptions yet</strong><span class="small dim">Connect Prowlarr or SABnzbd, or add one yourself.</span></div>`;
      const idx = new Map(items.map((v, i) => [v, i]));
      return GROUPS.map(([title, sub, test]) => {
        const g = items.filter(test);
        return g.length ? `<section class="sgroup"><h3>${esc(title)} <span>${g.length}</span></h3>${sub ? `<p class="small dim">${esc(sub)}</p>` : ""}<div class="slist">${g.map((v) => card(v, idx.get(v))).join("")}</div></section>` : "";
      }).join("");
    };
    const editor = (v, done) => {
      const date = v.renews ? v.renews.slice(0, 10) : "";
      const auto = v.source !== "You" && v.source;
      const m = modal(`<h2 style="margin-top:0">${v.name ? esc(v.name) : "Add a subscription"}</h2>
        ${auto ? `<p class="small dim" style="margin-top:-6px">Found in ${esc(auto)}. Anything you fill in here is added on top; dates and block sizes from ${esc(auto)} still update by themselves.</p>` : ""}
        <div class="subform">
          <label class="field wide"><span>Name</span><input type="text" data-s="name" value="${esc(v.name || "")}" placeholder="e.g. Plex Pass"></label>
          <label class="field"><span>Type</span><select data-s="kind">${Object.entries(KIND).map(([k, l]) => `<option value="${k}" ${v.kind === k ? "selected" : ""}>${l}</option>`).join("")}</select></label>
          <label class="field"><span>Plan</span><select data-s="plan"><option value="">Not set</option>${Object.entries(PLAN).map(([k, l]) => `<option value="${k}" ${v.plan === k ? "selected" : ""}>${l}</option>`).join("")}</select></label>
          <label class="field"><span>Cost</span><input data-s="cost" type="number" min="0" step="0.01" inputmode="decimal" value="${v.cost || ""}" placeholder="0.00"></label>
          <label class="field"><span>Currency</span><select data-s="currency">${["GBP", "USD", "EUR"].map((c) => `<option ${(v.currency || "GBP") === c ? "selected" : ""}>${c}</option>`).join("")}</select></label>
          <label class="field wide"><span>Renews or ends on</span><input data-s="renews" type="date" value="${esc(date)}"></label>
          <label class="field wide"><span>Account page</span><input data-s="url" type="url" value="${esc(v.url || "")}" placeholder="https://…"></label>
          <label class="field wide"><span>Notes</span><input type="text" data-s="notes" value="${esc(v.notes || "")}" placeholder="Anything worth remembering"></label>
        </div>
        <div class="row" style="margin-top:20px;justify-content:flex-end"><button class="btn" data-close>Cancel</button><button class="btn primary" id="subSave">Save</button></div>`);
      qs("#subSave", m.el).addEventListener("click", async (e) => {
        const val = (k) => qs(`[data-s="${k}"]`, m.el).value.trim();
        const body = { key: v.key || "", name: val("name"), kind: val("kind"), plan: val("plan"), cost: parseFloat(val("cost")) || 0, currency: val("currency"), renews: val("renews"), url: val("url"), notes: val("notes") };
        if (!body.name) { toast("Give it a name", "error"); return; }
        busy(e.currentTarget, true, "Saving…");
        try {
          await api(v.id ? `/subscriptions/${v.id}` : "/subscriptions", { method: v.id ? "PUT" : "POST", body });
          m.close(); toast("Saved", "ok"); done();
        } catch (err) { toast(err.message, "error"); busy(e.currentTarget, false); }
      });
    };
    const load = async () => {
      let d;
      try { d = await api("/subscriptions"); } catch (e) { panel.innerHTML = `<div class="notice error">${esc(e.message)}</div>`; return; }
      const items = d.items || [];
      const due = items.filter((v) => v.status === "urgent" || v.status === "soon" || v.status === "expired").length;
      const life = items.filter((v) => v.status === "lifetime").length;
      const yearly = Object.entries(d.yearly || {}).filter(([, v]) => v > 0);
      const next = items.filter((v) => v.daysLeft != null && v.daysLeft >= 0 && v.status !== "lifetime").sort((a, b) => a.daysLeft - b.daysLeft)[0];
      panel.innerHTML = `<div class="row between"><h2 style="margin:0">Subscriptions</h2><button class="btn sm primary" id="subAdd">${I.plus} Add subscription</button></div>
        <p class="lead" style="margin-top:8px">Your news servers, indexers and blocks in one place. Dates and data left are read from Prowlarr and SABnzbd; add costs and anything else yourself. You'll get a reminder 14, 7 and 1 days before something runs out, and when a block is 75% and 90% used.</p>
        <div class="subsum">
          <div class="subtile nxt"><span>Next up</span><b>${next ? esc(next.name) : "Nothing due"}</b><i>${next ? (next.daysLeft === 0 ? "runs out today" : `in ${next.daysLeft} day${next.daysLeft === 1 ? "" : "s"}`) : "you're all set"}</i></div>
          <div class="subtile"><span>Every year</span><b>${yearly.length ? yearly.map(([c, v]) => money(v, c)).join(" + ") : "–"}</b><i>${yearly.length ? "monthly and yearly plans" : "add costs to see a total"}</i></div>
          <div class="subtile ${due ? "warn" : ""}"><span>Needs attention</span><b>${due}</b><i>${due ? "within 30 days" : "nothing soon"}</i></div>
          <div class="subtile"><span>Lifetime</span><b>${life}</b><i>never renew</i></div>
        </div>
        ${!d.prowlarr ? `<div class="notice subhint">${I.spark} Connect Prowlarr in <a href="#/settings/tune">Tune-up</a> and your indexers' VIP dates show up here by themselves.</div>` : ""}
        ${!d.sabnzbd ? `<div class="notice subhint">${I.spark} Connect <a href="#/settings/sabnzbd">SABnzbd</a> to see your news servers and how much of each block is left.</div>` : `<div class="small dim subtipline">${I.hand} Tip: set a server's expiry date and block size (Quota) in SABnzbd → Config → Servers and they appear here automatically.</div>`}
        ${listHTML(items)}
        <div id="ureport"></div>`;
      usenetReport(qs("#ureport"));
      qs("#subAdd").addEventListener("click", () => editor({ source: "You", kind: "indexer", plan: "yearly", currency: "GBP" }, load));
      qsa("[data-edit]", panel).forEach((b) => b.addEventListener("click", () => editor(items[+b.dataset.edit], load)));
      qsa("[data-del]", panel).forEach((b) => b.addEventListener("click", async () => {
        if (!confirm("Remove this subscription?")) return;
        try { await api(`/subscriptions/${b.dataset.del}`, { method: "DELETE" }); toast("Removed", "ok"); load(); } catch (e) { toast(e.message, "error"); }
      }));
    };
    panel.innerHTML = `<div class="rwait"><span class="spinner"></span> Reading your accounts…</div>`;
    load();
    return;
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

// ---------- updates ----------
async function updateBox() {
  const box = qs("#updbox");
  if (!box || !isAdmin()) return;
  let u;
  try { u = await api("/update"); } catch { return; }
  if (!u.available) {
    const line = qs(".page-head .small.dim");
    if (line && !qs("#updCheck")) {
      line.insertAdjacentHTML("beforeend", ` · <a href="#" id="updCheck">${u.error ? "Couldn't check for updates, try again" : "Up to date, check again"}</a>`);
      qs("#updCheck").addEventListener("click", async (e) => {
        e.preventDefault();
        e.currentTarget.textContent = "Checking…";
        try { await api("/update?check=1"); } catch { /* shown below */ }
        qs("#updCheck").remove();
        updateBox();
      });
    }
    return;
  }
  const notes = (u.notes || "").split("\n").filter((l) => l.trim() && !l.startsWith("**") && !l.startsWith("## ")).slice(0, 4).map((l) => esc(l.replace(/^- /, ""))).join("<br>");
  if (u.how === "docker") {
    box.innerHTML = `<div class="updpill glass"><span class="updic">${I.up}</span><div><strong>Version ${esc(u.latest)} is out</strong><span class="small dim">In Portainer, open the Encorr container and press Recreate with "Re-pull image" on.</span></div><a class="btn sm" href="${esc(u.url)}" target="_blank" rel="noopener">What's new</a></div>`;
    return;
  }
  box.innerHTML = `<div class="updpill glass"><span class="updic">${I.up}</span><div><strong>Version ${esc(u.latest)} is out</strong><span class="small dim"${notes ? tip(notes.replace(/<br>/g, " · ")) : ""}>You're on ${esc(u.current)}. Takes about a minute.</span></div>
    ${u.how === "button" ? `<button class="btn sm primary" id="updGo">${I.download} Update now</button>` : `<a class="btn sm primary" href="${esc(u.url)}" target="_blank" rel="noopener">Download</a>`}</div>`;
  const go = qs("#updGo");
  if (go) go.addEventListener("click", async () => {
    if (!confirmBox(`Update Encorr to ${u.latest}? It restarts by itself and this page comes back in about a minute. Downloads keep going in SABnzbd meanwhile.`)) return;
    busy(go, true, "Downloading…");
    qsa("#bgBtn").forEach((b) => (b.disabled = true)); // nothing else while it updates
    try {
      await api("/update/install", { method: "POST", body: {} });
    } catch (e) { toast(e.message, "error"); busy(go, false); return; }
    const m = modal(`<div class="updwait"><span class="spinner"></span><h2>Updating to ${esc(u.latest)}</h2><p class="muted">Encorr is restarting with the new version. This page reloads by itself.</p></div>`);
    const started = Date.now();
    const poll = async () => {
      try {
        const st = await fetch("/api/v1/status", { cache: "no-store" }).then((r) => r.json());
        if (st.version === u.latest) { location.reload(); return; }
      } catch { /* still restarting */ }
      if (Date.now() - started > 180000) {
        m.close();
        toast("Encorr hasn't come back yet. If it doesn't, start it again by hand: the new version is in place.", "error");
        return;
      }
      setTimeout(poll, 2500);
    };
    setTimeout(poll, 4000);
  });
}

// ---------- Usenet report (Subscriptions page) ----------
async function usenetReport(box) {
  if (!box) return;
  box.innerHTML = `<section class="ureport"><div class="uhead"><h2>How they're doing</h2></div><div class="rwait"><span class="spinner"></span> Adding up the numbers…</div></section>`;
  let r;
  try { r = await api("/usenet/report"); } catch (e) { box.innerHTML = ""; return; }
  if (!r.servers.length && !r.indexers.length) { box.innerHTML = ""; return; }
  const gb = (v) => (v >= 1000 ? (v / 1000).toFixed(v >= 10000 ? 0 : 1) + " TB" : v >= 10 ? Math.round(v) + " GB" : v.toFixed(1) + " GB");
  const lasts = (d) => (d >= 730 ? `about ${Math.round(d / 365)} years` : d >= 60 ? `about ${Math.round(d / 30)} months` : `about ${d} days`);
  const sp = r.speed || {};
  const pct = Math.max(0, Math.min(100, sp.percent || 0));
  const speedCard = sp.samples ? `<div class="glass uspeed ${pct && pct < 40 ? "low" : ""}">
      <div><span class="small dim">Recent download speed</span><b>${Math.round(sp.recentMBs)} MB/s</b><span class="small dim">average of your last ${sp.samples} big download${sp.samples === 1 ? "" : "s"}</span></div>
      ${sp.lineMBs ? `<div class="ubarwrap"><div class="ubar"><i style="width:${pct}%"></i></div><span class="small">${pct}% of your ${Math.round(sp.lineMBs)} MB/s line</span></div>` : `<div class="small dim">Run the speed test in Tune-up to compare this with your line.</div>`}
    </div>` : "";
  const tips = r.tips.length ? `<div class="utips">${r.tips.map((t) => `<div class="utip">${I.spark}<span>${esc(t)}</span></div>`).join("")}</div>` : `<div class="utip ok">${I.check}<span>Nothing to change: your servers and indexers look healthy.</span></div>`;
  const servers = r.servers.length ? `<h3 class="usub">News servers <span class="small dim">last 30 days</span></h3>
    <div class="ulist">${r.servers.map((v) => `<div class="urow ${v.enabled ? "" : "off"}">
      <div class="uname"><strong>${esc(v.label)}</strong><span class="small dim">${v.block ? "Block" : "Unlimited"} · priority ${v.priority}${v.network ? ` · <span class="unet"${v.sameAs && v.sameAs.length ? tip("Same network as " + v.sameAs.join(", ")) : ""}>${esc(v.network)} network</span>` : ""}</span></div>
      <div class="ushare"><div class="ubar"><i style="width:${Math.min(100, v.share30)}%"></i></div><span class="small">${gb(v.gb30)} · ${v.share30}%</span></div>
      <div class="uextra small">${v.block ? `${gb(v.block.usedGB)} of ${gb(v.block.sizeGB)} used${v.block.daysLeft ? ` · lasts ${lasts(v.block.daysLeft)}` : " · not touched lately"}` : `${gb(v.gb7)} this week`}</div>
    </div>`).join("")}</div>` : "";
  const indexers = r.indexers.length ? `<h3 class="usub">Indexers <span class="small dim">last 90 days</span></h3>
    <div class="ulist">${r.indexers.map((v) => {
      const ok = v.grabs90 ? Math.round((v.imported90 / v.grabs90) * 100) : 0;
      return `<div class="urow ${v.enabled ? "" : "off"}">
        <div class="uname"><strong>${esc(v.name)}</strong><span class="small dim">${v.lifetime ? "Lifetime" : v.expires ? "Renews " + esc(new Date(v.expires).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })) : "No VIP date"} · priority ${v.priority}${v.avgMs ? ` · answers in ${v.avgMs} ms` : ""}</span></div>
        <div class="ushare"><div class="ubar"><i style="width:${Math.min(100, v.share90)}%"></i></div><span class="small">${v.grabs90} found · ${v.share90}%</span></div>
        <div class="uextra small">${v.grabs90 ? `${ok}% made it to your library${v.failed90 ? ` · ${v.failed90} failed` : ""}` : "Nothing grabbed lately"}</div>
      </div>`;
    }).join("")}</div>
    <p class="small dim" style="margin-top:8px">"Found" is how many downloads Radarr and Sonarr took from each indexer. One with a high share is earning its keep; one near zero may not be worth renewing. Priority decides who gets asked first, so lower-priority indexers naturally find less.</p>` : "";
  box.innerHTML = `<section class="ureport"><div class="uhead"><h2>How they're doing</h2><span class="small dim">from SABnzbd, Prowlarr, Radarr and Sonarr</span></div>
    ${speedCard}${tips}${servers}${indexers}
    ${r.missing && r.missing.length ? `<p class="small dim">Connect ${esc(r.missing.join(" and "))} to see more here.</p>` : ""}</section>`;
}
