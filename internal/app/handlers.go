package app

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/notify"
	"github.com/FiTH-g1gGLe/encorr/internal/plex"
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
	SignedIn      bool   `json:"signedIn"` // has signed in at least once
	Created       string `json:"created"`
	Requests      int    `json:"requests,omitempty"`
}

func toPublic(u *store.User) publicUser {
	return publicUser{ID: u.ID, Username: u.Username, Email: u.Email, Thumb: u.Thumb, Role: u.Role,
		DiscordID: u.DiscordID, WatchlistSync: u.WatchlistSync, SignedIn: u.PlexToken != "",
		Created: u.Created.Format(time.RFC3339)}
}

func (a *App) hStatus(w http.ResponseWriter, r *http.Request) {
	s := a.settings()
	hasAdmin := false
	a.Store.Read(func(d *store.Data) { hasAdmin = d.HasAdmin() })
	out := map[string]any{
		"version": Version, "setupComplete": s.SetupComplete, "hasAdmin": hasAdmin,
		"accent": s.Accent, "serverName": s.Plex.ServerName,
		"radarr": s.Radarr.Enabled, "sonarr": s.Sonarr.Enabled,
	}
	if u := a.currentUser(r); u != nil {
		out["user"] = toPublic(u)
	}
	writeJSON(w, 200, out)
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
	} else if inLib && typ == store.TypeTV && len(lib.Seasons) > 0 {
		si.Seasons = map[int]string{}
		for s, have := range lib.Seasons {
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
	cards, pages, err := a.TMDB().List(r.Context(), r.PathValue("kind"), page)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"results": a.annotate(cards, u.ID), "totalPages": pages})
}

func (a *App) hSearch(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, 200, map[string]any{"results": []any{}})
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	cards, pages, err := a.TMDB().Search(r.Context(), q, page)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"results": a.annotate(cards, u.ID), "totalPages": pages})
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
		// Use exact TMDB episode counts for seasons when Plex detail is known.
		if lib, ok := d.Library[store.LibKey(typ, id)]; ok && typ == store.TypeTV && len(lib.Seasons) > 0 {
			if si.Seasons == nil {
				si.Seasons = map[int]string{}
			}
			all := true
			for _, s := range det.Seasons {
				have := lib.Seasons[s.SeasonNumber]
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
	writeJSON(w, 200, map[string]any{"details": det, "status": si, "recommended": recs})
}

// ---------- requests ----------

type requestOut struct {
	store.Request
	User     string    `json:"user"`
	UserImg  string    `json:"userThumb,omitempty"`
	Progress *Progress `json:"progress,omitempty"`
	Modifier string    `json:"modifiedByName,omitempty"`
}

func (a *App) hRequests(w http.ResponseWriter, r *http.Request, u *store.User) {
	filter := r.URL.Query().Get("filter")
	var out []requestOut
	a.Store.Read(func(d *store.Data) {
		for _, rq := range d.Requests {
			if u.Role != store.RoleAdmin && rq.RequestedBy != u.ID && filter != "all" {
				continue
			}
			if filter == "mine" && rq.RequestedBy != u.ID {
				continue
			}
			if filter == "pending" && rq.Status != store.ReqPending {
				continue
			}
			if filter == "active" && rq.Status != store.ReqApproved && rq.Status != store.ReqPending {
				continue
			}
			ro := requestOut{Request: *rq}
			if ru := d.UserByID(rq.RequestedBy); ru != nil {
				ro.User, ro.UserImg = ru.Username, ru.Thumb
			}
			if mu := d.UserByID(rq.ModifiedBy); mu != nil && rq.ModifiedBy != rq.RequestedBy {
				ro.Modifier = mu.Username
			}
			if rq.Status == store.ReqApproved {
				if m := d.MediaByID(rq.MediaID); m != nil {
					ro.Progress = a.progressFor(rq.Type, m.ExternalID)
				}
			}
			out = append(out, ro)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	if out == nil {
		out = []requestOut{}
	}
	writeJSON(w, 200, map[string]any{"results": out})
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
		Role string `json:"role"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.Role != store.RoleAdmin && body.Role != store.RoleTrusted && body.Role != store.RoleFriend {
		fail(w, userErr("Role must be admin, trusted or friend."))
		return
	}
	id := pathInt(r, "id")
	err := a.Store.Update(func(d *store.Data) error {
		target := d.UserByID(id)
		if target == nil {
			return userErr("User not found.")
		}
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
		"accent": s.Accent, "quotas": s.Quotas,
		"plex":    map[string]any{"serverName": s.Plex.ServerName, "machineId": s.Plex.MachineID, "url": s.Plex.URL, "libraries": s.Plex.Libraries},
		"tmdb":    map[string]any{"hasKey": s.TMDBKey != "", "keyHint": s.TMDBKey.Masked(), "builtIn": tmdb.BuiltInKey != ""},
		"radarr":  maskArr(s.Radarr),
		"sonarr":  maskArr(s.Sonarr),
		"discord": map[string]any{"enabled": s.Discord.Enabled, "hasWebhook": s.Discord.WebhookURL != "", "webhookHint": s.Discord.WebhookURL.Masked()},
	})
}

func (a *App) hSaveGeneral(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		AppURL *string              `json:"appUrl"`
		Region *string              `json:"region"`
		Accent *string              `json:"accent"`
		Quotas *store.QuotaSettings `json:"quotas"`
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
		if body.Accent != nil && (*body.Accent == "violet" || *body.Accent == "coral") {
			st.Accent = *body.Accent
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
	name, ok := appName(r)
	if !ok {
		writeErr(w, 404, "Unknown app.")
		return
	}
	det := arr.Detect(name)
	if det.Found {
		// Prove it before suggesting it.
		cl := &arr.Client{Name: name, URL: det.URL, APIKey: det.APIKey}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		if _, err := cl.Status(ctx); err != nil {
			det.Note = "Found " + name + "'s settings, but it didn't answer: " + err.Error()
		}
	}
	writeJSON(w, 200, det)
}

func (a *App) hTestArr(w http.ResponseWriter, r *http.Request, u *store.User) {
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
	case s.Plex.URL == "":
		fail(w, userErr("Choose your Plex server first."))
		return
	case tmdbKey(s) == "":
		fail(w, userErr("Add a TMDB key first."))
		return
	}
	_ = a.Store.Update(func(d *store.Data) error { d.Settings.SetupComplete = true; return nil })
	_ = a.RunJob("plex-full")
	_ = a.RunJob("downloads")
	writeJSON(w, 200, map[string]bool{"ok": true})
}
