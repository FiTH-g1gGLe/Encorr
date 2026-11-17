package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/plex"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

type job struct {
	Name    string
	Label   string
	Every   time.Duration // 0 = daily at Hour
	Hour    int
	fn      func(ctx context.Context) (string, error)
	trigger chan struct{}
	running bool
}

type JobView struct {
	Name     string        `json:"name"`
	Label    string        `json:"label"`
	Schedule string        `json:"schedule"`
	Info     store.JobInfo `json:"info"`
	Running  bool          `json:"running"`
}

func (a *App) setupJobs() {
	a.jobs = map[string]*job{}
	add := func(j *job) { j.trigger = make(chan struct{}, 1); a.jobs[j.Name] = j }
	add(&job{Name: "plex-recent", Label: "Plex recently added scan", Every: 5 * time.Minute, fn: a.jobPlexRecent})
	add(&job{Name: "plex-full", Label: "Plex full library scan", Hour: 3, fn: a.jobPlexFull})
	add(&job{Name: "downloads", Label: "Download progress", Every: time.Minute, fn: a.jobDownloads})
	add(&job{Name: "watchlists", Label: "Plex watchlist sync", Every: 10 * time.Minute, fn: a.jobWatchlists})
	add(&job{Name: "cleanup", Label: "Tidy old sessions", Hour: 4, fn: a.jobCleanup})
}

func (j *job) schedule() string {
	if j.Every > 0 {
		if j.Every < time.Hour {
			return fmt.Sprintf("every %d min", int(j.Every.Minutes()))
		}
		return "every " + j.Every.String()
	}
	return fmt.Sprintf("daily at %02d:00", j.Hour)
}

func (a *App) startJobs() {
	for _, j := range a.jobs {
		j := j
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			// Stagger the first runs so start-up isn't a burst of calls.
			first := 20 * time.Second
			if j.Every == 0 {
				first = untilHour(j.Hour)
			}
			if j.Name == "plex-full" {
				// Run a first full scan soon after start if none has ever run.
				var last time.Time
				a.Store.Read(func(d *store.Data) { last = d.Jobs[j.Name].LastRun })
				if last.IsZero() {
					first = 45 * time.Second
				}
			}
			timer := time.NewTimer(first)
			defer timer.Stop()
			for {
				select {
				case <-a.ctx.Done():
					return
				case <-timer.C:
				case <-j.trigger:
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
				}
				a.runJob(j)
				next := j.Every
				if next == 0 {
					next = untilHour(j.Hour)
				}
				timer.Reset(next)
			}
		}()
	}
}

func untilHour(h int) time.Duration {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return time.Until(next)
}

func (a *App) runJob(j *job) {
	if !a.settings().SetupComplete {
		return
	}
	a.mu.Lock()
	if j.running {
		a.mu.Unlock()
		return
	}
	j.running = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); j.running = false; a.mu.Unlock() }()

	start := time.Now()
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	defer cancel()
	msg, err := j.fn(ctx)
	info := store.JobInfo{LastRun: start, LastOK: err == nil, Message: msg, Duration: time.Since(start).Round(time.Millisecond).String()}
	if err != nil {
		info.Message = err.Error()
		a.logf("job %s: %v", j.Name, err)
	}
	_ = a.Store.Update(func(d *store.Data) error { d.Jobs[j.Name] = info; return nil })
}

// RunJob triggers a job now.
func (a *App) RunJob(name string) error {
	j, ok := a.jobs[name]
	if !ok {
		return userErr("Unknown job.")
	}
	select {
	case j.trigger <- struct{}{}:
	default:
	}
	return nil
}

func (a *App) Jobs() []JobView {
	var infos map[string]store.JobInfo
	a.Store.Read(func(d *store.Data) {
		infos = map[string]store.JobInfo{}
		for k, v := range d.Jobs {
			infos[k] = v
		}
	})
	order := []string{"plex-recent", "plex-full", "downloads", "watchlists", "cleanup"}
	out := []JobView{}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, n := range order {
		j := a.jobs[n]
		out = append(out, JobView{Name: j.Name, Label: j.Label, Schedule: j.schedule(), Info: infos[n], Running: j.running})
	}
	return out
}

func (a *App) enabledLibraries() []store.PlexLibrary {
	var libs []store.PlexLibrary
	a.Store.Read(func(d *store.Data) {
		for _, l := range d.Settings.Plex.Libraries {
			if l.Enabled {
				libs = append(libs, l)
			}
		}
	})
	return libs
}

// resolveTMDB finds the TMDB id for a Plex item, falling back to IMDb.
func (a *App) resolveTMDB(ctx context.Context, it plex.Item, typ string) int {
	tmdbID, imdb, _ := it.IDs()
	if tmdbID > 0 || imdb == "" {
		return tmdbID
	}
	mv, tv, err := a.TMDB().FindByIMDB(ctx, imdb)
	if err != nil {
		return 0
	}
	if typ == store.TypeMovie {
		return mv
	}
	return tv
}

func seasonCounts(items []plex.Item) map[int]int {
	out := map[int]int{}
	for _, s := range items {
		if s.Type == "season" && s.Index > 0 {
			out[s.Index] = s.LeafCount
		}
	}
	return out
}

// jobPlexRecent checks what Plex added recently (light: a short list per library).
func (a *App) jobPlexRecent(ctx context.Context) (string, error) {
	srv := a.PlexServer()
	if srv == nil {
		return "", errors.New("no Plex server set up")
	}
	var last time.Time
	a.Store.Read(func(d *store.Data) { last = d.Jobs["plex-recent"].LastRun })
	since := last.Add(-15 * time.Minute)
	if last.IsZero() || time.Since(since) > 6*time.Hour {
		since = time.Now().Add(-6 * time.Hour)
	}
	found := 0
	for _, lib := range a.enabledLibraries() {
		if lib.Type == "movie" {
			items, err := srv.RecentlyAdded(ctx, lib.Key, 1, since)
			if err != nil {
				return "", err
			}
			for _, it := range items {
				if id := a.resolveTMDB(ctx, it, store.TypeMovie); id > 0 {
					a.markAvailable(store.TypeMovie, id, it.RatingKey, nil)
					found++
				}
			}
			continue
		}
		eps, err := srv.RecentlyAdded(ctx, lib.Key, 4, since)
		if err != nil {
			return "", err
		}
		shows := map[string]bool{}
		for _, e := range eps {
			if e.GrandparentRatingKey != "" {
				shows[e.GrandparentRatingKey] = true
			}
		}
		for key := range shows {
			if err := a.refreshShow(ctx, srv, key); err == nil {
				found++
			}
		}
	}
	return fmt.Sprintf("%d new or updated titles", found), nil
}

func (a *App) refreshShow(ctx context.Context, srv *plex.Server, ratingKey string) error {
	md, err := srv.Metadata(ctx, ratingKey)
	if err != nil {
		return err
	}
	id := a.resolveTMDB(ctx, *md, store.TypeTV)
	if id == 0 {
		return errors.New("no TMDB id")
	}
	seasons, err := srv.Seasons(ctx, ratingKey)
	if err != nil {
		return err
	}
	a.markAvailable(store.TypeTV, id, ratingKey, seasonCounts(seasons))
	return nil
}

// jobPlexFull rebuilds the library index (nightly safety net).
func (a *App) jobPlexFull(ctx context.Context) (string, error) {
	srv := a.PlexServer()
	if srv == nil {
		return "", errors.New("no Plex server set up")
	}
	movies, shows := 0, 0
	index := map[string]store.LibEntry{}
	var tracked map[string]bool // tv shows with requests: check their seasons in detail
	a.Store.Read(func(d *store.Data) {
		tracked = map[string]bool{}
		for _, m := range d.Media {
			if m.Type == store.TypeTV {
				tracked[store.LibKey(store.TypeTV, m.TMDBID)] = true
			}
		}
	})
	for _, lib := range a.enabledLibraries() {
		typ, plexType := store.TypeMovie, 1
		if lib.Type == "show" {
			typ, plexType = store.TypeTV, 2
		}
		items, err := srv.AllItems(ctx, lib.Key, plexType)
		if err != nil {
			return "", err
		}
		for _, it := range items {
			id, _, _ := it.IDs() // no TMDB lookups for the whole library: too heavy
			if id == 0 {
				continue
			}
			key := store.LibKey(typ, id)
			e := store.LibEntry{Key: it.RatingKey}
			if typ == store.TypeTV {
				shows++
				if tracked[key] {
					if ss, err := srv.Seasons(ctx, it.RatingKey); err == nil {
						e.Seasons = seasonCounts(ss)
					}
				}
			} else {
				movies++
			}
			index[key] = e
		}
	}
	// Keep season detail we already know for untracked shows.
	_ = a.Store.Update(func(d *store.Data) error {
		for k, e := range index {
			if e.Seasons == nil {
				if old, ok := d.Library[k]; ok {
					e.Seasons = old.Seasons
					index[k] = e
				}
			}
		}
		d.Library = index
		return nil
	})
	// Update requested media from the fresh index.
	type hit struct {
		typ string
		id  int
		e   store.LibEntry
	}
	var hits []hit
	a.Store.Read(func(d *store.Data) {
		for _, m := range d.Media {
			if e, ok := d.Library[store.LibKey(m.Type, m.TMDBID)]; ok {
				hits = append(hits, hit{m.Type, m.TMDBID, e})
			}
		}
	})
	for _, h := range hits {
		a.markAvailable(h.typ, h.id, h.e.Key, h.e.Seasons)
	}
	return fmt.Sprintf("%d films, %d shows in Plex", movies, shows), nil
}

// jobDownloads polls the Radarr/Sonarr queues for progress bars.
func (a *App) jobDownloads(ctx context.Context) (string, error) {
	out := map[string]map[int][]arr.QueueItem{"radarr": {}, "sonarr": {}}
	var errs []string
	n := 0
	if c := a.Radarr(); c != nil {
		q, err := c.Queue(ctx)
		if err != nil {
			errs = append(errs, err.Error())
		}
		for _, it := range q {
			out["radarr"][it.MovieID] = append(out["radarr"][it.MovieID], it)
			n++
		}
	}
	if c := a.Sonarr(); c != nil {
		q, err := c.Queue(ctx)
		if err != nil {
			errs = append(errs, err.Error())
		}
		for _, it := range q {
			out["sonarr"][it.SeriesID] = append(out["sonarr"][it.SeriesID], it)
			n++
		}
	}
	a.mu.Lock()
	a.downloads = out
	a.mu.Unlock()
	if len(errs) > 0 {
		return "", errors.New(strings.Join(errs, "; "))
	}
	return fmt.Sprintf("%d downloading", n), nil
}

// Progress summarises downloads for one Radarr/Sonarr item.
type Progress struct {
	Percent  int       `json:"percent"`
	TimeLeft string    `json:"timeLeft,omitempty"`
	ETA      time.Time `json:"eta,omitempty"`
	Status   string    `json:"status"`
	Items    int       `json:"items"`
}

func (a *App) progressFor(typ string, externalID int) *Progress {
	if externalID == 0 {
		return nil
	}
	svc := "radarr"
	if typ == store.TypeTV {
		svc = "sonarr"
	}
	a.mu.Lock()
	items := a.downloads[svc][externalID]
	a.mu.Unlock()
	if len(items) == 0 {
		return nil
	}
	var size, left float64
	p := &Progress{Items: len(items), Status: items[0].Status}
	for _, it := range items {
		size += it.Size
		left += it.SizeLeft
		if it.EstimatedCompletionTime.After(p.ETA) {
			p.ETA = it.EstimatedCompletionTime
			p.TimeLeft = it.TimeLeft
		}
	}
	if size > 0 {
		p.Percent = int((size - left) / size * 100)
	}
	return p
}

// jobWatchlists turns watchlist items into requests for users who opted in.
func (a *App) jobWatchlists(ctx context.Context) (string, error) {
	type who struct {
		id    int
		name  string
		token string
	}
	var users []who
	a.Store.Read(func(d *store.Data) {
		for _, u := range d.Users {
			if u.WatchlistSync && u.PlexToken != "" {
				users = append(users, who{u.ID, u.Username, string(u.PlexToken)})
			}
		}
	})
	made := 0
	var errs []string
	for _, u := range users {
		items, err := a.plexClient(u.token).Watchlist(ctx)
		if err != nil {
			errs = append(errs, u.name+": "+err.Error())
			continue
		}
		for _, it := range items {
			typ := store.TypeMovie
			if it.Type == "show" {
				typ = store.TypeTV
			} else if it.Type != "movie" {
				continue
			}
			id := a.resolveTMDB(ctx, it, typ)
			if id == 0 {
				continue
			}
			skip := false
			a.Store.Read(func(d *store.Data) {
				if _, ok := d.Library[store.LibKey(typ, id)]; ok {
					skip = true
					return
				}
				if m := d.MediaByTMDB(typ, id); m != nil {
					for _, r := range d.RequestsForMedia(m.ID) {
						if r.RequestedBy == u.id || r.Status == store.ReqPending || r.Status == store.ReqApproved {
							skip = true // already asked (or declined before): don't nag
						}
					}
				}
			})
			if skip {
				continue
			}
			if _, err := a.CreateRequest(ctx, u.id, NewRequest{Type: typ, TMDBID: id, Source: "watchlist"}); err == nil {
				made++
			}
		}
	}
	msg := fmt.Sprintf("%d new requests from %d watchlists", made, len(users))
	if len(errs) > 0 {
		return msg, errors.New(strings.Join(errs, "; "))
	}
	return msg, nil
}

func (a *App) jobCleanup(ctx context.Context) (string, error) {
	n := 0
	err := a.Store.Update(func(d *store.Data) error {
		keep := d.Sessions[:0]
		for _, s := range d.Sessions {
			if time.Now().Before(s.Expires) {
				keep = append(keep, s)
			} else {
				n++
			}
		}
		d.Sessions = keep
		return nil
	})
	return fmt.Sprintf("removed %d expired sessions", n), err
}
