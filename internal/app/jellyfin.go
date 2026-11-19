package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/jellyfin"
	"github.com/FiTH-g1gGLe/encorr/internal/netfind"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// ---------- connecting a Jellyfin server ----------

// cleanServerURL tidies an address typed by the owner.
func cleanServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", userErr("That doesn't look like a web address. Try something like http://localhost:8096")
	}
	u.RawQuery, u.Fragment = "", ""
	u.Path = strings.TrimRight(u.Path, "/")
	// People often paste the address of the web app itself.
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/web/index.html"), "/web")
	return u.String(), nil
}

type jfConnect struct {
	URL       string `json:"url"`
	PublicURL string `json:"publicUrl"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// connectJellyfin signs in as a Jellyfin administrator, makes Encorr its own
// API key and reads the libraries. The password is only used for this and
// never kept.
func (a *App) connectJellyfin(ctx context.Context, b jfConnect) (*store.JellyfinSettings, *jellyfin.User, error) {
	addr, err := cleanServerURL(b.URL)
	if err != nil {
		return nil, nil, err
	}
	if addr == "" {
		return nil, nil, userErr("Enter your Jellyfin address.")
	}
	pub, err := cleanServerURL(b.PublicURL)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(b.Username) == "" {
		return nil, nil, userErr("Enter the username of a Jellyfin administrator.")
	}
	c := a.jellyfinAt(addr, "")
	info, err := c.PublicInfo(ctx)
	if err != nil {
		return nil, nil, userErr(capitalise(err.Error()) + ".")
	}
	auth, err := c.SignIn(ctx, strings.TrimSpace(b.Username), b.Password)
	if err != nil {
		return nil, nil, userErr(capitalise(err.Error()) + ".")
	}
	if !auth.User.Policy.IsAdministrator {
		return nil, nil, userErr("That account isn't a Jellyfin administrator. Sign in with the account that manages the server.")
	}
	key, err := a.jellyfinAt(addr, auth.AccessToken).CreateAPIKey(ctx, "Encorr")
	if err != nil {
		return nil, nil, userErr("Couldn't make an API key for Encorr: " + err.Error())
	}
	k := a.jellyfinAt(addr, key)
	libs, err := k.Libraries(ctx)
	if err != nil {
		return nil, nil, userErr("Connected, but couldn't read your libraries: " + err.Error())
	}
	js := &store.JellyfinSettings{ServerName: info.ServerName, ServerID: info.ID, URL: addr, PublicURL: pub,
		Version: info.Version, APIKey: store.Secret(key)}
	old := a.settings().Jellyfin
	for _, l := range libs {
		kind := l.Kind()
		if kind == "" {
			continue
		}
		on := true // new libraries start on, like with Plex
		if old.ServerID == info.ID {
			for _, o := range old.Libraries {
				if o.Key == l.ItemID {
					on = o.Enabled
				}
			}
		}
		js.Libraries = append(js.Libraries, store.PlexLibrary{Key: l.ItemID, Title: l.Name, Type: kind, Enabled: on})
	}
	return js, &auth.User, nil
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func jfSettingsOut(j store.JellyfinSettings) map[string]any {
	return map[string]any{"connected": j.Ready(), "serverName": j.ServerName, "url": j.URL, "publicUrl": j.PublicURL,
		"version": j.Version, "libraries": j.Libraries, "hasKey": j.APIKey != "", "keyHint": j.APIKey.Masked()}
}

// hJellyfinConnect (owner) connects or reconnects Jellyfin and links the
// administrator account used to the owner, so they can sign in with it.
func (a *App) hJellyfinConnect(w http.ResponseWriter, r *http.Request, u *store.User) {
	var b jfConnect
	if err := readJSON(r, &b); err != nil {
		fail(w, err)
		return
	}
	js, jfu, err := a.connectJellyfin(r.Context(), b)
	if err != nil {
		fail(w, err)
		return
	}
	err = a.Store.Update(func(d *store.Data) error {
		if other := d.UserByJellyfinID(jfu.ID); other != nil && other.ID != u.ID {
			other.JellyfinID = "" // that Jellyfin account now belongs to the owner
		}
		if me := d.UserByID(u.ID); me != nil {
			me.JellyfinID = jfu.ID
		}
		d.Settings.Jellyfin = *js
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.activity("action", "Connected Jellyfin", js.ServerName+" at "+js.URL)
	if a.settings().SetupComplete {
		_ = a.RunJob("jellyfin-full")
	}
	writeJSON(w, 200, jfSettingsOut(*js))
}

// hJellyfinSetup is the first-run wizard's "use Jellyfin" path: the
// administrator who connects it becomes Encorr's owner.
func (a *App) hJellyfinSetup(w http.ResponseWriter, r *http.Request) {
	hasAdmin := false
	a.Store.Read(func(d *store.Data) { hasAdmin = d.HasAdmin() })
	if hasAdmin {
		writeErr(w, http.StatusForbidden, "Encorr already has an owner. Sign in, then connect Jellyfin in Settings.")
		return
	}
	var b jfConnect
	if err := readJSON(r, &b); err != nil {
		fail(w, err)
		return
	}
	js, jfu, err := a.connectJellyfin(r.Context(), b)
	if err != nil {
		fail(w, err)
		return
	}
	var userID int
	err = a.Store.Update(func(d *store.Data) error {
		if d.HasAdmin() {
			return userErr("Someone else finished setting up first.")
		}
		u := d.UserByJellyfinID(jfu.ID)
		if u == nil {
			u = &store.User{ID: d.NewID("user"), JellyfinID: jfu.ID, Created: time.Now()}
			d.Users = append(d.Users, u)
		}
		u.Username, u.Role = uniqueName(d, jfu.Name, u.ID), store.RoleAdmin
		d.Settings.Jellyfin = *js
		userID = u.ID
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if err := a.startSession(w, r, userID); err != nil {
		fail(w, err)
		return
	}
	a.logf("setup: %s connected Jellyfin %s and owns this Encorr", jfu.Name, js.ServerName)
	writeJSON(w, 200, map[string]any{"done": true, "jellyfin": jfSettingsOut(*js)})
}

// uniqueName keeps usernames unique, so password sign-in stays unambiguous.
func uniqueName(d *store.Data, name string, selfID int) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "jellyfin-user"
	}
	try := name
	for i := 2; ; i++ {
		if u := d.UserByName(try); u == nil || u.ID == selfID {
			return try
		}
		try = fmt.Sprintf("%s (%d)", name, i)
	}
}

func (a *App) hJellyfinLibraries(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Enabled []string `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	on := map[string]bool{}
	for _, k := range body.Enabled {
		on[k] = true
	}
	_ = a.Store.Update(func(d *store.Data) error {
		for i := range d.Settings.Jellyfin.Libraries {
			d.Settings.Jellyfin.Libraries[i].Enabled = on[d.Settings.Jellyfin.Libraries[i].Key]
		}
		return nil
	})
	if a.settings().SetupComplete {
		_ = a.RunJob("jellyfin-full")
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hJellyfinSavePublic(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		PublicURL string `json:"publicUrl"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	pub, err := cleanServerURL(body.PublicURL)
	if err != nil {
		fail(w, err)
		return
	}
	_ = a.Store.Update(func(d *store.Data) error { d.Settings.Jellyfin.PublicURL = pub; return nil })
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// hJellyfinDisconnect forgets the server (Encorr's API key stays listed in
// Jellyfin's dashboard until the owner deletes it there).
func (a *App) hJellyfinDisconnect(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.settings()
	if s.Plex.URL == "" && s.SetupComplete {
		fail(w, userErr("Jellyfin is your only media server. Connect Plex first if you want to switch."))
		return
	}
	name := s.Jellyfin.ServerName
	_ = a.Store.Update(func(d *store.Data) error { d.Settings.Jellyfin = store.JellyfinSettings{}; return nil })
	a.dropSource(srcJellyfin)
	a.activity("action", "Disconnected Jellyfin", name)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- signing in with Jellyfin ----------

// jfSignedIn finishes a Jellyfin sign-in for an account Jellyfin vouched for.
func (a *App) jfSignedIn(w http.ResponseWriter, r *http.Request, jfu jellyfin.User, how string) {
	s := a.settings()
	if jfu.Policy.IsDisabled {
		writeErr(w, http.StatusForbidden, "That Jellyfin account is turned off.")
		return
	}
	if jfu.ServerID != "" && s.Jellyfin.ServerID != "" && !strings.EqualFold(jfu.ServerID, s.Jellyfin.ServerID) {
		writeErr(w, http.StatusForbidden, "That account is on a different Jellyfin server.")
		return
	}
	var userID int
	var role string
	err := a.Store.Update(func(d *store.Data) error {
		u := d.UserByJellyfinID(jfu.ID)
		if u == nil {
			u = &store.User{ID: d.NewID("user"), JellyfinID: jfu.ID, Role: store.RoleFriend, Created: time.Now()}
			u.Username = uniqueName(d, jfu.Name, u.ID)
			d.Users = append(d.Users, u)
		}
		userID, role = u.ID, u.Role
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if err := a.startSession(w, r, userID); err != nil {
		fail(w, err)
		return
	}
	a.logf("sign-in: %s (Jellyfin %s, %s)", jfu.Name, how, role)
	writeJSON(w, 200, map[string]any{"done": true, "role": role})
}

func (a *App) jfReadyForSignIn(w http.ResponseWriter) bool {
	s := a.settings()
	switch {
	case !s.SetupComplete:
		writeErr(w, http.StatusForbidden, "Encorr isn't finished setting up yet.")
	case !s.Jellyfin.Ready():
		writeErr(w, http.StatusForbidden, "This Encorr isn't connected to Jellyfin.")
	default:
		return true
	}
	return false
}

// hJellyfinLogin signs in with a Jellyfin username and password. Encorr
// passes them to Jellyfin and keeps neither.
func (a *App) hJellyfinLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	if !a.jfReadyForSignIn(w) {
		return
	}
	if a.loginLimiter.Blocked(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "Too many wrong passwords. Wait 15 minutes and try again.")
		return
	}
	s := a.settings()
	res, err := a.jellyfinAt(s.Jellyfin.URL, "").SignIn(r.Context(), strings.TrimSpace(body.Username), body.Password)
	if err != nil {
		if strings.Contains(err.Error(), "wrong") {
			a.loginLimiter.Allow(clientIP(r))
			a.logf("Jellyfin sign-in failed for %q from %s", body.Username, clientIP(r))
			writeErr(w, http.StatusUnauthorized, "That Jellyfin username and password don't match.")
			return
		}
		fail(w, userErr("Couldn't reach Jellyfin: "+err.Error()))
		return
	}
	a.jfSignedIn(w, r, res.User, "password")
}

// Quick Connect secrets Encorr started, so the check endpoint can't be used
// to poke at Jellyfin with made-up secrets.
var jfQuick = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

func (a *App) hJellyfinQuickStart(w http.ResponseWriter, r *http.Request) {
	if !a.jfReadyForSignIn(w) {
		return
	}
	s := a.settings()
	qc, err := a.jellyfinAt(s.Jellyfin.URL, "").QuickConnectStart(r.Context())
	if err != nil {
		fail(w, userErr(capitalise(err.Error())+"."))
		return
	}
	jfQuick.Lock()
	for k, t := range jfQuick.m {
		if time.Since(t) > 10*time.Minute {
			delete(jfQuick.m, k)
		}
	}
	jfQuick.m[qc.Secret] = time.Now()
	jfQuick.Unlock()
	writeJSON(w, 200, map[string]any{"code": qc.Code, "secret": qc.Secret})
}

func (a *App) hJellyfinQuickCheck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Secret string `json:"secret"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	jfQuick.Lock()
	started, ok := jfQuick.m[body.Secret]
	jfQuick.Unlock()
	if !ok || time.Since(started) > 10*time.Minute {
		writeErr(w, http.StatusGone, "That code has expired. Start again.")
		return
	}
	if !a.jfReadyForSignIn(w) {
		return
	}
	s := a.settings()
	c := a.jellyfinAt(s.Jellyfin.URL, "")
	st, err := c.QuickConnectState(r.Context(), body.Secret)
	if err != nil {
		fail(w, userErr("Couldn't reach Jellyfin: "+err.Error()))
		return
	}
	if !st.Authenticated {
		writeJSON(w, 200, map[string]any{"done": false})
		return
	}
	jfQuick.Lock()
	delete(jfQuick.m, body.Secret)
	jfQuick.Unlock()
	res, err := c.QuickConnectFinish(r.Context(), body.Secret)
	if err != nil {
		fail(w, userErr("Jellyfin didn't finish the sign-in: "+err.Error()))
		return
	}
	// Double-check the account with Encorr's own key: it must still be on
	// this server and not turned off.
	if jf := a.Jellyfin(); jf != nil {
		if fresh, err := jf.UserByID(r.Context(), res.User.ID); err == nil {
			res.User.Policy = fresh.Policy
		} else if !errors.Is(err, context.Canceled) {
			fail(w, userErr("Couldn't check your Jellyfin account: "+err.Error()))
			return
		}
	}
	a.jfSignedIn(w, r, res.User, "Quick Connect")
}

// jfQuickEnabled says whether the sign-in page should offer Quick Connect.
// Cached for a minute: the status endpoint is hit on every page load.
var jfQuickCache struct {
	sync.Mutex
	at time.Time
	on bool
}

func (a *App) jfQuickEnabled(ctx context.Context) bool {
	jfQuickCache.Lock()
	defer jfQuickCache.Unlock()
	if time.Since(jfQuickCache.at) < time.Minute {
		return jfQuickCache.on
	}
	s := a.settings()
	on := false
	if s.Jellyfin.Ready() {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		on, _ = a.jellyfinAt(s.Jellyfin.URL, "").QuickConnectEnabled(cctx)
		cancel()
	}
	jfQuickCache.at, jfQuickCache.on = time.Now(), on
	return on
}

// ---------- title page and links ----------

// jfURL opens an item in Jellyfin's web app, at the address friends use.
func (a *App) jfURL(itemID string) string { return jfURLFor(a.settings(), itemID) }

func jfURLFor(st store.Settings, itemID string) string {
	s := st.Jellyfin
	base := s.PublicURL
	if base == "" {
		base = s.URL
	}
	return jellyfin.WebURL(base, itemID, s.ServerID)
}

// jellyfinDetails adds quality and a Play link for the title page.
func (a *App) jellyfinDetails(ctx context.Context, itemID string, isShow bool) *plexInfo {
	jf := a.Jellyfin()
	if jf == nil || itemID == "" {
		return nil
	}
	info := &plexInfo{URL: a.jfURL(itemID)}
	if isShow {
		return info
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	it, err := jf.Item(ctx, itemID)
	if err != nil {
		return info
	}
	for _, st := range it.MediaStreams {
		switch st.Type {
		case "Video":
			if info.Quality != "" {
				continue
			}
			switch h, wd := st.Height, st.Width; {
			case wd >= 3200 || h >= 2000:
				info.Quality = "4K"
			case wd >= 1800 || h >= 1000:
				info.Quality = "1080p"
			case wd >= 1200 || h >= 700:
				info.Quality = "720p"
			case wd > 0 || h > 0:
				info.Quality = "SD"
			}
			switch r := strings.ToUpper(st.VideoRangeTyp); {
			case strings.HasPrefix(r, "DOVI"):
				info.Quality += " DV"
			case strings.HasPrefix(r, "HDR10PLUS"):
				info.Quality += " HDR10+"
			case strings.HasPrefix(r, "HDR") || r == "HLG" || strings.EqualFold(st.VideoRange, "HDR"):
				info.Quality += " HDR"
			}
		case "Audio":
			if info.Audio != "" && !st.IsDefault {
				continue
			}
			switch ch := st.Channels; {
			case ch == 1:
				info.Audio = "Mono"
			case ch == 2:
				info.Audio = "Stereo"
			case ch > 2:
				info.Audio = fmt.Sprintf("%d.1", ch-1)
			}
		}
	}
	info.Quality = strings.TrimSpace(info.Quality)
	return info
}

// hFindJellyfin looks for Jellyfin for the connect form: its container name
// inside Docker, this machine, or the address the browser used. Allowed
// during first-run setup (before anyone owns Encorr) and for the owner. It
// only ever knocks on Jellyfin's public info page at those few addresses.
func (a *App) hFindJellyfin(w http.ResponseWriter, r *http.Request) {
	hasAdmin := false
	a.Store.Read(func(d *store.Data) { hasAdmin = d.HasAdmin() })
	if hasAdmin {
		if u := a.currentUser(r); u == nil || u.Role != store.RoleAdmin {
			writeErr(w, http.StatusForbidden, "Only admins can do that.")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	out := map[string]any{"found": false}
	if u := netfind.Find(ctx, netfind.Jellyfin, 0, netfind.HostOnly(r.Host)); u != "" {
		out["found"], out["url"] = true, u
	}
	writeJSON(w, 200, out)
}
