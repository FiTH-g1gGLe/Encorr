package netfind

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func portOf(t *testing.T, u string) int {
	pu, _ := url.Parse(u)
	p, _ := strconv.Atoi(pu.Port())
	return p
}

func TestFind(t *testing.T) {
	t.Setenv("ENCORR_DOCKER", "0")
	radarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><title>Radarr</title></html>"))
	}))
	defer radarr.Close()
	// Newer versions: a login page that doesn't say its name, but /ping works.
	locked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.Write([]byte(`{"status": "OK"}`))
			return
		}
		w.Write([]byte("<html>Please sign in</html>"))
	}))
	defer locked.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("hello")) }))
	defer other.Close()
	sab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sabnzbd/api" && r.URL.Query().Get("mode") == "version" {
			w.Write([]byte(`{"version": "4.3.2"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer sab.Close()
	based := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/radarr") {
			w.Write([]byte("<title>Radarr</title>"))
			return
		}
		http.NotFound(w, r)
	}))
	defer based.Close()
	ctx := context.Background()

	if got := Find(ctx, Radarr, portOf(t, radarr.URL), ""); got != "http://localhost:"+strconv.Itoa(portOf(t, radarr.URL)) {
		t.Fatalf("radarr: %q", got)
	}
	if got := Find(ctx, Sonarr, portOf(t, locked.URL), ""); got == "" {
		t.Fatal("found by /ping behind a login page")
	}
	if got := Find(ctx, Radarr, portOf(t, other.URL), ""); got != "" {
		t.Fatalf("something else on the port isn't Radarr: %q", got)
	}
	if got := Find(ctx, SABnzbd, portOf(t, sab.URL), ""); !strings.HasSuffix(got, "/sabnzbd") {
		t.Fatalf("sabnzbd: %q", got)
	}
	if got := FindAt(ctx, Radarr, portOf(t, based.URL), "radarr", ""); !strings.HasSuffix(got, "/radarr") {
		t.Fatalf("url base: %q", got)
	}
	if got := Find(ctx, Radarr, 1, ""); got != "" {
		t.Fatalf("nothing there: %q", got)
	}
}

func TestHostsAndHelpers(t *testing.T) {
	t.Setenv("ENCORR_DOCKER", "1")
	hs := Hosts(Radarr, "192.168.1.50")
	if hs[0] != "radarr" || hs[1] != "192.168.1.50" {
		t.Fatalf("docker hosts: %v", hs)
	}
	for _, h := range Hosts(Radarr, "localhost") {
		if h == "localhost" {
			t.Fatal("inside Docker, localhost is Encorr itself")
		}
	}
	t.Setenv("ENCORR_DOCKER", "0")
	if !InDocker() && Hosts(Jellyfin, "")[0] != "localhost" {
		t.Fatal("outside Docker, localhost first")
	}
	route := "Iface\tDestination\tGateway \tFlags\neth0\t00000000\t010011AC\t0003\neth0\t000011AC\t00000000\t0001\n"
	if g := parseRoute(strings.NewReader(route)); g != "172.17.0.1" {
		t.Fatalf("gateway: %q", g)
	}
	if HostOnly("192.168.1.50:5150") != "192.168.1.50" || HostOnly("[::1]:5150") != "::1" || HostOnly("encorr.local") != "encorr.local" {
		t.Fatal("host only")
	}
	if SwapHost("http://localhost:7878/radarr", "http://radarr:7878") != "http://radarr:7878/radarr" {
		t.Fatal(SwapHost("http://localhost:7878/radarr", "http://radarr:7878"))
	}
}
