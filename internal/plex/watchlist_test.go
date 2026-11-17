package plex_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/FiTH-g1gGLe/encorr/internal/fake"
	"github.com/FiTH-g1gGLe/encorr/internal/plex"
)

// Plex refuses big watchlist pages with a 400, so a long watchlist has to be
// read a page at a time. This one has 45 items: more than two pages.
func TestWatchlistReadsEveryPage(t *testing.T) {
	fk := fake.New()
	fk.OwnerWatchlist = 45
	srv := httptest.NewServer(fk)
	defer srv.Close()
	old := plex.DiscoverURL
	plex.DiscoverURL = srv.URL + "/discover"
	defer func() { plex.DiscoverURL = old }()

	c := &plex.Client{ClientID: "test", Token: fk.Owner.Token}
	items, err := c.Watchlist(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 45 {
		t.Fatalf("got %d items, want 45", len(items))
	}
	if tmdb, _, _ := items[44].IDs(); tmdb != 9044 {
		t.Fatalf("last item tmdb = %d, want 9044", tmdb)
	}
}
