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

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
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
	// Home rows: fixed ones first, then today's rotating ones (incl. an actor).
	var rows struct{ Rows []struct{ Key, Title string } }
	owner.ok("GET", "/discover-rows", nil, &rows)
	if len(rows.Rows) < 8 || rows.Rows[0].Key != "trending" {
		t.Fatalf("home rows: %+v", rows.Rows)
	}
	sawActor := false
	for _, r := range rows.Rows {
		var page struct {
			Results    []any
			Title      string
			TotalPages int
		}
		owner.ok("GET", "/discover/"+r.Key, nil, &page)
		if page.Title == "" || len(page.Results) == 0 {
			t.Fatalf("row %s: title %q, %d results", r.Key, page.Title, len(page.Results))
		}
		if strings.HasPrefix(r.Key, "person:") {
			sawActor = page.Title == "Starring Tom Hanks"
		}
	}
	if !sawActor {
		t.Fatal("expected an actor spotlight row")
	}
	if code, _ := owner.call("GET", "/discover/company:abc:movie", nil, nil); code < 400 {
		t.Fatal("bad row keys should be refused")
	}
	// "Just added to Plex" lists what's in Plex, newest first.
	var recent struct {
		Results []struct {
			ID     int
			Status string
		}
	}
	owner.ok("GET", "/discover/plex-recent", nil, &recent)
	if len(recent.Results) == 0 {
		t.Fatal("expected something in Just added to Plex")
	}
	for _, c := range recent.Results {
		if c.Status != "available" && c.Status != "partial" {
			t.Fatalf("recently added item %d should be in Plex, got %q", c.ID, c.Status)
		}
	}
	// Library sync status for the home screen.
	var libst struct{ Library struct{ Films, Shows int } }
	owner.ok("GET", "/status", nil, &libst)
	if libst.Library.Films == 0 {
		t.Fatal("expected the library counts in status")
	}
	// Title pages are rich: crew, cast, tags, money, releases, streaming, and
	// Plex's ratings and picture quality for things in the library.
	var rich struct {
		Details struct {
			Cast      []struct{ Name string }
			Crew      []struct{ Job string }
			Keywords  []struct{ ID int }
			Releases  []struct{ Kind string }
			Providers []struct{ Name string }
			Budget    int64
			Studios   []string
			Language  string
		}
		Plex *struct {
			URL     string
			Quality string
			Ratings []struct{ Source, Value string }
		}
	}
	owner.ok("GET", "/title/movie/105", nil, &rich)
	dd := rich.Details
	if len(dd.Cast) == 0 || len(dd.Crew) == 0 || dd.Crew[0].Job != "Director" || len(dd.Keywords) == 0 || len(dd.Releases) != 2 || len(dd.Providers) == 0 || dd.Budget == 0 || len(dd.Studios) == 0 || dd.Language != "English" {
		t.Fatalf("title details not rich enough: %+v", dd)
	}
	if rich.Plex == nil || rich.Plex.Quality != "4K HDR10" || len(rich.Plex.Ratings) != 3 || rich.Plex.Ratings[1].Value != "91%" {
		t.Fatalf("plex info: %+v", rich.Plex)
	}
	// Search splits films and TV.
	var sr struct{ Movies, TV []struct{ Type string } }
	owner.ok("GET", "/search?q=a", nil, &sr)
	if len(sr.Movies) == 0 || len(sr.TV) == 0 {
		t.Fatalf("search should find both: %d films, %d shows", len(sr.Movies), len(sr.TV))
	}
	for _, m := range sr.Movies {
		if m.Type != "movie" {
			t.Fatal("a show in the films list")
		}
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

	// 25. A request that's been sitting with nothing downloading gets a fresh
	// search from Radarr/Sonarr (once, not every run).
	var stalled string
	_ = a.Store.Update(func(d *store.Data) error {
		for _, r := range d.Requests {
			if m := d.MediaByID(r.MediaID); m != nil && m.ExternalID != 0 {
				r.Status, r.Updated = store.ReqApproved, time.Now().Add(-3*time.Hour)
				stalled = m.Title
				return nil
			}
		}
		return nil
	})
	if stalled == "" {
		t.Fatal("no request linked to Radarr/Sonarr to test with")
	}
	a.mu.Lock()
	a.downloads = map[string]map[int][]arr.QueueItem{"radarr": {}, "sonarr": {}}
	a.mu.Unlock()
	cmdsBefore := len(fk.Commands)
	msg, err := a.jobNudge(ctx)
	if err != nil || !strings.Contains(msg, stalled) {
		t.Fatalf("nudge: %q %v", msg, err)
	}
	searched := false
	for _, c := range fk.Commands[cmdsBefore:] {
		if c["name"] == "MoviesSearch" || c["name"] == "SeriesSearch" {
			searched = true
		}
	}
	if !searched {
		t.Fatalf("expected a search command, got %v", fk.Commands[cmdsBefore:])
	}
	if msg, _ := a.jobNudge(ctx); strings.Contains(msg, stalled) {
		t.Fatal("should not ask again straight away")
	}

	// 25b. Collections: see the whole series and request what's missing in one go.
	var col struct {
		Name    string
		Missing int
		Parts   []struct {
			ID     int
			Status string
		}
	}
	owner.ok("GET", "/collection/900", nil, &col)
	if col.Name == "" || len(col.Parts) != 4 || col.Parts[3].ID != 199 {
		t.Fatalf("collection: %+v", col)
	}
	colMissing := col.Missing
	var cr struct{ Requested int }
	owner.ok("POST", "/collection/900/request", map[string]any{}, &cr)
	if cr.Requested != colMissing {
		t.Fatalf("should request the %d missing films, requested %d", colMissing, cr.Requested)
	}
	owner.ok("GET", "/collection/900", nil, &col)
	if col.Missing != 0 {
		t.Fatalf("nothing should be left to request, %d left", col.Missing)
	}
	for _, p := range col.Parts {
		if p.ID == 199 && p.Status != "none" {
			t.Fatal("the undated sequel shouldn't be requested")
		}
	}

	// 26. In progress tells you what state each request is in, and the owner
	// can search again or confirm a stuck import from Encorr.
	if _, err := a.jobArrInfo(ctx); err != nil {
		t.Fatal(err)
	}
	var reqID, movieExt int
	var reqType string
	a.Store.Read(func(d *store.Data) {
		for _, r := range d.Requests {
			if m := d.MediaByID(r.MediaID); m != nil && m.ExternalID != 0 && r.Type == store.TypeMovie {
				reqID, movieExt, reqType = r.ID, m.ExternalID, r.Type
			}
		}
	})
	if reqID == 0 {
		t.Fatal("no film request linked to Radarr")
	}
	_ = a.Store.Update(func(d *store.Data) error {
		for _, r := range d.Requests {
			if r.ID == reqID {
				r.Status = store.ReqApproved
			}
		}
		return nil
	})
	var prog struct {
		Results []struct {
			ID         int
			State      string
			LastSearch *time.Time
			Progress   *struct{ Blocked bool }
		}
		Counts map[string]int
	}
	owner.ok("POST", "/requests/"+itoa(reqID)+"/search", map[string]any{}, nil)
	owner.ok("GET", "/requests?filter=active", nil, &prog)
	if len(prog.Counts) == 0 {
		t.Fatal("expected state counts")
	}
	for _, r := range prog.Results {
		if r.State == "" {
			t.Fatalf("request %d has no state", r.ID)
		}
		if r.ID == reqID && r.LastSearch == nil {
			t.Fatal("Search now should be remembered")
		}
	}
	// Make its download "finished but blocked", like a name mismatch.
	fk.Queue["radarr"] = []map[string]any{{"movieId": movieExt, "title": "Some.Film.2020.1080p", "size": 4e9, "sizeleft": 0,
		"status": "completed", "trackedDownloadState": "importBlocked", "trackedDownloadStatus": "warning",
		"downloadId": "SABnzbd_nzo_blocked", "outputPath": `D:\complete\movies\Some.Film`,
		"statusMessages": []any{map[string]any{"title": "Some.Film", "messages": []any{"Found matching movie via grab history, but release was matched to movie by ID. Manual Import required."}}}}}
	if _, err := a.jobDownloads(ctx); err != nil {
		t.Fatal(err)
	}
	owner.ok("GET", "/requests?filter=needs", nil, &prog)
	blocked := false
	for _, r := range prog.Results {
		if r.ID == reqID {
			blocked = r.State == "attention" && r.Progress != nil && r.Progress.Blocked
		}
	}
	if !blocked {
		t.Fatalf("expected the film to need attention with a blocked import: %+v", prog.Results)
	}
	cmdsBefore = len(fk.Commands)
	owner.ok("POST", "/requests/"+itoa(reqID)+"/import", map[string]any{}, nil)
	var mi map[string]any
	for _, c := range fk.Commands[cmdsBefore:] {
		if c["name"] == "ManualImport" {
			mi = c
		}
	}
	if mi == nil {
		t.Fatal("expected a ManualImport command")
	}
	files := mi["files"].([]any)
	f0 := files[0].(map[string]any)
	if len(files) != 1 || !strings.HasSuffix(f0["path"].(string), "release.mkv") || int(f0["movieId"].(float64)) != movieExt {
		t.Fatalf("should import the main file for the right film: %+v", files)
	}
	_ = reqType

	// 27. Requests are grouped (Needs you / Active / Coming soon / Ready)
	// with counts, whatever the filter.
	var grouped struct {
		Results []requestOut
		Groups  map[string]int
	}
	owner.ok("GET", "/requests?filter=ready", nil, &grouped)
	if grouped.Groups["all"] == 0 {
		t.Fatalf("expected group counts: %+v", grouped.Groups)
	}
	for _, r := range grouped.Results {
		if r.Group != "ready" || r.Arrived == nil {
			t.Fatalf("ready list should only hold arrived requests: %+v", r)
		}
	}

	// 28. The timeline merges Encorr's own events with Radarr's history.
	fk.History["radarr"] = []map[string]any{
		{"date": time.Now().Add(-time.Hour).Format(time.RFC3339), "eventType": "grabbed", "sourceTitle": "Some.Film.2020.1080p", "data": map[string]any{"indexer": "NZBgeek"}},
		{"date": time.Now().Add(-30 * time.Minute).Format(time.RFC3339), "eventType": "downloadFailed", "sourceTitle": "Some.Film.2020.1080p", "data": map[string]any{"message": "Missing articles"}},
	}
	var tl struct{ Items []timelineItem }
	owner.ok("GET", "/requests/"+itoa(reqID)+"/timeline", nil, &tl)
	var sawGrab, sawFail, sawSearch bool
	for _, it := range tl.Items {
		sawGrab = sawGrab || it.Kind == "grabbed" && strings.Contains(it.Detail, "NZBgeek")
		sawFail = sawFail || it.Kind == "failed" && strings.Contains(it.Detail, "Missing articles")
		sawSearch = sawSearch || it.Kind == "search"
	}
	if !sawGrab || !sawFail || !sawSearch {
		t.Fatalf("timeline is missing lines: %+v", tl.Items)
	}

	// 29. Download controls: move to front, pause, then swap for another copy.
	fk.Queue["radarr"] = []map[string]any{{"id": 501, "movieId": movieExt, "title": "Some.Film.2020.1080p", "size": 4e9, "sizeleft": 2e9,
		"status": "downloading", "trackedDownloadState": "downloading", "downloadId": "nzo_live"}}
	if _, err := a.jobDownloads(ctx); err != nil {
		t.Fatal(err)
	}
	owner.ok("POST", "/requests/"+itoa(reqID)+"/download/top", map[string]any{}, nil)
	owner.ok("POST", "/requests/"+itoa(reqID)+"/download/pause", map[string]any{}, nil)
	if strings.Join(fk.SABCalls, ",") != "switch:nzo_live,pause:nzo_live" {
		t.Fatalf("SABnzbd calls: %v", fk.SABCalls)
	}
	owner.ok("POST", "/requests/"+itoa(reqID)+"/download/retry", map[string]any{}, nil)
	if len(fk.Removed) != 1 || fk.Removed[0]["blocklist"] != "true" || fk.Removed[0]["skipRedownload"] != "false" {
		t.Fatalf("retry should remove, blocklist and re-search: %+v", fk.Removed)
	}
	if code, _ := owner.call("POST", "/requests/"+itoa(reqID)+"/download/nope", map[string]any{}, nil); code != 400 {
		t.Fatalf("unknown action: %d", code)
	}

	// 30. Pick a copy by hand.
	var rel struct{ Releases []releaseOut }
	owner.ok("GET", "/requests/"+itoa(reqID)+"/releases", nil, &rel)
	if len(rel.Releases) != 2 || !rel.Releases[0].Approved || rel.Releases[1].Approved || len(rel.Releases[1].Rejections) != 1 {
		t.Fatalf("releases: %+v", rel.Releases)
	}
	owner.ok("POST", "/requests/"+itoa(reqID)+"/releases", map[string]any{"guid": "g1", "indexerId": 1, "title": "Good.Copy.1080p.WEB-DL"}, nil)
	if len(fk.Grabbed) != 1 || fk.Grabbed[0]["guid"] != "g1" {
		t.Fatalf("grab: %+v", fk.Grabbed)
	}

	// 31. Episodes for a show: one row per episode of the requested seasons.
	var tvReq int
	a.Store.Read(func(d *store.Data) {
		for _, r := range d.Requests {
			if m := d.MediaByID(r.MediaID); m != nil && m.ExternalID != 0 && r.Type == store.TypeTV && r.TMDBID == 202 {
				tvReq = r.ID
			}
		}
	})
	if tvReq == 0 {
		t.Fatal("no Low Orbit request linked to Sonarr")
	}
	var eps struct{ Seasons []seasonEpisodes }
	owner.ok("GET", "/requests/"+itoa(tvReq)+"/episodes", nil, &eps)
	if len(eps.Seasons) != 2 || eps.Seasons[0].Total != 10 || len(eps.Seasons[0].Episodes) != 10 {
		t.Fatalf("episodes: %+v", eps.Seasons)
	}
	if e := eps.Seasons[0].Episodes[0]; e.Number != 1 || e.Title != "Episode 1" || e.Status == "" {
		t.Fatalf("first episode: %+v", e)
	}
	before = len(fk.Commands)
	owner.ok("POST", "/requests/"+itoa(tvReq)+"/search", map[string]any{"season": 2}, nil)
	if c := fk.Commands[before]; c["name"] != "SeasonSearch" || c["seasonNumber"].(float64) != 2 {
		t.Fatalf("season search: %+v", c)
	}
	if code, _ := friend.call("GET", "/requests/"+itoa(tvReq)+"/episodes", nil, nil); code != 404 {
		t.Fatalf("friends can't see someone else's request details: %d", code)
	}

	// 32. Auto-fix swaps a failed download and a stalled one for new copies.
	fk.Removed = nil
	fk.Queue["radarr"] = []map[string]any{
		{"id": 601, "movieId": movieExt, "title": "Broken.Copy", "size": 4e9, "sizeleft": 1e9, "status": "failed", "trackedDownloadState": "failedPending", "downloadId": "nzo_failed"},
	}
	if _, err := a.jobDownloads(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := a.autoFix(ctx, true); n != 0 {
		t.Fatal("a fresh failure gets a few minutes before Encorr steps in")
	}
	a.mu.Lock()
	a.fixes["radarr:nzo_failed"].seen = time.Now().Add(-time.Hour)
	a.mu.Unlock()
	if n, _ := a.autoFix(ctx, true); n != 1 || len(fk.Removed) != 1 || fk.Removed[0]["id"] != 601 || fk.Removed[0]["blocklist"] != "true" {
		t.Fatalf("failed download should be swapped: %d %+v", n, fk.Removed)
	}
	fk.Queue["radarr"] = []map[string]any{
		{"id": 602, "movieId": movieExt, "title": "Slow.Copy", "size": 4e9, "sizeleft": 1e9, "status": "downloading", "trackedDownloadState": "downloading", "downloadId": "nzo_slow"},
	}
	_, _ = a.jobDownloads(ctx)
	_, _ = a.autoFix(ctx, true)
	a.mu.Lock()
	a.fixes["radarr:nzo_slow"].moved = time.Now().Add(-2 * time.Hour)
	a.lastProgress = time.Now().Add(-2 * time.Hour)
	a.mu.Unlock()
	if n, _ := a.autoFix(ctx, true); n != 0 || len(fk.Removed) != 1 {
		t.Fatalf("when nothing at all is moving it's the connection, not the copy: %d %+v", n, fk.Removed)
	}
	a.mu.Lock()
	a.lastProgress = time.Now() // other downloads are moving fine
	a.mu.Unlock()
	if n, _ := a.autoFix(ctx, true); n != 1 || len(fk.Removed) != 2 || fk.Removed[1]["id"] != 602 {
		t.Fatalf("stalled download should be swapped: %d %+v", n, fk.Removed)
	}
	var actLog struct{ Items []store.Activity }
	owner.ok("GET", "/activity", nil, &actLog)
	if len(actLog.Items) < 3 || !strings.Contains(actLog.Items[0].Title, "stalled") || !strings.Contains(actLog.Items[1].Title, "Nothing has downloaded") || !strings.Contains(actLog.Items[2].Title, "failed") {
		t.Fatalf("activity log: %+v", actLog.Items)
	}

	// 33. Health fix buttons.
	fk.SABOrphans = []string{"Old.Job"}
	owner.ok("POST", "/health/sab-orphans", map[string]any{}, nil)
	if fk.SABCalls[len(fk.SABCalls)-1] != "orphans" || fk.SABOrphans != nil {
		t.Fatalf("orphans should be cleared: %v", fk.SABCalls)
	}
	if code, _ := friend.call("POST", "/health/sab-restart", map[string]any{}, nil); code != 403 {
		t.Fatalf("friends can't use fix buttons: %d", code)
	}

	// 34. Tune-up reads every app and lists recommendations.
	owner.ok("PUT", "/settings/prowlarr", map[string]any{"enabled": true, "url": fk.URL + "/prowlarr", "apiKey": "prowlarr-key"}, nil)
	type tuneResp struct {
		Findings []TuneFinding
		Apps     []TuneApp
		Changes  []store.TuneChange
	}
	var tr tuneResp
	owner.ok("GET", "/tune", nil, &tr)
	byID := map[string]TuneFinding{}
	for _, f := range tr.Findings {
		byID[f.ID] = f
		if secretKey.MatchString(f.ID) && !strings.HasPrefix(f.ID, "sab-") && !strings.HasPrefix(f.ID, "prowlarr-") {
			t.Fatalf("Tune-up must never touch connection settings: %s", f.ID)
		}
	}
	for _, a := range tr.Apps {
		if !a.Connected {
			t.Fatalf("%s should be connected: %+v", a.App, a)
		}
	}
	wantChange := []string{"sab-pwrar", "sab-unwanted-action", "sab-unwanted-list", "sab-unrar-dates", "sab-cache", "sab-pipelining:news.example.com",
		"radarr-redownload", "radarr-propers", "radarr-filedate", "radarr-freespace", "radarr-hardsubs", "radarr-foreign-subs",
		"sonarr-illegal", "sonarr-foreign-subs", "plex-fsevents", "plex-lowprio", "plex-bif"}
	for _, id := range wantChange {
		if f, ok := byID[id]; !ok || f.Status != "change" || !f.CanApply {
			t.Fatalf("expected %s to need changing: %+v", id, f)
		}
	}
	if _, ok := byID["sonarr-propers"]; ok {
		t.Fatal("the repacks advice only applies when TRaSH-style custom formats are in use")
	}
	for _, id := range []string{"sab-hopeless", "sonarr-redownload", "plex-partial"} {
		if f, ok := byID[id]; !ok || f.Status != "ok" {
			t.Fatalf("expected %s to be fine: %+v", id, f)
		}
	}
	for _, id := range []string{"sab-connections:news.example.com", "prowlarr-vip:GeekFake", "prowlarr-down:GeekFake", "prowlarr-sync:Sonarr", "radarr-recycle", "radarr-removecompleted"} {
		if f, ok := byID[id]; !ok || f.CanApply {
			t.Fatalf("expected advice %s: %+v", id, f)
		}
	}
	if _, ok := byID["sab-bandwidth"]; ok {
		t.Fatal("line speed needs a speed test first")
	}

	// 35. Applying changes writes them, and checks they took.
	var applied struct {
		Results []struct {
			ID    string
			OK    bool
			Error string
		}
		Changed int
	}
	owner.ok("POST", "/tune/apply", map[string]any{"ids": []string{"sab-pwrar", "sab-unwanted-list", "radarr-redownload", "plex-fsevents", "sab-pipelining:news.example.com", "sonarr-illegal", "nope"}}, &applied)
	if applied.Changed != 6 || applied.Results[6].OK {
		t.Fatalf("apply: %+v", applied)
	}
	if fk.SABMisc["pause_on_pwrar"] != 2 || fk.ArrConfig["radarr"]["downloadclient"]["autoRedownloadFailed"] != true ||
		fk.PlexPrefs["FSEventLibraryUpdatesEnabled"] != true || fk.SABServers[0]["pipelining_requests"] != 4 ||
		fk.ArrConfig["sonarr"]["naming"]["replaceIllegalCharacters"] != true {
		t.Fatalf("changes didn't land: sab=%v radarr=%v plex=%v server=%v", fk.SABMisc["pause_on_pwrar"], fk.ArrConfig["radarr"]["downloadclient"], fk.PlexPrefs, fk.SABServers[0])
	}
	if l, _ := fk.SABMisc["unwanted_extensions"].([]any); len(l) < 5 {
		t.Fatalf("unwanted list: %v", fk.SABMisc["unwanted_extensions"])
	}
	owner.ok("GET", "/tune", nil, &tr)
	for _, f := range tr.Findings {
		if (f.ID == "sab-pwrar" || f.ID == "plex-fsevents" || f.ID == "radarr-redownload") && f.Status != "ok" {
			t.Fatalf("%s should be fine now: %+v", f.ID, f)
		}
	}

	owner.ok("POST", "/tune/apply", map[string]any{"ids": []string{"radarr-freespace", "radarr-filedate", "plex-bif"}}, &applied)
	if applied.Changed != 3 || fk.ArrConfig["radarr"]["mediamanagement"]["minimumFreeSpaceWhenImporting"] != 1024.0 ||
		fk.ArrConfig["radarr"]["mediamanagement"]["fileDate"] != "none" || fk.PlexPrefs["GenerateBIFBehavior"] != "scheduled" {
		t.Fatalf("number/text settings: %+v %v %v", applied, fk.ArrConfig["radarr"]["mediamanagement"], fk.PlexPrefs["GenerateBIFBehavior"])
	}
	owner.ok("GET", "/tune", nil, &tr)

	// 36. Undo puts the old value back, but won't silently overwrite a later change.
	changeFor := func(rule string) store.TuneChange {
		for _, c := range tr.Changes {
			if c.Rule == rule && !c.Undone {
				return c
			}
		}
		t.Fatalf("no change recorded for %s: %+v", rule, tr.Changes)
		return store.TuneChange{}
	}
	owner.ok("POST", "/tune/undo/"+itoa(changeFor("plex-fsevents").ID), map[string]any{}, nil)
	if fk.PlexPrefs["FSEventLibraryUpdatesEnabled"] != false {
		t.Fatal("undo should switch it back off")
	}
	fk.SABMisc["pause_on_pwrar"] = 0 // someone changed it in SABnzbd since
	pw := changeFor("sab-pwrar").ID
	if code, m := owner.call("POST", "/tune/undo/"+itoa(pw), map[string]any{}, nil); code != 409 || !strings.Contains(m["error"].(string), "changed again") {
		t.Fatalf("undo over a later change should ask first: %d %v", code, m)
	}
	owner.ok("POST", "/tune/undo/"+itoa(pw), map[string]any{"force": true}, nil)
	if fk.SABMisc["pause_on_pwrar"] != 1 {
		t.Fatalf("forced undo: %v", fk.SABMisc["pause_on_pwrar"])
	}
	if code, _ := owner.call("POST", "/tune/undo/"+itoa(pw), map[string]any{}, nil); code != 400 {
		t.Fatal("can't undo twice")
	}

	// 37. Blocking foreign-subtitle releases, and undoing it.
	owner.ok("POST", "/tune/apply", map[string]any{"ids": []string{"radarr-foreign-subs", "sonarr-foreign-subs"}}, &applied)
	if applied.Changed != 2 || len(fk.ReleaseProfs) != 1 {
		t.Fatalf("foreign subs: %+v %v", applied, fk.ReleaseProfs)
	}
	var cfID any
	for _, cf := range fk.CustomFormats["radarr"] {
		if cf["name"] == foreignSubsName {
			cfID = cf["id"]
		}
	}
	scored := false
	for _, p := range fk.ProfilesPut {
		items, _ := p["formatItems"].([]any)
		for _, it := range items {
			im := it.(map[string]any)
			if fmt.Sprint(im["format"]) == fmt.Sprint(cfID) && im["score"].(float64) == -10000 {
				scored = true
			}
		}
	}
	if cfID == nil || !scored {
		t.Fatalf("custom format should be added and scored -10000: %v %v", cfID, fk.ProfilesPut)
	}
	owner.ok("GET", "/tune", nil, &tr)
	owner.ok("POST", "/tune/undo/"+itoa(changeFor("radarr-foreign-subs").ID), map[string]any{}, nil)
	for _, cf := range fk.CustomFormats["radarr"] {
		if cf["name"] == foreignSubsName {
			t.Fatal("undo should remove the custom format")
		}
	}

	// 38. Speed test: finds the slow part and feeds the recommendations.
	cloudflareURL = fk.URL + "/speedtest"
	var sp struct{ Result store.SpeedTest }
	owner.ok("POST", "/tune/speedtest", map[string]any{}, &sp)
	if sp.Result.Bottleneck != "disk" || sp.Result.Internet < 120 || len(sp.Result.Servers) != 1 || sp.Result.Servers[0].Rate != 60 || sp.Result.Servers[0].Connections != 4 && sp.Result.Servers[0].Connections != 60 {
		t.Fatalf("speed test: %+v", sp.Result)
	}
	owner.ok("GET", "/tune", nil, &tr)
	byID = map[string]TuneFinding{}
	for _, f := range tr.Findings {
		byID[f.ID] = f
	}
	if f := byID["sab-bandwidth"]; f.Status != "change" || !strings.HasSuffix(f.Recommended, "MB/s") {
		t.Fatalf("line speed should be suggested after a test: %+v", f)
	}
	if f, ok := byID["sab-completion:news.example.com"]; !ok || f.Level != "warn" {
		t.Fatalf("a provider missing 40%% of articles should be flagged: %+v", f)
	}

	// 39. Items dated in the future get reset.
	fk.Lock()
	fk.PlexMovies[105] = time.Now().AddDate(70, 0, 0)
	fk.Unlock()
	owner.ok("GET", "/tune", nil, &tr)
	found = false
	for _, f := range tr.Findings {
		if f.ID == "plex-future" && f.Status == "change" && strings.Contains(f.Current, "The Orchard Line") {
			found = true
		}
	}
	if !found {
		t.Fatalf("future-dated item should be found: %+v", tr.Findings)
	}
	owner.ok("POST", "/tune/apply", map[string]any{"ids": []string{"plex-future"}}, &applied)
	if applied.Changed != 1 || fk.PlexMovies[105].After(time.Now().Add(time.Hour)) {
		t.Fatalf("future date should be reset: %+v %v", applied, fk.PlexMovies[105])
	}

	// 40. Owners only.
	if code, _ := friend.call("GET", "/tune", nil, nil); code != 403 {
		t.Fatalf("friends can't see Tune-up: %d", code)
	}
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
