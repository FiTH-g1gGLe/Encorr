package arr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestReplaceEpisodes(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/api/v3"))
		mu.Unlock()
		switch p := strings.TrimPrefix(r.URL.Path, "/api/v3"); {
		case p == "/history/series":
			// One season pack covers episodes 11 and 12; episode 13 is elsewhere.
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "eventType": "grabbed", "episodeId": 11, "downloadId": "pack"},
				{"id": 2, "eventType": "grabbed", "episodeId": 12, "downloadId": "pack"},
				{"id": 3, "eventType": "downloadFolderImported", "episodeId": 11},
				{"id": 4, "eventType": "grabbed", "episodeId": 13, "downloadId": "other"},
			})
		case p == "/episode":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 11, "seasonNumber": 1, "episodeNumber": 1, "hasFile": true, "episodeFileId": 90},
				{"id": 12, "seasonNumber": 1, "episodeNumber": 2, "hasFile": true, "episodeFileId": 90},
				{"id": 13, "seasonNumber": 1, "episodeNumber": 3, "hasFile": true, "episodeFileId": 91},
			})
		default:
			w.WriteHeader(200)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, APIKey: "k"}
	n, err := c.ReplaceEpisodes(context.Background(), 5, []int{11, 12})
	if err != nil || n != 1 {
		t.Fatalf("blocklisted %d, err %v", n, err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"POST /history/failed/1", "DELETE /episodefile/90", "POST /command"}
	for _, w := range want {
		if !slices.Contains(calls, w) {
			t.Fatalf("missing %q in %v", w, calls)
		}
	}
	for _, bad := range []string{"POST /history/failed/2", "POST /history/failed/4", "DELETE /episodefile/91"} {
		if slices.Contains(calls, bad) {
			t.Fatalf("should not have called %q: %v", bad, calls)
		}
	}
}
