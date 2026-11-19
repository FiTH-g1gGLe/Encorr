// People and your profile.
"use strict";

// ---------- users ----------
const ROLE_NAMES = { admin: "Owner", trusted: "Trusted", friend: "Friend" };
const ROLE_HELP = { admin: "Can do everything, including settings", trusted: "Requests start downloading straight away", friend: "Requests wait for your approval" };

async function pageUsers(main) {
  main.innerHTML = `
    <div class="page-head"><div><h1>People</h1><div class="muted">Everyone who can request. Make accounts for friends here.</div></div>
      <div class="row">${state.status && state.status.plex !== false ? `<button class="btn" id="import">${I.plex} Import Plex friends</button>` : ""}<button class="btn primary" id="add">${I.plus} Add a person</button></div></div>
    <div class="people" id="ulist"><span class="spinner"></span></div>
    <div class="glass roles">${Object.keys(ROLE_NAMES).map((r) => `<div><strong>${ROLE_NAMES[r]}</strong><span>${ROLE_HELP[r]}</span></div>`).join("")}</div>`;
  const load = async () => {
    const r = await api("/users");
    const box = qs("#ulist");
    if (!box) return;
    box.innerHTML = r.results.map((u) => {
      const how = [u.hasPassword ? "a username and password" : "", u.plex ? "Plex" : "", u.jellyfin ? "Jellyfin" : ""].filter(Boolean).join(" or ");
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
  const imp = qs("#import");
  if (imp) imp.addEventListener("click", async (e) => {
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
      <p class="small muted" style="margin-top:-6px">${u.hasPassword ? "Change the password you sign in with." : `Set a password so you can also sign in with your username, without ${u.jellyfin && !u.plex ? "Jellyfin" : "Plex"}.`}</p>
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
