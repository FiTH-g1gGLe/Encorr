package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/netfind"
	"github.com/FiTH-g1gGLe/encorr/internal/notify"
	"github.com/FiTH-g1gGLe/encorr/internal/plex"
	"github.com/FiTH-g1gGLe/encorr/internal/sab"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
	"github.com/FiTH-g1gGLe/encorr/internal/tmdb"
)

// ---------- status & auth ----------

type publicUser struct {
	ID            int    `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email,omitempty"`
	Thumb         string `json:"thumb,omitempty"`
	Role          string `json:"role"`
	DiscordID     string `json:"discordId,omitempty"`
	WatchlistSync bool   `json:"watchlistSync"`
	SignedIn      bool   `json:"signedIn"`    // has signed in at least once
	Plex          bool   `json:"plex"`        // can sign in with Plex
	Jellyfin      bool   `json:"jellyfin"`    // can sign in with Jellyfin
	HasPassword   bool   `json:"hasPassword"` // can sign in with username + password
	MustChange    bool   `json:"mustChangePassword,omitempty"`
	Created       string `json:"created"`
	Requests      int    `json:"requests,omitempty"`
	MovieLimit    *int   `json:"movieLimit,omitempty"` // set for this person; nil = default
	TVLimit       *int   `json:"tvLimit,omitempty"`
}

func toPublic(u *store.User) publicUser {
	return publicUser{ID: u.ID, Username: u.Username, Email: u.Email, Thumb: u.Thumb, Role: u.Role,
		DiscordID: u.DiscordID, WatchlistSync: u.WatchlistSync, SignedIn: u.PlexToken != "" || !u.LastSeen.IsZero(),
		Plex: u.PlexID != 0, Jellyfin: u.JellyfinID != "", HasPassword: u.PasswordHash != "", MustChange: u.MustChangePassword,
		Created: u.Created.Format(time.RFC3339), MovieLimit: u.MovieLimit, TVLimit: u.TVLimit}
}

func (a *App) hStatus(w http.ResponseWriter, r *http.Request) {
	s := a.settings()
	hasAdmin := false
	a.Store.Read(func(d *store.Data) { hasAdmin = d.HasAdmin() })
	out := map[string]any{
		"version": Version, "setupComplete": s.SetupComplete, "hasAdmin": hasAdmin,
		"accent": s.Accent, "theme": s.Theme, "serverName": s.Plex.ServerName, "appUrl": s.AppURL,
		"radarr": s.Radarr.Enabled, "sonarr": s.Sonarr.Enabled,
		// Which sign-in buttons to show, and what the library is called.
		"plex": s.Plex.URL != "" || !s.Jellyfin.Ready(), "jellyfin": s.Jellyfin.Ready(),
		"jellyfinName": s.Jellyfin.ServerName, "libraryName": a.libName(),
		"docker": netfind.InDocker(),
	}
	if s.Jellyfin.Ready() && s.SetupComplete {
		out["quickConnect"] = a.jfQuickEnabled(r.Context())
	}
	if out["serverName"] == "" {
		out["serverName"] = s.Jellyfin.ServerName
	}
	if u := a.currentUser(r); u != nil {
		out["user"] = toPublic(u)
		out["library"] = a.librarySync()
	}
	writeJSON(w, 200, out)
}

// librarySync says how well Encorr knows the library: counts, whether a
// full read has worked on every connected server, and whether one is
// running now.
func (a *App) librarySync() map[string]any {
	s := a.settings()
	var names []string
	if s.Plex.URL != "" || !s.Jellyfin.Ready() {
		names = append(names, "plex-full")
	}
	if s.Jellyfin.Ready() {
		names = append(names, "jellyfin-full")
	}
	films, shows := 0, 0
	synced := true
	var lastRun time.Time
	a.Store.Read(func(d *store.Data) {
		for k := range d.Library {
			if strings.HasPrefix(k, store.TypeTV+":") {
				shows++
			} else {
				films++
			}
		}
		for _, n := range names {
			j := d.Jobs[n]
			synced = synced && j.LastOK
			if j.LastRun.After(lastRun) {
				lastRun = j.LastRun
			}
		}
	})
	running := false
	a.mu.Lock()
	for _, n := range names {
		if j, ok := a.jobs[n]; ok && j.running {
			running = true
		}
	}
	a.mu.Unlock()
	return map[string]any{"films": films, "shows": shows, "synced": synced, "running": running, "lastRun": lastRun, "name": a.libName()}
}

func (a *App) hPlexPin(w http.ResponseWriter, r *http.Request) {
	c := a.plexClient("")
	pin, err := c.CreatePin(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": pin.ID, "code": pin.Code, "url": c.AuthURL(pin.Code, "")})
}

// hPlexCheck finishes the PIN sign-in once the user has approved it on plex.tv.
func (a *App) hPlexCheck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID int `json:"id"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	ctx := r.Context()
	pin, err := a.plexClient("").CheckPin(ctx, body.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if pin.AuthToken == "" {
		writeJSON(w, 200, map[string]any{"done": false})
		return
	}
	acct, err := a.plexClient(pin.AuthToken).Account(ctx)
	if err != nil {
		fail(w, err)
		return
	}

	s := a.settings()
	var hasAdmin bool
	var existing *store.User
	a.Store.Read(func(d *store.Data) {
		hasAdmin = d.HasAdmin()
		if u := d.UserByPlexID(acct.ID); u != nil {
			cp := *u
			existing = &cp
		}
	})

	role := store.RoleFriend
	switch {
	case !hasAdmin:
		role = store.RoleAdmin // first person to sign in owns this Encorr
	case existing != nil && existing.Role == store.RoleAdmin:
		role = store.RoleAdmin
	default:
		if s.Plex.MachineID == "" {
			writeErr(w, http.StatusForbidden, "Encorr isn't finished setting up yet. Ask the server owner to complete setup.")
			return
		}
		ok, err := a.plexClient(pin.AuthToken).HasServer(ctx, s.Plex.MachineID)
		if err != nil {
			fail(w, err)
			return
		}
		if !ok {
			writeErr(w, http.StatusForbidden, "Your Plex account doesn't have access to "+s.Plex.ServerName+". Ask the owner to share it with you first.")
			return
		}
		if existing != nil {
			role = existing.Role
		}
	}

	var userID int
	err = a.Store.Update(func(d *store.Data) error {
		u := d.UserByPlexID(acct.ID)
		if u == nil {
			u = &store.User{ID: d.NewID("user"), PlexID: acct.ID, Created: time.Now()}
			d.Users = append(d.Users, u)
		}
		u.Username, u.Email, u.Thumb, u.Role = acct.Username, acct.Email, acct.Thumb, role
		u.PlexToken = store.Secret(pin.AuthToken)
		if role == store.RoleAdmin && !hasAdmin {
			d.Settings.Plex.Token = store.Secret(pin.AuthToken)
		}
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
	a.logf("sign-in: %s (%s)", acct.Username, role)
	writeJSON(w, 200, map[string]any{"done": true, "role": role})
}

// hLogin signs in with an Encorr username and password.
func (a *App) hLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	var hash string
	var id int
	var ready bool
	a.Store.Read(func(d *store.Data) {
		ready = d.Settings.SetupComplete
		if u := d.UserByName(body.Username); u != nil && u.PasswordHash != "" {
			hash, id = u.PasswordHash, u.ID
		}
	})
	if !ready {
		writeErr(w, http.StatusForbidden, "Encorr isn't finished setting up yet.")
		return
	}
	if a.loginLimiter.Blocked(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "Too many wrong passwords. Wait 15 minutes and try again.")
		return
	}
	if hash == "" {
		a.loginLimiter.Allow(clientIP(r))
		checkPassword(dummyHash, body.Password) // same delay either way
		writeErr(w, http.StatusUnauthorized, "That username and password don't match. Check them, or ask the owner to reset your password.")
		return
	}
	if !checkPassword(hash, body.Password) {
		a.loginLimiter.Allow(clientIP(r))
		a.logf("sign-in failed for %q from %s", body.Username, clientIP(r))
		writeErr(w, http.StatusUnauthorized, "That username and password don't match. Check them, or ask the owner to reset your password.")
		return
	}
	if err := a.startSession(w, r, id); err != nil {
		fail(w, err)
		return
	}
	var pub publicUser
	a.Store.Read(func(d *store.Data) {
		if u := d.UserByID(id); u != nil {
			pub = toPublic(u)
		}
	})
	a.logf("sign-in: %s (password)", pub.Username)
	writeJSON(w, 200, map[string]any{"done": true, "user": pub})
}

// hChangeMyPassword lets anyone set or change their own password.
func (a *App) hChangeMyPassword(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	if err := validPassword(body.New); err != nil {
		fail(w, err)
		return
	}
	// Changing an existing password needs the old one, except straight after
	// the owner set or reset it (MustChangePassword).
	if u.PasswordHash != "" && !u.MustChangePassword && !checkPassword(u.PasswordHash, body.Current) {
		fail(w, userErr("Your current password isn't right."))
		return
	}
	hash, err := hashPassword(body.New)
	if err != nil {
		fail(w, err)
		return
	}
	var pub publicUser
	err = a.Store.Update(func(d *store.Data) error {
		t := d.UserByID(u.ID)
		if t == nil {
			return userErr("User not found.")
		}
		t.PasswordHash, t.MustChangePassword = hash, false
		pub = toPublic(t)
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, pub)
}

// hCreateUser makes an Encorr account (username + password) for someone.
func (a *App) hCreateUser(w http.ResponseWriter, r *http.Request, admin *store.User) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	name := strings.TrimSpace(body.Username)
	if err := validUsername(name); err != nil {
		fail(w, err)
		return
	}
	if body.Password == "" {
		body.Password = newPassword()
	}
	if err := validPassword(body.Password); err != nil {
		fail(w, err)
		return
	}
	if body.Role == "" {
		body.Role = store.RoleFriend
	}
	if body.Role != store.RoleAdmin && body.Role != store.RoleTrusted && body.Role != store.RoleFriend {
		fail(w, userErr("Role must be admin, trusted or friend."))
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		fail(w, err)
		return
	}
	var pub publicUser
	err = a.Store.Update(func(d *store.Data) error {
		if d.UserByName(name) != nil {
			return userErr("Someone already has the username " + name + ". Pick another.")
		}
		u := &store.User{ID: d.NewID("user"), Username: name, Role: body.Role, PasswordHash: hash,
			MustChangePassword: true, Created: time.Now()}
		d.Users = append(d.Users, u)
		pub = toPublic(u)
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.logf("%s added an account for %s (%s)", admin.Username, name, body.Role)
	writeJSON(w, 200, map[string]any{"user": pub, "password": body.Password})
}

// hResetPassword lets an admin set a new password for anyone.
func (a *App) hResetPassword(w http.ResponseWriter, r *http.Request, admin *store.User) {
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.Password == "" {
		body.Password = newPassword()
	}
	if err := validPassword(body.Password); err != nil {
		fail(w, err)
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		fail(w, err)
		return
	}
	id := pathInt(r, "id")
	var name string
	err = a.Store.Update(func(d *store.Data) error {
		t := d.UserByID(id)
		if t == nil {
			return userErr("User not found.")
		}
		t.PasswordHash = hash
		t.MustChangePassword = t.ID != admin.ID
		name = t.Username
		// Sign them out everywhere: the old password no longer works.
		if t.ID != admin.ID {
			keep := d.Sessions[:0]
			for _, s := range d.Sessions {
				if s.UserID != t.ID {
					keep = append(keep, s)
				}
			}
			d.Sessions = keep
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.logf("%s set a new password for %s", admin.Username, name)
	writeJSON(w, 200, map[string]any{"ok": true, "username": name, "password": body.Password})
}

// hDeleteUser removes an account (their past requests stay in the list).
func (a *App) hDeleteUser(w http.ResponseWriter, r *http.Request, admin *store.User) {
	id := pathInt(r, "id")
	if id == admin.ID {
		fail(w, userErr("You can't remove your own account."))
		return
	}
	err := a.Store.Update(func(d *store.Data) error {
		t := d.UserByID(id)
		if t == nil {
			return userErr("User not found.")
		}
		if t.Role == store.RoleAdmin {
			n := 0
			for _, x := range d.Users {
				if x.Role == store.RoleAdmin {
					n++
				}
			}
			if n <= 1 {
				return userErr("Encorr needs at least one admin.")
			}
		}
		users := d.Users[:0]
		for _, x := range d.Users {
			if x.ID != id {
				users = append(users, x)
			}
		}
		d.Users = users
		keep := d.Sessions[:0]
		for _, s := range d.Sessions {
			if s.UserID != id {
				keep = append(keep, s)
			}
		}
		d.Sessions = keep
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = a.Store.Update(func(d *store.Data) error {
			keep := d.Sessions[:0]
			for _, s := range d.Sessions {
				if s.Token != c.Value {
					keep = append(keep, s)
				}
			}
			d.Sessions = keep
			return nil
		})
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hMe(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeJSON(w, 200, toPublic(u))
}

func (a *App) hUpdateMe(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		DiscordID     *string `json:"discordId"`
		WatchlistSync *bool   `json:"watchlistSync"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	var out store.User
	err := a.Store.Update(func(d *store.Data) error {
		uu := d.UserByID(u.ID)
		if body.DiscordID != nil {
			id := strings.TrimSpace(*body.DiscordID)
			if id != "" {
				if _, err := strconv.ParseUint(id, 10, 64); err != nil {
					return userErr("A Discord user ID is a long number. In Discord, turn on Developer Mode, right-click your name and choose Copy User ID.")
				}
			}
			uu.DiscordID = id
		}
		if body.WatchlistSync != nil {
			uu.WatchlistSync = *body.WatchlistSync
		}
		out = *uu
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, toPublic(&out))
}

// ---------- browsing ----------

type statusInfo struct {
	Status       string         `json:"status"` // available | partial | processing | pending | none
	Seasons      map[int]string `json:"seasons,omitempty"`
	Progress     *Progress      `json:"progress,omitempty"`
	MyRequest    *store.Request `json:"myRequest,omitempty"`
	RequestCount int            `json:"requestCount,omitempty"`
}

// statusOf works out what Encorr knows about a title (call with read lock).
func (a *App) statusOf(d *store.Data, typ string, tmdbID int, userID int) statusInfo {
	si := statusInfo{Status: "none"}
	lib, inLib := d.Library[store.LibKey(typ, tmdbID)]
	m := d.MediaByTMDB(typ, tmdbID)
	switch {
	case inLib && typ == store.TypeTV && m != nil && m.Status == store.MediaPartial:
		si.Status = store.MediaPartial
	case inLib:
		si.Status = store.MediaAvailable // in Plex (seasons refined on the title page)
	case m != nil && m.Status != store.MediaUnknown:
		si.Status = m.Status
	}
	if m != nil {
		for _, r := range d.RequestsForMedia(m.ID) {
			if r.Status == store.ReqDeclined {
				continue
			}
			si.RequestCount++
			if r.RequestedBy == userID {
				cp := *r
				si.MyRequest = &cp
			}
		}
		if typ == store.TypeTV {
			si.Seasons = map[int]string{}
			for s, want := range m.SeasonEps {
				have := m.SeasonHave[s]
				switch {
				case want > 0 && have >= want:
					si.Seasons[s] = store.MediaAvailable
				case have > 0:
					si.Seasons[s] = store.MediaPartial
				}
			}
			for _, r := range d.RequestsForMedia(m.ID) {
				for _, s := range r.Seasons {
					if _, ok := si.Seasons[s]; ok {
						continue
					}
					switch r.Status {
					case store.ReqPending:
						si.Seasons[s] = store.MediaPending
					case store.ReqApproved:
						si.Seasons[s] = store.MediaProcessing
					}
				}
			}
		}
		if m.ExternalID > 0 && si.Status != store.MediaAvailable {
			si.Progress = a.progressFor(typ, m.ExternalID)
		}
	} else if inLib && typ == store.TypeTV && len(lib.Have()) > 0 {
		si.Seasons = map[int]string{}
		for s, have := range lib.Have() {
			if have > 0 {
				si.Seasons[s] = store.MediaPartial // exact counts compared on the title page
			}
		}
	}
	return si
}

type cardOut struct {
	tmdb.Card
	Status string `json:"status"`
}

func (a *App) annotate(cards []tmdb.Card, userID int) []cardOut {
	out := make([]cardOut, len(cards))
	a.Store.Read(func(d *store.Data) {
		for i, c := range cards {
			out[i] = cardOut{Card: c, Status: a.statusOf(d, c.Type, c.ID, userID).Status}
		}
	})
	return out
}

func (a *App) hDiscover(w http.ResponseWriter, r *http.Request, u *store.User) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	key := r.PathValue("kind")
	if key == "plex-recent" {
		cards := []tmdb.Card{}
		if page <= 1 {
			cards = append(cards, a.plexRecent(r.Context())...)
		}
		writeJSON(w, 200, map[string]any{"results": a.annotate(cards, u.ID), "totalPages": 1, "title": "Just added to " + a.libName()})
		return
	}
	cards, pages, err := a.TMDB().List(r.Context(), key, page)
	if err != nil {
		fail(w, err)
		return
	}
	out := map[string]any{"results": a.annotate(cards, u.ID), "totalPages": pages}
	if page <= 1 {
		out["title"] = a.TMDB().RowTitle(r.Context(), key)
	}
	writeJSON(w, 200, out)
}

// hDiscoverRows lists today's home-screen rows (some rotate daily).
func (a *App) hDiscoverRows(w http.ResponseWriter, r *http.Request, u *store.User) {
	rows := a.TMDB().Rows(r.Context(), time.Now())
	for i := range rows {
		if rows[i].Key == "plex-recent" {
			rows[i].Title = "Just added to " + a.libName() // Plex, Jellyfin or your library
		}
	}
	writeJSON(w, 200, map[string]any{"rows": rows})
}

// hSearch finds films and TV separately, so the page can show them apart.
func (a *App) hSearch(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, 200, map[string]any{"movies": []any{}, "tv": []any{}, "results": []any{}})
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	var movies, tv []tmdb.Card
	var mErr, tErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); movies, _, mErr = a.TMDB().SearchType(r.Context(), "movie", q, page) }()
	go func() { defer wg.Done(); tv, _, tErr = a.TMDB().SearchType(r.Context(), "tv", q, page) }()
	wg.Wait()
	if mErr != nil && tErr != nil {
		fail(w, mErr)
		return
	}
	ma, ta := a.annotate(movies, u.ID), a.annotate(tv, u.ID)
	writeJSON(w, 200, map[string]any{"movies": ma, "tv": ta, "results": append(append([]any{}, toAny(ma)...), toAny(ta)...)})
}

func toAny[T any](xs []T) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

func (a *App) hTitle(w http.ResponseWriter, r *http.Request, u *store.User) {
	typ, id := r.PathValue("type"), pathInt(r, "id")
	var det *tmdb.Details
	var err error
	switch typ {
	case store.TypeMovie:
		det, err = a.TMDB().Movie(r.Context(), id)
	case store.TypeTV:
		det, err = a.TMDB().TV(r.Context(), id)
	default:
		writeErr(w, 404, "Unknown type.")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	if typ == store.TypeTV {
		a.ensureSeasons(r.Context(), id)
	}
	var si statusInfo
	var recs []cardOut
	a.Store.Read(func(d *store.Data) {
		si = a.statusOf(d, typ, id, u.ID)
		// Use exact TMDB episode counts for seasons when the servers' detail is known.
		if lib, ok := d.Library[store.LibKey(typ, id)]; ok && typ == store.TypeTV && len(lib.Have()) > 0 {
			have := lib.Have()
			if si.Seasons == nil {
				si.Seasons = map[int]string{}
			}
			all := true
			for _, s := range det.Seasons {
				have := have[s.SeasonNumber]
				switch {
				case have > 0 && have >= s.EpisodeCount:
					si.Seasons[s.SeasonNumber] = store.MediaAvailable
				case have > 0:
					if si.Seasons[s.SeasonNumber] == "" || si.Seasons[s.SeasonNumber] == store.MediaPartial {
						si.Seasons[s.SeasonNumber] = store.MediaPartial
					}
					all = false
				default:
					all = false
				}
			}
			if all {
				si.Status = store.MediaAvailable
			} else if si.Status == store.MediaAvailable {
				si.Status = store.MediaPartial
			}
		}
		for _, c := range det.Recommended {
			recs = append(recs, cardOut{Card: c, Status: a.statusOf(d, c.Type, c.ID, u.ID).Status})
		}
	})
	det.Recommended = nil
	out := map[string]any{"details": det, "status": si, "recommended": recs}
	var lib store.LibEntry
	a.Store.Read(func(d *store.Data) { lib = d.Library[store.LibKey(typ, id)] })
	pi, ji := a.plexDetails(r.Context(), lib.Key), a.jellyfinDetails(r.Context(), lib.JF, typ == store.TypeTV)
	if pi != nil {
		out["plex"] = pi
	}
	if ji != nil {
		out["jellyfin"] = ji
		if pi == nil {
			out["plex"] = &plexInfo{Quality: ji.Quality, Audio: ji.Audio} // quality chips
		} else if pi.Quality == "" {
			pi.Quality, pi.Audio = ji.Quality, ji.Audio
		}
	}
	writeJSON(w, 200, out)
}

// ---------- requests ----------

type requestOut struct {
	store.Request
	User     string    `json:"user"`
	UserImg  string    `json:"userThumb,omitempty"`
	Progress *Progress `json:"progress,omitempty"`
	Modifier string    `json:"modifiedByName,omitempty"`
	// For TV still on its way: episodes of the requested seasons in Plex.
	Have    int         `json:"have,omitempty"`
	Want    int         `json:"want,omitempty"`
	Seasons []seasonOut `json:"seasonProgress,omitempty"`
	// State sums up where an open request is: attention, downloading,
	// finishing, searching, waiting (not out yet) or pending.
	State      string     `json:"state,omitempty"`
	Release    *time.Time `json:"release,omitempty"`    // film: when it comes out to buy/stream
	NextAiring *time.Time `json:"nextAiring,omitempty"` // show: next new episode
	LastSearch *time.Time `json:"lastSearch,omitempty"` // when Encorr last asked for a fresh search
	// Group is the filter it lives under: needs | active | upcoming | ready | declined.
	Group       string     `json:"group"`
	NextSearch  *time.Time `json:"nextSearch,omitempty"` // when Encorr will look again by itself
	Problem     string     `json:"problem,omitempty"`    // why it needs a hand
	Arrived     *time.Time `json:"arrived,omitempty"`    // when it reached Plex
	PlexURL     string     `json:"plexUrl,omitempty"`
	JellyfinURL string     `json:"jellyfinUrl,omitempty"`
	New         bool       `json:"new,omitempty"`     // ready since the requester last looked
	Managed     bool       `json:"managed,omitempty"` // has a live download an admin can control
}

type seasonOut struct {
	Season      int `json:"season"`
	Have        int `json:"have"`
	Want        int `json:"want"`
	Downloading int `json:"downloading,omitempty"`
}

// statePriority orders In progress: things that need a hand first.
var statePriority = map[string]int{"attention": 0, "pending": 1, "downloading": 2, "finishing": 3, "searching": 4, "waiting": 5}

// readyFor is how long finished requests stay on the Ready list.
const readyFor = 30 * 24 * time.Hour

// groupOf files a request under one of the request filters.
func groupOf(ro *requestOut, admin bool) string {
	switch ro.Status {
	case store.ReqPending:
		if admin {
			return "needs"
		}
		return "active"
	case store.ReqFailed:
		return "needs"
	case store.ReqDeclined:
		return "declined"
	case store.ReqCompleted:
		return "ready"
	}
	switch ro.State {
	case "attention":
		return "needs"
	case "waiting":
		return "upcoming"
	}
	return "active"
}

// plexURL opens a library item in Plex's web app.
func (a *App) plexURL(ratingKey string) string { return plexURLFor(a.settings(), ratingKey) }

// plexURLFor is plexURL for code already holding the store lock (calling
// a.settings() there would take the lock twice).
func plexURLFor(s store.Settings, ratingKey string) string {
	mid := s.Plex.MachineID
	if mid == "" || ratingKey == "" {
		return ""
	}
	return fmt.Sprintf("https://app.plex.tv/desktop/#!/server/%s/details?key=%s", mid, url.QueryEscape("/library/metadata/"+ratingKey))
}

func (a *App) hRequests(w http.ResponseWriter, r *http.Request, u *store.User) {
	filter := r.URL.Query().Get("filter")
	older := r.URL.Query().Get("older") == "1"
	admin := u.Role == store.RoleAdmin
	st := a.settings() // read before taking the store lock below
	var all []requestOut
	var nudgeRan time.Time
	now := time.Now()
	a.Store.Read(func(d *store.Data) {
		nudgeRan = d.Jobs["nudge"].LastRun
		seen := u.ReadySeen
		if me := d.UserByID(u.ID); me != nil {
			seen = me.ReadySeen
		}
		for _, rq := range d.Requests {
			if !admin && rq.RequestedBy != u.ID && filter != "all" {
				continue
			}
			ro := requestOut{Request: *rq}
			ro.Events = nil // the timeline has its own endpoint
			if ru := d.UserByID(rq.RequestedBy); ru != nil {
				ro.User, ro.UserImg = ru.Username, ru.Thumb
			}
			if mu := d.UserByID(rq.ModifiedBy); mu != nil && rq.ModifiedBy != rq.RequestedBy {
				ro.Modifier = mu.Username
			}
			m := d.MediaByID(rq.MediaID)
			switch rq.Status {
			case store.ReqApproved:
				if m != nil {
					a.describeOpen(&ro, m)
				}
			case store.ReqFailed:
				ro.State = "attention"
				ro.Problem = rq.Error
			case store.ReqPending:
				ro.State = "pending"
			case store.ReqCompleted:
				t := rq.Updated
				if rq.DoneAt != nil {
					t = *rq.DoneAt
				} else if m != nil && m.AvailableAt != nil && m.AvailableAt.After(rq.Created) {
					t = *m.AvailableAt
				}
				ro.Arrived = &t
				ro.New = rq.RequestedBy == u.ID && t.After(seen) && !seen.IsZero()
				if m != nil {
					ro.PlexURL = plexURLFor(st, m.PlexKey)
					if m.JFKey != "" && st.Jellyfin.Ready() {
						ro.JellyfinURL = jfURLFor(st, m.JFKey)
					}
				}
			}
			if ro.State == "attention" && ro.Problem == "" && ro.Progress != nil {
				ro.Problem = ro.Progress.Note
			}
			if ro.State == "searching" {
				next := rq.Updated.Add(2 * time.Hour)
				if ro.LastSearch != nil {
					next = ro.LastSearch.Add(nudgeEvery)
				}
				if next.Before(now) {
					next = nudgeRan.Add(2 * time.Hour)
					if next.Before(now) {
						next = now.Add(5 * time.Minute)
					}
				}
				ro.NextSearch = &next
			}
			ro.Managed = ro.Progress != nil && ro.Progress.Items > 0
			ro.Group = groupOf(&ro, admin)
			all = append(all, ro)
		}
	})
	// Counts cover what this person can see on their own lists.
	counts := map[string]int{}
	states := map[string]int{}
	newReady := 0
	var out []requestOut
	for _, ro := range all {
		mine := admin || ro.RequestedBy == u.ID
		stale := ro.Group == "ready" && ro.Arrived != nil && now.Sub(*ro.Arrived) > readyFor
		if mine {
			if !stale {
				counts[ro.Group]++
			}
			counts["all"]++
			if ro.New {
				newReady++
			}
		}
		keep := false
		switch filter {
		case "needs", "active", "upcoming":
			keep = mine && ro.Group == filter
		case "ready":
			keep = mine && ro.Group == "ready" && (older || !stale)
		case "mine":
			keep = ro.RequestedBy == u.ID
		case "pending": // older clients
			keep = mine && ro.Status == store.ReqPending
		case "completed":
			keep = mine && ro.Status == store.ReqCompleted
		case "all", "":
			keep = true
		}
		if !keep {
			continue
		}
		if ro.State != "" {
			states[ro.State]++
		}
		out = append(out, ro)
	}
	sort.Slice(out, func(i, j int) bool {
		x, y := out[i], out[j]
		switch filter {
		case "active", "needs":
			if x.State != y.State {
				return statePriority[x.State] < statePriority[y.State]
			}
		case "upcoming":
			dx, dy := soonest(x), soonest(y)
			if !dx.Equal(dy) {
				return dx.Before(dy)
			}
		case "ready":
			if x.Arrived != nil && y.Arrived != nil {
				return x.Arrived.After(*y.Arrived)
			}
		}
		return x.Created.After(y.Created)
	})
	if out == nil {
		out = []requestOut{}
	}
	if filter == "ready" && newReady > 0 {
		_ = a.Store.Update(func(d *store.Data) error {
			if me := d.UserByID(u.ID); me != nil {
				me.ReadySeen = now
			}
			return nil
		})
	} else if filter == "ready" {
		_ = a.Store.Update(func(d *store.Data) error {
			if me := d.UserByID(u.ID); me != nil && me.ReadySeen.IsZero() {
				me.ReadySeen = now
			}
			return nil
		})
	}
	writeJSON(w, 200, map[string]any{"results": out, "counts": states, "groups": counts, "newReady": newReady})
}

// soonest is the date something coming soon is expected (far future if unknown).
func soonest(ro requestOut) time.Time {
	if ro.Release != nil {
		return *ro.Release
	}
	if ro.NextAiring != nil {
		return *ro.NextAiring
	}
	return time.Now().AddDate(10, 0, 0)
}

func (a *App) hCreateRequest(w http.ResponseWriter, r *http.Request, u *store.User) {
	var nr NewRequest
	if err := readJSON(r, &nr); err != nil {
		fail(w, err)
		return
	}
	nr.Source = "web"
	if r.Header.Get("X-Encorr-Client") == "android" {
		nr.Source = "app"
	}
	req, err := a.CreateRequest(r.Context(), u.ID, nr)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 201, req)
}

func (a *App) hDeleteRequest(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := a.DeleteRequest(pathInt(r, "id"), *u); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hApprove(w http.ResponseWriter, r *http.Request, u *store.User) {
	req, err := a.SetStatus(pathInt(r, "id"), u.ID, true, "")
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, req)
}

func (a *App) hDecline(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = readJSON(r, &body)
	req, err := a.SetStatus(pathInt(r, "id"), u.ID, false, body.Reason)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, req)
}

func (a *App) hRetry(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := a.Retry(pathInt(r, "id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- users ----------

func (a *App) hUsers(w http.ResponseWriter, r *http.Request, u *store.User) {
	var out []publicUser
	a.Store.Read(func(d *store.Data) {
		count := map[int]int{}
		for _, rq := range d.Requests {
			count[rq.RequestedBy]++
		}
		for _, uu := range d.Users {
			p := toPublic(uu)
			p.Requests = count[uu.ID]
			out = append(out, p)
		}
	})
	writeJSON(w, 200, map[string]any{"results": out})
}

func (a *App) hUpdateUser(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Role   string `json:"role"`
		Limits *struct {
			Custom bool `json:"custom"` // false: go back to the default
			Movie  int  `json:"movie"`  // 0 = no limit
			TV     int  `json:"tv"`
		} `json:"limits"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.Role == "" && body.Limits == nil {
		fail(w, userErr("Nothing to change."))
		return
	}
	if body.Role != "" && body.Role != store.RoleAdmin && body.Role != store.RoleTrusted && body.Role != store.RoleFriend {
		fail(w, userErr("Role must be admin, trusted or friend."))
		return
	}
	if l := body.Limits; l != nil && (l.Movie < 0 || l.TV < 0 || l.Movie > 1000 || l.TV > 1000) {
		fail(w, userErr("Limits must be between 0 (no limit) and 1000."))
		return
	}
	id := pathInt(r, "id")
	err := a.Store.Update(func(d *store.Data) error {
		target := d.UserByID(id)
		if target == nil {
			return userErr("User not found.")
		}
		if body.Role != "" {
			if target.Role == store.RoleAdmin && body.Role != store.RoleAdmin {
				admins := 0
				for _, x := range d.Users {
					if x.Role == store.RoleAdmin {
						admins++
					}
				}
				if admins <= 1 {
					return userErr("Encorr needs at least one admin.")
				}
			}
			target.Role = body.Role
		}
		if l := body.Limits; l != nil {
			if l.Custom {
				mv, tv := l.Movie, l.TV
				target.MovieLimit, target.TVLimit = &mv, &tv
			} else {
				target.MovieLimit, target.TVLimit = nil, nil
			}
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// hImportUsers adds everyone the owner shares the server with, so they can
// sign in straight away and admins can set roles in advance.
func (a *App) hImportUsers(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.settings()
	friends, err := a.plexClient(string(s.Plex.Token)).Friends(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	added := 0
	err = a.Store.Update(func(d *store.Data) error {
		for _, f := range friends {
			shared := s.Plex.MachineID == ""
			for _, sv := range f.Servers {
				if sv.MachineIdentifier == s.Plex.MachineID {
					shared = true
				}
			}
			if !shared || d.UserByPlexID(f.ID) != nil {
				continue
			}
			name := f.Username
			if name == "" {
				name = f.Title
			}
			d.Users = append(d.Users, &store.User{ID: d.NewID("user"), PlexID: f.ID, Username: name,
				Email: f.Email, Thumb: f.Thumb, Role: store.RoleFriend, Created: time.Now()})
			added++
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]int{"added": added})
}

// ---------- admin tools ----------

func (a *App) hHealth(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeJSON(w, 200, map[string]any{"checks": a.Health(r.Context())})
}

func (a *App) hJobs(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeJSON(w, 200, map[string]any{"jobs": a.Jobs()})
}

func (a *App) hRunJob(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := a.RunJob(r.PathValue("name")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hBackup(w http.ResponseWriter, r *http.Request, u *store.User) {
	p, err := a.Store.Backup()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"path": p})
}

// ---------- settings ----------

func maskArr(s store.ArrSettings) map[string]any {
	return map[string]any{
		"enabled": s.Enabled, "url": s.URL, "apiKeyHint": s.APIKey.Masked(), "hasKey": s.APIKey != "",
		"qualityProfileId": s.QualityProfileID, "qualityProfileName": s.QualityProfileName,
		"rootFolder": s.RootFolder, "languageProfileId": s.LanguageProfileID,
		"minimumAvailability": s.MinimumAvailability, "seasonFolders": s.SeasonFolders,
		"tagRequests": s.TagRequests, "searchOnAdd": s.SearchOnAdd,
	}
}

func (a *App) hSettings(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.settings()
	writeJSON(w, 200, map[string]any{
		"setupComplete": s.SetupComplete, "appUrl": s.AppURL, "region": s.Region, "language": s.Language,
		"accent": s.Accent, "theme": s.Theme, "quotas": s.Quotas, "autoFix": s.AutoFixOn(),
		"plex":     map[string]any{"serverName": s.Plex.ServerName, "machineId": s.Plex.MachineID, "url": s.Plex.URL, "libraries": s.Plex.Libraries},
		"jellyfin": jfSettingsOut(s.Jellyfin),
		"tmdb":     map[string]any{"hasKey": s.TMDBKey != "", "keyHint": s.TMDBKey.Masked(), "builtIn": tmdb.BuiltInKey != ""},
		"radarr":   maskArr(s.Radarr),
		"sonarr":   maskArr(s.Sonarr),
		"discord":  map[string]any{"enabled": s.Discord.Enabled, "hasWebhook": s.Discord.WebhookURL != "", "webhookHint": s.Discord.WebhookURL.Masked()},
		"prowlarr": map[string]any{"enabled": s.Prowlarr.Enabled, "url": s.Prowlarr.URL, "hasKey": s.Prowlarr.APIKey != "", "keyHint": s.Prowlarr.APIKey.Masked()},
		"sabnzbd":  map[string]any{"enabled": s.SABnzbd.Enabled, "url": s.SABnzbd.URL, "hasKey": s.SABnzbd.APIKey != "", "keyHint": s.SABnzbd.APIKey.Masked(), "detected": s.SABnzbd.Detected},
	})
}

func (a *App) hSaveGeneral(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		AppURL  *string              `json:"appUrl"`
		Region  *string              `json:"region"`
		Accent  *string              `json:"accent"`
		Theme   *string              `json:"theme"`
		Quotas  *store.QuotaSettings `json:"quotas"`
		AutoFix *bool                `json:"autoFix"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	err := a.Store.Update(func(d *store.Data) error {
		st := &d.Settings
		if body.AppURL != nil {
			v := strings.TrimRight(strings.TrimSpace(*body.AppURL), "/")
			if v != "" && !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
				return userErr("The public address should start with http:// or https://")
			}
			st.AppURL = v
		}
		if body.Region != nil && len(*body.Region) == 2 {
			st.Region = strings.ToUpper(*body.Region)
			st.Language = "en-" + st.Region
		}
		if body.Theme != nil && (*body.Theme == "black" || *body.Theme == "navy") {
			st.Theme = *body.Theme
		}
		if body.Accent != nil && validAccents[*body.Accent] {
			st.Accent = *body.Accent
		}
		if body.AutoFix != nil {
			v := *body.AutoFix
			st.AutoFix = &v
		}
		if body.Quotas != nil {
			q := *body.Quotas
			if q.Days <= 0 {
				q.Days = 7
			}
			if q.FriendMovieLimit < 0 || q.FriendTVLimit < 0 {
				return userErr("Limits can't be negative (use 0 for no limit).")
			}
			st.Quotas = q
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hSaveTMDB(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Key string `json:"key"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	key := strings.TrimSpace(body.Key)
	if key == "" { // go back to the built-in key
		_ = a.Store.Update(func(d *store.Data) error { d.Settings.TMDBKey = ""; return nil })
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	s := a.settings()
	if err := tmdb.New(key, s.Language, s.Region).Validate(r.Context()); err != nil {
		fail(w, userErr("That key didn't work: "+err.Error()))
		return
	}
	_ = a.Store.Update(func(d *store.Data) error { d.Settings.TMDBKey = store.Secret(key); return nil })
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hPlexServers(w http.ResponseWriter, r *http.Request, u *store.User) {
	servers, err := a.plexClient(string(a.settings().Plex.Token)).Servers(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	type srv struct {
		Name      string `json:"name"`
		MachineID string `json:"machineId"`
		Owned     bool   `json:"owned"`
		Local     bool   `json:"local"`
	}
	out := []srv{}
	for _, s := range servers {
		local := false
		for _, c := range s.Connections {
			if c.Local {
				local = true
			}
		}
		out = append(out, srv{s.Name, s.ClientIdentifier, s.Owned, local})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Owned && !out[j].Owned })
	writeJSON(w, 200, map[string]any{"servers": out})
}

func (a *App) hPickServer(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		MachineID string `json:"machineId"`
		URL       string `json:"url"` // optional manual address
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	s := a.settings()
	pc := a.plexClient(string(s.Plex.Token))
	servers, err := pc.Servers(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	var pick *plex.Resource
	for i := range servers {
		if servers[i].ClientIdentifier == body.MachineID {
			pick = &servers[i]
		}
	}
	if pick == nil {
		fail(w, userErr("That server isn't on your Plex account."))
		return
	}
	if !pick.Owned {
		fail(w, userErr("Pick a server you own: Encorr needs the owner's access to watch the libraries."))
		return
	}
	token := pick.AccessToken
	if token == "" {
		token = string(s.Plex.Token)
	}
	addr := strings.TrimRight(strings.TrimSpace(body.URL), "/")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if addr == "" {
		addr, err = pc.BestConnection(ctx, *pick)
		if err != nil {
			fail(w, userErr(err.Error()+". You can type the address yourself, e.g. http://localhost:32400"))
			return
		}
	}
	srv := &plex.Server{URL: addr, ClientID: s.ClientID, Token: token}
	libs, err := srv.Libraries(ctx)
	if err != nil {
		fail(w, userErr("Connected to "+pick.Name+" but couldn't list libraries: "+err.Error()))
		return
	}
	var saved []store.PlexLibrary
	_ = a.Store.Update(func(d *store.Data) error {
		old := map[string]bool{}
		for _, l := range d.Settings.Plex.Libraries {
			old[l.Key] = l.Enabled
		}
		p := &d.Settings.Plex
		p.ServerName, p.MachineID, p.URL, p.Token = pick.Name, pick.ClientIdentifier, addr, store.Secret(token)
		p.Libraries = nil
		for _, l := range libs {
			en, ok := old[l.Key]
			if !ok {
				en = true
			}
			p.Libraries = append(p.Libraries, store.PlexLibrary{Key: l.Key, Title: l.Title, Type: l.Type, Enabled: en})
		}
		saved = p.Libraries
		return nil
	})
	if a.settings().SetupComplete {
		_ = a.RunJob("plex-full") // a different server: read its library
	}
	writeJSON(w, 200, map[string]any{"serverName": pick.Name, "url": addr, "libraries": saved})
}

func (a *App) hSaveLibraries(w http.ResponseWriter, r *http.Request, u *store.User) {
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
		for i := range d.Settings.Plex.Libraries {
			d.Settings.Plex.Libraries[i].Enabled = on[d.Settings.Plex.Libraries[i].Key]
		}
		return nil
	})
	if a.settings().SetupComplete {
		_ = a.RunJob("plex-full") // re-read the library with the new choice
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func appName(r *http.Request) (string, bool) {
	switch strings.ToLower(r.PathValue("app")) {
	case "radarr":
		return "Radarr", true
	case "sonarr":
		return "Sonarr", true
	}
	return "", false
}

func (a *App) hDetect(w http.ResponseWriter, r *http.Request, u *store.User) {
	hint := netfind.HostOnly(r.Host) // the address the browser used to reach Encorr
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	switch strings.ToLower(r.PathValue("app")) {
	case "sabnzbd":
		det := sab.Detect()
		if det.Found {
			det.URL = findReachable(ctx, netfind.SABnzbd, det.URL, hint)
			if _, err := (&sab.Client{URL: det.URL, APIKey: det.APIKey}).Test(ctx); err != nil {
				det.Note = "Found SABnzbd's settings, but it didn't answer: " + err.Error()
			}
		} else if u := netfind.Find(ctx, netfind.SABnzbd, 0, hint); u != "" {
			det.URL, det.Note = u, "Found SABnzbd at "+u+". Now paste its API key: SABnzbd → Config → General → API Key."
		} else if netfind.InDocker() {
			det.Note = dockerNotFound("SABnzbd", 8080, hint)
		}
		writeJSON(w, 200, det)
		return
	}
	name, ok := appName(r)
	if strings.EqualFold(r.PathValue("app"), "prowlarr") {
		name, ok = "Prowlarr", true
	}
	if !ok {
		writeErr(w, 404, "Unknown app.")
		return
	}
	app, _ := netfind.ByName(name)
	det := arr.Detect(name)
	if det.Found {
		det.URL = findReachable(ctx, app, det.URL, hint)
		// Prove it before suggesting it.
		cl := &arr.Client{Name: name, URL: det.URL, APIKey: det.APIKey}
		if name == "Prowlarr" {
			cl.APIVersion = "v1"
		}
		if _, err := cl.Status(ctx); err != nil {
			det.Note = "Found " + name + "'s settings, but it didn't answer: " + err.Error()
		}
	} else if u := netfind.Find(ctx, app, 0, hint); u != "" {
		det.URL, det.Note = u, "Found "+name+" at "+u+". Now paste its API key: "+name+" → Settings → General → API Key."
	} else if netfind.InDocker() {
		det.URL, det.Note = "", dockerNotFound(name, app.Port, hint)
	}
	writeJSON(w, 200, det)
}

// findReachable swaps the "localhost" from a settings file for an address
// Encorr can actually reach (inside Docker, localhost is Encorr itself).
func findReachable(ctx context.Context, app netfind.App, found, hint string) string {
	if !netfind.InDocker() {
		return found
	}
	pu, err := url.Parse(found)
	if err != nil {
		return found
	}
	port, _ := strconv.Atoi(pu.Port())
	if u := netfind.FindAt(ctx, app, port, pu.Path, hint); u != "" {
		return netfind.SwapHost(found, u)
	}
	return found
}

func dockerNotFound(name string, port int, hint string) string {
	host := hint
	if host == "" || host == "localhost" || strings.HasPrefix(host, "127.") {
		host = "192.168.1.50"
	}
	return fmt.Sprintf("Encorr is running in Docker and couldn't find %s on the Docker network or this server. Type its address, like http://%s:%d (not localhost: inside Docker that's Encorr itself).", name, host, port)
}

func (a *App) hTestArr(w http.ResponseWriter, r *http.Request, u *store.User) {
	if r.PathValue("app") == "sabnzbd" {
		a.hTestSAB(w, r)
		return
	}
	name, ok := appName(r)
	if !ok {
		writeErr(w, 404, "Unknown app.")
		return
	}
	var body struct {
		URL    string `json:"url"`
		APIKey string `json:"apiKey"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	key := strings.TrimSpace(body.APIKey)
	if key == "" { // re-test with the saved key
		s := a.settings()
		if name == "Radarr" {
			key = string(s.Radarr.APIKey)
		} else {
			key = string(s.Sonarr.APIKey)
		}
	}
	url := normaliseURL(body.URL)
	if url == "" || key == "" {
		writeJSON(w, 200, arr.TestResult{Error: "Enter the address and API key (" + name + " → Settings → General → API Key)."})
		return
	}
	cl := &arr.Client{Name: name, URL: url, APIKey: key}
	writeJSON(w, 200, cl.Test(r.Context()))
}

func normaliseURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "http://" + u
	}
	return u
}

func (a *App) hSaveArr(w http.ResponseWriter, r *http.Request, u *store.User) {
	name, ok := appName(r)
	if !ok {
		writeErr(w, 404, "Unknown app.")
		return
	}
	var body struct {
		store.ArrSettings
		APIKey string `json:"apiKey"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	in := body.ArrSettings
	in.URL = normaliseURL(in.URL)
	if in.Enabled {
		if in.URL == "" || in.QualityProfileID == 0 || in.RootFolder == "" {
			fail(w, userErr("Press Test first, then choose a quality profile and a root folder."))
			return
		}
	}
	err := a.Store.Update(func(d *store.Data) error {
		dst := &d.Settings.Radarr
		if name == "Sonarr" {
			dst = &d.Settings.Sonarr
		}
		key := dst.APIKey
		if strings.TrimSpace(body.APIKey) != "" {
			key = store.Secret(strings.TrimSpace(body.APIKey))
		}
		if in.Enabled && key == "" {
			return userErr("Enter the API key.")
		}
		in.APIKey = key
		if name == "Radarr" && in.MinimumAvailability == "" {
			in.MinimumAvailability = "released"
		}
		*dst = in
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) sabBody(r *http.Request) (url, key string, enabled bool, err error) {
	var body struct {
		Enabled bool   `json:"enabled"`
		URL     string `json:"url"`
		APIKey  string `json:"apiKey"`
	}
	if err = readJSON(r, &body); err != nil {
		return
	}
	url, key = normaliseURL(body.URL), strings.TrimSpace(body.APIKey)
	if key == "" {
		key = string(a.settings().SABnzbd.APIKey)
	}
	return url, key, body.Enabled, nil
}

func (a *App) hTestSAB(w http.ResponseWriter, r *http.Request) {
	url, key, _, err := a.sabBody(r)
	if err != nil {
		fail(w, err)
		return
	}
	if url == "" || key == "" {
		writeJSON(w, 200, map[string]any{"ok": false, "error": "Enter the address and API key (SABnzbd → Config → General → API Key)."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	v, err := (&sab.Client{URL: url, APIKey: key}).Test(ctx)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "version": v})
}

func (a *App) hSaveSAB(w http.ResponseWriter, r *http.Request, u *store.User) {
	url, key, enabled, err := a.sabBody(r)
	if err != nil {
		fail(w, err)
		return
	}
	if enabled {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if _, err := (&sab.Client{URL: url, APIKey: key}).Test(ctx); err != nil {
			fail(w, userErr(err.Error()))
			return
		}
	}
	err = a.Store.Update(func(d *store.Data) error {
		d.Settings.SABnzbd = store.SabSettings{Enabled: enabled, URL: url, APIKey: store.Secret(key)}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.mu.Lock()
	a.sabTried = true // the user has decided: don't auto-detect over it
	a.mu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hSaveDiscord(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Enabled    bool   `json:"enabled"`
		WebhookURL string `json:"webhookUrl"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	hook := strings.TrimSpace(body.WebhookURL)
	if hook != "" && !strings.HasPrefix(hook, "https://discord.com/api/webhooks/") && !strings.HasPrefix(hook, "https://discordapp.com/api/webhooks/") {
		fail(w, userErr("That doesn't look like a Discord webhook. In Discord: channel settings → Integrations → Webhooks → Copy Webhook URL."))
		return
	}
	err := a.Store.Update(func(d *store.Data) error {
		if hook != "" {
			d.Settings.Discord.WebhookURL = store.Secret(hook)
		}
		if body.Enabled && d.Settings.Discord.WebhookURL == "" {
			return userErr("Paste the webhook address first.")
		}
		d.Settings.Discord.Enabled = body.Enabled
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hTestDiscord(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		WebhookURL string `json:"webhookUrl"`
	}
	_ = readJSON(r, &body)
	hook := strings.TrimSpace(body.WebhookURL)
	if hook == "" {
		hook = string(a.settings().Discord.WebhookURL)
	}
	if hook == "" {
		fail(w, userErr("Paste the webhook address first."))
		return
	}
	if err := notify.Discord(r.Context(), hook, notify.Message{Event: notify.EventTest, MentionID: u.DiscordID}); err != nil {
		fail(w, userErr(err.Error()))
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hCompleteSetup(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.settings()
	switch {
	case s.Plex.URL == "" && !s.Jellyfin.Ready():
		fail(w, userErr("Connect Plex or Jellyfin first."))
		return
	case tmdbKey(s) == "":
		fail(w, userErr("Add a TMDB key first."))
		return
	}
	_ = a.Store.Update(func(d *store.Data) error { d.Settings.SetupComplete = true; return nil })
	if s.Plex.URL != "" {
		_ = a.RunJob("plex-full")
	}
	if s.Jellyfin.Ready() {
		_ = a.RunJob("jellyfin-full")
	}
	_ = a.RunJob("downloads")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// describeOpen fills in the live picture for an approved request: download
// progress, episodes per season, release dates and an overall state.
func (a *App) describeOpen(ro *requestOut, m *store.Media) {
	ro.Progress = a.progressFor(ro.Type, m.ExternalID)
	svc := "radarr"
	if ro.Type == store.TypeTV {
		svc = "sonarr"
		dl := map[int]map[int]bool{} // season -> episodes downloading
		a.mu.Lock()
		for _, it := range a.downloads["sonarr"][m.ExternalID] {
			sn := it.SeasonNumber
			if it.Episode != nil {
				sn = it.Episode.SeasonNumber
			}
			if dl[sn] == nil {
				dl[sn] = map[int]bool{}
			}
			dl[sn][it.EpisodeID] = true
		}
		a.mu.Unlock()
		for _, s := range ro.Request.Seasons {
			want, have := m.SeasonEps[s], m.SeasonHave[s]
			if have > want {
				have = want
			}
			ro.Want += want
			ro.Have += have
			down := len(dl[s])
			if down > want-have {
				down = want - have
			}
			ro.Seasons = append(ro.Seasons, seasonOut{Season: s, Have: have, Want: want, Downloading: down})
		}
	}
	a.mu.Lock()
	if t, ok := a.nudged[svc+":"+strconv.Itoa(m.ExternalID)]; ok {
		ro.LastSearch = &t
	}
	mi, hasMovie := a.movieInfo[m.ExternalID]
	si, hasShow := a.showInfo[m.ExternalID]
	a.mu.Unlock()
	notOut := hasMovie && !mi.IsAvailable && !mi.HasFile && mi.Status != "" && mi.Status != "released"
	if ro.Type == store.TypeMovie && notOut {
		for _, t := range []time.Time{mi.DigitalRelease, mi.PhysicalRelease} {
			if !t.IsZero() && (ro.Release == nil || t.Before(*ro.Release)) {
				tt := t
				ro.Release = &tt
			}
		}
	}
	if ro.Type == store.TypeTV && hasShow && !si.NextAiring.IsZero() {
		t := si.NextAiring
		ro.NextAiring = &t
	}
	p := ro.Progress
	switch {
	case p != nil && (p.Stage == "Stuck" || p.Stage == "Failed"):
		ro.State = "attention"
	case p != nil && (p.Stage == "Unpacking" || p.Stage == "Importing" || p.Stage == "Waiting for Plex"):
		ro.State = "finishing"
	case p != nil:
		ro.State = "downloading"
	case ro.Type == store.TypeMovie && notOut:
		ro.State = "waiting"
	case ro.Type == store.TypeTV && ro.Want == 0:
		ro.State = "waiting" // nothing has aired yet
	case ro.Type == store.TypeTV && ro.Have >= ro.Want:
		ro.State = "finishing" // everything's there; Encorr is just confirming with Plex
	default:
		ro.State = "searching"
	}
}

// hSearchNow asks Radarr/Sonarr to search for a request straight away: the
// whole thing, one season, or a few episodes.
func (a *App) hSearchNow(w http.ResponseWriter, r *http.Request, u *store.User) {
	reqID := pathInt(r, "id")
	svc, c, id, err := a.arrFor(reqID)
	if err != nil {
		fail(w, err)
		return
	}
	var body struct {
		Season   int   `json:"season"`
		Episodes []int `json:"episodes"`
	}
	_ = readJSON(r, &body)
	kind := "movie"
	if svc == "sonarr" {
		kind = "tv"
	}
	what := "everything"
	switch {
	case svc == "sonarr" && len(body.Episodes) > 0:
		err = c.SearchEpisodes(r.Context(), body.Episodes)
		what = fmt.Sprintf("%d episode(s)", len(body.Episodes))
	case svc == "sonarr" && body.Season > 0:
		err = c.SearchSeason(r.Context(), id, body.Season)
		what = fmt.Sprintf("season %d", body.Season)
	default:
		err = c.Search(r.Context(), kind, id)
	}
	if err != nil {
		fail(w, err)
		return
	}
	a.noteNudged(svc + ":" + strconv.Itoa(id))
	_ = a.Store.Update(func(d *store.Data) error {
		if rq := d.RequestByID(reqID); rq != nil {
			rq.Log("search", "Asked for a fresh search ("+what+") by "+u.Username)
		}
		return nil
	})
	writeJSON(w, 200, map[string]any{"ok": true, "message": c.Name + " is searching now"})
}

// hImportNow confirms the imports Radarr/Sonarr were waiting on.
func (a *App) hImportNow(w http.ResponseWriter, r *http.Request, u *store.User) {
	svc, c, id, err := a.arrFor(pathInt(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	kind := "movie"
	if svc == "sonarr" {
		kind = "tv"
	}
	n, err := c.ImportBlocked(r.Context(), kind, id)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	_ = a.RunJob("downloads")
	writeJSON(w, 200, map[string]any{"ok": true, "files": n, "message": fmt.Sprintf("Importing %d file(s) now", n)})
}

// arrFor finds the Radarr/Sonarr item behind a request.
func (a *App) arrFor(reqID int) (svc string, c *arr.Client, id int, err error) {
	var typ string
	a.Store.Read(func(d *store.Data) {
		for _, rq := range d.Requests {
			if rq.ID == reqID {
				typ = rq.Type
				if m := d.MediaByID(rq.MediaID); m != nil {
					id = m.ExternalID
				}
			}
		}
	})
	if typ == "" {
		return "", nil, 0, userErr("Request not found.")
	}
	if id == 0 {
		return "", nil, 0, userErr("This request hasn't been sent to Radarr/Sonarr yet.")
	}
	svc, c = "radarr", a.Radarr()
	if typ == store.TypeTV {
		svc, c = "sonarr", a.Sonarr()
	}
	if c == nil {
		return "", nil, 0, errors.New(map[string]string{"radarr": "Radarr", "sonarr": "Sonarr"}[svc] + " isn't set up")
	}
	return svc, c, id, nil
}

// validAccents are the accent colours the stylesheet defines.
var validAccents = map[string]bool{"violet": true, "blue": true, "teal": true, "green": true, "yellow": true, "coral": true, "red": true, "pink": true}
