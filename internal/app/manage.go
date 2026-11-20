package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// ---------- activity log ----------

// activity records something Encorr did to keep downloads moving, on the
// Health page and (when it's about a request) on that request's timeline.
func (a *App) activity(kind, title, detail string, reqIDs ...int) {
	_ = a.Store.Update(func(d *store.Data) error {
		rid := 0
		if len(reqIDs) > 0 {
			rid = reqIDs[0]
		}
		d.Activity = append(d.Activity, store.Activity{At: time.Now(), Kind: kind, Title: title, Detail: detail, RequestID: rid})
		if len(d.Activity) > 200 {
			d.Activity = d.Activity[len(d.Activity)-200:]
		}
		text := title
		if detail != "" {
			text += ": " + detail
		}
		for _, id := range reqIDs {
			if r := d.RequestByID(id); r != nil {
				r.Log(kind, text)
			}
		}
		return nil
	})
	a.logf("%s: %s %s", kind, title, detail)
}

// openRequestsFor finds the open requests behind a Radarr/Sonarr item.
func (a *App) openRequestsFor(svc string, externalID int) (ids []int, title string) {
	typ := store.TypeMovie
	if svc == "sonarr" {
		typ = store.TypeTV
	}
	a.Store.Read(func(d *store.Data) {
		for _, m := range d.Media {
			if m.Type != typ || m.ExternalID != externalID {
				continue
			}
			title = m.Title
			for _, r := range d.RequestsForMedia(m.ID) {
				if r.Status == store.ReqApproved {
					ids = append(ids, r.ID)
				}
			}
		}
	})
	return ids, title
}

// requestInfo copies a request and its media out of the store.
func (a *App) requestInfo(id int) (store.Request, store.Media, bool) {
	var rq store.Request
	var m store.Media
	ok := false
	a.Store.Read(func(d *store.Data) {
		if r := d.RequestByID(id); r != nil {
			rq, ok = *r, true
			if mm := d.MediaByID(r.MediaID); mm != nil {
				m = *mm
			}
		}
	})
	return rq, m, ok
}

func canSee(u *store.User, rq store.Request) bool {
	return u.Role == store.RoleAdmin || rq.RequestedBy == u.ID
}

// ---------- episodes ----------

type episodeOut struct {
	ID      int        `json:"id"`
	Number  int        `json:"number"`
	Title   string     `json:"title"`
	Air     *time.Time `json:"air,omitempty"`
	Status  string     `json:"status"` // have | downloading | missing | unaired | skipped
	Quality string     `json:"quality,omitempty"`
	Size    float64    `json:"size,omitempty"`
	Percent int        `json:"percent,omitempty"`
	Stage   string     `json:"stage,omitempty"`
}

type seasonEpisodes struct {
	Season      int          `json:"season"`
	Total       int          `json:"total"`
	Aired       int          `json:"aired"`
	Have        int          `json:"have"`
	Downloading int          `json:"downloading"`
	Missing     int          `json:"missing"`
	Episodes    []episodeOut `json:"episodes"`
}

// hEpisodes shows every episode of the requested seasons: in Plex,
// downloading, missing or not aired yet. It asks Sonarr, which knows.
func (a *App) hEpisodes(w http.ResponseWriter, r *http.Request, u *store.User) {
	rq, m, ok := a.requestInfo(pathInt(r, "id"))
	if !ok || !canSee(u, rq) {
		writeErr(w, 404, "Request not found.")
		return
	}
	if rq.Type != store.TypeTV {
		writeErr(w, 400, "Only shows have episodes.")
		return
	}
	c := a.Sonarr()
	if c == nil || m.ExternalID == 0 {
		writeJSON(w, 200, map[string]any{"seasons": []any{}, "message": "Sonarr hasn't got this show yet."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	eps, err := c.EpisodeDetails(ctx, m.ExternalID)
	if err != nil {
		fail(w, err)
		return
	}
	files, _ := c.EpisodeFiles(ctx, m.ExternalID)
	fq := map[int]arr.EpisodeFile{}
	for _, f := range files {
		fq[f.ID] = f
	}
	type dl struct {
		pct   int
		stage string
	}
	downloading := map[int]dl{}
	a.mu.Lock()
	snap := a.sabSnap
	for _, it := range a.downloads["sonarr"][m.ExternalID] {
		pct := 0
		if it.Size > 0 {
			pct = int((it.Size - it.SizeLeft) / it.Size * 100)
		}
		stage := "Downloading"
		if snap != nil {
			if sl, ok := snap.Slots[strings.ToLower(it.DownloadID)]; ok {
				pct = int(sl.Percent)
				switch {
				case sl.PostProc:
					stage = "Unpacking"
				case sl.Status == "Paused":
					stage = "Paused"
				case sl.Status == "Queued":
					stage = "Queued"
				}
			}
		}
		if it.Blocked() {
			stage = "Needs importing"
		}
		downloading[it.EpisodeID] = dl{pct, stage}
	}
	a.mu.Unlock()
	want := map[int]bool{}
	for _, s := range rq.Seasons {
		want[s] = true
	}
	now := time.Now()
	bySeason := map[int]*seasonEpisodes{}
	for _, e := range eps {
		if !want[e.SeasonNumber] {
			continue
		}
		se := bySeason[e.SeasonNumber]
		if se == nil {
			se = &seasonEpisodes{Season: e.SeasonNumber}
			bySeason[e.SeasonNumber] = se
		}
		eo := episodeOut{ID: e.ID, Number: e.EpisodeNumber, Title: e.Title}
		aired := !e.AirDateUTC.IsZero() && e.AirDateUTC.Before(now)
		if !e.AirDateUTC.IsZero() {
			t := e.AirDateUTC
			eo.Air = &t
		}
		se.Total++
		if aired {
			se.Aired++
		}
		if d, ok := downloading[e.ID]; ok && !e.HasFile {
			eo.Status, eo.Percent, eo.Stage = "downloading", d.pct, d.stage
			se.Downloading++
		} else if e.HasFile {
			eo.Status = "have"
			if f, ok := fq[e.EpisodeFileID]; ok {
				eo.Quality, eo.Size = f.Quality.Quality.Name, f.Size
			}
			se.Have++
		} else if !aired {
			eo.Status = "unaired"
		} else if !e.Monitored {
			eo.Status = "skipped"
		} else {
			eo.Status = "missing"
			se.Missing++
		}
		se.Episodes = append(se.Episodes, eo)
	}
	out := []seasonEpisodes{}
	for _, s := range rq.Seasons {
		if se := bySeason[s]; se != nil {
			sort.Slice(se.Episodes, func(i, j int) bool { return se.Episodes[i].Number < se.Episodes[j].Number })
			out = append(out, *se)
		}
	}
	writeJSON(w, 200, map[string]any{"seasons": out})
}

// ---------- timeline ----------

type timelineItem struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
	Detail string    `json:"detail,omitempty"`
}

// hTimeline tells the story of a request: Encorr's own events plus what
// Radarr/Sonarr did (found a copy, failed, imported, upgraded).
func (a *App) hTimeline(w http.ResponseWriter, r *http.Request, u *store.User) {
	rq, m, ok := a.requestInfo(pathInt(r, "id"))
	if !ok || !canSee(u, rq) {
		writeErr(w, 404, "Request not found.")
		return
	}
	var items []timelineItem
	for _, e := range rq.Events {
		items = append(items, timelineItem{At: e.At, Kind: e.Kind, Text: e.Text})
	}
	if len(rq.Events) == 0 {
		items = append(items, timelineItem{At: rq.Created, Kind: "requested", Text: "Requested"})
	}
	if m.ExternalID != 0 {
		c, kind := a.Radarr(), "movie"
		if rq.Type == store.TypeTV {
			c, kind = a.Sonarr(), "tv"
		}
		if c != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			hist, err := c.History(ctx, kind, m.ExternalID)
			cancel()
			if err == nil {
				items = append(items, historyLines(hist, rq)...)
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.After(items[j].At) })
	if len(items) > 80 {
		items = items[:80]
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// historyLines turns Radarr/Sonarr history into plain timeline lines. Runs
// of the same thing for a show ("imported" x8) become one line.
func historyLines(hist []arr.HistoryItem, rq store.Request) []timelineItem {
	want := map[int]bool{}
	for _, s := range rq.Seasons {
		want[s] = true
	}
	sort.Slice(hist, func(i, j int) bool { return hist[i].Date.After(hist[j].Date) })
	var out []timelineItem
	type run struct {
		item timelineItem
		eps  []string
		n    int
	}
	var cur *run
	flush := func() {
		if cur == nil {
			return
		}
		it := cur.item
		if cur.n > 1 {
			it.Text = fmt.Sprintf("%s (%d episodes)", it.Text, cur.n)
			if len(cur.eps) > 0 {
				it.Detail = strings.Join(cur.eps, ", ")
				if len(it.Detail) > 160 {
					it.Detail = it.Detail[:157] + "…"
				}
			}
		}
		out = append(out, it)
		cur = nil
	}
	for _, h := range hist {
		if rq.Type == store.TypeTV && h.Episode != nil && len(want) > 0 && !want[h.Episode.SeasonNumber] {
			continue
		}
		var kind, text, detail string
		switch h.EventType {
		case "grabbed":
			kind, text, detail = "grabbed", "Found a copy and started downloading", h.SourceTitle
			if ix := h.Data["indexer"]; ix != "" {
				detail += " · from " + ix
			}
		case "downloadFailed":
			kind, text, detail = "failed", "Download failed", strings.TrimSpace(h.Data["message"]+" "+h.SourceTitle)
		case "downloadFolderImported", "movieFileImported", "episodeFileImported":
			kind, text, detail = "imported", "Added to your library", h.SourceTitle
		case "movieFileDeleted", "episodeFileDeleted":
			if strings.EqualFold(h.Data["reason"], "Upgrade") {
				kind, text = "upgrade", "Swapped for a better copy"
			} else {
				kind, text = "removed", "File removed"
				if reason := h.Data["reason"]; reason != "" {
					detail = reason
				}
			}
		case "downloadIgnored":
			kind, text, detail = "removed", "Download ignored", h.SourceTitle
		default:
			continue
		}
		ep := ""
		if h.Episode != nil {
			ep = fmt.Sprintf("S%dE%d", h.Episode.SeasonNumber, h.Episode.EpisodeNumber)
		}
		if cur != nil && cur.item.Kind == kind && cur.item.At.Sub(h.Date) < 20*time.Minute && rq.Type == store.TypeTV {
			cur.n++
			if ep != "" {
				cur.eps = append(cur.eps, ep)
			}
			continue
		}
		flush()
		if ep != "" && rq.Type == store.TypeTV {
			text += " · " + ep
		}
		cur = &run{item: timelineItem{At: h.Date, Kind: kind, Text: text, Detail: detail}, n: 1}
		if ep != "" {
			cur.eps = []string{ep}
		}
	}
	flush()
	// Grouped runs carry "· S2E8" from their first item: drop it.
	for i := range out {
		if strings.Contains(out[i].Text, " episodes)") {
			if k := strings.Index(out[i].Text, " · S"); k > 0 {
				j := strings.Index(out[i].Text, " (")
				out[i].Text = out[i].Text[:k] + out[i].Text[j:]
			}
		}
	}
	return out
}

// ---------- download controls ----------

// queueFor copies the live queue items behind a request.
func (a *App) queueFor(svc string, externalID int) []arr.QueueItem {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]arr.QueueItem(nil), a.downloads[svc][externalID]...)
}

// byDownload keeps one queue item per actual download (a season pack shows
// once per episode in Sonarr's queue).
func byDownload(items []arr.QueueItem) []arr.QueueItem {
	seen := map[string]bool{}
	var out []arr.QueueItem
	for _, it := range items {
		k := it.DownloadID
		if k == "" {
			k = "q" + strconv.Itoa(it.ID)
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, it)
		}
	}
	return out
}

var downloadActions = map[string]string{
	"retry":  "Removed the download, marked that copy as bad and asked for another",
	"stop":   "Stopped the download",
	"pause":  "Paused the download",
	"resume": "Resumed the download",
	"top":    "Moved the download to the front of the queue",
}

// hDownload controls a request's live download: retry with another copy,
// stop, pause, resume or move it to the front.
func (a *App) hDownload(w http.ResponseWriter, r *http.Request, u *store.User) {
	reqID := pathInt(r, "id")
	act := r.PathValue("action")
	label, known := downloadActions[act]
	if !known {
		writeErr(w, 400, "Unknown action.")
		return
	}
	svc, c, ext, err := a.arrFor(reqID)
	if err != nil {
		fail(w, err)
		return
	}
	items := byDownload(a.queueFor(svc, ext))
	if len(items) == 0 {
		writeErr(w, 400, "Nothing is downloading for this right now.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var errs []string
	done := 0
	switch act {
	case "retry", "stop":
		for _, it := range items {
			if err := c.RemoveFromQueue(ctx, it.ID, act == "retry", act == "retry"); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			done++
		}
	default:
		s := a.SAB()
		if s == nil {
			writeErr(w, 400, "Pause, resume and move to front need SABnzbd set up in Settings.")
			return
		}
		for _, it := range items {
			if it.DownloadID == "" {
				continue
			}
			var err error
			switch act {
			case "pause":
				err = s.Pause(ctx, it.DownloadID)
			case "resume":
				err = s.Resume(ctx, it.DownloadID)
			case "top":
				err = s.MoveToTop(ctx, it.DownloadID)
			}
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			done++
		}
	}
	if done == 0 && len(errs) > 0 {
		writeErr(w, 400, errs[0])
		return
	}
	a.activity("action", label, "by "+u.Username, reqID)
	go func() { _ = a.RunJob("downloads") }()
	writeJSON(w, 200, map[string]any{"ok": true, "message": label})
}

type releaseOut struct {
	GUID       string   `json:"guid"`
	IndexerID  int      `json:"indexerId"`
	Indexer    string   `json:"indexer"`
	Title      string   `json:"title"`
	Size       float64  `json:"size"`
	Age        string   `json:"age"`
	Quality    string   `json:"quality"`
	Approved   bool     `json:"approved"`
	Rejections []string `json:"rejections,omitempty"`
	FullSeason bool     `json:"fullSeason,omitempty"`
	Languages  []string `json:"languages,omitempty"`
}

func ageText(h float64) string {
	switch {
	case h < 1:
		return "under an hour"
	case h < 48:
		return fmt.Sprintf("%.0f hours", h)
	case h < 24*60:
		return fmt.Sprintf("%.0f days", h/24)
	case h < 24*365*2:
		return fmt.Sprintf("%.0f months", h/24/30)
	}
	return fmt.Sprintf("%.0f years", h/24/365)
}

// hReleases runs an interactive search so an admin can pick a copy.
func (a *App) hReleases(w http.ResponseWriter, r *http.Request, u *store.User) {
	reqID := pathInt(r, "id")
	svc, c, ext, err := a.arrFor(reqID)
	if err != nil {
		fail(w, err)
		return
	}
	kind, season, episode := "movie", 0, 0
	if svc == "sonarr" {
		kind = "tv"
		episode, _ = strconv.Atoi(r.URL.Query().Get("episode"))
		season, _ = strconv.Atoi(r.URL.Query().Get("season"))
		if season == 0 {
			if rq, _, ok := a.requestInfo(reqID); ok && len(rq.Seasons) > 0 {
				season = rq.Seasons[0]
			}
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if episode > 0 { // make sure the episode belongs to this show
		eps, err := c.EpisodeDetails(ctx, ext)
		if err != nil {
			fail(w, err)
			return
		}
		found := false
		for _, e := range eps {
			found = found || e.ID == episode
		}
		if !found {
			fail(w, userErr("That episode isn't part of this show."))
			return
		}
	}
	list, err := c.ReleasesFor(ctx, kind, ext, season, episode)
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]releaseOut, 0, len(list))
	for _, x := range list {
		ro := releaseOut{GUID: x.GUID, IndexerID: x.IndexerID, Indexer: x.Indexer, Title: x.Title, Size: x.Size,
			Age: ageText(x.AgeHours), Quality: x.Quality.Quality.Name, Approved: x.Approved && !x.Rejected, FullSeason: x.FullSeason}
		if len(x.Rejections) > 3 {
			ro.Rejections = x.Rejections[:3]
		} else {
			ro.Rejections = x.Rejections
		}
		for _, l := range x.Languages {
			if l.Name != "" && l.Name != "English" {
				ro.Languages = append(ro.Languages, l.Name)
			}
		}
		out = append(out, ro)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Approved && !out[j].Approved })
	if len(out) > 100 {
		out = out[:100]
	}
	writeJSON(w, 200, map[string]any{"releases": out, "season": season, "episode": episode})
}

// hGrab downloads the copy an admin picked.
func (a *App) hGrab(w http.ResponseWriter, r *http.Request, u *store.User) {
	reqID := pathInt(r, "id")
	var body struct {
		GUID      string `json:"guid"`
		IndexerID int    `json:"indexerId"`
		Title     string `json:"title"`
	}
	if err := readJSON(r, &body); err != nil || body.GUID == "" {
		writeErr(w, 400, "Pick a release first.")
		return
	}
	_, c, _, err := a.arrFor(reqID)
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := c.Grab(ctx, body.GUID, body.IndexerID); err != nil {
		fail(w, err)
		return
	}
	a.activity("action", "Picked a copy by hand", body.Title, reqID)
	go func() { time.Sleep(3 * time.Second); _ = a.RunJob("downloads") }()
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Downloading that copy now"})
}

// ---------- health actions ----------

// hHealthAction runs one of the fix buttons on the Health page.
func (a *App) hHealthAction(w http.ResponseWriter, r *http.Request, u *store.User) {
	act := r.PathValue("action")
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	msg := ""
	var err error
	s := a.SAB()
	needSAB := func() bool {
		if s == nil {
			err = errors.New("SABnzbd isn't set up in Settings")
			return false
		}
		return true
	}
	switch act {
	case "sab-pause":
		if needSAB() {
			err, msg = s.PauseAll(ctx), "Paused all downloads"
		}
	case "sab-resume":
		if needSAB() {
			err, msg = s.ResumeAll(ctx), "Resumed downloads"
		}
	case "sab-restart":
		if needSAB() {
			err, msg = s.Restart(ctx), "Restarting SABnzbd. It'll be back in about a minute"
		}
	case "sab-orphans":
		if needSAB() {
			err, msg = s.DeleteOrphans(ctx), "Cleared SABnzbd's leftover folders"
		}
	case "sab-warnings":
		if needSAB() {
			err, msg = s.ClearWarnings(ctx), "Cleared SABnzbd's warnings"
		}
	case "autofix":
		var n int
		n, err = a.autoFix(ctx, true)
		msg = fmt.Sprintf("Checked every download: %d fixed", n)
	case "search-missing":
		var asked []string
		if c := a.Sonarr(); c != nil {
			if e := c.Command(ctx, map[string]any{"name": "MissingEpisodeSearch", "monitored": true}); e == nil {
				asked = append(asked, "Sonarr")
			} else {
				err = e
			}
		}
		if c := a.Radarr(); c != nil {
			if e := c.Command(ctx, map[string]any{"name": "MissingMoviesSearch"}); e == nil {
				asked = append(asked, "Radarr")
			} else {
				err = e
			}
		}
		msg = "Searching for everything missing in " + strings.Join(asked, " and ")
	default:
		writeErr(w, 400, "Unknown action.")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	if act != "autofix" {
		a.activity("action", msg, "by "+u.Username)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg})
}

// hActivity lists recent auto-fixes and actions, newest first.
// LogPath is Encorr's log file, set by main so the owner can clear it.
var LogPath string

// hClearActivity empties the activity list and the log file.
func (a *App) hClearActivity(w http.ResponseWriter, r *http.Request, u *store.User) {
	_ = a.Store.Update(func(d *store.Data) error {
		d.Activity = nil
		return nil
	})
	if LogPath != "" {
		if err := os.Truncate(LogPath, 0); err != nil {
			fail(w, userErr("Cleared the activity list, but couldn't empty the log file: "+err.Error()))
			return
		}
	}
	a.logf("%s cleared the logs", u.Username)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hActivity(w http.ResponseWriter, r *http.Request, u *store.User) {
	var out []store.Activity
	a.Store.Read(func(d *store.Data) {
		for i := len(d.Activity) - 1; i >= 0 && len(out) < 60; i-- {
			out = append(out, d.Activity[i])
		}
	})
	if out == nil {
		out = []store.Activity{}
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
