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
	"github.com/FiTH-g1gGLe/encorr/internal/notify"
	"github.com/FiTH-g1gGLe/encorr/internal/plex"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
	"github.com/FiTH-g1gGLe/encorr/internal/tmdb"
)

const Version = "0.1.0"

type App struct {
	Store *store.Store
	Log   *log.Logger

	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	tmdbC     *tmdb.Client
	tmdbSig   string
	downloads map[string]map[int][]arr.QueueItem // "radarr"/"sonarr" -> external id -> items

	jobs    map[string]*job
	limiter *rateLimiter
	wg      sync.WaitGroup
}

func New(st *store.Store, logger *log.Logger) *App {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{
		Store:     st,
		Log:       logger,
		ctx:       ctx,
		cancel:    cancel,
		downloads: map[string]map[int][]arr.QueueItem{},
		limiter:   newRateLimiter(20, time.Minute),
	}
	a.setupJobs()
	return a
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

// PlexServer returns the configured server, or nil if not set up yet.
func (a *App) PlexServer() *plex.Server {
	s := a.settings()
	if s.Plex.URL == "" || s.Plex.Token == "" {
		return nil
	}
	return &plex.Server{URL: s.Plex.URL, ClientID: s.ClientID, Token: string(s.Plex.Token)}
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
