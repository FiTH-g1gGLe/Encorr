package jellyfin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FiTH-g1gGLe/encorr/internal/fake"
)

func TestClient(t *testing.T) {
	fk := fake.New()
	srv := httptest.NewServer(fk)
	defer srv.Close()
	ctx := context.Background()
	c := &Client{URL: srv.URL + "/jellyfin/", DeviceID: "test-install"}

	info, err := c.PublicInfo(ctx)
	if err != nil || info.ID != "jf-server-1" || info.ServerName != "Jelly Box" {
		t.Fatalf("public info: %+v %v", info, err)
	}
	if _, err := c.SignIn(ctx, "jfadmin", "nope"); err == nil || !strings.Contains(err.Error(), "wrong") {
		t.Fatalf("bad password: %v", err)
	}
	if _, err := c.SignIn(ctx, "jfgone", "gonepass"); err == nil {
		t.Fatal("disabled accounts can't sign in")
	}
	auth, err := c.SignIn(ctx, "JFADMIN", "jfpass")
	if err != nil || !auth.User.Policy.IsAdministrator || auth.AccessToken == "" {
		t.Fatalf("admin sign-in: %+v %v", auth, err)
	}

	// API keys: handed back directly (10.11+) or read back by name (older).
	admin := &Client{URL: c.URL, DeviceID: c.DeviceID, Token: auth.AccessToken}
	key, err := admin.CreateAPIKey(ctx, "Encorr")
	if err != nil || key != "jf-api-key-1" {
		t.Fatalf("new key: %q %v", key, err)
	}
	fk.Lock()
	fk.JFOldKeys = true
	fk.Unlock()
	key2, err := admin.CreateAPIKey(ctx, "Encorr")
	if err != nil || key2 != "jf-api-key-2" {
		t.Fatalf("old-style key: %q %v", key2, err)
	}
	mate, _ := c.SignIn(ctx, "jfmate", "matepass")
	if _, err := (&Client{URL: c.URL, Token: mate.AccessToken}).CreateAPIKey(ctx, "Encorr"); err == nil {
		t.Fatal("only administrators can make keys")
	}

	k := &Client{URL: c.URL, DeviceID: c.DeviceID, Token: key}
	if _, err := k.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Client{URL: c.URL, Token: "wrong"}).Ping(ctx); err != ErrUnauthorized {
		t.Fatalf("wrong key: %v", err)
	}
	libs, err := k.Libraries(ctx)
	if err != nil || len(libs) != 3 || libs[0].Kind() != "movie" || libs[1].Kind() != "show" || libs[2].Kind() != "" {
		t.Fatalf("libraries: %+v %v", libs, err)
	}

	fk.AddToJellyfin("movie", 101, nil)
	fk.AddToJellyfin("tv", 201, map[int]int{1: 10, 2: 4})
	movies, err := k.AllItems(ctx, "lib-films", "Movie")
	if err != nil || len(movies) != 1 {
		t.Fatalf("movies: %+v %v", movies, err)
	}
	if id, imdb, _ := movies[0].IDs(); id != 101 || imdb != "tt0000101" {
		t.Fatalf("film ids: %v %q", id, imdb)
	}
	shows, _ := k.AllItems(ctx, "lib-tv", "Series")
	if len(shows) != 1 {
		t.Fatalf("shows: %+v", shows)
	}
	counts, err := k.SeasonCounts(ctx, shows[0].ID)
	if err != nil || counts[1] != 10 || counts[2] != 4 || len(counts) != 2 {
		t.Fatalf("season counts (double episodes and copies once, missing ones not at all): %v %v", counts, err)
	}
	// Older servers that ignore IsMissing still don't count missing episodes.
	fk.Lock()
	fk.JFIgnoreMissing = true
	fk.Unlock()
	if counts, _ := k.SeasonCounts(ctx, shows[0].ID); counts[1] != 10 {
		t.Fatalf("virtual episodes counted: %v", counts)
	}
	eps, err := k.Newest(ctx, "lib-tv", "Episode", 5)
	if err != nil || len(eps) != 5 || eps[0].SeriesID == "" {
		t.Fatalf("newest episodes: %+v %v", eps, err)
	}
	found, err := k.Search(ctx, "Movie", movies[0].Name)
	if err != nil || len(found) != 1 {
		t.Fatalf("search: %+v %v", found, err)
	}
	it, err := k.Item(ctx, movies[0].ID)
	if err != nil || len(it.MediaStreams) == 0 {
		t.Fatalf("item: %+v %v", it, err)
	}
	if err := k.ScanPath(ctx, `P:\Media\Films\New (2026)`); err != nil {
		t.Fatal(err)
	}
	fk.Lock()
	scans := fk.JFScans
	fk.Unlock()
	if len(scans) != 1 || scans[0] != `P:\Media\Films\New (2026)` {
		t.Fatalf("scan: %v", scans)
	}

	// Quick Connect: code -> approved in a Jellyfin app -> signed in, once.
	if on, err := c.QuickConnectEnabled(ctx); err != nil || !on {
		t.Fatalf("quick connect enabled: %v %v", on, err)
	}
	qc, err := c.QuickConnectStart(ctx)
	if err != nil || len(qc.Code) != 6 {
		t.Fatalf("quick connect start: %+v %v", qc, err)
	}
	if st, _ := c.QuickConnectState(ctx, qc.Secret); st.Authenticated {
		t.Fatal("not approved yet")
	}
	if _, err := c.QuickConnectFinish(ctx, qc.Secret); err == nil {
		t.Fatal("can't finish before approval")
	}
	fk.ApproveQuickConnect(qc.Code, "jfmate")
	if st, _ := c.QuickConnectState(ctx, qc.Secret); !st.Authenticated {
		t.Fatal("approved")
	}
	res, err := c.QuickConnectFinish(ctx, qc.Secret)
	if err != nil || res.User.Name != "jfmate" {
		t.Fatalf("quick connect finish: %+v %v", res, err)
	}
	if _, err := c.QuickConnectFinish(ctx, qc.Secret); err == nil {
		t.Fatal("a secret works once")
	}

	if u := WebURL("http://jf.example/", "abc", "srv"); u != "http://jf.example/web/index.html#!/details?id=abc&serverId=srv" {
		t.Fatal(u)
	}
}

func TestProviderIDsAndEmby(t *testing.T) {
	it := Item{ProviderIDs: map[string]string{"TheMovieDb": "42", "IMDB": "tt1", "tvdb": " 7 "}}
	if tm, im, tv := it.IDs(); tm != 42 || im != "tt1" || tv != 7 {
		t.Fatalf("provider ids in any case: %v %v %v", tm, im, tv)
	}
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ServerName":"E","Id":"x","ProductName":"Emby Server"}`))
	}))
	defer emby.Close()
	if _, err := (&Client{URL: emby.URL}).PublicInfo(context.Background()); err == nil || !strings.Contains(err.Error(), "Emby") {
		t.Fatalf("emby: %v", err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer other.Close()
	if _, err := (&Client{URL: other.URL}).PublicInfo(context.Background()); err == nil {
		t.Fatal("not a Jellyfin server")
	}
}
