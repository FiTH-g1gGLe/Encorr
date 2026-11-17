package arr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseConfig(t *testing.T) {
	xml := `<Config><BindAddress>*</BindAddress><Port>7878</Port><SslPort>9898</SslPort><EnableSsl>False</EnableSsl>
<ApiKey>abc123</ApiKey><UrlBase>radarr</UrlBase></Config>`
	d, err := ParseConfig([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if d.URL != "http://localhost:7878/radarr" || d.APIKey != "abc123" {
		t.Fatalf("got %+v", d)
	}
	if _, err := ParseConfig([]byte(`<Config><Port>8989</Port></Config>`)); err == nil {
		t.Fatal("missing key should fail")
	}
}

func TestTagLabel(t *testing.T) {
	cases := map[string]string{"g1gGLe": "encorr-g1ggle", "Mr Smith!": "encorr-mr-smith", "": "encorr-user", "élan": "encorr-lan"}
	for in, want := range cases {
		if got := TagLabel(in); got != want {
			t.Errorf("TagLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// An existing, unmonitored film is switched on (PUT), not re-added (POST).
func TestAddMovieExistingUnmonitored(t *testing.T) {
	var put map[string]any
	posted := false
	searched := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/movie" && r.Method == "GET":
			json.NewEncoder(w).Encode([]any{map[string]any{"id": 9, "tmdbId": 5, "monitored": false, "hasFile": false, "tags": []int{1}}})
		case r.URL.Path == "/api/v3/movie/9" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{"id": 9, "tmdbId": 5, "monitored": false, "tags": []int{1}, "path": "/films/x"})
		case r.URL.Path == "/api/v3/movie/9" && r.Method == "PUT":
			json.NewDecoder(r.Body).Decode(&put)
			json.NewEncoder(w).Encode(put)
		case r.URL.Path == "/api/v3/movie" && r.Method == "POST":
			posted = true
		case r.URL.Path == "/api/v3/command":
			searched = true
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{Name: "Radarr", URL: srv.URL, APIKey: "k"}
	_, err := c.AddMovie(context.Background(), AddMovieOptions{TMDBID: 5, QualityProfileID: 4, Tags: []int{2}, SearchNow: true, MinimumAvailability: "released"})
	if err != nil {
		t.Fatal(err)
	}
	if posted {
		t.Fatal("should not POST an existing film")
	}
	if put["monitored"] != true || put["path"] != "/films/x" {
		t.Fatalf("PUT body lost data or didn't monitor: %v", put)
	}
	if tags := put["tags"].([]any); len(tags) != 2 {
		t.Fatalf("tags not merged: %v", put["tags"])
	}
	if !searched {
		t.Fatal("search not triggered")
	}
}

// A film that's already downloaded is left alone.
func TestAddMovieAlreadyDownloaded(t *testing.T) {
	calls := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		json.NewEncoder(w).Encode([]any{map[string]any{"id": 9, "tmdbId": 5, "monitored": true, "hasFile": true}})
	}))
	defer srv.Close()
	c := &Client{Name: "Radarr", URL: srv.URL, APIKey: "k"}
	if _, err := c.AddMovie(context.Background(), AddMovieOptions{TMDBID: 5, SearchNow: true}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected only the lookup, got %v", calls)
	}
}

// Requesting a new season of a show Sonarr already has monitors just that
// season's episodes, retrying while Sonarr loads them.
func TestAddSeriesExistingRetriesEpisodes(t *testing.T) {
	episodeWait = 10 * time.Millisecond
	epCalls := 0
	var monitored []any
	var putSeasons []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/series" && r.Method == "GET":
			json.NewEncoder(w).Encode([]any{map[string]any{"id": 3, "tvdbId": 77, "seasons": []any{
				map[string]any{"seasonNumber": 1, "monitored": true}, map[string]any{"seasonNumber": 2, "monitored": false}}}})
		case r.URL.Path == "/api/v3/series/3" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{"id": 3, "tvdbId": 77})
		case r.URL.Path == "/api/v3/series/3" && r.Method == "PUT":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			putSeasons = b["seasons"].([]any)
			json.NewEncoder(w).Encode(b)
		case r.URL.Path == "/api/v3/episode":
			epCalls++
			eps := []any{map[string]any{"id": 11, "seasonNumber": 1, "monitored": true}}
			if epCalls >= 3 { // season 2 appears on the third try
				eps = append(eps, map[string]any{"id": 21, "seasonNumber": 2, "monitored": false}, map[string]any{"id": 22, "seasonNumber": 2, "monitored": false})
			}
			json.NewEncoder(w).Encode(eps)
		case r.URL.Path == "/api/v3/episode/monitor":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			monitored = b["episodeIds"].([]any)
		case r.URL.Path == "/api/v3/command":
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{Name: "Sonarr", URL: srv.URL, APIKey: "k"}
	if _, err := c.AddSeries(context.Background(), AddSeriesOptions{TVDBID: 77, Seasons: []int{2}, SearchNow: true}); err != nil {
		t.Fatal(err)
	}
	if epCalls != 3 {
		t.Fatalf("expected 3 episode checks, got %d", epCalls)
	}
	if len(monitored) != 2 {
		t.Fatalf("should monitor season 2's two episodes, got %v", monitored)
	}
	for _, s := range putSeasons {
		sm := s.(map[string]any)
		if sm["seasonNumber"].(float64) == 2 && sm["monitored"] != true {
			t.Fatal("season 2 not monitored")
		}
		if sm["seasonNumber"].(float64) == 1 && sm["monitored"] != true {
			t.Fatal("season 1 should stay monitored")
		}
	}
}

func TestFriendlyErrors(t *testing.T) {
	c := &Client{Name: "Radarr", URL: "http://127.0.0.1:1", APIKey: "k"}
	_, err := c.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "isn't answering") {
		t.Fatalf("got %v", err)
	}
}
