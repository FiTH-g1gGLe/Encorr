package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/fake"
	"github.com/FiTH-g1gGLe/encorr/internal/plex"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
	"github.com/FiTH-g1gGLe/encorr/internal/tmdb"
	"github.com/FiTH-g1gGLe/encorr/internal/web"
)

type client struct {
	t    *testing.T
	base string
	hc   *http.Client
}

func newClient(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: base, hc: &http.Client{Jar: jar}}
}

func (c *client) call(method, path string, body any, out any) (int, map[string]any) {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+"/api/v1"+path, rdr)
	req.Header.Set("X-Encorr", "1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var generic map[string]any
	_ = json.Unmarshal(data, &generic)
	if out != nil {
		_ = json.Unmarshal(data, out)
	}
	return resp.StatusCode, generic
}

func (c *client) ok(method, path string, body any, out any) map[string]any {
	c.t.Helper()
	code, m := c.call(method, path, body, out)
	if code >= 300 {
		c.t.Fatalf("%s %s: HTTP %d %v", method, path, code, m)
	}
	return m
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestEndToEnd(t *testing.T) {
	fk := fake.New()
	fakeSrv := httptest.NewServer(fk)
	defer fakeSrv.Close()
	fk.URL = fakeSrv.URL
	plex.PlexTVURL = fakeSrv.URL + "/plextv"
	plex.DiscoverURL = fakeSrv.URL + "/discover"
	tmdb.BaseURL = fakeSrv.URL + "/tmdb"

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(st, log.New(io.Discard, "", 0))
	defer a.Stop()
	srv := httptest.NewServer(a.Handler(web.Static()))
	defer srv.Close()
	ctx := context.Background()

	owner := newClient(t, srv.URL)

	// Settings are locked until someone signs in.
	if code, _ := owner.call("GET", "/settings", nil, nil); code != 401 {
		t.Fatalf("settings before sign-in: want 401, got %d", code)
	}
	// CSRF guard.
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/auth/plex/pin", nil)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 403 {
		t.Fatalf("missing X-Encorr header should be blocked, got %d", resp.StatusCode)
	}

	// 1. Owner signs in with Plex and becomes admin.
	pin := owner.ok("POST", "/auth/plex/pin", map[string]any{}, nil)
	if !strings.Contains(pin["url"].(string), "code=ENCR") {
		t.Fatalf("auth url: %v", pin["url"])
	}
	res := owner.ok("POST", "/auth/plex/check", map[string]any{"id": 77}, nil)
	if res["role"] != "admin" {
		t.Fatalf("first sign-in should be admin, got %v", res)
	}

	// 2. Pick server and libraries.
	var servers struct {
		Servers []struct{ MachineID string }
	}
	owner.ok("GET", "/settings/plex/servers", nil, &servers)
	if len(servers.Servers) != 1 {
		t.Fatalf("servers: %+v", servers)
	}
	var picked struct {
		Libraries []store.PlexLibrary
	}
	owner.ok("POST", "/settings/plex/server", map[string]any{"machineId": "machine-1"}, &picked)
	if len(picked.Libraries) != 2 || !picked.Libraries[0].Enabled {
		t.Fatalf("libraries: %+v", picked)
	}

	// 3. TMDB key: a bad key is refused, a good one saved.
	if code, m := owner.call("PUT", "/settings/tmdb", map[string]any{"key": "bad"}, nil); code != 400 {
		t.Fatalf("bad tmdb key: %d %v", code, m)
	}
	owner.ok("PUT", "/settings/tmdb", map[string]any{"key": "good-key"}, nil)

	// 4. Radarr and Sonarr: test fills dropdowns, then save.
	for _, app := range []string{"radarr", "sonarr"} {
		var tr struct {
			OK          bool
			Profiles    []struct{ ID int }
			RootFolders []struct{ Path string }
			Error       string
		}
		owner.ok("POST", "/settings/"+app+"/test", map[string]any{"url": fakeSrv.URL + "/" + app, "apiKey": app + "-key"}, &tr)
		if !tr.OK || len(tr.Profiles) != 3 || len(tr.RootFolders) != 1 {
			t.Fatalf("%s test: %+v", app, tr)
		}
		owner.ok("PUT", "/settings/arr/"+app, map[string]any{"enabled": true, "url": fakeSrv.URL + "/" + app, "apiKey": app + "-key",
			"qualityProfileId": 4, "qualityProfileName": "HD-1080p", "rootFolder": tr.RootFolders[0].Path,
			"tagRequests": true, "searchOnAdd": true, "seasonFolders": true}, nil)
	}
	// Wrong key gives a friendly message.
	var bad struct {
		OK    bool
		Error string
	}
	owner.ok("POST", "/settings/radarr/test", map[string]any{"url": fakeSrv.URL + "/radarr", "apiKey": "nope"}, &bad)
	if bad.OK || !strings.Contains(bad.Error, "API key") {
		t.Fatalf("wrong key message: %+v", bad)
	}

	// Discord webhook (set directly: the API only accepts real discord.com URLs).
	_ = st.Update(func(d *store.Data) error {
		d.Settings.Discord = store.DiscordSetting{Enabled: true, WebhookURL: store.Secret(fakeSrv.URL + "/discord/hook")}
		return nil
	})
	owner.ok("POST", "/settings/complete", map[string]any{}, nil)

	// 5. Full scan finds what's already in Plex.
	if _, err := a.jobPlexFull(ctx); err != nil {
		t.Fatal(err)
	}
	var disc struct {
		Results []struct {
			ID     int
			Type   string
			Status string
		}
	}
	owner.ok("GET", "/discover/movies", nil, &disc)
	found := false
	for _, c := range disc.Results {
		if c.ID == 105 {
			found = true
			if c.Status != "available" {
				t.Fatalf("film 105 should show as in Plex, got %q", c.Status)
			}
		}
	}
	if !found {
		t.Fatal("film 105 missing from discover")
	}
	// A show in Plex that nobody requested: its page checks seasons live.
	var parish struct{ Status statusInfo }
	owner.ok("GET", "/title/tv/203", nil, &parish)
	if parish.Status.Status != "available" || parish.Status.Seasons[1] != "available" {
		t.Fatalf("show already in Plex: %+v", parish.Status)
	}

	// 6. A friend signs in.
	fk.Lock()
	fk.NextLogin = &fk.Friend
	fk.Unlock()
	friend := newClient(t, srv.URL)
	friend.ok("POST", "/auth/plex/pin", map[string]any{}, nil)
	res = friend.ok("POST", "/auth/plex/check", map[string]any{"id": 77}, nil)
	if res["role"] != "friend" {
		t.Fatalf("friend role: %v", res)
	}
	if code, _ := friend.call("GET", "/settings", nil, nil); code != 403 {
		t.Fatalf("friends must not see settings, got %d", code)
	}

	// 7. Friend requests a film: waits for approval.
	var r1 store.Request
	friend.ok("POST", "/requests", map[string]any{"type": "movie", "tmdbId": 101, "note": "extended cut please"}, &r1)
	if r1.Status != store.ReqPending {
		t.Fatalf("friend request should be pending, got %s", r1.Status)
	}
	if code, m := friend.call("POST", "/requests", map[string]any{"type": "movie", "tmdbId": 105}, nil); code != 400 || !strings.Contains(m["error"].(string), "already in Plex") {
		t.Fatalf("film already in Plex: %d %v", code, m)
	}
	if code, m := owner.call("POST", "/requests", map[string]any{"type": "movie", "tmdbId": 101}, nil); code != 400 || !strings.Contains(m["error"].(string), "waiting for approval") {
		t.Fatalf("duplicate request: %d %v", code, m)
	}
	waitFor(t, "requested notification", func() bool { fk.Lock(); defer fk.Unlock(); return len(fk.Discord) >= 1 })

	// 8. Owner approves: it goes to Radarr with tag, profile and folder.
	owner.ok("POST", "/requests/"+itoa(r1.ID)+"/approve", map[string]any{}, nil)
	var radarrMovie map[string]any
	waitFor(t, "Radarr add", func() bool {
		fk.Lock()
		defer fk.Unlock()
		for _, m := range fk.RadarrMovies {
			radarrMovie = m
			return true
		}
		return false
	})
	if radarrMovie["qualityProfileId"].(float64) != 4 || radarrMovie["rootFolderPath"] != `P:\Media\Films` {
		t.Fatalf("radarr body: %v", radarrMovie)
	}
	if tags := radarrMovie["tags"].([]any); len(tags) != 1 {
		t.Fatalf("expected requester tag, got %v", radarrMovie["tags"])
	}
	fk.Lock()
	if fk.Tags["radarr"][0]["label"] != "encorr-matey" {
		t.Fatalf("tag label: %v", fk.Tags["radarr"])
	}
	fk.Unlock()

	// 9. Download progress shows on the title page.
	if _, err := a.jobDownloads(ctx); err != nil {
		t.Fatal(err)
	}
	var title struct {
		Status statusInfo
	}
	waitFor(t, "external id saved", func() bool {
		var ok bool
		st.Read(func(d *store.Data) { m := d.MediaByTMDB("movie", 101); ok = m != nil && m.ExternalID > 0 })
		return ok
	})
	friend.ok("GET", "/title/movie/101", nil, &title)
	if title.Status.Progress == nil || title.Status.Progress.Percent != 75 {
		t.Fatalf("progress: %+v", title.Status)
	}
	if title.Status.MyRequest == nil || title.Status.Status != "processing" {
		t.Fatalf("title status: %+v", title.Status)
	}

	// 9b. With SABnzbd connected, progress is live (fake SAB says 90%).
	if code, m := owner.call("PUT", "/settings/sabnzbd", map[string]any{"enabled": true, "url": fk.URL + "/sabnzbd", "apiKey": "wrong"}, nil); code != 400 {
		t.Fatalf("bad SAB key should be refused: %d %v", code, m)
	}
	owner.ok("PUT", "/settings/sabnzbd", map[string]any{"enabled": true, "url": fk.URL + "/sabnzbd", "apiKey": fake.SABKey}, nil)
	if _, err := a.jobDownloads(ctx); err != nil {
		t.Fatal(err)
	}
	friend.ok("GET", "/title/movie/101", nil, &title)
	if p := title.Status.Progress; p == nil || p.Percent != 90 || !p.Live || p.Stage != "Downloading" || p.Speed != "186 MB/s" || p.TimeLeft != "9s" {
		t.Fatalf("live progress: %+v", title.Status.Progress)
	}

	// 10. Download finishes, Plex adds it, request completes, friend is told.
	before := len(fk.Discord)
	fk.AddToPlex("movie", 101, nil)
	if _, err := a.jobPlexRecent(ctx); err != nil {
		t.Fatal(err)
	}
	var done store.Request
	st.Read(func(d *store.Data) { done = *d.RequestByID(r1.ID) })
	if done.Status != store.ReqCompleted {
		t.Fatalf("request should be completed, got %s", done.Status)
	}
	waitFor(t, "ready notification", func() bool {
		fk.Lock()
		defer fk.Unlock()
		for _, m := range fk.Discord[before:] {
			b, _ := json.Marshal(m)
			if strings.Contains(string(b), "Your encore is ready") {
				return true
			}
		}
		return false
	})

	// 11. Owner requests TV: auto-approved, only the chosen seasons monitored.
	var r2 store.Request
	owner.ok("POST", "/requests", map[string]any{"type": "tv", "tmdbId": 202, "seasons": []int{1, 2}}, &r2)
	if r2.Status != store.ReqApproved {
		t.Fatalf("admin request should auto-approve, got %s", r2.Status)
	}
	var series map[string]any
	waitFor(t, "Sonarr add", func() bool {
		fk.Lock()
		defer fk.Unlock()
		for _, s := range fk.SonarrSeries {
			series = s
			return true
		}
		return false
	})
	monitored := map[float64]bool{}
	for _, s := range series["seasons"].([]any) {
		sm := s.(map[string]any)
		monitored[sm["seasonNumber"].(float64)] = sm["monitored"].(bool)
	}
	if !monitored[1] || !monitored[2] || monitored[3] {
		t.Fatalf("season monitoring: %v", monitored)
	}

	// 12. A show whose only season is already in Plex can't be requested again.
	if code, m := friend.call("POST", "/requests", map[string]any{"type": "tv", "tmdbId": 203}, nil); code != 400 {
		t.Fatalf("show already in Plex: %d %v", code, m)
	}

	// 13. Partial TV: season 1 lands, request stays open until season 2 does.
	fk.AddToPlex("tv", 202, map[int]int{1: 10})
	if _, err := a.jobPlexRecent(ctx); err != nil {
		t.Fatal(err)
	}
	st.Read(func(d *store.Data) { done = *d.RequestByID(r2.ID) })
	if done.Status != store.ReqApproved {
		t.Fatalf("TV request should wait for season 2, got %s", done.Status)
	}
	var tvTitle struct{ Status statusInfo }
	owner.ok("GET", "/title/tv/202", nil, &tvTitle)
	if tvTitle.Status.Status != "partial" || tvTitle.Status.Seasons[1] != "available" || tvTitle.Status.Seasons[2] != "processing" {
		t.Fatalf("partial TV status: %+v", tvTitle.Status)
	}
	fk.AddToPlex("tv", 202, map[int]int{2: 10})
	time.Sleep(1100 * time.Millisecond) // addedAt is in whole seconds
	if _, err := a.jobPlexRecent(ctx); err != nil {
		t.Fatal(err)
	}
	st.Read(func(d *store.Data) { done = *d.RequestByID(r2.ID) })
	if done.Status != store.ReqCompleted {
		t.Fatalf("TV request should complete with both seasons, got %s", done.Status)
	}

	// 14. Watchlist sync turns the friend's watchlist into a request.
	friend.ok("PATCH", "/me", map[string]any{"watchlistSync": true, "discordId": "374591050192257027"}, nil)
	if code, _ := friend.call("PATCH", "/me", map[string]any{"discordId": "not-a-number"}, nil); code != 400 {
		t.Fatal("bad Discord ID should be rejected")
	}
	// Until a full Plex scan has worked, the watchlist waits (so it can't
	// request films you already own).
	if msg, err := a.jobWatchlists(ctx); err != nil || !strings.Contains(msg, "waiting") {
		t.Fatalf("watchlist should wait for a full scan first: %q %v", msg, err)
	}
	if !a.runJob(a.jobs["plex-full"]) {
		t.Fatal("full Plex scan failed")
	}
	if _, err := a.jobWatchlists(ctx); err != nil {
		t.Fatal(err)
	}
	var mine struct{ Results []requestOut }
	friend.ok("GET", "/requests?filter=mine", nil, &mine)
	foundWL := false
	for _, r := range mine.Results {
		if r.TMDBID == 104 && r.Source == "watchlist" && r.Status == store.ReqPending {
			foundWL = true
		}
	}
	if !foundWL {
		t.Fatalf("watchlist request missing: %+v", mine.Results)
	}
	// Running again doesn't duplicate.
	_, _ = a.jobWatchlists(ctx)
	friend.ok("GET", "/requests?filter=mine", nil, &mine)
	if len(mine.Results) != 2 {
		t.Fatalf("watchlist sync duplicated requests: %d", len(mine.Results))
	}

	// 15. Quotas: limit friends to 2 films a week.
	owner.ok("PUT", "/settings/general", map[string]any{"quotas": map[string]int{"friendMovieLimit": 2, "friendTvLimit": 1, "days": 7}}, nil)
	if code, m := friend.call("POST", "/requests", map[string]any{"type": "movie", "tmdbId": 102}, nil); code != 400 || !strings.Contains(m["error"].(string), "limit") {
		t.Fatalf("quota: %d %v", code, m)
	}

	// 16. Friend can cancel their own pending request; decline works for admins.
	var pend struct{ Results []requestOut }
	owner.ok("GET", "/requests?filter=pending", nil, &pend)
	if len(pend.Results) != 1 {
		t.Fatalf("pending: %+v", pend.Results)
	}
	owner.ok("POST", "/requests/"+itoa(pend.Results[0].ID)+"/decline", map[string]any{"reason": "not this week"}, nil)

	// 17. Users: import Plex friends (only those sharing this server), roles.
	imp := owner.ok("POST", "/users/import", map[string]any{}, nil)
	if imp["added"].(float64) != 0 { // the friend already signed in; the neighbour shares another server
		t.Fatalf("import: %v", imp)
	}
	var users struct{ Results []publicUser }
	owner.ok("GET", "/users", nil, &users)
	var friendID int
	for _, u := range users.Results {
		if u.Username == "matey" {
			friendID = u.ID
		}
	}
	owner.ok("PATCH", "/users/"+itoa(friendID), map[string]any{"role": "trusted"}, nil)
	if code, m := owner.call("PATCH", "/users/1", map[string]any{"role": "friend"}, nil); code != 400 || !strings.Contains(m["error"].(string), "at least one admin") {
		t.Fatalf("last admin guard: %d %v", code, m)
	}
	// Now trusted: requests go straight through.
	var r3 store.Request
	friend.ok("POST", "/requests", map[string]any{"type": "movie", "tmdbId": 106}, &r3)
	if r3.Status != store.ReqApproved {
		t.Fatalf("trusted request should be approved, got %s", r3.Status)
	}

	// 18. Health page is green for the connected services.
	var health struct{ Checks []Check }
	owner.ok("GET", "/health", nil, &health)
	for _, c := range health.Checks {
		if c.Name == "Plex" || c.Name == "Radarr" || c.Name == "Sonarr" || strings.HasPrefix(c.Name, "TMDB") {
			if c.Status != "ok" {
				t.Fatalf("health %s: %+v", c.Name, c)
			}
		}
	}

	// 19. Secrets are encrypted on disk.
	raw, _ := readFile(st.Dir() + "/encorr.json")
	if strings.Contains(raw, "radarr-key") || strings.Contains(raw, "tok-owner") || strings.Contains(raw, "good-key") {
		t.Fatal("secrets stored in plain text")
	}

	// 20. Built-in TMDB key: clearing your own key falls back to it.
	tmdb.BuiltInKey = "good-key"
	defer func() { tmdb.BuiltInKey = "" }()
	owner.ok("PUT", "/settings/tmdb", map[string]any{"key": ""}, nil)
	var cfg struct {
		TMDB struct{ HasKey, BuiltIn bool }
	}
	owner.ok("GET", "/settings", nil, &cfg)
	if cfg.TMDB.HasKey || !cfg.TMDB.BuiltIn {
		t.Fatalf("tmdb settings: %+v", cfg.TMDB)
	}
	owner.ok("GET", "/discover/movies", nil, nil)

	// 22. Plex's recently-added list misses a film (slow scan): Encorr still
	// finds it by searching Plex for the title, and it shows under Completed.
	var r4 store.Request
	owner.ok("POST", "/requests", map[string]any{"type": "movie", "tmdbId": 103}, &r4)
	waitFor(t, "film 103 sent to Radarr", func() bool {
		var ok bool
		st.Read(func(d *store.Data) { m := d.MediaByTMDB("movie", 103); ok = m != nil && m.ExternalID > 0 })
		return ok
	})
	if _, err := a.jobDownloads(ctx); err != nil {
		t.Fatal(err)
	}
	fk.AddToPlexQuietly(103)
	if _, err := a.jobDownloads(ctx); err != nil { // it left the queue
		t.Fatal(err)
	}
	waitFor(t, "Plex asked to scan the film's folder", func() bool {
		fk.Lock()
		defer fk.Unlock()
		for _, sc := range fk.Scans {
			if strings.Contains(sc, `Films\Paper Kingdoms`) {
				return true
			}
		}
		return false
	})
	var t103 struct{ Status statusInfo }
	owner.ok("GET", "/title/movie/103", nil, &t103)
	if p := t103.Status.Progress; p == nil || p.Stage != "Waiting for Plex" {
		t.Fatalf("after download: %+v", t103.Status.Progress)
	}
	if _, err := a.jobPlexRecent(ctx); err != nil {
		t.Fatal(err)
	}
	var comp struct{ Results []store.Request }
	owner.ok("GET", "/requests?filter=completed", nil, &comp)
	found = false
	for _, r := range comp.Results {
		if r.Status != store.ReqCompleted {
			t.Fatalf("completed tab has %s request", r.Status)
		}
		found = found || r.ID == r4.ID
	}
	if !found {
		t.Fatalf("film 103 should be completed: %+v", comp.Results)
	}
	var act struct{ Results []store.Request }
	owner.ok("GET", "/requests?filter=active", nil, &act)
	for _, r := range act.Results {
		if r.Status == store.ReqCompleted || r.Status == store.ReqPending {
			t.Fatalf("in-progress tab has %s request", r.Status)
		}
	}

	// 23. Encorr accounts: the owner makes one, the friend signs in with a
	// username and password, must pick their own password, then requests.
	var made struct {
		User     publicUser
		Password string
	}
	if code, _ := owner.call("POST", "/users", map[string]any{"username": "bad name"}, nil); code != 400 {
		t.Fatalf("username with a space should be refused: %d", code)
	}
	owner.ok("POST", "/users", map[string]any{"username": "Dave", "role": "friend"}, &made)
	if made.Password == "" || !made.User.HasPassword || !made.User.MustChange || made.User.Plex {
		t.Fatalf("new account: %+v", made)
	}
	if code, _ := owner.call("POST", "/users", map[string]any{"username": "dave"}, nil); code != 400 {
		t.Fatalf("duplicate username (any case) should be refused: %d", code)
	}
	var stored string
	st.Read(func(d *store.Data) { stored = d.UserByName("dave").PasswordHash })
	if strings.Contains(stored, made.Password) || !strings.HasPrefix(stored, "pbkdf2-sha256$") {
		t.Fatalf("password must be stored hashed, got %q", stored)
	}
	dave := newClient(t, srv.URL)
	if code, m := dave.call("POST", "/auth/login", map[string]any{"username": "dave", "password": "wrong-password"}, nil); code != 401 || !strings.Contains(m["error"].(string), "don't match") {
		t.Fatalf("wrong password: %d %v", code, m)
	}
	if code, _ := dave.call("POST", "/auth/login", map[string]any{"username": "nobody", "password": "whatever1"}, nil); code != 401 {
		t.Fatalf("unknown user: %d", code)
	}
	var login struct{ User publicUser }
	dave.ok("POST", "/auth/login", map[string]any{"username": "DAVE", "password": made.Password}, &login)
	if !login.User.MustChange || login.User.Role != "friend" {
		t.Fatalf("login: %+v", login.User)
	}
	if code, _ := dave.call("POST", "/me/password", map[string]any{"new": "short"}, nil); code != 400 {
		t.Fatalf("short password should be refused: %d", code)
	}
	dave.ok("POST", "/me/password", map[string]any{"new": "daves-own-password"}, nil)
	if code, _ := dave.call("POST", "/me/password", map[string]any{"current": "nope", "new": "another-password"}, nil); code != 400 {
		t.Fatalf("changing needs the current password now: %d", code)
	}
	var dr store.Request
	dave.ok("POST", "/requests", map[string]any{"type": "tv", "tmdbId": 201, "seasons": []int{1}}, &dr)
	if dr.Status != store.ReqPending {
		t.Fatalf("friend's request should wait for approval, got %s", dr.Status)
	}
	// Owner resets it: Dave is signed out and must use the new one.
	var reset struct{ Password string }
	owner.ok("PUT", fmt.Sprintf("/users/%d/password", made.User.ID), map[string]any{}, &reset)
	if code, _ := dave.call("GET", "/me", nil, nil); code != 401 {
		t.Fatalf("reset should sign Dave out: %d", code)
	}
	if code, _ := dave.call("POST", "/auth/login", map[string]any{"username": "dave", "password": "daves-own-password"}, nil); code != 401 {
		t.Fatalf("old password should stop working: %d", code)
	}
	dave.ok("POST", "/auth/login", map[string]any{"username": "dave", "password": reset.Password}, nil)
	// Removing the account signs them out.
	owner.ok("DELETE", fmt.Sprintf("/users/%d", made.User.ID), nil, nil)
	if code, _ := dave.call("GET", "/me", nil, nil); code != 401 {
		t.Fatalf("removed account should be signed out: %d", code)
	}
	if code, _ := owner.call("DELETE", fmt.Sprintf("/users/%d", ownerID(st)), nil, nil); code != 400 {
		t.Fatalf("owner can't remove themselves: %d", code)
	}

	// 21. Web UI is served.
	resp, err := http.Get(srv.URL + "/requests")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("web UI: %v %v", err, resp)
	}

	// 24. Removing a watchlist request sticks: the next sync doesn't add it back.
	var wl *store.Request
	a.Store.Read(func(d *store.Data) {
		for _, r := range d.Requests {
			if r.Source == "watchlist" && r.TMDBID == 104 {
				c := *r
				wl = &c
			}
		}
	})
	if wl == nil {
		t.Fatal("expected the watchlist request to still be there")
	}
	owner.ok("DELETE", "/requests/"+itoa(wl.ID), nil, nil)
	if _, err := a.jobWatchlists(ctx); err != nil {
		t.Fatal(err)
	}
	a.Store.Read(func(d *store.Data) {
		for _, r := range d.Requests {
			if r.Source == "watchlist" && r.TMDBID == 104 {
				t.Fatal("removed watchlist request came back")
			}
		}
	})
}

func ownerID(st *store.Store) int {
	id := 0
	st.Read(func(d *store.Data) {
		for _, u := range d.Users {
			if u.Role == store.RoleAdmin {
				id = u.ID
			}
		}
	})
	return id
}
