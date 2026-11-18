package app

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/store"
	"github.com/FiTH-g1gGLe/encorr/internal/tmdb"
)

// ---------- "Just added to Plex" row ----------

type recentCache struct {
	mu    sync.Mutex
	at    time.Time
	cards []tmdb.Card
}

// plexRecent lists the films and shows added to Plex most recently, newest
// first, as TMDB cards (so they look like every other row). Cached for a
// few minutes.
func (a *App) plexRecent(ctx context.Context) []tmdb.Card {
	a.recent.mu.Lock()
	defer a.recent.mu.Unlock()
	if time.Since(a.recent.at) < 5*time.Minute && a.recent.cards != nil {
		return a.recent.cards
	}
	srv := a.PlexServer()
	if srv == nil {
		return nil
	}
	type hit struct {
		typ   string
		id    int
		added int64
	}
	var hits []hit
	seen := map[string]bool{}
	add := func(typ string, id int, added int64) {
		k := store.LibKey(typ, id)
		if id > 0 && !seen[k] {
			seen[k] = true
			hits = append(hits, hit{typ, id, added})
		}
	}
	for _, lib := range a.enabledLibraries() {
		if lib.Type == "movie" {
			items, err := srv.Newest(ctx, lib.Key, 1, 20)
			if err != nil {
				continue
			}
			for _, it := range items {
				add(store.TypeMovie, a.resolveTMDB(ctx, it, store.TypeMovie), it.AddedAt)
			}
			continue
		}
		// For TV, new episodes bring their show to the front.
		eps, err := srv.Newest(ctx, lib.Key, 4, 60)
		if err != nil {
			continue
		}
		shows := map[string]int64{}
		var order []string
		for _, e := range eps {
			if e.GrandparentRatingKey == "" {
				continue
			}
			if _, ok := shows[e.GrandparentRatingKey]; !ok {
				order = append(order, e.GrandparentRatingKey)
				shows[e.GrandparentRatingKey] = e.AddedAt
			}
		}
		for i, key := range order {
			if i >= 20 {
				break
			}
			if md, err := srv.Metadata(ctx, key); err == nil {
				add(store.TypeTV, a.resolveTMDB(ctx, *md, store.TypeTV), shows[key])
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].added > hits[j].added })
	if len(hits) > 20 {
		hits = hits[:20]
	}
	cards := make([]tmdb.Card, len(hits))
	var wg sync.WaitGroup
	for i, h := range hits {
		wg.Add(1)
		go func(i int, h hit) {
			defer wg.Done()
			var d *tmdb.Details
			var err error
			if h.typ == store.TypeTV {
				d, err = a.TMDB().TV(ctx, h.id)
			} else {
				d, err = a.TMDB().Movie(ctx, h.id)
			}
			if err == nil {
				cards[i] = d.Card
			}
		}(i, h)
	}
	wg.Wait()
	out := cards[:0]
	for _, c := range cards {
		if c.ID != 0 {
			out = append(out, c)
		}
	}
	a.recent.at, a.recent.cards = time.Now(), out
	return out
}

// ---------- collections ----------

func (a *App) hCollection(w http.ResponseWriter, r *http.Request, u *store.User) {
	col, err := a.TMDB().Collection(r.Context(), pathInt(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	parts := a.annotate(col.Parts, u.ID)
	missing := 0
	for _, p := range parts {
		if requestable(p) {
			missing++
		}
	}
	writeJSON(w, 200, map[string]any{"id": col.ID, "name": col.Name, "overview": col.Overview,
		"poster": col.Poster, "backdrop": col.Backdrop, "parts": parts, "missing": missing})
}

// requestable: not in Plex, not already asked for, and has a release date
// (TMDB lists far-off sequels with no date at all).
func requestable(c cardOut) bool {
	return (c.Status == "" || c.Status == "none") && c.Date != ""
}

// hRequestCollection requests every film in a series you haven't got.
func (a *App) hRequestCollection(w http.ResponseWriter, r *http.Request, u *store.User) {
	col, err := a.TMDB().Collection(r.Context(), pathInt(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	made, failed := 0, 0
	var lastErr error
	for _, p := range a.annotate(col.Parts, u.ID) {
		if !requestable(p) {
			continue
		}
		if _, err := a.CreateRequest(r.Context(), u.ID, NewRequest{Type: store.TypeMovie, TMDBID: p.ID, Source: "web"}); err != nil {
			failed++
			lastErr = err
			continue
		}
		made++
	}
	if made == 0 && lastErr != nil {
		fail(w, lastErr)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "requested": made, "failed": failed})
}
