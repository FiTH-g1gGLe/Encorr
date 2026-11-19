package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/netfind"
	"github.com/FiTH-g1gGLe/encorr/internal/notify"
	"github.com/FiTH-g1gGLe/encorr/internal/plex"
	"github.com/FiTH-g1gGLe/encorr/internal/sab"
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
	add(&job{Name: "jellyfin-recent", Label: "Jellyfin recently added scan", Every: 5 * time.Minute, fn: a.jobJellyfinRecent})
	add(&job{Name: "jellyfin-full", Label: "Jellyfin full library scan", Hour: 3, fn: a.jobJellyfinFull})
	add(&job{Name: "downloads", Label: "Download progress", Every: 15 * time.Second, fn: a.jobDownloads})
	add(&job{Name: "watchlists", Label: "Plex watchlist sync", Every: 10 * time.Minute, fn: a.jobWatchlists})
	add(&job{Name: "cleanup", Label: "Tidy old sessions", Hour: 4, fn: a.jobCleanup})
	add(&job{Name: "nudge", Label: "Search again for stalled requests", Every: 2 * time.Hour, fn: a.jobNudge})
	add(&job{Name: "arr-info", Label: "Release dates from Radarr/Sonarr", Every: 20 * time.Minute, fn: a.jobArrInfo})
	add(&job{Name: "autofix", Label: "Fix stuck downloads", Every: 5 * time.Minute, fn: a.jobAutoFix})
	add(&job{Name: "subscriptions", Label: "Subscription reminders", Every: 6 * time.Hour, fn: a.jobSubscriptions})
}

func (j *job) schedule() string {
	if j.Every > 0 {
		if j.Every < time.Minute {
			return fmt.Sprintf("every %d sec", int(j.Every.Seconds()))
		}
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
			if j.Name == "plex-full" || j.Name == "jellyfin-full" {
				// Scan the whole library soon after start unless a full scan
				// worked within the last day: Encorr needs to know what you
				// already have before it can say "In Plex" or skip watchlist
				// titles you own.
				var last store.JobInfo
				a.Store.Read(func(d *store.Data) { last = d.Jobs[j.Name] })
				if !last.LastOK || time.Since(last.LastRun) > 24*time.Hour {
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
				ok := a.runJob(j)
				next := j.Every
				if next == 0 {
					next = untilHour(j.Hour)
					if !ok && next > time.Hour {
						next = time.Hour // a nightly job that failed tries again in an hour
					}
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

func (a *App) runJob(j *job) bool {
	if !a.settings().SetupComplete {
		return true
	}
	a.mu.Lock()
	if j.running {
		a.mu.Unlock()
		return true
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
	return err == nil
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
	order := []string{"plex-recent", "plex-full", "jellyfin-recent", "jellyfin-full", "downloads", "watchlists", "nudge", "arr-info", "cleanup"}
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
	tmdbID, imdb, tvdb := it.IDs()
	if tmdbID > 0 {
		return tmdbID
	}
	if imdb == "" {
		if typ == store.TypeTV && tvdb > 0 {
			if id, err := a.TMDB().FindByTVDB(ctx, tvdb); err == nil {
				return id
			}
		}
		return 0
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
		if a.Jellyfin() != nil {
			return "Plex isn't connected (Jellyfin is)", nil
		}
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
					a.markAvailable(srcPlex, store.TypeMovie, id, it.RatingKey, nil)
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
	found += a.checkApproved(ctx)
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
	a.markAvailable(srcPlex, store.TypeTV, id, ratingKey, seasonCounts(seasons))
	return nil
}

// jobPlexFull rebuilds the library index (nightly safety net).
func (a *App) jobPlexFull(ctx context.Context) (string, error) {
	srv := a.PlexServer()
	if srv == nil {
		if a.Jellyfin() != nil {
			a.dropSource(srcPlex) // Plex was disconnected: forget what it had
			return "Plex isn't connected (Jellyfin is)", nil
		}
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
	// Keep season detail we already know for untracked shows, and keep
	// everything Jellyfin knows: this scan only replaces Plex's half.
	_ = a.Store.Update(func(d *store.Data) error {
		for k, old := range d.Library {
			e, inPlex := index[k]
			if inPlex && e.Seasons == nil && old.Key != "" {
				e.Seasons = old.Seasons
			}
			e.JF, e.JFSeasons = old.JF, old.JFSeasons
			if !e.Empty() {
				index[k] = e
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
		if h.e.Key != "" {
			a.markAvailable(srcPlex, h.typ, h.id, h.e.Key, h.e.Seasons)
		}
	}
	return fmt.Sprintf("%d films, %d shows in Plex", movies, shows), nil
}

// jobDownloads polls the Radarr/Sonarr queues (and SABnzbd, when set up)
// for progress bars, and spots downloads that have just finished.
func (a *App) jobDownloads(ctx context.Context) (string, error) {
	a.autoDetectSAB()
	out := map[string]map[int][]arr.QueueItem{"radarr": {}, "sonarr": {}}
	var errs []string
	n := 0
	ok := map[string]bool{}
	for svc, c := range map[string]*arr.Client{"radarr": a.Radarr(), "sonarr": a.Sonarr()} {
		if c == nil {
			continue
		}
		q, err := c.Queue(ctx)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		ok[svc] = true
		for _, it := range q {
			id := it.MovieID
			if svc == "sonarr" {
				id = it.SeriesID
			}
			out[svc][id] = append(out[svc][id], it)
			n++
		}
	}
	var snap *sab.Snapshot
	if c := a.SAB(); c != nil {
		s, err := c.Snapshot(ctx)
		if err != nil {
			errs = append(errs, err.Error())
		}
		snap = s
	}
	a.mu.Lock()
	var finished [][2]any
	for svc := range ok {
		for id := range a.downloads[svc] {
			if _, still := out[svc][id]; !still {
				a.landed[svc][id] = time.Now()
				finished = append(finished, [2]any{svc, id})
			}
		}
		for id, t := range a.landed[svc] {
			if _, again := out[svc][id]; again || time.Since(t) > 6*time.Hour {
				delete(a.landed[svc], id)
			}
		}
		a.downloads[svc] = out[svc]
	}
	a.sabSnap = snap
	stuckNow := a.sabWatch.see(snap, time.Now())
	a.mu.Unlock()
	if stuckNow != nil {
		a.logf("SABnzbd looks stuck: %q has been on %q for %s, with %d finished downloads waiting behind it",
			stuckNow.Name, stuckNow.Action, time.Since(stuckNow.Since).Round(time.Minute), stuckNow.Waiting)
		a.activity("warn", "SABnzbd looks stuck", fmt.Sprintf("%s has been on %s for %s", stuckNow.Name, stuckNow.Action, time.Since(stuckNow.Since).Round(time.Minute)))
		a.Notify(notify.Message{Event: notify.EventAttention, Title: "SABnzbd looks stuck",
			Detail: fmt.Sprintf("It has been on %s (%s) for %s, and %d finished downloads are waiting behind it. Check Settings → Health in Encorr.",
				stuckNow.Name, stuckNow.Action, time.Since(stuckNow.Since).Round(time.Minute), stuckNow.Waiting)})
	}
	// Something left the queue: it's been imported, so look for it in Plex
	// shortly (Plex needs a moment to scan it) rather than waiting for the
	// next scheduled scan.
	for _, f := range finished {
		svc, id := f[0].(string), f[1].(int)
		typ := store.TypeMovie
		if svc == "sonarr" {
			typ = store.TypeTV
		}
		a.logf("%s finished downloading item %d; checking %s for it", svc, id, a.libName())
		go a.checkPlexSoon(typ, id)
		go a.issuesArrived(svc, id)
	}
	if len(errs) > 0 {
		return "", errors.New(strings.Join(errs, "; "))
	}
	return fmt.Sprintf("%d downloading", n), nil
}

// autoDetectSAB fills in SABnzbd by itself the first time it finds it on
// this machine, so live progress works without an extra setup step.
func (a *App) autoDetectSAB() {
	s := a.settings()
	a.mu.Lock()
	tried := a.sabTried
	a.sabTried = true
	a.mu.Unlock()
	if s.SABnzbd.URL != "" || tried {
		return
	}
	d := sab.Detect()
	if !d.Found {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 12*time.Second)
	defer cancel()
	d.URL = findReachable(ctx, netfind.SABnzbd, d.URL, "") // in Docker, not localhost
	c := &sab.Client{URL: d.URL, APIKey: d.APIKey}
	if _, err := c.Test(ctx); err != nil {
		a.logf("found SABnzbd settings at %s but couldn't connect: %v", d.ConfigPath, err)
		return
	}
	_ = a.Store.Update(func(dd *store.Data) error {
		dd.Settings.SABnzbd = store.SabSettings{Enabled: true, URL: d.URL, APIKey: store.Secret(d.APIKey), Detected: true}
		return nil
	})
	a.logf("found SABnzbd at %s: progress bars now come straight from it", d.URL)
}

// nudgePlex asks Plex to scan the folder Radarr/Sonarr just imported into,
// so it appears in seconds even if Plex's own "scan automatically" setting
// or Radarr/Sonarr's Plex connection isn't set up.
func (a *App) nudgePlex(ctx context.Context, typ string, externalID int) {
	srv := a.PlexServer()
	c, kind := a.Radarr(), "movie"
	if typ == store.TypeTV {
		c, kind = a.Sonarr(), "series"
	}
	if srv == nil || c == nil {
		return
	}
	path, err := c.ItemPath(ctx, kind, externalID)
	if err != nil || path == "" {
		return
	}
	libs, err := srv.Libraries(ctx)
	if err != nil {
		return
	}
	norm := func(p string) string {
		return strings.ToLower(strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")) + "/"
	}
	for _, lib := range libs {
		for _, loc := range lib.Location {
			if strings.HasPrefix(norm(path), norm(loc.Path)) {
				if err := srv.ScanPath(ctx, lib.Key, path); err == nil {
					a.logf("asked Plex to scan %s", path)
				}
				return
			}
		}
	}
}

// checkPlexSoon looks for a just-imported title in Plex a few times,
// starting straight away.
func (a *App) checkPlexSoon(typ string, externalID int) {
	nctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	a.nudgePlex(nctx, typ, externalID)
	a.nudgeJellyfin(nctx, typ, externalID)
	cancel()
	for _, wait := range []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute} {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(wait):
		}
		var m *store.Media
		a.Store.Read(func(d *store.Data) {
			for _, x := range d.Media {
				if x.Type == typ && x.ExternalID == externalID {
					c := *x
					m = &c
				}
			}
		})
		if m == nil {
			return
		}
		if a.findInLibrary(a.ctx, *m) && a.mediaDone(m.ID) {
			return
		}
	}
}

// mediaDone reports whether no request for this media is still on its way.
func (a *App) mediaDone(mediaID int) bool {
	done := true
	a.Store.Read(func(d *store.Data) {
		for _, r := range d.RequestsForMedia(mediaID) {
			if r.Status == store.ReqApproved {
				done = false
			}
		}
	})
	return done
}

// findInPlex searches Plex by title for one requested title and, when the
// ids match, updates it (completing requests). It doesn't rely on Plex's
// "recently added" list, which can miss things.
func (a *App) findInPlex(ctx context.Context, m store.Media) bool {
	srv := a.PlexServer()
	if srv == nil || m.Title == "" {
		return false
	}
	libType, plexType := "movie", 1
	if m.Type == store.TypeTV {
		libType, plexType = "show", 2
	}
	for _, lib := range a.enabledLibraries() {
		if lib.Type != libType {
			continue
		}
		items, err := srv.FindByTitle(ctx, lib.Key, plexType, m.Title)
		if err != nil {
			a.logf("Plex title search for %q failed: %v", m.Title, err)
			continue
		}
		for _, it := range items {
			if a.resolveTMDB(ctx, it, m.Type) != m.TMDBID {
				continue
			}
			if m.Type == store.TypeMovie {
				a.markAvailable(srcPlex, store.TypeMovie, m.TMDBID, it.RatingKey, nil)
			} else if err := a.refreshShow(ctx, srv, it.RatingKey); err != nil {
				continue
			}
			return true
		}
	}
	return false
}

// checkApproved looks up every request that's still on its way directly in
// Plex. Cheap (one search per title) and catches anything the
// recently-added scan missed.
func (a *App) checkApproved(ctx context.Context) int {
	var list []store.Media
	a.Store.Read(func(d *store.Data) {
		seen := map[int]bool{}
		for _, r := range d.Requests {
			if r.Status != store.ReqApproved || seen[r.MediaID] {
				continue
			}
			seen[r.MediaID] = true
			if m := d.MediaByID(r.MediaID); m != nil {
				list = append(list, *m)
			}
		}
	})
	if len(list) > 60 {
		list = list[:60]
	}
	found := 0
	for _, m := range list {
		if ctx.Err() != nil {
			break
		}
		if m.Type == store.TypeTV {
			m = a.refreshAired(ctx, m)
		}
		if a.findInLibrary(ctx, m) {
			found++
		}
	}
	return found
}

// Progress summarises downloads for one Radarr/Sonarr item.
type Progress struct {
	Percent  int       `json:"percent"`
	TimeLeft string    `json:"timeLeft,omitempty"`
	ETA      time.Time `json:"eta,omitempty"`
	Status   string    `json:"status"`
	// Stage is the plain-English step: Queued, Paused, Downloading,
	// Unpacking, Importing, Stuck, Waiting for Plex.
	Stage string `json:"stage"`
	Speed string `json:"speed,omitempty"` // overall SABnzbd speed, e.g. "182 MB/s"
	Live  bool   `json:"live"`            // numbers came straight from SABnzbd
	Items int    `json:"items"`
	Note  string `json:"note,omitempty"` // Radarr/Sonarr's warning, if any
	// Blocked: downloaded, but Radarr/Sonarr want someone to confirm the
	// import (an admin can do that from Encorr with "Import it").
	Blocked bool `json:"blocked,omitempty"`
}

func fmtSpeed(kb float64) string {
	switch {
	case kb <= 0:
		return ""
	case kb >= 1024:
		return fmt.Sprintf("%.0f MB/s", kb/1024)
	default:
		return fmt.Sprintf("%.0f KB/s", kb)
	}
}

// tidyTime turns "00:04:10" / "1.02:03:04" / "0:00:05" into "4m 10s" style.
func tidyTime(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	days := 0
	if d, rest, ok := strings.Cut(t, "."); ok && !strings.Contains(d, ":") {
		days, _ = strconv.Atoi(d)
		t = rest
	}
	parts := strings.Split(t, ":")
	if len(parts) != 3 {
		return t
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	sec, _ := strconv.Atoi(parts[2])
	h += days * 24
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, sec)
	case sec > 0:
		return fmt.Sprintf("%ds", sec)
	}
	return ""
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
	_, hasLanded := a.landed[svc][externalID]
	snap := a.sabSnap
	a.mu.Unlock()
	if len(items) == 0 {
		if hasLanded {
			return &Progress{Percent: 100, Stage: "Waiting for Plex", Status: "imported"}
		}
		return nil
	}
	p := &Progress{Items: len(items)}
	var size, left float64
	stages := map[string]int{}
	for _, it := range items {
		isize, ileft, tl := it.Size, it.SizeLeft, it.TimeLeft
		stage := ""
		if snap != nil && it.DownloadID != "" {
			if sl, ok := snap.Slots[strings.ToLower(it.DownloadID)]; ok {
				p.Live = true
				if sl.MB > 0 {
					isize, ileft = sl.MB*1e6, sl.MBLeft*1e6
				}
				tl = sl.TimeLeft
				switch {
				case sl.PostProc:
					stage, ileft, tl = "Unpacking", 0, ""
				case sl.Status == "Paused" || (snap.Paused && sl.Status == "Queued"):
					stage = "Paused"
				case sl.Status == "Queued":
					stage = "Queued"
				case sl.Status == "Completed":
					stage, ileft = "Importing", 0
				default:
					stage = "Downloading"
				}
			}
		}
		if stage == "" {
			state := strings.ToLower(it.TrackedDownloadState)
			st := strings.ToLower(it.Status)
			switch {
			case state == "importpending" || state == "importing" || state == "imported":
				stage, ileft = "Importing", 0
			case state == "failedpending" || state == "failed" || st == "failed":
				stage = "Failed"
			case st == "warning":
				stage = "Stuck"
			case st == "paused":
				stage = "Paused"
			case st == "queued" || st == "delay":
				stage = "Queued"
			case st == "completed":
				stage, ileft = "Importing", 0
			default:
				stage = "Downloading"
			}
			if it.Blocked() {
				stage = "Stuck"
			}
		}
		if it.Blocked() {
			p.Blocked = true
			stage = "Stuck"
		}
		if w := it.Warning(); w != "" && (stage == "Stuck" || stage == "Failed") && p.Note == "" {
			p.Note = w
		}
		if stage == "Stuck" || stage == "Failed" {
			if p.Note == "" {
				p.Note = "Check the Activity page in " + map[string]string{"radarr": "Radarr", "sonarr": "Sonarr"}[svc]
			}
		}
		stages[stage]++
		size += isize
		left += ileft
		if tl = tidyTime(tl); tl != "" && (p.TimeLeft == "" || len(tl) > len(p.TimeLeft)) {
			p.TimeLeft = tl
		}
		if it.EstimatedCompletionTime.After(p.ETA) {
			p.ETA = it.EstimatedCompletionTime
		}
		p.Status = it.Status
	}
	// The overall stage is the least-finished one.
	for _, s := range []string{"Stuck", "Failed", "Downloading", "Paused", "Queued", "Unpacking", "Importing"} {
		if stages[s] > 0 {
			p.Stage = s
			break
		}
	}
	if size > 0 {
		p.Percent = int((size - left) / size * 100)
	}
	if p.Percent > 100 {
		p.Percent = 100
	}
	if p.Percent < 0 {
		p.Percent = 0
	}
	if p.Stage == "Downloading" && p.Live && snap != nil {
		p.Speed = fmtSpeed(snap.KBPerSec)
	}
	if p.Stage != "Downloading" {
		p.TimeLeft = ""
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
	known := false
	a.Store.Read(func(d *store.Data) {
		known = d.Jobs["plex-full"].LastOK
		for _, u := range d.Users {
			if u.WatchlistSync && u.PlexToken != "" {
				users = append(users, who{u.ID, u.Username, string(u.PlexToken)})
			}
		}
	})
	if len(users) > 0 && !known {
		// Without a full picture of the library we'd request films you
		// already own. Wait for the full Plex scan to finish first.
		return "waiting for the first full Plex library scan before reading watchlists", nil
	}
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
				if d.WatchlistSkip[store.SkipKey(u.id, typ, id)] {
					skip = true // they removed it from Encorr before
					return
				}
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

// refreshAired brings a show's episode counts up to date with what has
// actually aired (older requests counted announced episodes too, which kept
// them "in progress" for ever). TMDB answers come from a 6-hour cache.
func (a *App) refreshAired(ctx context.Context, m store.Media) store.Media {
	det, err := a.TMDB().TV(ctx, m.TMDBID)
	if err != nil || len(det.Seasons) == 0 {
		return m
	}
	eps := airedEps(det.Seasons)
	_ = a.Store.Update(func(d *store.Data) error {
		if mm := d.MediaByID(m.ID); mm != nil {
			mm.SeasonEps = eps
			m = *mm
		}
		return nil
	})
	return m
}

// nudgeEvery is how often Encorr asks Radarr/Sonarr to search again for a
// request that has nothing downloading.
const nudgeEvery = 12 * time.Hour

// jobNudge asks Radarr/Sonarr to search again for requests that are still
// open but have nothing downloading. Radarr and Sonarr only search once,
// when something is added; after that they wait for new uploads to appear.
// If those first downloads failed or were removed, the request would sit
// "looking for a copy" for ever.
func (a *App) jobNudge(ctx context.Context) (string, error) {
	type item struct {
		svc   string
		id    int
		title string
	}
	var todo []item
	cutoff := time.Now().Add(-2 * time.Hour)
	a.Store.Read(func(d *store.Data) {
		seen := map[string]bool{}
		for _, r := range d.Requests {
			if r.Status != store.ReqApproved || r.Updated.After(cutoff) {
				continue // give a fresh request time to get going
			}
			m := d.MediaByID(r.MediaID)
			if m == nil || m.ExternalID == 0 {
				continue
			}
			svc := "radarr"
			if m.Type == store.TypeTV {
				svc = "sonarr"
			}
			k := svc + ":" + strconv.Itoa(m.ExternalID)
			if !seen[k] {
				seen[k] = true
				todo = append(todo, item{svc, m.ExternalID, m.Title})
			}
		}
	})
	var asked []string
	var errs []string
	for _, it := range todo {
		k := it.svc + ":" + strconv.Itoa(it.id)
		a.mu.Lock()
		busy := len(a.downloads[it.svc][it.id]) > 0
		if a.nudged == nil {
			a.nudged = map[string]time.Time{}
		}
		recent := time.Since(a.nudged[k]) < nudgeEvery
		a.mu.Unlock()
		if busy || recent {
			continue
		}
		var c *arr.Client
		body := map[string]any{}
		if it.svc == "sonarr" {
			c, body["name"], body["seriesId"] = a.Sonarr(), "SeriesSearch", it.id
		} else {
			c, body["name"], body["movieIds"] = a.Radarr(), "MoviesSearch", []int{it.id}
		}
		if c == nil {
			continue
		}
		if err := c.Command(ctx, body); err != nil {
			errs = append(errs, it.title+": "+err.Error())
			continue
		}
		a.mu.Lock()
		a.nudged[k] = time.Now()
		a.mu.Unlock()
		asked = append(asked, it.title)
	}
	msg := "nothing stalled"
	if len(asked) > 0 {
		msg = fmt.Sprintf("asked for a fresh search: %s", strings.Join(asked, ", "))
		a.logf("%s", msg)
	}
	if len(errs) > 0 {
		return msg, errors.New(strings.Join(errs, "; "))
	}
	return msg, nil
}

// jobArrInfo keeps release dates (films) and next-episode dates (shows) from
// Radarr/Sonarr, so In progress can say "out on 1 Dec" instead of "looking".
func (a *App) jobArrInfo(ctx context.Context) (string, error) {
	var msgs []string
	var errs []string
	if c := a.Radarr(); c != nil {
		if list, err := c.Movies(ctx); err != nil {
			errs = append(errs, "Radarr: "+err.Error())
		} else {
			m := make(map[int]arr.MovieInfo, len(list))
			for _, x := range list {
				m[x.ID] = x
			}
			a.mu.Lock()
			a.movieInfo = m
			a.mu.Unlock()
			msgs = append(msgs, fmt.Sprintf("%d films", len(list)))
		}
	}
	if c := a.Sonarr(); c != nil {
		if list, err := c.AllSeries(ctx); err != nil {
			errs = append(errs, "Sonarr: "+err.Error())
		} else {
			m := make(map[int]arr.SeriesInfo, len(list))
			for _, x := range list {
				m[x.ID] = x
			}
			a.mu.Lock()
			a.showInfo = m
			a.mu.Unlock()
			msgs = append(msgs, fmt.Sprintf("%d shows", len(list)))
		}
	}
	msg := "updated " + strings.Join(msgs, " and ")
	if len(errs) > 0 {
		return msg, errors.New(strings.Join(errs, "; "))
	}
	return msg, nil
}
