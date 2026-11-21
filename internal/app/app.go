// Package app wires Encorr together: the request engine, background jobs,
// notifications and the /api/v1 web API.
package app

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/jellyfin"
	"github.com/FiTH-g1gGLe/encorr/internal/notify"
	"github.com/FiTH-g1gGLe/encorr/internal/plex"
	"github.com/FiTH-g1gGLe/encorr/internal/sab"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
	"github.com/FiTH-g1gGLe/encorr/internal/tmdb"
)

// Version can be set at build time with -ldflags "-X .../internal/app.Version=x.y.z".
var Version = "0.1.1"

type App struct {
	Store *store.Store
	Log   *log.Logger

	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	tmdbC     *tmdb.Client
	tmdbSig   string
	downloads map[string]map[int][]arr.QueueItem // "radarr"/"sonarr" -> external id -> items
	sabSnap   *sab.Snapshot                      // live SABnzbd numbers (nil without SABnzbd)
	sabWatch  sabWatch                           // spots SABnzbd's unpacking getting stuck
	landed    map[string]map[int]time.Time       // finished downloading, waiting for Plex
	nudged    map[string]time.Time               // "sonarr:12" -> last time Encorr asked for a fresh search
	queueSeen map[string]bool                    // the Radarr/Sonarr queue has been read since start
	relJobs   map[string]*releaseJob             // background release searches
	wokeAt    time.Time                          // something was just requested or grabbed
	movieInfo map[int]arr.MovieInfo              // Radarr id -> release dates etc.
	showInfo  map[int]arr.SeriesInfo             // Sonarr id -> next episode etc.
	recent    recentCache                        // "Just added to Plex" row
	fixes     map[string]*fixState               // auto-fix memory per download
	// orphansCleared is when auto-fix last cleared SABnzbd's leftover folders.
	orphansCleared time.Time
	lastProgress   time.Time           // when any download last moved (auto-fix)
	conn           *ConnTuner          // the connection helper, while running and after
	tuneFails      map[string]tuneFail // Tune-up changes whose last Apply failed
	stallWarned    time.Time

	sabTried      bool
	prowlarrTried bool
	// Background is set by main: runs Encorr as a Windows service.
	Background Background
	// Restart is set by main: starts the (just updated) program again.
	Restart  func()
	upd      updater
	quit     chan struct{}
	quitOnce sync.Once
	jobs     map[string]*job
	limiter  *rateLimiter
	// Wrong passwords: 10 tries per 15 minutes per address.
	loginLimiter *rateLimiter
	wg           sync.WaitGroup
}

func New(st *store.Store, logger *log.Logger) *App {
	jellyfin.Version = Version
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{
		Store:        st,
		Log:          logger,
		ctx:          ctx,
		cancel:       cancel,
		downloads:    map[string]map[int][]arr.QueueItem{},
		landed:       map[string]map[int]time.Time{"radarr": {}, "sonarr": {}},
		limiter:      newRateLimiter(20, time.Minute),
		loginLimiter: newRateLimiter(10, 15*time.Minute),
		quit:         make(chan struct{}),
	}
	a.Store.Read(func(d *store.Data) {
		a.nudged = map[string]time.Time{}
		for k, t := range d.Nudged {
			a.nudged[k] = t
		}
	})
	a.setupJobs()
	return a
}

// noteNudged records a search request, in memory and on disk. Call with a.mu
// not held.
func (a *App) noteNudged(k string) {
	now := time.Now()
	a.mu.Lock()
	if a.nudged == nil {
		a.nudged = map[string]time.Time{}
	}
	a.nudged[k] = now
	a.mu.Unlock()
	_ = a.Store.Update(func(d *store.Data) error {
		if d.Nudged == nil {
			d.Nudged = map[string]time.Time{}
		}
		d.Nudged[k] = now
		for key, t := range d.Nudged { // forget old ones
			if time.Since(t) > 14*24*time.Hour {
				delete(d.Nudged, key)
			}
		}
		return nil
	})
}

// Start begins background jobs.
func (a *App) Start() { a.startJobs() }

// Stop ends background work and waits briefly for it to finish.
func (a *App) Stop() {
	a.cancel()
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func (a *App) settings() store.Settings {
	var s store.Settings
	a.Store.Read(func(d *store.Data) { s = d.Settings })
	return s
}

// TMDB returns a client for the saved key (rebuilt when the key changes).
func (a *App) TMDB() *tmdb.Client {
	s := a.settings()
	key := tmdbKey(s)
	sig := key + "|" + s.Language + "|" + s.Region
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tmdbC == nil || a.tmdbSig != sig {
		a.tmdbC = tmdb.New(key, s.Language, s.Region)
		a.tmdbSig = sig
	}
	return a.tmdbC
}

// tmdbKey is the user's own key if they set one, otherwise Encorr's built-in key.
func tmdbKey(s store.Settings) string {
	if s.TMDBKey != "" {
		return string(s.TMDBKey)
	}
	return tmdb.BuiltInKey
}

func arrClient(name string, s store.ArrSettings) *arr.Client {
	if !s.Enabled || s.URL == "" || s.APIKey == "" {
		return nil
	}
	return &arr.Client{Name: name, URL: s.URL, APIKey: string(s.APIKey)}
}

func (a *App) Radarr() *arr.Client { return arrClient("Radarr", a.settings().Radarr) }
func (a *App) Sonarr() *arr.Client { return arrClient("Sonarr", a.settings().Sonarr) }

// Prowlarr is optional: Tune-up uses it to check indexers.
func (a *App) Prowlarr() *arr.Client {
	c := arrClient("Prowlarr", a.settings().Prowlarr)
	if c != nil {
		c.APIVersion = "v1"
	}
	return c
}

// PlexServer returns the configured server, or nil if not set up yet.
// SAB returns the SABnzbd client, or nil when it isn't set up.
func (a *App) SAB() *sab.Client {
	s := a.settings().SABnzbd
	if !s.Enabled || s.URL == "" || s.APIKey == "" {
		return nil
	}
	return &sab.Client{URL: s.URL, APIKey: string(s.APIKey)}
}

func (a *App) PlexServer() *plex.Server {
	s := a.settings()
	if s.Plex.URL == "" || s.Plex.Token == "" {
		return nil
	}
	return &plex.Server{URL: s.Plex.URL, ClientID: s.ClientID, Token: string(s.Plex.Token)}
}

// Jellyfin returns a client for the owner's Jellyfin server (with Encorr's
// API key), or nil when it isn't connected.
func (a *App) Jellyfin() *jellyfin.Client {
	s := a.settings()
	if !s.Jellyfin.Ready() {
		return nil
	}
	return &jellyfin.Client{URL: s.Jellyfin.URL, Token: string(s.Jellyfin.APIKey), DeviceID: s.ClientID}
}

// jellyfinAt is a client for signing in (no token) at an address.
func (a *App) jellyfinAt(url, token string) *jellyfin.Client {
	return &jellyfin.Client{URL: url, Token: token, DeviceID: a.settings().ClientID}
}

// libName is what people's media server is called in messages: "Plex",
// "Jellyfin", or "your library" when both are connected.
func (a *App) libName() string {
	s := a.settings()
	hasPlex, hasJF := s.Plex.URL != "", s.Jellyfin.Ready()
	switch {
	case hasPlex && hasJF:
		return "your library"
	case hasJF:
		return "Jellyfin"
	}
	return "Plex"
}

// sourceName names one server.
func sourceName(src string) string {
	if src == srcJellyfin {
		return "Jellyfin"
	}
	return "Plex"
}

func (a *App) plexClient(token string) *plex.Client {
	return &plex.Client{ClientID: a.settings().ClientID, Token: token}
}

// ImageURL turns a TMDB path into a full poster URL.
func ImageURL(path, size string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http") {
		return path
	}
	return tmdb.ImageBase + size + path
}

// Notify sends a message on every enabled channel, in the background.
func (a *App) Notify(m notify.Message) {
	s := a.settings()
	if !s.Discord.Enabled || s.Discord.WebhookURL == "" {
		return
	}
	if m.Library == "" {
		m.Library = a.libName()
	}
	if m.URL == "" && s.AppURL != "" {
		m.URL = strings.TrimRight(s.AppURL, "/") + "/"
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := notify.Discord(ctx, string(s.Discord.WebhookURL), m); err != nil {
			a.Log.Printf("discord: %v", err)
		}
	}()
}

func (a *App) logf(format string, args ...any) { a.Log.Printf(format, args...) }

func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }
