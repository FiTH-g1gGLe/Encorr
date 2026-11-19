package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FiTH-g1gGLe/encorr/internal/fake"
	"github.com/FiTH-g1gGLe/encorr/internal/plex"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
	"github.com/FiTH-g1gGLe/encorr/internal/tmdb"
	"github.com/FiTH-g1gGLe/encorr/internal/web"
)

// TestJellyfinOnly sets Encorr up with Jellyfin and no Plex, then adds Plex.
func TestJellyfinOnly(t *testing.T) {
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
	jfURL := fakeSrv.URL + "/jellyfin"

	owner := newClient(t, srv.URL)
	if code, m := owner.call("POST", "/auth/jellyfin/setup", map[string]any{"url": jfURL, "username": "jfmate", "password": "matepass"}, nil); code != 400 || !strings.Contains(fmt.Sprint(m["error"]), "administrator") {
		t.Fatalf("only an administrator can set up: %d %v", code, m)
	}
	if code, _ := owner.call("GET", "/settings", nil, nil); code != 401 {
		t.Fatal("still nobody signed in")
	}
	if res := owner.ok("POST", "/auth/jellyfin/setup", map[string]any{"url": jfURL, "username": "jfadmin", "password": "jfpass"}, nil); res["done"] != true {
		t.Fatalf("setup: %v", res)
	}
	if code, _ := newClient(t, srv.URL).call("POST", "/auth/jellyfin/setup", map[string]any{"url": jfURL, "username": "jfadmin", "password": "jfpass"}, nil); code != 403 {
		t.Fatal("there's an owner now: nobody else can take over")
	}
	me := owner.ok("GET", "/me", nil, nil)
	if me["role"] != store.RoleAdmin || me["username"] != "jfadmin" || me["jellyfin"] != true {
		t.Fatalf("owner: %v", me)
	}
	stt := owner.ok("GET", "/status", nil, nil)
	if stt["plex"] != false || stt["jellyfin"] != true || stt["libraryName"] != "Jellyfin" || stt["serverName"] != "Jelly Box" {
		t.Fatalf("status: %v", stt)
	}
	// Friends can't sign in until setup is finished.
	if code, _ := newClient(t, srv.URL).call("POST", "/auth/jellyfin/login", map[string]any{"username": "jfmate", "password": "matepass"}, nil); code != 403 {
		t.Fatalf("before setup is finished: %d", code)
	}
	owner.ok("PUT", "/settings/tmdb", map[string]any{"key": "good-key"}, nil)
	owner.ok("POST", "/settings/complete", map[string]any{}, nil)

	// Plex jobs stay quiet without Plex; Jellyfin's do the work.
	if msg, err := a.jobPlexRecent(ctx); err != nil || !strings.Contains(msg, "Jellyfin is") {
		t.Fatalf("plex recent without Plex: %q %v", msg, err)
	}
	if msg, err := a.jobPlexFull(ctx); err != nil || !strings.Contains(msg, "Jellyfin is") {
		t.Fatalf("plex full without Plex: %q %v", msg, err)
	}
	fk.AddToJellyfin("movie", 101, nil)
	if msg, err := a.jobJellyfinFull(ctx); err != nil || !strings.HasPrefix(msg, "1 films") {
		t.Fatalf("jellyfin full: %q %v", msg, err)
	}
	var hc struct{ Checks []Check }
	owner.ok("GET", "/health", nil, &hc)
	if hc.Checks[0].Name != "Plex" || hc.Checks[0].Status != "off" || hc.Checks[1].Name != "Jellyfin" || hc.Checks[1].Status != "ok" {
		t.Fatalf("health: %+v %+v", hc.Checks[0], hc.Checks[1])
	}
	for _, c := range hc.Checks {
		if strings.Contains(c.Name, "Plex") && c.Status == "warn" {
			t.Fatalf("a Plex job looks broken without Plex: %+v", c)
		}
	}
	var title struct {
		Status   statusInfo
		Jellyfin *plexInfo
	}
	owner.ok("GET", "/title/movie/101", nil, &title)
	if title.Status.Status != store.MediaAvailable || title.Jellyfin == nil {
		t.Fatalf("title: %+v %+v", title.Status, title.Jellyfin)
	}
	if code, m := owner.call("POST", "/requests", map[string]any{"type": "movie", "tmdbId": 101}, nil); code != 400 || !strings.Contains(fmt.Sprint(m["error"]), "already in Jellyfin") {
		t.Fatalf("wording follows the server: %d %v", code, m)
	}
	// Jellyfin is the only server: it can't be disconnected by accident.
	if code, _ := owner.call("DELETE", "/settings/jellyfin", nil, nil); code != 400 {
		t.Fatalf("disconnect the only server: %d", code)
	}

	// Friends sign in with Jellyfin; their names stay unique.
	a.Store.Update(func(d *store.Data) error { // an Encorr-only account already called jfmate
		d.Users = append(d.Users, &store.User{ID: d.NewID("user"), Username: "jfmate", Role: store.RoleFriend, PasswordHash: "x"})
		return nil
	})
	f := newClient(t, srv.URL)
	f.ok("POST", "/auth/jellyfin/login", map[string]any{"username": "jfmate", "password": "matepass"}, nil)
	if fm := f.ok("GET", "/me", nil, nil); fm["username"] != "jfmate (2)" {
		t.Fatalf("unique name: %v", fm["username"])
	}
	// Quick Connect turned off in Jellyfin: the sign-in page won't offer it.
	fk.Lock()
	fk.JFQuickOn = false
	fk.Unlock()
	jfQuickCache.Lock()
	jfQuickCache.at = jfQuickCache.at.AddDate(-1, 0, 0)
	jfQuickCache.Unlock()
	if s := f.ok("GET", "/status", nil, nil); s["quickConnect"] != false {
		t.Fatalf("quick connect off: %v", s["quickConnect"])
	}
	if code, m := f.call("POST", "/auth/jellyfin/quick", map[string]any{}, nil); code != 400 || !strings.Contains(fmt.Sprint(m["error"]), "Quick Connect is turned off") {
		t.Fatalf("quick connect off: %d %v", code, m)
	}

	// Later, the owner adds Plex: it links to them rather than making a new person.
	var pin struct{ ID int }
	owner.ok("POST", "/auth/plex/pin", map[string]any{}, &pin)
	if res := owner.ok("POST", "/settings/plex/link", map[string]any{"id": pin.ID}, nil); res["done"] != true {
		t.Fatalf("link plex: %v", res)
	}
	if me := owner.ok("GET", "/me", nil, nil); me["plex"] != true || me["jellyfin"] != true || me["role"] != store.RoleAdmin {
		t.Fatalf("owner has both: %v", me)
	}
	owner.ok("POST", "/settings/plex/server", map[string]any{"machineId": "machine-1"}, nil)
	if s := owner.ok("GET", "/status", nil, nil); s["libraryName"] != "your library" || s["plex"] != true {
		t.Fatalf("both servers: %v", s)
	}
	if code, _ := f.call("POST", "/settings/plex/link", map[string]any{"id": pin.ID}, nil); code != 403 {
		t.Fatal("only the owner links Plex")
	}
	nUsers := 0
	a.Store.Read(func(d *store.Data) { nUsers = len(d.Users) })
	if nUsers != 3 {
		t.Fatalf("owner, the Encorr-only account and the Jellyfin friend: %d people", nUsers)
	}
}
