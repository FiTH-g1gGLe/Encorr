package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/jellyfin"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// jfIDCache remembers TVDB/IMDb -> TMDB answers, so the nightly scan
// doesn't ask TMDB about the same show every night.
var jfIDCache = struct {
	sync.Mutex
	m map[string]int
}{m: map[string]int{}}

// jfLibraries are the Jellyfin libraries Encorr watches.
func (a *App) jfLibraries() []store.PlexLibrary {
	var libs []store.PlexLibrary
	a.Store.Read(func(d *store.Data) {
		for _, l := range d.Settings.Jellyfin.Libraries {
			if l.Enabled {
				libs = append(libs, l)
			}
		}
	})
	return libs
}

// jfResolver finds TMDB ids for Jellyfin items. Jellyfin usually knows the
// TMDB id; shows matched by TVDB only, or films by IMDb only, are looked up:
// first among requested titles, then (a limited number per run) on TMDB.
type jfResolver struct {
	a       *App
	byTVDB  map[int]int
	byIMDB  map[string]int
	lookups int // TMDB lookups left this run
}

func (a *App) newResolver(budget int) *jfResolver {
	r := &jfResolver{a: a, byTVDB: map[int]int{}, byIMDB: map[string]int{}, lookups: budget}
	a.Store.Read(func(d *store.Data) {
		for _, m := range d.Media {
			if m.TVDBID > 0 && m.Type == store.TypeTV {
				r.byTVDB[m.TVDBID] = m.TMDBID
			}
			if m.IMDBID != "" {
				r.byIMDB[strings.ToLower(m.IMDBID)] = m.TMDBID
			}
		}
	})
	return r
}

func (r *jfResolver) tmdb(ctx context.Context, it jellyfin.Item, typ string) int {
	id, imdb, tvdb := it.IDs()
	if id > 0 {
		return id
	}
	if typ == store.TypeTV && tvdb > 0 {
		if v, ok := r.byTVDB[tvdb]; ok {
			return v
		}
	}
	if imdb != "" {
		if v, ok := r.byIMDB[strings.ToLower(imdb)]; ok {
			return v
		}
	}
	key := ""
	switch {
	case typ == store.TypeTV && tvdb > 0:
		key = fmt.Sprintf("tvdb:%d", tvdb)
	case imdb != "":
		key = typ + ":imdb:" + strings.ToLower(imdb)
	default:
		return 0
	}
	jfIDCache.Lock()
	v, ok := jfIDCache.m[key]
	jfIDCache.Unlock()
	if ok {
		return v
	}
	if r.lookups <= 0 {
		return 0
	}
	r.lookups--
	got := 0
	if typ == store.TypeTV && tvdb > 0 {
		if v, err := r.a.TMDB().FindByTVDB(ctx, tvdb); err == nil {
			got = v
		} else {
			return 0 // try again another time
		}
	} else {
		mv, tv, err := r.a.TMDB().FindByIMDB(ctx, imdb)
		if err != nil {
			return 0
		}
		got = mv
		if typ == store.TypeTV {
			got = tv
		}
	}
	jfIDCache.Lock()
	jfIDCache.m[key] = got
	jfIDCache.Unlock()
	return got
}

// jobJellyfinRecent checks what Jellyfin added recently.
func (a *App) jobJellyfinRecent(ctx context.Context) (string, error) {
	jf := a.Jellyfin()
	if jf == nil {
		return "Jellyfin isn't connected", nil
	}
	var last time.Time
	a.Store.Read(func(d *store.Data) { last = d.Jobs["jellyfin-recent"].LastRun })
	since := last.Add(-15 * time.Minute)
	if last.IsZero() || time.Since(since) > 6*time.Hour {
		since = time.Now().Add(-6 * time.Hour)
	}
	res := a.newResolver(40)
	found := 0
	for _, lib := range a.jfLibraries() {
		if lib.Type == "movie" {
			items, err := jf.Newest(ctx, lib.Key, "Movie", 40)
			if err != nil {
				return "", err
			}
			for _, it := range items {
				if it.DateCreated.Before(since) {
					continue
				}
				if id := res.tmdb(ctx, it, store.TypeMovie); id > 0 {
					a.markAvailable(srcJellyfin, store.TypeMovie, id, it.ID, nil)
					found++
				}
			}
			continue
		}
		eps, err := jf.Newest(ctx, lib.Key, "Episode", 150)
		if err != nil {
			return "", err
		}
		shows := map[string]bool{}
		for _, e := range eps {
			if e.SeriesID != "" && !e.DateCreated.Before(since) {
				shows[e.SeriesID] = true
			}
		}
		for id := range shows {
			if err := a.refreshShowJF(ctx, jf, res, id); err == nil {
				found++
			}
		}
	}
	if a.PlexServer() == nil {
		// Without Plex, this job also looks up requests still on their way.
		found += a.checkApproved(ctx)
	}
	return fmt.Sprintf("%d new or updated titles", found), nil
}

func (a *App) refreshShowJF(ctx context.Context, jf *jellyfin.Client, res *jfResolver, seriesID string) error {
	it, err := jf.Item(ctx, seriesID)
	if err != nil {
		return err
	}
	id := res.tmdb(ctx, *it, store.TypeTV)
	if id == 0 {
		return errors.New("no TMDB id")
	}
	counts, err := jf.SeasonCounts(ctx, seriesID)
	if err != nil {
		return err
	}
	a.markAvailable(srcJellyfin, store.TypeTV, id, seriesID, counts)
	return nil
}

// jobJellyfinFull rebuilds Jellyfin's half of the library index.
func (a *App) jobJellyfinFull(ctx context.Context) (string, error) {
	jf := a.Jellyfin()
	if jf == nil {
		a.dropSource(srcJellyfin)
		return "Jellyfin isn't connected", nil
	}
	libs := a.jfLibraries()
	type entry struct {
		id      string
		seasons map[int]int
	}
	index := map[string]entry{}
	tracked := map[string]bool{}
	a.Store.Read(func(d *store.Data) {
		for _, m := range d.Media {
			if m.Type == store.TypeTV {
				tracked[store.LibKey(store.TypeTV, m.TMDBID)] = true
			}
		}
	})
	res := a.newResolver(300)
	movies, shows := 0, 0
	for _, lib := range libs {
		typ, kind := store.TypeMovie, "Movie"
		if lib.Type == "show" {
			typ, kind = store.TypeTV, "Series"
		}
		items, err := jf.AllItems(ctx, lib.Key, kind)
		if err != nil {
			return "", err
		}
		for _, it := range items {
			id := res.tmdb(ctx, it, typ)
			if id == 0 {
				continue
			}
			key := store.LibKey(typ, id)
			e := entry{id: it.ID}
			if typ == store.TypeTV {
				shows++
				if tracked[key] {
					if c, err := jf.SeasonCounts(ctx, it.ID); err == nil {
						e.seasons = c
					}
				}
			} else {
				movies++
			}
			index[key] = e
		}
	}
	// Replace only Jellyfin's half; keep known season detail for untracked shows.
	var hits []struct {
		typ string
		id  int
		e   store.LibEntry
	}
	_ = a.Store.Update(func(d *store.Data) error {
		for k, old := range d.Library {
			n, ok := index[k]
			if !ok {
				old.JF, old.JFSeasons = "", nil
			} else {
				if n.seasons == nil && old.JF == n.id {
					n.seasons = old.JFSeasons
				}
				old.JF, old.JFSeasons = n.id, n.seasons
				delete(index, k)
			}
			if old.Empty() {
				delete(d.Library, k)
			} else {
				d.Library[k] = old
			}
		}
		for k, n := range index {
			d.Library[k] = store.LibEntry{JF: n.id, JFSeasons: n.seasons}
		}
		for _, m := range d.Media {
			if e, ok := d.Library[store.LibKey(m.Type, m.TMDBID)]; ok && e.JF != "" {
				hits = append(hits, struct {
					typ string
					id  int
					e   store.LibEntry
				}{m.Type, m.TMDBID, e})
			}
		}
		return nil
	})
	for _, h := range hits {
		a.markAvailable(srcJellyfin, h.typ, h.id, h.e.JF, h.e.JFSeasons)
	}
	return fmt.Sprintf("%d films, %d shows in Jellyfin", movies, shows), nil
}

// dropSource forgets one server's half of the library, after it's
// disconnected.
func (a *App) dropSource(src string) {
	_ = a.Store.Update(func(d *store.Data) error {
		for k, e := range d.Library {
			if src == srcJellyfin {
				e.JF, e.JFSeasons = "", nil
			} else {
				e.Key, e.Seasons = "", nil
			}
			if e.Empty() {
				delete(d.Library, k)
			} else {
				d.Library[k] = e
			}
		}
		for _, m := range d.Media {
			if src == srcJellyfin {
				m.JFKey = ""
			} else {
				m.PlexKey = ""
			}
		}
		return nil
	})
}

// nudgeJellyfin tells Jellyfin about the folder Radarr/Sonarr just imported
// into, so it shows up straight away.
func (a *App) nudgeJellyfin(ctx context.Context, typ string, externalID int) {
	jf := a.Jellyfin()
	c, kind := a.Radarr(), "movie"
	if typ == store.TypeTV {
		c, kind = a.Sonarr(), "series"
	}
	if jf == nil || c == nil {
		return
	}
	path, err := c.ItemPath(ctx, kind, externalID)
	if err != nil || path == "" {
		return
	}
	libs, err := jf.Libraries(ctx)
	if err != nil {
		return
	}
	norm := func(p string) string {
		return strings.ToLower(strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")) + "/"
	}
	for _, lib := range libs {
		for _, loc := range lib.Locations {
			if strings.HasPrefix(norm(path), norm(loc)) {
				if err := jf.ScanPath(ctx, path); err == nil {
					a.logf("asked Jellyfin to scan %s", path)
				}
				return
			}
		}
	}
	// Radarr/Sonarr and Jellyfin see the files under different paths (Docker,
	// network shares): Jellyfin's own monitoring or schedule picks it up.
}

// findInJellyfin searches Jellyfin by title for one requested title.
func (a *App) findInJellyfin(ctx context.Context, m store.Media) bool {
	jf := a.Jellyfin()
	if jf == nil || m.Title == "" {
		return false
	}
	kind := "Movie"
	if m.Type == store.TypeTV {
		kind = "Series"
	}
	items, err := jf.Search(ctx, kind, m.Title)
	if err != nil {
		a.logf("Jellyfin title search for %q failed: %v", m.Title, err)
		return false
	}
	res := a.newResolver(5)
	for _, it := range items {
		if res.tmdb(ctx, it, m.Type) != m.TMDBID {
			continue
		}
		if m.Type == store.TypeMovie {
			a.markAvailable(srcJellyfin, store.TypeMovie, m.TMDBID, it.ID, nil)
		} else if err := a.refreshShowJF(ctx, jf, res, it.ID); err != nil {
			continue
		}
		return true
	}
	return false
}

// findInLibrary looks for a requested title on every connected server.
func (a *App) findInLibrary(ctx context.Context, m store.Media) bool {
	p := a.findInPlex(ctx, m)
	j := a.findInJellyfin(ctx, m)
	return p || j
}
