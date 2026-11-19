package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/notify"
)

// How long a download can make no progress before Encorr swaps it.
const (
	stallAfter      = 60 * time.Minute
	failedAfter     = 15 * time.Minute
	importRetryWait = 15 * time.Minute
	orphanEvery     = 6 * time.Hour
)

// fixState is what Encorr remembers about one download between checks.
type fixState struct {
	seen        time.Time
	left        float64
	moved       time.Time
	importTries int
	lastImport  time.Time
	warned      bool
	gone        bool
}

// autoFix sorts out the usual download problems by itself:
//   - finished, but Radarr/Sonarr want someone to confirm the import → import it
//   - the download failed → remove it, block that copy and look for another
//   - no progress for an hour → same: swap it for another copy
//   - SABnzbd has leftover folders from old jobs → clear them
//
// force runs it even when auto-fix is switched off (the Health page button).
// It returns how many things it fixed.
func (a *App) autoFix(ctx context.Context, force bool) (int, error) {
	if !force && !a.settings().AutoFixOn() {
		return 0, nil
	}
	now := time.Now()
	fixed := 0
	a.mu.Lock()
	if a.fixes == nil {
		a.fixes = map[string]*fixState{}
	}
	for _, st := range a.fixes {
		st.gone = true
	}
	snap := a.sabSnap
	type todo struct {
		svc  string
		ext  int
		item arr.QueueItem
	}
	var work []todo
	for svc, byID := range a.downloads {
		for ext, items := range byID {
			for _, it := range byDownload(items) {
				work = append(work, todo{svc, ext, it})
			}
		}
	}
	a.mu.Unlock()

	// First pass: note what moved since last time.
	type seen struct {
		todo
		key       string
		st        *fixState
		stIn      fixState
		sabStatus string
		postProc  bool
	}
	var items []seen
	a.mu.Lock()
	for _, w := range work {
		it := w.item
		key := w.svc + ":" + it.DownloadID
		if it.DownloadID == "" {
			key = fmt.Sprintf("%s:q%d", w.svc, it.ID)
		}
		st := a.fixes[key]
		if st == nil {
			st = &fixState{seen: now, left: it.SizeLeft, moved: now}
			a.fixes[key] = st
		}
		st.gone = false
		left := it.SizeLeft
		sabStatus, postProc := "", false
		if snap != nil {
			if sl, ok := snap.Slots[strings.ToLower(it.DownloadID)]; ok {
				left, sabStatus, postProc = sl.MBLeft*1e6, sl.Status, sl.PostProc
			}
		}
		if left < st.left-1e6 {
			a.lastProgress = now // something is downloading, so the connection is fine
		}
		if left < st.left-1e6 || postProc {
			st.left, st.moved = left, now
		}
		items = append(items, seen{w, key, st, *st, sabStatus, postProc})
	}
	// A stall only means a bad copy if other downloads are moving. When
	// nothing moves at all, it's the internet or the Usenet provider.
	connectionOK := now.Sub(a.lastProgress) < 30*time.Minute
	sabPaused := snap != nil && snap.Paused
	a.mu.Unlock()

	imported := map[string]bool{}
	nothingMoving := false
	for _, x := range items {
		w, it, key, st, stIn, sabStatus, postProc := x.todo, x.item, x.key, x.st, x.stIn, x.sabStatus, x.postProc

		c := a.Radarr()
		kind := "movie"
		if w.svc == "sonarr" {
			c, kind = a.Sonarr(), "tv"
		}
		if c == nil {
			continue
		}
		reqs, title := a.openRequestsFor(w.svc, w.ext)
		if title == "" {
			title = it.Title
		}
		state := strings.ToLower(it.TrackedDownloadState)
		status := strings.ToLower(it.Status)

		switch {
		// 1. Downloaded, but waiting for someone to confirm the import.
		case it.Blocked():
			ik := fmt.Sprintf("%s:%d", w.svc, w.ext)
			if imported[ik] || stIn.importTries >= 2 || now.Sub(stIn.lastImport) < importRetryWait {
				if stIn.importTries >= 2 && !stIn.warned {
					a.markWarned(key)
					a.activity("warn", "Couldn't import "+title+" by itself", firstNonEmpty(it.Warning(), "Radarr/Sonarr want a person to look at it"), reqs...)
					a.Notify(notify.Message{Event: notify.EventAttention, Title: title,
						Detail: "Downloaded, but it couldn't be imported automatically. Open Requests → Needs you in Encorr."})
				}
				continue
			}
			imported[ik] = true
			n, err := c.ImportBlocked(ctx, kind, w.ext)
			a.mu.Lock()
			st.importTries++
			st.lastImport = now
			a.mu.Unlock()
			if err == nil && n > 0 {
				fixed++
				a.activity("fix", "Imported "+title, fmt.Sprintf("%d file(s) were waiting for someone to confirm the import", n), reqs...)
				a.Notify(notify.Message{Event: notify.EventFixed, Title: title, Detail: "It was downloaded but stuck waiting to be imported. Encorr imported it."})
			}

		// 2. The download failed: block that copy and get another.
		case state == "failedpending" || state == "failed" || status == "failed":
			if now.Sub(stIn.seen) < failedAfter {
				continue
			}
			if err := c.RemoveFromQueue(ctx, it.ID, true, true); err != nil {
				continue
			}
			fixed++
			a.forget(key)
			a.activity("fix", "Swapped a failed download of "+title, firstNonEmpty(it.Warning(), it.ErrorMessage, "That copy was broken")+". Marked it as bad and asked for another", reqs...)
			a.Notify(notify.Message{Event: notify.EventFixed, Title: title, Detail: "The download failed, so Encorr blocked that copy and asked for another."})

		// 3. No progress for an hour while it should be downloading.
		case !sabPaused && !postProc && sabStatus != "Paused" && sabStatus != "Queued" &&
			state != "importpending" && state != "importing" && status != "paused" && status != "queued" && status != "delay" && status != "completed" &&
			(status == "warning" || sabStatus == "Downloading") &&
			now.Sub(stIn.moved) > stallAfter:
			if !connectionOK {
				nothingMoving = true
				continue
			}
			if err := c.RemoveFromQueue(ctx, it.ID, true, true); err != nil {
				continue
			}
			fixed++
			a.forget(key)
			a.activity("fix", "Swapped a stalled download of "+title,
				fmt.Sprintf("No progress for %s (%s). Marked that copy as bad and asked for another", now.Sub(stIn.moved).Round(time.Minute), firstNonEmpty(it.Warning(), "probably missing pieces")), reqs...)
			a.Notify(notify.Message{Event: notify.EventFixed, Title: title, Detail: "The download stopped making progress, so Encorr swapped it for another copy."})
		}
	}

	if nothingMoving {
		a.mu.Lock()
		warn := now.Sub(a.stallWarned) > 6*time.Hour
		if warn {
			a.stallWarned = now
		}
		a.mu.Unlock()
		if warn {
			a.activity("warn", "Nothing has downloaded for over an hour", "Downloads are waiting but none are moving. Usually the internet or the Usenet provider is down for a while. Encorr leaves them alone rather than throwing good copies away")
			a.Notify(notify.Message{Event: notify.EventAttention, Title: "Downloads aren't moving",
				Detail: "Nothing has downloaded for over an hour. Check the server's internet and your Usenet provider in SABnzbd."})
		}
	}

	// Forget downloads that have left the queue.
	a.mu.Lock()
	for k, st := range a.fixes {
		if st.gone {
			delete(a.fixes, k)
		}
	}
	lastOrphans := a.orphansCleared
	a.mu.Unlock()

	// 4. Leftover folders in SABnzbd, when it isn't busy unpacking.
	if s := a.SAB(); s != nil && now.Sub(lastOrphans) > orphanEvery && (snap == nil || snap.Working == nil) {
		if in, err := s.Info(ctx); err == nil && len(in.Orphans) > 0 {
			if err := s.DeleteOrphans(ctx); err == nil {
				fixed++
				a.activity("fix", fmt.Sprintf("Cleared %d leftover folder(s) in SABnzbd", len(in.Orphans)), "Scraps from old downloads that failed or were removed")
			}
		}
		a.mu.Lock()
		a.orphansCleared = now
		a.mu.Unlock()
	}
	return fixed, nil
}

func (a *App) markWarned(key string) {
	a.mu.Lock()
	if st := a.fixes[key]; st != nil {
		st.warned = true
	}
	a.mu.Unlock()
}

func (a *App) forget(key string) {
	a.mu.Lock()
	delete(a.fixes, key)
	a.mu.Unlock()
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

// jobAutoFix is the background job wrapper.
func (a *App) jobAutoFix(ctx context.Context) (string, error) {
	if !a.settings().AutoFixOn() {
		return "switched off in Settings", nil
	}
	n, err := a.autoFix(ctx, false)
	if n == 0 {
		return "nothing needed fixing", err
	}
	return fmt.Sprintf("fixed %d", n), err
}
