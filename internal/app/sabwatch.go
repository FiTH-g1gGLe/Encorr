package app

import (
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/sab"
)

// stuckAfter is how long SABnzbd's unpacking step can sit on exactly the same
// progress before Encorr calls it stuck. Big season packs can take a while to
// check, but none sit on one step for this long.
const stuckAfter = 20 * time.Minute

// sabWatch notices when SABnzbd's post-processing hasn't moved. A broken or
// password-protected archive can make the unpacking tool hang for ever, and
// because SABnzbd unpacks one job at a time, everything behind it waits too.
type sabWatch struct {
	key     string // name + progress line last seen
	since   time.Time
	waiting int
	told    bool // already logged/notified for this key
}

// SABStuck is what Health shows when unpacking has stalled.
type SABStuck struct {
	Name    string
	Action  string
	Since   time.Time
	Waiting int
}

// see records a poll. It returns non-nil exactly once per stall: the moment
// it first counts as stuck.
func (w *sabWatch) see(s *sab.Snapshot, now time.Time) *SABStuck {
	if s == nil || s.Working == nil {
		*w = sabWatch{}
		return nil
	}
	key := s.Working.Name + "\x00" + s.Working.Status + "\x00" + s.Working.Action
	if key != w.key {
		*w = sabWatch{key: key, since: now}
	}
	w.waiting = s.Waiting
	if !w.told && now.Sub(w.since) >= stuckAfter {
		w.told = true
		st := w.stuck(s)
		return st
	}
	return nil
}

// current reports the stall, if there is one right now.
func (w *sabWatch) current(s *sab.Snapshot, now time.Time) *SABStuck {
	if s == nil || s.Working == nil || w.key == "" || now.Sub(w.since) < stuckAfter {
		return nil
	}
	return w.stuck(s)
}

func (w *sabWatch) stuck(s *sab.Snapshot) *SABStuck {
	action := s.Working.Action
	if action == "" {
		action = s.Working.Status
	}
	return &SABStuck{Name: s.Working.Name, Action: action, Since: w.since, Waiting: w.waiting}
}

// SABStalled is the current stall for the Health page (nil when all's well).
func (a *App) SABStalled() *SABStuck {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sabWatch.current(a.sabSnap, time.Now())
}
