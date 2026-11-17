package sab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseINI(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sabnzbd.ini")
	os.WriteFile(p, []byte("__version__ = 19\n[misc]\nhost = ::\nport = 8080\nurl_base = /sabnzbd\napi_key = abc123\n[servers]\napi_key = nope\n"), 0o600)
	d, err := ParseINI(p)
	if err != nil || d.URL != "http://localhost:8080/sabnzbd" || d.APIKey != "abc123" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "k" {
			w.Write([]byte(`{"status":false,"error":"API Key Incorrect"}`))
			return
		}
		switch r.URL.Query().Get("mode") {
		case "queue":
			w.Write([]byte(`{"queue":{"kbpersec":"204800.0","slots":[{"nzo_id":"SABnzbd_nzo_A","status":"Downloading","percentage":"42","mb":"1000","mbleft":"580","timeleft":"0:00:03"}]}}`))
		case "history":
			w.Write([]byte(`{"history":{"slots":[{"nzo_id":"SABnzbd_nzo_B","status":"Extracting","bytes":5e9}]}}`))
		case "version":
			w.Write([]byte(`{"version":"4.3.3"}`))
		}
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, APIKey: "k"}
	s, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a := s.Slots["sabnzbd_nzo_a"]; a.Percent != 42 || a.MBLeft != 580 {
		t.Fatalf("%+v", a)
	}
	if b := s.Slots["sabnzbd_nzo_b"]; !b.PostProc {
		t.Fatalf("%+v", b)
	}
	if _, err := (&Client{URL: srv.URL, APIKey: "bad"}).Test(context.Background()); err == nil || err.Error() != "SABnzbd rejected the API key" {
		t.Fatalf("want key error, got %v", err)
	}
}
