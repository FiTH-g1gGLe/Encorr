package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Update(func(d *Data) error {
		d.Settings.Radarr.APIKey = "super-secret"
		d.Users = append(d.Users, &User{ID: 1, Username: "a", PlexToken: "plex-token"})
		return nil
	})
	raw, _ := os.ReadFile(filepath.Join(dir, "encorr.json"))
	if strings.Contains(string(raw), "super-secret") || strings.Contains(string(raw), "plex-token") {
		t.Fatal("secret written in plain text")
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s2.Read(func(d *Data) {
		if d.Settings.Radarr.APIKey != "super-secret" || d.Users[0].PlexToken != "plex-token" {
			t.Fatalf("round trip failed: %q %q", d.Settings.Radarr.APIKey, d.Users[0].PlexToken)
		}
		if d.Settings.Region != "GB" || d.Settings.Quotas.Days != 7 {
			t.Fatal("UK defaults missing")
		}
	})
	if _, err := s2.Backup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "encorr.json.bak")); err != nil {
		t.Fatal("previous copy not kept")
	}
}

func TestWrongKeyIsReported(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	_ = s.Update(func(d *Data) error { d.Settings.TMDBKey = "k"; return nil })
	_ = os.Remove(filepath.Join(dir, "secret.key"))
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "secret.key") {
		t.Fatalf("expected a clear key error, got %v", err)
	}
}
