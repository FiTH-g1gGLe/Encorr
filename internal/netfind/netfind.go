// Package netfind finds the other apps (Radarr, Sonarr, Prowlarr, SABnzbd,
// Jellyfin, Plex) on the network when Encorr can't read their settings files,
// which is always the case inside Docker.
//
// Inside a container "localhost" is the container itself, so it tries, in
// order: the app's usual container name (radarr, sonarr...), the address
// the person used to open Encorr, and the Docker host. An address only
// counts when the app there answers as itself.
package netfind

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// InDocker reports whether Encorr runs in a container.
func InDocker() bool {
	switch os.Getenv("ENCORR_DOCKER") {
	case "1":
		return true
	case "0":
		return false
	}
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

// Gateway is the container's default gateway: the Docker host on a normal
// bridge network. "" when it can't be read (not Linux, no route).
func Gateway() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	return parseRoute(f)
}

func parseRoute(r io.Reader) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 3 || fs[1] != "00000000" {
			continue
		}
		b, err := hex.DecodeString(fs[2])
		if err != nil || len(b) != 4 {
			continue
		}
		return net.IPv4(b[3], b[2], b[1], b[0]).String() // little-endian
	}
	return ""
}

// App describes how to recognise one app.
type App struct {
	Name     string   // "Radarr"
	Port     int      // usual port
	Names    []string // usual container names
	Path     string   // where to knock
	Sign     string   // text its answer contains (case-insensitive)
	Prefixes []string // URL bases to try, e.g. "/sabnzbd"
	// Alt is a second knock for apps that show a login page first: the
	// Servarr apps answer /ping without signing in.
	AltPath, AltSign string
}

// Known apps.
var (
	Radarr   = App{Name: "Radarr", Port: 7878, Names: []string{"radarr"}, Path: "/", Sign: "radarr", AltPath: "/ping", AltSign: `"ok"`}
	Sonarr   = App{Name: "Sonarr", Port: 8989, Names: []string{"sonarr"}, Path: "/", Sign: "sonarr", AltPath: "/ping", AltSign: `"ok"`}
	Prowlarr = App{Name: "Prowlarr", Port: 9696, Names: []string{"prowlarr"}, Path: "/", Sign: "prowlarr", AltPath: "/ping", AltSign: `"ok"`}
	SABnzbd  = App{Name: "SABnzbd", Port: 8080, Names: []string{"sabnzbd", "sab"}, Path: "/api?mode=version&output=json", Sign: "version", Prefixes: []string{"/sabnzbd", ""}}
	Jellyfin = App{Name: "Jellyfin", Port: 8096, Names: []string{"jellyfin"}, Path: "/System/Info/Public", Sign: "jellyfin"}
)

// ByName looks an app up by its name, any case.
func ByName(name string) (App, bool) {
	for _, a := range []App{Radarr, Sonarr, Prowlarr, SABnzbd, Jellyfin} {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return App{}, false
}

var client = &http.Client{Timeout: 2 * time.Second}

// Hosts lists where to look, best first. hint is the host part of the
// address the person used to open Encorr (from the browser), when known.
func Hosts(app App, hint string) []string {
	var hs []string
	seen := map[string]bool{}
	add := func(h string) {
		h = strings.TrimSpace(h)
		if h != "" && !seen[h] {
			seen[h] = true
			hs = append(hs, h)
		}
	}
	if InDocker() {
		for _, n := range app.Names {
			add(n)
		}
		if !isLoopback(hint) {
			add(hint)
		}
		add(Gateway())
		add("host.docker.internal")
	} else {
		add("localhost")
		if !isLoopback(hint) {
			add(hint)
		}
	}
	return hs
}

func isLoopback(h string) bool {
	h = strings.Trim(strings.ToLower(h), "[]")
	return h == "" || h == "localhost" || strings.HasPrefix(h, "127.") || h == "::1"
}

// HostOnly strips the port from a Host header ("192.168.1.50:5150").
func HostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.Trim(hostport, "[]")
}

// Find returns the first address where the app answers as itself, such as
// "http://radarr:7878" or "http://192.168.1.50:8080/sabnzbd". port 0 means
// the usual one. "" when it isn't found.
func Find(ctx context.Context, app App, port int, hint string) string {
	return FindAt(ctx, app, port, "", hint)
}

// FindAt is Find for an app with a known URL base (such as "/radarr").
func FindAt(ctx context.Context, app App, port int, base, hint string) string {
	if base = strings.TrimRight(base, "/"); base != "" {
		if !strings.HasPrefix(base, "/") {
			base = "/" + base
		}
		app.Prefixes = []string{base}
	}
	if port == 0 {
		port = app.Port
	}
	prefixes := app.Prefixes
	if len(prefixes) == 0 {
		prefixes = []string{""}
	}
	type cand struct {
		i   int
		url string
	}
	var cands []cand
	for _, h := range Hosts(app, hint) {
		for _, p := range prefixes {
			cands = append(cands, cand{len(cands), fmt.Sprintf("http://%s%s", net.JoinHostPort(h, strconv.Itoa(port)), p)})
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ok := make([]bool, len(cands))
	var wg sync.WaitGroup
	for _, c := range cands {
		wg.Add(1)
		go func(c cand) {
			defer wg.Done()
			ok[c.i] = knock(ctx, c.url+app.Path, app.Sign) || (app.AltPath != "" && knock(ctx, c.url+app.AltPath, app.AltSign))
		}(c)
	}
	wg.Wait()
	for _, c := range cands { // best first, not fastest first
		if ok[c.i] {
			return c.url
		}
	}
	return ""
}

func knock(ctx context.Context, u, sign string) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return false
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return strings.Contains(strings.ToLower(string(b)), sign)
}

// SwapHost replaces the host and port of a URL found in a settings file
// ("http://localhost:7878/radarr") with one Encorr can reach
// ("http://radarr:7878"), keeping any URL base.
func SwapHost(found, reachable string) string {
	i := strings.Index(found, "://")
	if i < 0 {
		return reachable
	}
	rest := found[i+3:]
	path := ""
	if j := strings.Index(rest, "/"); j >= 0 {
		path = rest[j:]
	}
	if path != "" && strings.HasSuffix(reachable, path) {
		return reachable
	}
	return strings.TrimRight(reachable, "/") + path
}
