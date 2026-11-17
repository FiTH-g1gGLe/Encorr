package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

const sessionCookie = "encorr_session"

type ctxKey struct{}

// Handler builds the HTTP handler: API under /api/v1, the web UI otherwise.
func (a *App) Handler(static fs.FS) http.Handler {
	mux := http.NewServeMux()
	api := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, h) }

	// Public
	api("GET /api/v1/status", a.hStatus)
	api("POST /api/v1/auth/plex/pin", a.limited(a.hPlexPin))
	api("POST /api/v1/auth/plex/check", a.limited(a.hPlexCheck))
	api("POST /api/v1/auth/login", a.limited(a.hLogin))
	api("POST /api/v1/auth/logout", a.hLogout)

	// Signed-in users
	api("GET /api/v1/me", a.user(a.hMe))
	api("PATCH /api/v1/me", a.user(a.hUpdateMe))
	api("POST /api/v1/me/password", a.user(a.hChangeMyPassword))
	api("GET /api/v1/discover/{kind}", a.user(a.hDiscover))
	api("GET /api/v1/search", a.user(a.hSearch))
	api("GET /api/v1/title/{type}/{id}", a.user(a.hTitle))
	api("GET /api/v1/requests", a.user(a.hRequests))
	api("POST /api/v1/requests", a.user(a.hCreateRequest))
	api("DELETE /api/v1/requests/{id}", a.user(a.hDeleteRequest))

	// Admins
	api("POST /api/v1/requests/{id}/approve", a.admin(a.hApprove))
	api("POST /api/v1/requests/{id}/decline", a.admin(a.hDecline))
	api("POST /api/v1/requests/{id}/retry", a.admin(a.hRetry))
	api("GET /api/v1/users", a.admin(a.hUsers))
	api("PATCH /api/v1/users/{id}", a.admin(a.hUpdateUser))
	api("POST /api/v1/users/import", a.admin(a.hImportUsers))
	api("POST /api/v1/users", a.admin(a.hCreateUser))
	api("PUT /api/v1/users/{id}/password", a.admin(a.hResetPassword))
	api("DELETE /api/v1/users/{id}", a.admin(a.hDeleteUser))
	api("GET /api/v1/health", a.admin(a.hHealth))
	api("GET /api/v1/jobs", a.admin(a.hJobs))
	api("POST /api/v1/jobs/{name}/run", a.admin(a.hRunJob))
	api("POST /api/v1/backup", a.admin(a.hBackup))

	// Settings: open during first-run setup until an admin exists, admin-only after.
	api("GET /api/v1/settings", a.setup(a.hSettings))
	api("PUT /api/v1/settings/general", a.setup(a.hSaveGeneral))
	api("PUT /api/v1/settings/tmdb", a.setup(a.hSaveTMDB))
	api("GET /api/v1/settings/plex/servers", a.setup(a.hPlexServers))
	api("POST /api/v1/settings/plex/server", a.setup(a.hPickServer))
	api("PUT /api/v1/settings/plex/libraries", a.setup(a.hSaveLibraries))
	api("GET /api/v1/settings/detect/{app}", a.setup(a.hDetect))
	api("POST /api/v1/settings/{app}/test", a.setup(a.hTestArr))
	api("PUT /api/v1/settings/arr/{app}", a.setup(a.hSaveArr))
	api("PUT /api/v1/settings/sabnzbd", a.setup(a.hSaveSAB))
	api("PUT /api/v1/settings/discord", a.setup(a.hSaveDiscord))
	api("POST /api/v1/settings/discord/test", a.setup(a.hTestDiscord))
	api("POST /api/v1/settings/complete", a.setup(a.hCompleteSetup))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "Unknown API address.")
	})

	// Web UI: serve files, fall back to index.html for app routes.
	files := http.FileServer(http.FS(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := static.Open(p); err == nil {
				f.Close()
				if strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".css") {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(static, "index.html")
		if err != nil {
			http.Error(w, "web UI missing", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(b)
	})

	return a.secure(mux)
}

// secure adds safety headers, a CSRF guard and panic recovery.
func (a *App) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.logf("panic on %s: %v", r.URL.Path, rec)
				writeErr(w, 500, "Something went wrong inside Encorr. Check the log.")
			}
		}()
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https://image.tmdb.org https://*.plex.tv https://plex.tv data:; frame-src https://www.youtube-nocookie.com; style-src 'self' 'unsafe-inline'; connect-src 'self'")
		// Browsers can't add custom headers cross-site without CORS, so this
		// header proves a state-changing call came from Encorr's own pages.
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-Encorr") != "1" {
				writeErr(w, http.StatusForbidden, "Request blocked (missing X-Encorr header).")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// Behind Cloudflare Tunnel the visitor's address arrives in this header.
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" && isLoopback(host) {
		return ip
	}
	return host
}

func isLoopback(h string) bool {
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (a *App) limited(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.limiter.Allow(clientIP(r)) {
			writeErr(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait a minute and try again.")
			return
		}
		h(w, r)
	}
}

// currentUser returns the signed-in user (a copy) or nil.
func (a *App) currentUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	var u *store.User
	a.Store.Read(func(d *store.Data) {
		for _, s := range d.Sessions {
			if s.Token == c.Value && time.Now().Before(s.Expires) {
				if uu := d.UserByID(s.UserID); uu != nil {
					cp := *uu
					u = &cp
				}
				return
			}
		}
	})
	return u
}

type userHandler func(w http.ResponseWriter, r *http.Request, u *store.User)

func (a *App) user(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "Please sign in.")
			return
		}
		h(w, r, u)
	}
}

func (a *App) admin(h userHandler) http.HandlerFunc {
	return a.user(func(w http.ResponseWriter, r *http.Request, u *store.User) {
		if u.Role != store.RoleAdmin {
			writeErr(w, http.StatusForbidden, "Only admins can do that.")
			return
		}
		h(w, r, u)
	})
}

// setup allows access before the first admin exists (first-run wizard).
func (a *App) setup(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hasAdmin := false
		a.Store.Read(func(d *store.Data) { hasAdmin = d.HasAdmin() })
		if !hasAdmin {
			writeErr(w, http.StatusUnauthorized, "Sign in with Plex first: the first person to sign in becomes the admin.")
			return
		}
		a.admin(h)(w, r)
	}
}

func (a *App) startSession(w http.ResponseWriter, r *http.Request, userID int) error {
	tok := store.RandomToken(32)
	exp := time.Now().Add(30 * 24 * time.Hour)
	err := a.Store.Update(func(d *store.Data) error {
		d.Sessions = append(d.Sessions, &store.Session{Token: tok, UserID: userID, Expires: exp})
		if u := d.UserByID(userID); u != nil {
			u.LastSeen = time.Now()
		}
		return nil
	})
	if err != nil {
		return err
	}
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", Expires: exp,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	return nil
}

// ---------- JSON helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail maps engine errors to friendly responses.
func fail(w http.ResponseWriter, err error) {
	var ue *UserError
	if errors.As(err, &ue) {
		writeErr(w, http.StatusBadRequest, ue.Msg)
		return
	}
	writeErr(w, http.StatusBadGateway, err.Error())
}

func readJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return userErr("Couldn't read that request.")
	}
	return nil
}

func pathInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.PathValue(name))
	return n
}
