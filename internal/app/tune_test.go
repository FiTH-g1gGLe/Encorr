package app

import (
	"testing"

	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

func TestForeignSubsOnlyForEnglishRegions(t *testing.T) {
	e := newEnv()
	e.region = "NL"
	if f := checkForeignSubs(e, "radarr"); f != nil {
		t.Fatalf("Dutch users usually want Dutch subtitles: %+v", f)
	}
	e.region = "GB"
	if f := checkForeignSubs(e, "radarr"); len(f) != 1 || f[0].Status != "change" {
		t.Fatalf("UK users get the blocker: %+v", f)
	}
	e.cfRegex["radarr"] = []string{`\bNL[ ._-]?Subs?\b`}
	if f := checkForeignSubs(e, "radarr"); f[0].Status != "ok" {
		t.Fatal("an existing block should count")
	}
}

func TestDiagnose(t *testing.T) {
	cases := []struct {
		t    store.SpeedTest
		want string
	}{
		{store.SpeedTest{Internet: 8, CPU: 300000, DownloadDir: 400}, "internet"},
		{store.SpeedTest{Internet: 200, CPU: 60000, DownloadDir: 400}, "cpu"},
		{store.SpeedTest{Internet: 200, CPU: 300000, DownloadDir: 50}, "disk"},
		{store.SpeedTest{Internet: 200, CPU: 300000, DownloadDir: 400, Servers: []store.ServerResult{{Tried: 100000, Rate: 40}}}, "provider"},
		{store.SpeedTest{Internet: 200, CPU: 300000, DownloadDir: 400, Servers: []store.ServerResult{{Tried: 100, Rate: 10}}}, "settings"},
	}
	for _, c := range cases {
		if got, adv := diagnose(&c.t); got != c.want || len(adv) == 0 {
			t.Fatalf("%+v: got %s want %s", c.t, got, c.want)
		}
	}
}

func TestCanonAndSecrets(t *testing.T) {
	if !sameVal(true, 1.0) || !sameVal("true", "1") || sameVal(1, 2) || !sameVal([]any{"a", "b"}, []string{"a", "b"}) {
		t.Fatal("value comparison")
	}
	if sizeBytes("1G") != 1e9 || sizeBytes("450M") != 450e6 || sizeBytes("") != 0 {
		t.Fatal("sizes")
	}
	for _, k := range []string{"api_key", "host", "password", "secureConnections", "allowedNetworks", "https_port", "port", "url_base", "username"} {
		if !secretKey.MatchString(k) {
			t.Fatalf("%s must be protected", k)
		}
	}
	if secretKey.MatchString("minimumFreeSpaceWhenImporting") || secretKey.MatchString("pause_on_pwrar") {
		t.Fatal("ordinary settings mustn't look like secrets")
	}
	for _, r := range tuneRules() {
		k := r.key
		for _, p := range []string{"misc.", "pref.", "mediamanagement.", "downloadclient.", "indexer.", "naming."} {
			if len(k) > len(p) && k[:len(p)] == p {
				k = k[len(p):]
			}
		}
		if k != "" && secretKey.MatchString(k) {
			t.Fatalf("rule %s touches a protected setting %s", r.id, r.key)
		}
	}
}

func TestConnHelperMaths(t *testing.T) {
	if c := connCandidates(45); c[0] != 8 || c[len(c)-1] != 40 {
		t.Fatalf("up to the current count: %v", c)
	}
	if c := connCandidates(10); c[len(c)-1] != 30 {
		t.Fatalf("cautious setups still try up to 30: %v", c)
	}
	steps := []ConnStep{{8, 80}, {12, 118}, {16, 158}, {20, 199}, {25, 201}, {30, 200}}
	if b := pickBest(steps); b != 22 {
		t.Fatalf("smallest within 5%% of the top, plus 2: %d", b)
	}
	if b := pickBest([]ConnStep{{8, 50}, {12, 50}}); b != 10 {
		t.Fatalf("already saturated at 8: %d", b)
	}
	if driveOf(`D:\complete`) != "D:" || driveOf(`\\nas\media\tv`) != `\\nas\media` || driveOf("/mnt/media") != "" {
		t.Fatalf("drives: %q %q", driveOf(`D:\complete`), driveOf(`\\nas\media\tv`))
	}
}
