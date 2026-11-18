package app

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/notify"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
	"github.com/FiTH-g1gGLe/encorr/internal/tmdb"
)

// UserError is shown to the user as-is (HTTP 400/409).
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userErr(msg string) error { return &UserError{msg} }

// Details abstracts TMDB lookups so the engine can be tested without TMDB.
type Details interface {
	Movie(ctx context.Context, id int) (*tmdb.Details, error)
	TV(ctx context.Context, id int) (*tmdb.Details, error)
}

// detailsSource is swappable in tests.
var detailsSource = func(a *App) Details { return a.TMDB() }

type NewRequest struct {
	Type    string `json:"type"`
	TMDBID  int    `json:"tmdbId"`
	Seasons []int  `json:"seasons"`
	Note    string `json:"note"`
	Source  string `json:"-"`
}

func autoApproves(role string) bool { return role == store.RoleAdmin || role == store.RoleTrusted }

// CreateRequest checks rights, duplicates and quotas, records the request and
// (for Admin/Trusted users) approves it straight away.
func (a *App) CreateRequest(ctx context.Context, userID int, nr NewRequest) (*store.Request, error) {
	if nr.Type != store.TypeMovie && nr.Type != store.TypeTV {
		return nil, userErr("Choose a film or a TV show.")
	}
	if nr.TMDBID <= 0 {
		return nil, userErr("Missing title.")
	}
	nr.Note = strings.TrimSpace(nr.Note)
	if len(nr.Note) > 300 {
		nr.Note = nr.Note[:300]
	}
	if nr.Source == "" {
		nr.Source = "web"
	}

	src := detailsSource(a)
	var det *tmdb.Details
	var err error
	if nr.Type == store.TypeMovie {
		det, err = src.Movie(ctx, nr.TMDBID)
	} else {
		det, err = src.TV(ctx, nr.TMDBID)
	}
	if err != nil {
		return nil, err
	}

	// For TV, work out which seasons are being asked for.
	if nr.Type == store.TypeTV {
		valid := map[int]bool{}
		for _, s := range det.Seasons {
			valid[s.SeasonNumber] = true
		}
		if len(nr.Seasons) == 0 {
			for _, s := range det.Seasons {
				nr.Seasons = append(nr.Seasons, s.SeasonNumber)
			}
		}
		var clean []int
		seen := map[int]bool{}
		for _, s := range nr.Seasons {
			if valid[s] && !seen[s] {
				clean = append(clean, s)
				seen[s] = true
			}
		}
		sort.Ints(clean)
		if len(clean) == 0 {
			return nil, userErr("Pick at least one season.")
		}
		nr.Seasons = clean
	} else {
		nr.Seasons = nil
	}

	if nr.Type == store.TypeTV {
		a.ensureSeasons(ctx, nr.TMDBID)
	}

	var req *store.Request
	var user store.User
	err = a.Store.Update(func(d *store.Data) error {
		u := d.UserByID(userID)
		if u == nil {
			return userErr("Please sign in again.")
		}
		user = *u
		lib, inLib := d.Library[store.LibKey(nr.Type, nr.TMDBID)]
		m := d.MediaByTMDB(nr.Type, nr.TMDBID)

		if nr.Type == store.TypeMovie {
			if inLib || (m != nil && m.Status == store.MediaAvailable) {
				return userErr("This film is already in Plex.")
			}
			if m != nil {
				for _, r := range d.RequestsForMedia(m.ID) {
					if r.Status == store.ReqPending {
						return userErr("Someone has already requested this film: it's waiting for approval.")
					}
					if r.Status == store.ReqApproved {
						return userErr("This film has already been requested and is on its way.")
					}
				}
			}
		} else {
			// Drop seasons that are already in Plex or already requested.
			taken := map[int]bool{}
			if inLib {
				for s, have := range lib.Seasons {
					if want := seasonEps(det, s); want > 0 && have >= want {
						taken[s] = true
					}
				}
			}
			if m != nil {
				for _, r := range d.RequestsForMedia(m.ID) {
					if r.Status == store.ReqPending || r.Status == store.ReqApproved || r.Status == store.ReqCompleted {
						for _, s := range r.Seasons {
							taken[s] = true
						}
					}
				}
			}
			var left []int
			for _, s := range nr.Seasons {
				if !taken[s] {
					left = append(left, s)
				}
			}
			if len(left) == 0 {
				return userErr("Those seasons are already in Plex or already requested.")
			}
			nr.Seasons = left
		}

		// Quotas apply to Friends only.
		if u.Role == store.RoleFriend {
			q := d.Settings.Quotas
			limit := q.FriendMovieLimit
			kind := "films"
			if nr.Type == store.TypeTV {
				limit, kind = q.FriendTVLimit, "shows"
			}
			if limit > 0 && q.Days > 0 {
				since := time.Now().AddDate(0, 0, -q.Days)
				n := 0
				for _, r := range d.Requests {
					if r.RequestedBy == u.ID && r.Type == nr.Type && r.Created.After(since) && r.Status != store.ReqDeclined {
						n++
					}
				}
				if n >= limit {
					return userErr("You've reached your limit of " + itoa(limit) + " " + kind + " every " + itoa(q.Days) + " days. Try again later, or ask an admin.")
				}
			}
		}

		now := time.Now()
		if m == nil {
			m = &store.Media{ID: d.NewID("media"), Type: nr.Type, TMDBID: nr.TMDBID, Status: store.MediaUnknown, Created: now}
			d.Media = append(d.Media, m)
		}
		m.TVDBID, m.IMDBID, m.Title, m.Year, m.Poster = det.TVDBID, det.IMDBID, det.Title, det.Year, det.Poster
		if nr.Type == store.TypeTV {
			m.SeasonEps = airedEps(det.Seasons)
		}
		if inLib {
			m.PlexKey = lib.Key
		}
		m.Updated = now

		req = &store.Request{
			ID: d.NewID("request"), MediaID: m.ID, Type: nr.Type, TMDBID: nr.TMDBID,
			Title: det.Title, Year: det.Year, Poster: det.Poster, Seasons: nr.Seasons,
			Status: store.ReqPending, RequestedBy: u.ID, Source: nr.Source, Note: nr.Note,
			Created: now, Updated: now,
		}
		if autoApproves(u.Role) {
			req.Status = store.ReqApproved
			req.ModifiedBy = u.ID
			if m.Status != store.MediaPartial {
				m.Status = store.MediaProcessing
			}
		} else if m.Status == store.MediaUnknown {
			m.Status = store.MediaPending
		}
		d.Requests = append(d.Requests, req)
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := *req
	msg := notify.Message{Title: out.Title, Year: out.Year, Poster: ImageURL(out.Poster, "w342"),
		RequestedBy: user.Username, Seasons: out.Seasons, Detail: out.Note}
	if out.Status == store.ReqApproved {
		msg.Event = notify.EventAuto
		a.Notify(msg)
		a.sendToArrAsync(out.ID)
	} else {
		msg.Event = notify.EventRequested
		a.Notify(msg)
	}
	a.logf("request #%d: %s %q by %s (%s)", out.ID, out.Type, out.Title, user.Username, out.Status)
	return &out, nil
}

func seasonEps(det *tmdb.Details, season int) int {
	for _, s := range det.Seasons {
		if s.SeasonNumber == season {
			return s.EpisodeCount
		}
	}
	return 0
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// SetStatus approves or declines a pending request (admins only).
func (a *App) SetStatus(reqID, adminID int, approve bool, reason string) (*store.Request, error) {
	var out store.Request
	var requester store.User
	err := a.Store.Update(func(d *store.Data) error {
		r := d.RequestByID(reqID)
		if r == nil {
			return userErr("That request no longer exists.")
		}
		if r.Status != store.ReqPending {
			return userErr("Only requests waiting for approval can be approved or declined.")
		}
		r.ModifiedBy = adminID
		r.Updated = time.Now()
		m := d.MediaByID(r.MediaID)
		if approve {
			r.Status = store.ReqApproved
			if m != nil && m.Status != store.MediaAvailable && m.Status != store.MediaPartial {
				m.Status = store.MediaProcessing
			}
		} else {
			r.Status = store.ReqDeclined
			r.Error = strings.TrimSpace(reason)
			if m != nil && m.Status == store.MediaPending && !hasActive(d, m.ID) {
				m.Status = store.MediaUnknown
			}
		}
		if u := d.UserByID(r.RequestedBy); u != nil {
			requester = *u
		}
		out = *r
		return nil
	})
	if err != nil {
		return nil, err
	}
	msg := notify.Message{Title: out.Title, Year: out.Year, Poster: ImageURL(out.Poster, "w342"),
		RequestedBy: requester.Username, MentionID: requester.DiscordID, Seasons: out.Seasons}
	if approve {
		msg.Event = notify.EventApproved
		a.sendToArrAsync(out.ID)
	} else {
		msg.Event = notify.EventDeclined
		msg.Detail = out.Error
	}
	a.Notify(msg)
	return &out, nil
}

func hasActive(d *store.Data, mediaID int) bool {
	for _, r := range d.RequestsForMedia(mediaID) {
		if r.Status == store.ReqPending || r.Status == store.ReqApproved {
			return true
		}
	}
	return false
}

// Retry resends a failed request.
func (a *App) Retry(reqID int) error {
	err := a.Store.Update(func(d *store.Data) error {
		r := d.RequestByID(reqID)
		if r == nil {
			return userErr("That request no longer exists.")
		}
		if r.Status != store.ReqFailed {
			return userErr("Only failed requests can be retried.")
		}
		r.Status, r.Error, r.Updated = store.ReqApproved, "", time.Now()
		return nil
	})
	if err == nil {
		a.sendToArrAsync(reqID)
	}
	return err
}

// DeleteRequest removes a request. Users may cancel their own pending ones.
func (a *App) DeleteRequest(reqID int, user store.User) error {
	return a.Store.Update(func(d *store.Data) error {
		for i, r := range d.Requests {
			if r.ID != reqID {
				continue
			}
			if user.Role != store.RoleAdmin && (r.RequestedBy != user.ID || r.Status != store.ReqPending) {
				return userErr("You can only cancel your own requests while they're waiting for approval.")
			}
			d.Requests = append(d.Requests[:i], d.Requests[i+1:]...)
			if r.Source == "watchlist" {
				// Removed on purpose: don't let the watchlist sync add it back.
				if d.WatchlistSkip == nil {
					d.WatchlistSkip = map[string]bool{}
				}
				d.WatchlistSkip[store.SkipKey(r.RequestedBy, r.Type, r.TMDBID)] = true
			}
			if m := d.MediaByID(r.MediaID); m != nil && !hasActive(d, m.ID) && (m.Status == store.MediaPending || m.Status == store.MediaProcessing) {
				m.Status = store.MediaUnknown
			}
			return nil
		}
		return userErr("That request no longer exists.")
	})
}

func (a *App) sendToArrAsync(reqID int) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
		defer cancel()
		if err := a.SendToArr(ctx, reqID); err != nil {
			a.logf("request #%d: send failed: %v", reqID, err)
		}
	}()
}

// SendToArr hands an approved request to Radarr or Sonarr.
func (a *App) SendToArr(ctx context.Context, reqID int) error {
	var r store.Request
	var m store.Media
	var u store.User
	var s store.Settings
	found := false
	a.Store.Read(func(d *store.Data) {
		if rr := d.RequestByID(reqID); rr != nil {
			r = *rr
			if mm := d.MediaByID(rr.MediaID); mm != nil {
				m = *mm
				found = true
			}
			if uu := d.UserByID(rr.RequestedBy); uu != nil {
				u = *uu
			}
		}
		s = d.Settings
	})
	if !found || r.Status != store.ReqApproved {
		return nil
	}

	var externalID int
	var sendErr error
	if r.Type == store.TypeMovie {
		c := arrClient("Radarr", s.Radarr)
		if c == nil {
			sendErr = errors.New("Radarr isn't set up yet: add it in Settings, then retry")
		} else {
			var tags []int
			if s.Radarr.TagRequests {
				if id, err := c.EnsureTag(ctx, arr.TagLabel(u.Username)); err == nil {
					tags = append(tags, id)
				}
			}
			mv, err := c.AddMovie(ctx, arr.AddMovieOptions{
				TMDBID: m.TMDBID, Title: m.Title, Year: m.Year,
				QualityProfileID: s.Radarr.QualityProfileID, RootFolder: s.Radarr.RootFolder,
				MinimumAvailability: s.Radarr.MinimumAvailability, Tags: tags, SearchNow: s.Radarr.SearchOnAdd,
			})
			if err != nil {
				sendErr = err
			} else {
				externalID = mv.ID
			}
		}
	} else {
		c := arrClient("Sonarr", s.Sonarr)
		if c == nil {
			sendErr = errors.New("Sonarr isn't set up yet: add it in Settings, then retry")
		} else {
			var tags []int
			if s.Sonarr.TagRequests {
				if id, err := c.EnsureTag(ctx, arr.TagLabel(u.Username)); err == nil {
					tags = append(tags, id)
				}
			}
			sr, err := c.AddSeries(ctx, arr.AddSeriesOptions{
				TVDBID: m.TVDBID, Title: m.Title, QualityProfileID: s.Sonarr.QualityProfileID,
				LanguageProfileID: s.Sonarr.LanguageProfileID, RootFolder: s.Sonarr.RootFolder,
				Seasons: r.Seasons, SeasonFolders: s.Sonarr.SeasonFolders, Tags: tags, SearchNow: s.Sonarr.SearchOnAdd,
			})
			if err != nil {
				sendErr = err
			} else {
				externalID = sr.ID
			}
		}
	}

	_ = a.Store.Update(func(d *store.Data) error {
		rr := d.RequestByID(reqID)
		mm := d.MediaByID(r.MediaID)
		if rr == nil {
			return nil
		}
		rr.Updated = time.Now()
		if sendErr != nil {
			rr.Status = store.ReqFailed
			rr.Error = sendErr.Error()
			return nil
		}
		rr.Error = ""
		if mm != nil && externalID > 0 {
			mm.ExternalID = externalID
		}
		return nil
	})
	if sendErr != nil {
		a.Notify(notify.Message{Event: notify.EventFailed, Title: r.Title, Year: r.Year,
			Poster: ImageURL(r.Poster, "w342"), RequestedBy: u.Username, Seasons: r.Seasons, Detail: sendErr.Error()})
		return sendErr
	}
	a.logf("request #%d sent to %s", reqID, map[string]string{store.TypeMovie: "Radarr", store.TypeTV: "Sonarr"}[r.Type])
	return nil
}

// markAvailable updates media and requests when Plex has something, and
// tells requesters. For TV, seasons maps season number -> episodes in Plex.
func (a *App) markAvailable(typ string, tmdbID int, plexKey string, seasons map[int]int) {
	type ping struct {
		r store.Request
		u store.User
	}
	var pings []ping
	_ = a.Store.Update(func(d *store.Data) error {
		key := store.LibKey(typ, tmdbID)
		e := d.Library[key]
		e.Key = plexKey
		if seasons != nil {
			e.Seasons = seasons
		}
		d.Library[key] = e

		m := d.MediaByTMDB(typ, tmdbID)
		if m == nil {
			return nil
		}
		now := time.Now()
		m.PlexKey = plexKey
		if typ == store.TypeTV && seasons != nil {
			m.SeasonHave = seasons
		}
		full := func(s int) bool {
			if typ == store.TypeMovie {
				return true
			}
			// A season with nothing aired yet has nothing to wait for: it
			// mustn't hold a request open until it airs.
			return m.SeasonHave[s] >= m.SeasonEps[s]
		}
		// Media status.
		if typ == store.TypeMovie {
			m.Status = store.MediaAvailable
		} else {
			all, some := true, false
			for s, want := range m.SeasonEps {
				if want == 0 {
					continue // not aired yet
				}
				if full(s) {
					some = true
				} else {
					all = false
				}
			}
			switch {
			case all && some:
				m.Status = store.MediaAvailable
			case some || len(m.SeasonHave) > 0:
				m.Status = store.MediaPartial
			}
		}
		if m.Status == store.MediaAvailable && m.AvailableAt == nil {
			m.AvailableAt = &now
		}
		m.Updated = now
		// Requests complete when everything they asked for is there.
		for _, r := range d.RequestsForMedia(m.ID) {
			if r.Status != store.ReqApproved {
				continue
			}
			done := true
			for _, s := range r.Seasons {
				if !full(s) {
					done = false
				}
			}
			if done {
				r.Status = store.ReqCompleted
				r.Updated = now
				p := ping{r: *r}
				if u := d.UserByID(r.RequestedBy); u != nil {
					p.u = *u
				}
				pings = append(pings, p)
			}
		}
		return nil
	})
	for _, p := range pings {
		a.Notify(notify.Message{Event: notify.EventAvailable, Title: p.r.Title, Year: p.r.Year,
			Poster: ImageURL(p.r.Poster, "w342"), RequestedBy: p.u.Username, MentionID: p.u.DiscordID, Seasons: p.r.Seasons})
		a.logf("request #%d complete: %q is in Plex", p.r.ID, p.r.Title)
	}
}

// ensureSeasons asks Plex for a show's seasons when it's in the library but
// hasn't been checked in detail yet, so already-downloaded seasons show as
// available and can't be requested again.
func (a *App) ensureSeasons(ctx context.Context, tmdbID int) {
	var key string
	var known bool
	a.Store.Read(func(d *store.Data) {
		if e, ok := d.Library[store.LibKey(store.TypeTV, tmdbID)]; ok {
			key, known = e.Key, e.Seasons != nil
		}
	})
	if key == "" || known {
		return
	}
	srv := a.PlexServer()
	if srv == nil {
		return
	}
	if ss, err := srv.Seasons(ctx, key); err == nil {
		a.markAvailable(store.TypeTV, tmdbID, key, seasonCounts(ss))
	}
}

// airedEps maps season -> episodes aired so far, which is what Plex can
// actually have.
func airedEps(seasons []tmdb.Season) map[int]int {
	out := map[int]int{}
	for _, s := range seasons {
		out[s.SeasonNumber] = s.Aired
	}
	return out
}
