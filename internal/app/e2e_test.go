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
	"slices"
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
	if p := title.Status.Progress; p == nil || p.Percent != 90 || !p.Live || p.Stage != "Downloading" || p.Speed != "200 MB/s" || p.TimeLeft != "9s" {
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

	// 15b. People can see what's left, and the owner can set a limit per person.
	type quotaOut struct {
		Movie, TV Quota
	}
	var qo quotaOut
	friend.ok("GET", "/me/quota", nil, &qo)
	if qo.Movie.Limit != 2 || qo.Movie.Left != 0 || qo.Movie.NextFree == nil || qo.TV.Limit != 1 || qo.Movie.Custom {
		t.Fatalf("friend quota: %+v", qo)
	}
	var fme publicUser
	friend.ok("GET", "/me", nil, &fme)
	owner.ok("PATCH", "/users/"+itoa(fme.ID), map[string]any{"limits": map[string]any{"custom": true, "movie": 5, "tv": 0}}, nil)
	qo = quotaOut{}
	friend.ok("GET", "/me/quota", nil, &qo)
	if qo.Movie.Limit != 5 || qo.Movie.Left != 3 || !qo.Movie.Custom || qo.TV.Limit != 0 {
		t.Fatalf("custom quota: %+v", qo)
	}
	if code, _ := owner.call("PATCH", "/users/"+itoa(fme.ID), map[string]any{"limits": map[string]any{"custom": true, "movie": -1}}, nil); code != 400 {
		t.Fatalf("negative limit allowed: %d", code)
	}
	owner.ok("PATCH", "/users/"+itoa(fme.ID), map[string]any{"limits": map[string]any{"custom": false}}, nil)
	qo = quotaOut{}
	friend.ok("GET", "/me/quota", nil, &qo)
	if qo.Movie.Limit != 2 || qo.Movie.Custom {
		t.Fatalf("back to default: %+v", qo)
	}
	var oq quotaOut
	owner.ok("GET", "/me/quota", nil, &oq)
	if oq.Movie.Limit != 0 || oq.TV.Limit != 0 {
		t.Fatalf("owners have no limit: %+v", oq)
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
		if !a.Connected && a.App != "jellyfin" { // not set up yet: shown so people know it can be checked
			t.Fatalf("%s should be connected: %+v", a.App, a)
		}
	}
	wantChange := []string{"sab-pwrar", "sab-unwanted-action", "sab-unwanted-list", "sab-unrar-dates", "sab-cache", "sab-pipelining:news.example.com",
		"radarr-redownload", "radarr-propers", "radarr-filedate", "radarr-freespace", "radarr-hardsubs", "radarr-foreign-subs",
		"sonarr-illegal", "sonarr-foreign-subs", "plex-fsevents", "plex-lowprio", "plex-bif",
		"plex-hwaccel", "plex-hwencode", "plex-tonemap"}
	for _, id := range wantChange {
		if f, ok := byID[id]; !ok || f.Status != "change" || !f.CanApply {
			t.Fatalf("expected %s to need changing: %+v", id, f)
		}
	}
	if f, ok := byID["plex-lan"]; !ok || f.Level != "info" || f.CanApply {
		t.Fatalf("an empty LAN networks list should be advice only: %+v", f)
	}
	if _, ok := byID["plex-nopass"]; ok {
		t.Fatal("the Plex Pass note is only for owners without it")
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
	// SABnzbd makes a new server for a name it doesn't know: the change must
	// land on the real server and make nothing new.
	fk.Lock()
	nServers := len(fk.SABServers)
	for _, sv := range fk.SABServers {
		if sv["name"] == "pipelining_requests" || sv["host"] == "" {
			t.Fatalf("a stray server was made: %v", sv)
		}
	}
	fk.Unlock()
	if nServers != 2 {
		t.Fatalf("servers: %d", nServers)
	}
	// Changing a block server keeps its usage counter.
	if err := a.setSABServer(ctx, a.SAB(), "block.example.net", "connections", "15"); err != nil {
		t.Fatal(err)
	}
	fk.Lock()
	blk := fk.SABServers[1]
	fk.Unlock()
	if blk["connections"] != 15 || blk["usage_at_start"] != 1e11 {
		t.Fatalf("block server: %v", blk)
	}
	if err := a.setSABServer(ctx, a.SAB(), "no-such-server", "connections", "5"); err == nil {
		t.Fatal("never write to a server that isn't there (SABnzbd would make it)")
	}
	// Leftovers from the old bug are removed, and nothing else.
	fk.Lock()
	fk.SABServers = append(fk.SABServers, map[string]any{"name": "pipelining_requests", "host": "", "enable": 1},
		map[string]any{"name": "connections", "host": "", "enable": 1},
		map[string]any{"name": "spare", "host": ""}) // empty but not one of ours: left alone
	fk.Unlock()
	if n := a.cleanStraySABServers(ctx); n != 2 {
		t.Fatalf("cleaned %d", n)
	}
	fk.Lock()
	var names []string
	for _, sv := range fk.SABServers {
		names = append(names, fmt.Sprint(sv["name"]))
	}
	fk.SABServers = fk.SABServers[:2]
	fk.Unlock()
	if strings.Join(names, ",") != "news.example.com,block.example.net,spare" {
		t.Fatalf("servers after clean-up: %v", names)
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
	owner.ok("POST", "/tune/apply", map[string]any{"ids": []string{"plex-hwaccel", "plex-hwencode", "plex-tonemap"}}, &applied)
	if applied.Changed != 3 || fk.PlexPrefs["HardwareAcceleratedCodecs"] != true || fk.PlexPrefs["HardwareAcceleratedEncoders"] != true || fk.PlexPrefs["TranscoderToneMapping"] != true {
		t.Fatalf("Plex playback settings: %+v %v", applied, fk.PlexPrefs)
	}
	owner.ok("GET", "/tune", nil, &tr)

	// 36. Undo puts the old value back, but won't silently overwrite a later change.
	changeFor := func(rule string) store.TuneChange {
		var fresh tuneResp
		owner.ok("GET", "/tune", nil, &fresh)
		for _, c := range fresh.Changes {
			if c.Rule == rule && !c.Undone {
				return c
			}
		}
		t.Fatalf("no change recorded for %s: %+v", rule, fresh.Changes)
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

	// 40. Speed-test-driven and drive checks; the connection helper.
	owner.ok("GET", "/tune", nil, &tr)
	byID = map[string]TuneFinding{}
	for _, f := range tr.Findings {
		byID[f.ID] = f
	}
	if f := byID["sab-pause-pp"]; f.Status != "change" || f.Recommended != "On" {
		t.Fatalf("a slow download drive should suggest pausing while unpacking: %+v", f)
	}
	if f := byID["sab-propagation"]; f.Status != "change" || f.Level != "review" || f.Recommended != "5 min" {
		t.Fatalf("propagation delay: %+v", f)
	}
	if f := byID["sab-same-drive"]; f.Status != "notice" || !strings.Contains(f.Current, "D:") || !strings.Contains(f.Current, "P:") {
		t.Fatalf("completed folder on another drive should be explained: %+v", f)
	}
	fk.Queue["radarr"] = nil
	if code, m := owner.call("POST", "/tune/connections/start", map[string]any{}, nil); code != 400 || !strings.Contains(m["error"].(string), "download first") {
		t.Fatalf("the helper needs something downloading: %d %v", code, m)
	}
	connSettle, connSample, connTick = 5*time.Millisecond, 20*time.Millisecond, 5*time.Millisecond
	fk.Queue["radarr"] = []map[string]any{{"id": 701, "movieId": movieExt, "title": "Big.Film", "size": 4e10, "sizeleft": 3e10, "status": "downloading", "downloadId": "nzo_big"}}
	owner.ok("POST", "/tune/connections/start", map[string]any{}, nil)
	var ct ConnTuner
	waitFor(t, "the connection helper", func() bool {
		owner.ok("GET", "/tune/connections", nil, &ct)
		return !ct.Running
	})
	if ct.Error != "" || ct.Best != 22 || fk.SABServers[0]["connections"] != 22 || len(ct.Steps) != 9 {
		t.Fatalf("speed tops out at 20 connections, so 22 (with 2 spare): %+v server=%v", ct, fk.SABServers[0]["connections"])
	}
	owner.ok("GET", "/tune", nil, &tr)
	owner.ok("POST", "/tune/undo/"+itoa(changeFor("sab-connections-helper").ID), map[string]any{}, nil)
	if fk.SABServers[0]["connections"] != 60 {
		t.Fatalf("undo should restore the original 60: %v", fk.SABServers[0]["connections"])
	}

	// If downloads finish halfway, the helper puts the original back.
	connSettle = 150 * time.Millisecond
	owner.ok("POST", "/tune/connections/start", map[string]any{}, nil)
	if code, _ := owner.call("POST", "/tune/connections/start", map[string]any{}, nil); code != 400 {
		t.Fatal("only one run at a time")
	}
	fk.Lock()
	fk.Queue["radarr"] = nil
	fk.Unlock()
	waitFor(t, "the helper to give up", func() bool {
		owner.ok("GET", "/tune/connections", nil, &ct)
		return !ct.Running
	})
	if !strings.Contains(ct.Error, "finished or paused") || fk.SABServers[0]["connections"] != 60 {
		t.Fatalf("should restore the original when downloads stop: %+v %v", ct, fk.SABServers[0]["connections"])
	}

	// 41. Radarr/Sonarr hygiene checks.
	owner.ok("GET", "/tune", nil, &tr)
	byID = map[string]TuneFinding{}
	for _, f := range tr.Findings {
		byID[f.ID] = f
	}
	if f := byID["radarr-minscore"]; f.Status != "change" || !f.CanApply || !strings.Contains(f.Current, "HD-1080p (-10000)") {
		t.Fatalf("a -10000 minimum lets never-want formats through: %+v", f)
	}
	if f := byID["sonarr-minscore"]; f.Status != "ok" {
		t.Fatalf("sonarr's profiles are fine: %+v", f)
	}
	if f := byID["sonarr-rename"]; f.Status != "change" || f.Level != "review" {
		t.Fatalf("renaming episodes: %+v", f)
	}
	if f := byID["radarr-rename"]; f.Status != "ok" {
		t.Fatalf("radarr already renames: %+v", f)
	}
	if f := byID["sonarr-category"]; f.Level != "warn" {
		t.Fatalf("an empty category should be flagged: %+v", f)
	}
	if f := byID["radarr-manual-indexers"]; f.Current != "Hand Added" {
		t.Fatalf("indexers outside Prowlarr: %+v", f)
	}
	if f := byID["radarr-backups"]; f.Level != "warn" || f.Current != "None found" {
		t.Fatalf("no backups should be flagged: %+v", f)
	}
	if _, ok := byID["sonarr-backups"]; ok {
		t.Fatal("a backup from two days ago is fine")
	}
	if f := byID["radarr-loglevel"]; f.Current != "Trace" {
		t.Fatalf("trace logging: %+v", f)
	}
	for _, f := range tr.Findings {
		if strings.Contains(f.Current+f.Why, "hunter2") || strings.Contains(f.Current+f.Why, "radarr-key") {
			t.Fatal("host secrets must never surface")
		}
	}
	owner.ok("POST", "/tune/apply", map[string]any{"ids": []string{"radarr-minscore"}}, &applied)
	if applied.Changed != 1 || num(fk.QProfiles["radarr"][1]["minFormatScore"]) != 0 {
		t.Fatalf("minimum score fix: %+v %v", applied, fk.QProfiles["radarr"][1])
	}
	owner.ok("GET", "/tune", nil, &tr)
	owner.ok("POST", "/tune/undo/"+itoa(changeFor("radarr-minscore").ID), map[string]any{}, nil)
	if num(fk.QProfiles["radarr"][1]["minFormatScore"]) != -10000 {
		t.Fatalf("undo should restore -10000: %v", fk.QProfiles["radarr"][1])
	}

	// 42. Subscriptions: detected from Prowlarr and SABnzbd, plus your own.
	type subResp struct {
		Items  []SubView
		Yearly map[string]float64
		OneOff map[string]float64
	}
	var subR subResp
	owner.ok("GET", "/subscriptions", nil, &subR)
	subs := map[string]SubView{}
	for _, v := range subR.Items {
		subs[v.Key] = v
	}
	if v := subs["prowlarr:GeekFake"]; v.Status != "soon" || v.DaysLeft == nil || *v.DaysLeft != 10 || v.Source != "Prowlarr" {
		t.Fatalf("VIP in 10 days: %+v", v)
	}
	if v := subs["prowlarr:NinjaFake"]; v.Plan != "lifetime" || v.Status != "lifetime" {
		t.Fatalf("lifetime VIP: %+v", v)
	}
	if v := subs["sab:block.example.net"]; v.Plan != "block" || v.BlockSize != 2e12 || v.BlockUsed != 1.6e12 || v.Status != "soon" {
		t.Fatalf("block 80%% used: %+v", v)
	}
	if v := subs["sab:news.example.com"]; v.Status != "unknown" {
		t.Fatalf("a monthly plan without a date: %+v", v)
	}
	owner.ok("POST", "/subscriptions", map[string]any{"key": "sab:news.example.com", "plan": "monthly", "cost": 5.5, "currency": "gbp", "renews": time.Now().AddDate(0, 0, 40).Format("2006-01-02")}, nil)
	owner.ok("POST", "/subscriptions", map[string]any{"name": "Plex Pass", "kind": "other", "plan": "yearly", "cost": 30, "currency": "GBP", "renews": time.Now().AddDate(0, 0, 1).Format("2006-01-02")}, nil)
	if code, _ := owner.call("POST", "/subscriptions", map[string]any{"name": "Bad", "renews": "next week"}, nil); code != 400 {
		t.Fatal("bad dates are rejected")
	}
	owner.ok("GET", "/subscriptions", nil, &subR)
	subs = map[string]SubView{}
	var plexPass SubView
	for _, v := range subR.Items {
		subs[v.Key] = v
		if v.Name == "Plex Pass" {
			plexPass = v
		}
	}
	if v := subs["sab:news.example.com"]; v.Plan != "monthly" || v.Cost != 5.5 || v.Status != "ok" || v.Name != "Example News" {
		t.Fatalf("details added to a detected server: %+v", v)
	}
	if plexPass.Status != "urgent" || plexPass.Source != "You" || subR.Items[0].Name != "Plex Pass" {
		t.Fatalf("your own entry, renewing tomorrow, first: %+v (first %s)", plexPass, subR.Items[0].Name)
	}
	if subR.Yearly["GBP"] != 5.5*12+30 {
		t.Fatalf("yearly total: %v", subR.Yearly)
	}
	// Reminders go out once per milestone.
	fk.Lock()
	before = len(fk.Discord)
	fk.Unlock()
	if msg, _ := a.jobSubscriptions(ctx); msg != "3 reminder(s)" {
		t.Fatalf("Plex Pass tomorrow, GeekFake in 10 days and the block at 80%% should each remind: %s", msg)
	}
	waitFor(t, "subscription reminders on Discord", func() bool { fk.Lock(); defer fk.Unlock(); return len(fk.Discord)-before >= 3 })
	if msg, _ := a.jobSubscriptions(ctx); msg != "nothing due" {
		t.Fatalf("no repeats: %s", msg)
	}
	owner.ok("DELETE", "/subscriptions/"+itoa(plexPass.ID), nil, nil)
	if code, _ := friend.call("GET", "/subscriptions", nil, nil); code != 403 {
		t.Fatal("owners only")
	}

	// 43. Jellyfin alongside Plex (its own block: its names stay here).
	{
		if st := owner.ok("GET", "/status", nil, nil); st["jellyfin"] != false || st["libraryName"] != "Plex" {
			t.Fatalf("before Jellyfin: %v", st)
		}
		jfURL := fakeSrv.URL + "/jellyfin"
		if code, _ := friend.call("POST", "/settings/jellyfin", map[string]any{"url": jfURL, "username": "jfadmin", "password": "jfpass"}, nil); code != 403 {
			t.Fatal("only the owner connects Jellyfin")
		}
		if code, m := owner.call("POST", "/settings/jellyfin", map[string]any{"url": jfURL, "username": "jfadmin", "password": "nope"}, nil); code != 400 || !strings.Contains(fmt.Sprint(m["error"]), "Wrong Jellyfin username or password") {
			t.Fatalf("wrong password: %d %v", code, m)
		}
		if code, m := owner.call("POST", "/settings/jellyfin", map[string]any{"url": jfURL, "username": "jfmate", "password": "matepass"}, nil); code != 400 || !strings.Contains(fmt.Sprint(m["error"]), "administrator") {
			t.Fatalf("not an administrator: %d %v", code, m)
		}
		if code, m := owner.call("POST", "/settings/jellyfin", map[string]any{"url": fakeSrv.URL + "/nothing-here", "username": "jfadmin", "password": "jfpass"}, nil); code != 400 {
			t.Fatalf("not a Jellyfin server: %d %v", code, m)
		}
		var jfOut struct {
			Connected  bool
			ServerName string
			Libraries  []store.PlexLibrary
		}
		// Pasting the web app's address works too.
		owner.ok("POST", "/settings/jellyfin", map[string]any{"url": jfURL + "/web/index.html", "username": " jfadmin ", "password": "jfpass"}, &jfOut)
		if !jfOut.Connected || jfOut.ServerName != "Jelly Box" || len(jfOut.Libraries) != 2 || jfOut.Libraries[0].Type != "movie" || jfOut.Libraries[1].Type != "show" {
			t.Fatalf("connected: %+v", jfOut)
		}
		if a.settings().Jellyfin.URL != jfURL {
			t.Fatalf("address tidied: %q", a.settings().Jellyfin.URL)
		}
		// The API key is Encorr's own, kept secret; the password isn't kept at all.
		sc, raw := owner.call("GET", "/settings", nil, nil)
		if b, _ := json.Marshal(raw); sc != 200 || strings.Contains(string(b), "jf-api-key") || strings.Contains(string(b), "jfpass") {
			t.Fatalf("settings leak: %s", b)
		}
		if strings.Contains(fmt.Sprint(a.settings()), "jfpass") {
			t.Fatal("the Jellyfin password must never be saved")
		}
		st := owner.ok("GET", "/status", nil, nil)
		if st["jellyfin"] != true || st["libraryName"] != "your library" || st["quickConnect"] != true || st["plex"] != true {
			t.Fatalf("with both: %v", st)
		}
		if me := owner.ok("GET", "/me", nil, nil); me["jellyfin"] != true {
			t.Fatalf("the administrator account used is linked to the owner: %v", me)
		}

		// Library: Jellyfin's half of the index sits next to Plex's.
		fk.AddToJellyfin("movie", 105, nil) // also in Plex
		fk.AddToJellyfin("movie", 106, nil) // only in Jellyfin
		fk.AddToJellyfin("tv", 203, map[int]int{1: 3, 2: 5})
		if msg, err := a.jobJellyfinFull(ctx); err != nil || !strings.Contains(msg, "2 films, 1 shows in Jellyfin") {
			t.Fatalf("jellyfin full: %s %v", msg, err)
		}
		libOf := func(typ string, id int) (e store.LibEntry) {
			a.Store.Read(func(d *store.Data) { e = d.Library[store.LibKey(typ, id)] })
			return
		}
		if e := libOf(store.TypeMovie, 105); e.Key == "" || e.JF != "jm105" {
			t.Fatalf("in both: %+v", e)
		}
		if e := libOf(store.TypeMovie, 106); e.Key != "" || e.JF != "jm106" {
			t.Fatalf("only Jellyfin: %+v", e)
		}
		// Plex's nightly scan keeps Jellyfin's half, and the other way round.
		if _, err := a.jobPlexFull(ctx); err != nil {
			t.Fatal(err)
		}
		if e := libOf(store.TypeMovie, 106); e.JF != "jm106" {
			t.Fatalf("Plex's scan dropped a Jellyfin-only film: %+v", e)
		}
		if e := libOf(store.TypeMovie, 105); e.Key == "" || e.JF == "" {
			t.Fatalf("both halves kept: %+v", e)
		}
		// Seasons: whichever server has more episodes counts.
		a.markAvailable(srcJellyfin, store.TypeTV, 203, "js203", map[int]int{1: 3, 2: 5})
		if e := libOf(store.TypeTV, 203); e.Have()[1] != 6 || e.Have()[2] != 5 || e.Seasons[1] != 6 {
			t.Fatalf("merged seasons: %+v %v", e, e.Have())
		}
		// Films only in Jellyfin can't be requested again.
		if code, m := owner.call("POST", "/requests", map[string]any{"type": "movie", "tmdbId": 106}, nil); code != 400 || !strings.Contains(fmt.Sprint(m["error"]), "already in your library") {
			t.Fatalf("already in Jellyfin: %d %v", code, m)
		}
		// Title page: Play on Jellyfin, with quality from Jellyfin.
		var title struct {
			Status   statusInfo
			Jellyfin *plexInfo
			Plex     *plexInfo
		}
		owner.ok("GET", "/title/movie/106", nil, &title)
		if title.Status.Status != store.MediaAvailable || title.Jellyfin == nil || !strings.Contains(title.Jellyfin.URL, "/web/index.html#!/details?id=jm106&serverId=jf-server-1") ||
			title.Plex == nil || title.Plex.Quality != "4K DV" || title.Plex.Audio != "5.1" {
			t.Fatalf("title page: %+v %+v %+v", title.Status, title.Jellyfin, title.Plex)
		}

		// A request that arrives in Jellyfin first completes, with a Play link at
		// the address friends use.
		owner.ok("PUT", "/settings/jellyfin/public", map[string]any{"publicUrl": "https://jelly.example.com/"}, nil)
		var jr struct{ ID int }
		const fresh = 107 // a new film nobody has asked for yet
		fk.Lock()
		fk.Movies = append(fk.Movies, fake.Movie{ID: fresh, Title: "Harbour Lights", Year: 2026, Overview: "A lighthouse keeper's last winter.", IMDB: "tt0000107"})
		fk.Unlock()
		owner.ok("POST", "/requests", map[string]any{"type": "movie", "tmdbId": fresh}, &jr)
		fk.AddToJellyfin("movie", fresh, nil)
		var mf store.Media
		a.Store.Read(func(d *store.Data) { mf = *d.MediaByTMDB(store.TypeMovie, fresh) })
		if !a.findInLibrary(ctx, mf) {
			t.Fatal("found in Jellyfin by title")
		}
		var reqs struct{ Results []requestOut }
		owner.ok("GET", "/requests?filter=all", nil, &reqs)
		var got *requestOut
		for i := range reqs.Results {
			if reqs.Results[i].ID == jr.ID {
				got = &reqs.Results[i]
			}
		}
		if got == nil || got.Status != store.ReqCompleted || !strings.HasPrefix(got.JellyfinURL, "https://jelly.example.com/web/") || got.PlexURL != "" {
			t.Fatalf("completed from Jellyfin: %+v", got)
		}
		var events []store.Event
		a.Store.Read(func(d *store.Data) { events = d.RequestByID(jr.ID).Events })
		var logged bool
		for _, ev := range events {
			logged = logged || ev.Text == "In Jellyfin and ready to watch"
		}
		if !logged {
			t.Fatalf("timeline: %+v", events)
		}

		// After a download, Jellyfin is told about the folder straight away.
		var radarrID int
		a.Store.Read(func(d *store.Data) {
			for _, m := range d.Media {
				if m.Type == store.TypeMovie && m.ExternalID > 0 {
					radarrID = m.ExternalID
				}
			}
		})
		a.nudgeJellyfin(ctx, store.TypeMovie, radarrID)
		fk.Lock()
		scans := append([]string(nil), fk.JFScans...)
		fk.Unlock()
		if len(scans) != 1 || !strings.HasPrefix(scans[0], `P:\Media\Films\`) {
			t.Fatalf("jellyfin scan: %v", scans)
		}

		// Friends sign in with Jellyfin: password, or a Quick Connect code.
		jfFriend := newClient(t, srv.URL)
		if code, _ := jfFriend.call("POST", "/auth/jellyfin/login", map[string]any{"username": "jfmate", "password": "wrong"}, nil); code != 401 {
			t.Fatalf("wrong Jellyfin password: %d", code)
		}
		if code, _ := jfFriend.call("POST", "/auth/jellyfin/login", map[string]any{"username": "jfgone", "password": "gonepass"}, nil); code != 401 {
			t.Fatalf("turned-off account: %d", code)
		}
		if res := jfFriend.ok("POST", "/auth/jellyfin/login", map[string]any{"username": "jfmate", "password": "matepass"}, nil); res["role"] != store.RoleFriend {
			t.Fatalf("jellyfin friend: %v", res)
		}
		me := jfFriend.ok("GET", "/me", nil, nil)
		if me["username"] != "jfmate" || me["jellyfin"] != true || me["plex"] != false {
			t.Fatalf("jellyfin friend: %v", me)
		}
		if code, _ := jfFriend.call("GET", "/settings", nil, nil); code != 403 {
			t.Fatal("friends can't open settings")
		}
		qc := newClient(t, srv.URL)
		start := qc.ok("POST", "/auth/jellyfin/quick", map[string]any{}, nil)
		secret, codeStr := fmt.Sprint(start["secret"]), fmt.Sprint(start["code"])
		if len(codeStr) != 6 {
			t.Fatalf("quick connect code: %v", start)
		}
		if res := qc.ok("POST", "/auth/jellyfin/quick/check", map[string]any{"secret": secret}, nil); res["done"] != false {
			t.Fatal("not approved yet")
		}
		if code, _ := qc.call("POST", "/auth/jellyfin/quick/check", map[string]any{"secret": "qs-made-up"}, nil); code != 410 {
			t.Fatalf("made-up secrets are refused: %d", code)
		}
		fk.ApproveQuickConnect(codeStr, "jfmate")
		if res := qc.ok("POST", "/auth/jellyfin/quick/check", map[string]any{"secret": secret}, nil); res["done"] != true {
			t.Fatalf("quick connect: %v", res)
		}
		if me2 := qc.ok("GET", "/me", nil, nil); me2["id"] != me["id"] {
			t.Fatalf("Quick Connect and password reach the same person: %v vs %v", me2["id"], me["id"])
		}
		if code, _ := qc.call("POST", "/auth/jellyfin/quick/check", map[string]any{"secret": secret}, nil); code != 410 {
			t.Fatalf("a code works once: %d", code)
		}
		// The owner can sign in with the linked Jellyfin account.
		jfOwner := newClient(t, srv.URL)
		if res := jfOwner.ok("POST", "/auth/jellyfin/login", map[string]any{"username": "jfadmin", "password": "jfpass"}, nil); res["role"] != store.RoleAdmin {
			t.Fatalf("owner by Jellyfin: %v", res)
		}

		// Health and jobs know about Jellyfin.
		var hc struct{ Checks []Check }
		owner.ok("GET", "/health", nil, &hc)
		checks := hc.Checks
		if len(checks) < 2 || checks[1].Name != "Jellyfin" || checks[1].Status != "ok" || !strings.Contains(checks[1].Message, "Jelly Box") {
			t.Fatalf("health: %+v", checks[:2])
		}
		if msg, err := a.jobJellyfinRecent(ctx); err != nil || !strings.Contains(msg, "new or updated") {
			t.Fatalf("jellyfin recent: %s %v", msg, err)
		}

		// Tune-up: per-library settings, applied and undone exactly.
		var tune struct {
			Findings []TuneFinding
			Apps     []TuneApp
			Changes  []store.TuneChange
		}
		owner.ok("GET", "/tune", nil, &tune)
		jfFind := map[string]TuneFinding{}
		for _, f := range tune.Findings {
			if f.App == "jellyfin" {
				jfFind[f.ID] = f
			}
		}
		if f := jfFind["jf-realtime"]; f.Status != "change" || !f.CanApply || f.Current != "Off in Films" {
			t.Fatalf("realtime off in one library: %+v", f)
		}
		if f := jfFind["jf-trickplay"]; f.Status != "change" {
			t.Fatalf("trickplay during scans: %+v", f)
		}
		if f := jfFind["jf-chapters"]; f.Status != "ok" {
			t.Fatalf("chapters already right: %+v", f)
		}
		if _, ok := jfFind["jf-quickconnect"]; ok {
			t.Fatal("Quick Connect is on: nothing to say")
		}
		owner.ok("POST", "/tune/apply", map[string]any{"ids": []string{"jf-realtime"}}, nil)
		fk.Lock()
		films, tv := fk.JFLibOpts["lib-films"], fk.JFLibOpts["lib-tv"]
		fk.Unlock()
		if films["EnableRealtimeMonitor"] != true || tv["EnableRealtimeMonitor"] != true || films["PathInfos"] == nil || films["ExtractTrickplayImagesDuringLibraryScan"] != true {
			t.Fatalf("applied to Films only, rest of its options kept: %v / %v", films, tv)
		}
		var hist struct{ Changes []store.TuneChange } // a new value: reused ones keep stale fields
		owner.ok("GET", "/tune", nil, &hist)
		var chID int
		for _, c := range hist.Changes {
			if c.Rule == "jf-realtime" && !c.Undone {
				chID = c.ID
			}
		}
		if chID == 0 {
			t.Fatalf("no change recorded: %+v", hist.Changes)
		}
		owner.ok("POST", "/tune/undo/"+itoa(chID), map[string]any{}, nil)
		fk.Lock()
		films, tv = fk.JFLibOpts["lib-films"], fk.JFLibOpts["lib-tv"]
		fk.Unlock()
		if films["EnableRealtimeMonitor"] != false || tv["EnableRealtimeMonitor"] != true {
			t.Fatalf("undo puts each library back as it was: %v / %v", films["EnableRealtimeMonitor"], tv["EnableRealtimeMonitor"])
		}
		fk.Lock()
		fk.JFConfig["QuickConnectAvailable"] = false
		fk.Unlock()
		var tune2 struct{ Findings []TuneFinding }
		owner.ok("GET", "/tune", nil, &tune2)
		var qcTip *TuneFinding
		for i := range tune2.Findings {
			if tune2.Findings[i].ID == "jf-quickconnect" {
				qcTip = &tune2.Findings[i]
			}
		}
		if qcTip == nil || qcTip.CanApply || !strings.Contains(qcTip.Fix, "Enable Quick Connect") {
			t.Fatalf("Quick Connect tip, never changed by Encorr: %+v", qcTip)
		}

		// Disconnecting forgets Jellyfin's half (Plex is still there).
		owner.ok("DELETE", "/settings/jellyfin", nil, nil)
		if e := libOf(store.TypeMovie, 106); !e.Empty() {
			t.Fatalf("Jellyfin-only film forgotten: %+v", e)
		}
		if e := libOf(store.TypeMovie, 105); e.Key == "" || e.JF != "" {
			t.Fatalf("Plex half kept: %+v", e)
		}
		if code, _ := newClient(t, srv.URL).call("POST", "/auth/jellyfin/login", map[string]any{"username": "jfmate", "password": "matepass"}, nil); code != 403 {
			t.Fatalf("no Jellyfin sign-in once disconnected: %d", code)
		}
		if st := owner.ok("GET", "/status", nil, nil); st["libraryName"] != "Plex" || st["jellyfin"] != false {
			t.Fatalf("back to Plex only: %v", st)
		}
	}

	// 44. A change an app turns down is remembered and shown, with the reason.
	{
		fk.Lock()
		fk.ArrRefuse = map[string]string{"sonarr.mediamanagement": "Path does not exist"}
		fk.ArrConfig["sonarr"]["mediamanagement"]["minimumFreeSpaceWhenImporting"] = 100
		fk.Unlock()
		var res struct {
			Results []struct {
				ID    string
				OK    bool
				Error string
			}
		}
		owner.ok("POST", "/tune/apply", map[string]any{"ids": []string{"sonarr-freespace"}}, &res)
		if len(res.Results) != 1 || res.Results[0].OK || !strings.Contains(res.Results[0].Error, "Path does not exist") {
			t.Fatalf("refused change: %+v", res)
		}
		var tr2 struct{ Findings []TuneFinding }
		owner.ok("GET", "/tune", nil, &tr2)
		var got *TuneFinding
		for i := range tr2.Findings {
			if tr2.Findings[i].ID == "sonarr-freespace" {
				got = &tr2.Findings[i]
			}
		}
		if got == nil || got.Status != "change" || !strings.Contains(got.Failed, "Path does not exist") {
			t.Fatalf("failure shown on the card: %+v", got)
		}
		var acts struct{ Items []store.Activity }
		owner.ok("GET", "/activity", nil, &acts)
		logged := false
		for _, x := range acts.Items {
			logged = logged || strings.Contains(x.Title, "couldn't change") && strings.Contains(x.Detail, "Path does not exist")
		}
		if !logged {
			t.Fatalf("activity: %+v", acts.Items[:min(3, len(acts.Items))])
		}
		fk.Lock()
		fk.ArrRefuse = nil
		fk.Unlock()
		owner.ok("POST", "/tune/apply", map[string]any{"ids": []string{"sonarr-freespace"}}, &res)
		var tr3 struct{ Findings []TuneFinding }
		owner.ok("GET", "/tune", nil, &tr3)
		for _, f := range tr3.Findings {
			if f.ID == "sonarr-freespace" && (f.Status == "change" || f.Failed != "") {
				t.Fatalf("after it works, the card clears: %+v", f)
			}
		}
	}

	// 46. "Something's wrong" reports, and getting a new copy.
	{
		var tm, arrID int
		for id, m := range fk.RadarrMovies {
			tm, arrID = int(m["tmdbId"].(float64)), id
			break
		}
		a.markAvailable(srcPlex, store.TypeMovie, tm, "pk-issue", nil)
		fk.History["radarr"] = []map[string]any{{"id": 77, "eventType": "grabbed", "date": time.Now().UTC().Format(time.RFC3339), "sourceTitle": "Bad.Copy"}}
		var is store.Issue
		friend.ok("POST", "/issues", map[string]any{"type": "movie", "tmdbId": tm, "kind": "play", "note": "stops after 10 minutes"}, &is)
		if is.ID == 0 || is.Status != store.IssueOpen {
			t.Fatalf("report: %+v", is)
		}
		if code, _ := friend.call("POST", "/issues", map[string]any{"type": "movie", "tmdbId": tm, "kind": "audio"}, nil); code != 400 {
			t.Fatalf("a second report of the same thing should be refused: %d", code)
		}
		if code, _ := friend.call("POST", "/issues", map[string]any{"type": "movie", "tmdbId": 999, "kind": "play"}, nil); code != 400 {
			t.Fatalf("only things in the library can be reported: %d", code)
		}
		if code, _ := friend.call("POST", "/issues", map[string]any{"type": "movie", "tmdbId": tm, "kind": "nonsense"}, nil); code != 400 {
			t.Fatalf("unknown kind accepted: %d", code)
		}
		var il struct {
			Results []issueOut
			Open    int
		}
		friend.ok("GET", "/issues", nil, &il)
		if len(il.Results) != 1 || !il.Results[0].Mine || il.Results[0].CanReplace || il.Results[0].KindName != "Won't play" {
			t.Fatalf("friend's issues: %+v", il)
		}
		il.Results = nil
		owner.ok("GET", "/issues", nil, &il)
		if il.Open < 1 || !il.Results[0].CanReplace || il.Results[0].Reporter == "" {
			t.Fatalf("owner's issues: %+v", il)
		}
		if code, _ := friend.call("POST", "/issues/"+itoa(is.ID)+"/replace", map[string]any{}, nil); code != 403 {
			t.Fatalf("friends can't replace files: %d", code)
		}
		cmds := len(fk.Commands)
		owner.ok("POST", "/issues/"+itoa(is.ID)+"/replace", map[string]any{}, nil)
		if !slices.Contains(fk.Failed, "radarr:77") || !slices.Contains(fk.DeletedFiles, "radarr:moviefile/"+itoa(500+arrID)) || len(fk.Commands) <= cmds || fk.Commands[len(fk.Commands)-1]["name"] != "MoviesSearch" {
			t.Fatalf("replace: failed=%v deleted=%v cmds=%v", fk.Failed, fk.DeletedFiles, fk.Commands[cmds:])
		}
		if code, _ := owner.call("POST", "/issues/"+itoa(is.ID)+"/replace", map[string]any{}, nil); code != 400 {
			t.Fatalf("replacing twice should be refused: %d", code)
		}
		a.issuesArrived("radarr", arrID)
		il.Results = nil
		friend.ok("GET", "/issues", nil, &il)
		if il.Results[0].Status != store.IssueResolved || il.Open != 0 {
			t.Fatalf("the new copy arriving should close it: %+v", il.Results[0])
		}
		var is2 store.Issue
		friend.ok("POST", "/issues", map[string]any{"type": "movie", "tmdbId": tm, "kind": "subtitles"}, &is2)
		friend.ok("POST", "/issues/"+itoa(is2.ID)+"/resolve", map[string]any{"reply": "found them in the menu"}, nil)
		if code, _ := friend.call("POST", "/issues/"+itoa(is2.ID)+"/resolve", map[string]any{}, nil); code != 400 {
			t.Fatalf("resolving twice: %d", code)
		}
	}

	// 45. Owners only.
	var ui UpdateInfo
	owner.ok("GET", "/update", nil, &ui)
	if ui.Current == "" || ui.How == "" || ui.Available {
		t.Fatalf("update info for a development build: %+v", ui)
	}
	if code, _ := owner.call("POST", "/update/install", map[string]any{}, nil); code != 400 {
		t.Fatalf("nothing to install should be refused: %d", code)
	}
	if code, _ := friend.call("GET", "/update", nil, nil); code != 403 {
		t.Fatalf("friends can't see updates: %d", code)
	}
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
