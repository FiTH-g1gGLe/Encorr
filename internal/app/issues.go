package app

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/notify"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
	"github.com/FiTH-g1gGLe/encorr/internal/tmdb"
)

// issueKinds are the problems people can report, in plain words.
var issueKinds = map[string]string{
	"play":      "Won't play",
	"video":     "Poor picture",
	"audio":     "Sound problem",
	"subtitles": "Subtitles missing or wrong",
	"wrong":     "Wrong film or episode",
	"other":     "Something else",
}

type issueOut struct {
	*store.Issue
	KindName   string `json:"kindName"`
	Reporter   string `json:"reporter"`
	Mine       bool   `json:"mine"`
	CanReplace bool   `json:"canReplace"`
}

func issueScope(i *store.Issue) string {
	switch {
	case i.Type != store.TypeTV || i.Season == 0:
		return ""
	case i.Episode == 0:
		return fmt.Sprintf("Season %d", i.Season)
	}
	return fmt.Sprintf("S%02dE%02d", i.Season, i.Episode)
}

func issueLabel(i *store.Issue) string {
	if s := issueScope(i); s != "" {
		return i.Title + " (" + s + ")"
	}
	return i.Title
}

// hReportIssue lets anyone report a problem with something in the library.
func (a *App) hReportIssue(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Type    string `json:"type"`
		TMDBID  int    `json:"tmdbId"`
		Season  int    `json:"season"`
		Episode int    `json:"episode"`
		Kind    string `json:"kind"`
		Note    string `json:"note"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.Type != store.TypeMovie && body.Type != store.TypeTV || body.TMDBID <= 0 {
		fail(w, userErr("Choose a film or a TV show."))
		return
	}
	if issueKinds[body.Kind] == "" {
		fail(w, userErr("Pick what's wrong."))
		return
	}
	body.Note = strings.TrimSpace(body.Note)
	if len(body.Note) > 500 {
		body.Note = body.Note[:500]
	}
	if body.Kind == "other" && body.Note == "" {
		fail(w, userErr("Say what's wrong in the note."))
		return
	}
	switch {
	case body.Type == store.TypeMovie:
		body.Season, body.Episode = 0, 0
	case body.Season < 0 || body.Episode < 0 || body.Season > 200 || body.Episode > 1000:
		fail(w, userErr("That season or episode doesn't look right."))
		return
	case body.Season == 0:
		body.Episode = 0
	}
	lib := a.libName()
	inLib := false
	a.Store.Read(func(d *store.Data) {
		e, ok := d.Library[store.LibKey(body.Type, body.TMDBID)]
		inLib = ok && !e.Empty()
	})
	if !inLib {
		fail(w, userErr("That isn't in "+lib+", so there's nothing to report yet."))
		return
	}
	src := detailsSource(a)
	var det *tmdb.Details
	var err error
	if body.Type == store.TypeTV {
		det, err = src.TV(r.Context(), body.TMDBID)
	} else {
		det, err = src.Movie(r.Context(), body.TMDBID)
	}
	if err != nil {
		fail(w, err)
		return
	}
	var is *store.Issue
	var reporter store.User
	err = a.Store.Update(func(d *store.Data) error {
		me := d.UserByID(u.ID)
		if me == nil {
			return userErr("Please sign in again.")
		}
		reporter = *me
		e, ok := d.Library[store.LibKey(body.Type, body.TMDBID)]
		if !ok || e.Empty() {
			return userErr("That isn't in " + lib + ", so there's nothing to report yet.")
		}
		open := 0
		for _, x := range d.Issues {
			if x.Status == store.IssueResolved || x.ReportedBy != u.ID {
				continue
			}
			open++
			if x.Type == body.Type && x.TMDBID == body.TMDBID && x.Season == body.Season && x.Episode == body.Episode {
				return userErr("You've already reported this. The owner will look at it.")
			}
		}
		if open >= 10 && me.Role != store.RoleAdmin {
			return userErr("You have 10 problems waiting already. Give the owner a chance to catch up.")
		}
		now := time.Now()
		is = &store.Issue{ID: d.NewID("issue"), Type: body.Type, TMDBID: body.TMDBID, TVDBID: det.TVDBID,
			Title: det.Title, Year: det.Year, Poster: det.Poster, Season: body.Season, Episode: body.Episode,
			Kind: body.Kind, Note: body.Note, Status: store.IssueOpen, ReportedBy: u.ID, Created: now}
		is.Log("reported", issueKinds[body.Kind]+" (reported by "+me.Username+")")
		d.Issues = append(d.Issues, is)
		if len(d.Issues) > 500 {
			d.Issues = d.Issues[len(d.Issues)-500:]
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	detail := reporter.Username + " says: " + issueKinds[is.Kind]
	if is.Note != "" {
		detail += ". \"" + is.Note + "\""
	}
	a.activity("warn", "Problem reported: "+issueLabel(is), detail)
	a.Notify(notify.Message{Event: notify.EventReported, Title: issueLabel(is), Year: is.Year, Poster: ImageURL(is.Poster, "w342"),
		Detail: detail + ". Look at it in Encorr → Requests."})
	writeJSON(w, 200, is)
}

// hIssues lists problems: everything for the owner, your own otherwise.
// Sorted problems stay listed for a week.
func (a *App) hIssues(w http.ResponseWriter, r *http.Request, u *store.User) {
	admin := u.Role == store.RoleAdmin
	canReplace := map[string]bool{store.TypeMovie: a.Radarr() != nil, store.TypeTV: a.Sonarr() != nil}
	out := []issueOut{}
	open := 0
	a.Store.Read(func(d *store.Data) {
		for _, x := range d.Issues {
			if !admin && x.ReportedBy != u.ID {
				continue
			}
			if x.Status != store.IssueResolved {
				open++
			} else if time.Since(x.Updated) > 7*24*time.Hour {
				continue
			}
			name := ""
			if rep := d.UserByID(x.ReportedBy); rep != nil {
				name = rep.Username
			}
			out = append(out, issueOut{Issue: x, KindName: issueKinds[x.Kind], Reporter: name, Mine: x.ReportedBy == u.ID,
				CanReplace: admin && x.Status == store.IssueOpen && canReplace[x.Type]})
		}
	})
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := out[i].Status == store.IssueResolved, out[j].Status == store.IssueResolved
		if ri != rj {
			return !ri
		}
		return out[i].Created.After(out[j].Created)
	})
	writeJSON(w, 200, map[string]any{"results": out, "open": open})
}

// hReplaceIssue asks Radarr/Sonarr for a different copy: the bad release is
// blocklisted, the file deleted and a new search started.
func (a *App) hReplaceIssue(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := pathInt(r, "id")
	var is store.Issue
	found := false
	a.Store.Read(func(d *store.Data) {
		for _, x := range d.Issues {
			if x.ID == id {
				is, found = *x, true
			}
		}
	})
	if !found {
		fail(w, userErr("That problem isn't in the list any more."))
		return
	}
	if is.Status != store.IssueOpen {
		fail(w, userErr("A new copy has already been asked for, or it's sorted."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	app, arrID, what, err := a.replaceCopy(ctx, &is)
	if err != nil {
		fail(w, err)
		return
	}
	_ = a.Store.Update(func(d *store.Data) error {
		for _, x := range d.Issues {
			if x.ID == id {
				x.Status, x.ArrApp, x.ArrID = store.IssueReplacing, app, arrID
				x.Log("action", what+" ("+u.Username+")")
			}
		}
		return nil
	})
	a.activity("action", "Getting a new copy: "+issueLabel(&is), what)
	writeJSON(w, 200, map[string]any{"ok": true, "message": what})
}

// replaceCopy does the Radarr/Sonarr side of a replacement and says what
// happened in plain words.
func (a *App) replaceCopy(ctx context.Context, is *store.Issue) (string, int, string, error) {
	if is.Type == store.TypeMovie {
		c := a.Radarr()
		if c == nil {
			return "", 0, "", userErr("Connect Radarr to get a new copy.")
		}
		m, err := c.MovieByTMDB(ctx, is.TMDBID)
		if err != nil {
			return "", 0, "", err
		}
		if m == nil || m.ID == 0 {
			return "", 0, "", userErr("Radarr doesn't have this film, so it can't replace it. Sort it out in Plex instead.")
		}
		bl, err := c.ReplaceMovie(ctx, m.ID)
		if err != nil {
			return "", 0, "", err
		}
		msg := "Deleted the old copy and asked Radarr for a different one"
		if bl {
			msg += " (the old release is blocklisted so it won't come back)"
		}
		return "radarr", m.ID, msg, nil
	}
	c := a.Sonarr()
	if c == nil {
		return "", 0, "", userErr("Connect Sonarr to get a new copy.")
	}
	tvdb := is.TVDBID
	if tvdb == 0 {
		a.Store.Read(func(d *store.Data) {
			if m := d.MediaByTMDB(store.TypeTV, is.TMDBID); m != nil {
				tvdb = m.TVDBID
			}
		})
	}
	if tvdb == 0 {
		return "", 0, "", userErr("Couldn't match this show in Sonarr.")
	}
	s, err := c.SeriesByTVDB(ctx, tvdb)
	if err != nil {
		return "", 0, "", err
	}
	if s == nil || s.ID == 0 {
		return "", 0, "", userErr("Sonarr doesn't have this show, so it can't replace it.")
	}
	if is.Season == 0 {
		return "", 0, "", userErr("Pick a season to replace: replacing a whole show at once is too much. Ask them which season, or do it in Sonarr.")
	}
	eps, err := c.EpisodeDetails(ctx, s.ID)
	if err != nil {
		return "", 0, "", err
	}
	var ids []int
	for _, e := range eps {
		if e.SeasonNumber == is.Season && (is.Episode == 0 || e.EpisodeNumber == is.Episode) && e.HasFile {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) == 0 {
		return "", 0, "", userErr("Sonarr has no files for " + issueScope(is) + ", so there's nothing to replace.")
	}
	bl, err := c.ReplaceEpisodes(ctx, s.ID, ids)
	if err != nil {
		return "", 0, "", err
	}
	n := "episode"
	if len(ids) > 1 {
		n = fmt.Sprintf("%d episodes", len(ids))
	}
	msg := "Deleted the old " + n + " and asked Sonarr for a different copy"
	if bl > 0 {
		msg += " (the old release is blocklisted so it won't come back)"
	}
	return "sonarr", s.ID, msg, nil
}

// hResolveIssue marks a problem as sorted, with an optional reply. The owner
// can close any; the person who reported it can close their own.
func (a *App) hResolveIssue(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Reply string `json:"reply"`
	}
	_ = readJSON(r, &body)
	body.Reply = strings.TrimSpace(body.Reply)
	if len(body.Reply) > 300 {
		body.Reply = body.Reply[:300]
	}
	id := pathInt(r, "id")
	var is store.Issue
	var rep store.User
	err := a.Store.Update(func(d *store.Data) error {
		for _, x := range d.Issues {
			if x.ID != id {
				continue
			}
			if u.Role != store.RoleAdmin && x.ReportedBy != u.ID {
				return userErr("Only the owner can do that.")
			}
			if x.Status == store.IssueResolved {
				return userErr("It's already sorted.")
			}
			x.Status = store.IssueResolved
			text := "Marked as sorted by " + u.Username
			if body.Reply != "" {
				text += ": " + body.Reply
			}
			x.Log("ready", text)
			is = *x
			if p := d.UserByID(x.ReportedBy); p != nil {
				rep = *p
			}
			return nil
		}
		return userErr("That problem isn't in the list any more.")
	})
	if err != nil {
		fail(w, err)
		return
	}
	if rep.ID != 0 && rep.ID != u.ID {
		detail := "The problem " + rep.Username + " reported is sorted."
		if body.Reply != "" {
			detail += " " + u.Username + " says: \"" + body.Reply + "\""
		}
		a.Notify(notify.Message{Event: notify.EventResolved, Title: issueLabel(&is), Year: is.Year, Poster: ImageURL(is.Poster, "w342"),
			MentionID: rep.DiscordID, Detail: detail})
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// issuesArrived closes problems whose replacement copy has just been
// imported.
func (a *App) issuesArrived(app string, arrID int) {
	var done []store.Issue
	var people []store.User
	lib := a.libName()
	_ = a.Store.Update(func(d *store.Data) error {
		for _, x := range d.Issues {
			if x.Status == store.IssueReplacing && x.ArrApp == app && x.ArrID == arrID {
				x.Status = store.IssueResolved
				x.Log("ready", "The new copy is in "+lib)
				done = append(done, *x)
				var p store.User
				if u := d.UserByID(x.ReportedBy); u != nil {
					p = *u
				}
				people = append(people, p)
			}
		}
		return nil
	})
	for i, is := range done {
		a.Notify(notify.Message{Event: notify.EventResolved, Title: issueLabel(&is), Year: is.Year, Poster: ImageURL(is.Poster, "w342"),
			MentionID: people[i].DiscordID, Detail: "A new copy is in " + lib + ". Give it another go."})
	}
}
