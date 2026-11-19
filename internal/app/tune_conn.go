package app

// The connection helper finds the best number of connections for the main
// news server by trying a few values while something is downloading:
// start low, step up, measure SABnzbd's real speed at each, keep the
// smallest number that reaches top speed (plus a couple spare). More
// connections than needed only adds overhead and can upset the provider.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// How long each step settles and is measured. Tests shorten these.
var (
	connSettle = 25 * time.Second
	connSample = 35 * time.Second
	connTick   = 5 * time.Second
)

// ConnStep is one measured connection count.
type ConnStep struct {
	Connections int     `json:"connections"`
	MBs         float64 `json:"mbs"`
}

// ConnTuner is the helper's progress, shown live on the Tune-up page.
type ConnTuner struct {
	Running  bool       `json:"running"`
	Server   string     `json:"server,omitempty"`
	Label    string     `json:"label,omitempty"`
	Original int        `json:"original,omitempty"`
	Trying   int        `json:"trying,omitempty"`
	Steps    []ConnStep `json:"steps"`
	Best     int        `json:"best,omitempty"`
	Message  string     `json:"message,omitempty"`
	Error    string     `json:"error,omitempty"`
	Started  time.Time  `json:"started,omitempty"`
	Finished time.Time  `json:"finished,omitempty"`
	cancel   context.CancelFunc
}

// connCandidates are the values to try, up to the current count (or 30 if
// that's lower, so a cautious setup can still find more speed).
func connCandidates(current int) []int {
	limit := current
	if limit < 30 {
		limit = 30
	}
	var out []int
	for _, n := range []int{8, 12, 16, 20, 25, 30, 40, 50, 60} {
		if n <= limit {
			out = append(out, n)
		}
	}
	return out
}

// pickBest is the smallest count within 5% of the top speed, plus 2 spare
// (but never more than the most tried).
func pickBest(steps []ConnStep) int {
	if len(steps) == 0 {
		return 0
	}
	top, most := 0.0, 0
	for _, s := range steps {
		if s.MBs > top {
			top = s.MBs
		}
		if s.Connections > most {
			most = s.Connections
		}
	}
	sorted := append([]ConnStep(nil), steps...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Connections < sorted[j].Connections })
	for _, s := range sorted {
		if s.MBs >= top*0.95 {
			n := s.Connections + 2
			if n > most {
				n = most
			}
			return n
		}
	}
	return most
}

// StartConnTuner begins the helper in the background.
func (a *App) StartConnTuner(by string) error {
	c := a.SAB()
	if c == nil {
		return userErr("SABnzbd isn't connected.")
	}
	a.mu.Lock()
	if a.conn != nil && a.conn.Running {
		a.mu.Unlock()
		return userErr("It's already running.")
	}
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
	cfg, err := c.GetConfig(ctx)
	if err != nil {
		cancel()
		return err
	}
	// The main server: enabled, priority 0, most connections.
	var name, label string
	best := -1
	for _, s := range cfg.Servers {
		if canon(s["enable"]) == "0" || num(s["priority"]) != 0 {
			continue
		}
		if n := int(num(s["connections"])); n > best {
			best = n
			name, _ = s["name"].(string)
			label = name
			if dn, _ := s["displayname"].(string); dn != "" {
				label = dn
			}
		}
	}
	if name == "" {
		cancel()
		return userErr("SABnzbd has no main news server switched on.")
	}
	snap, err := c.Snapshot(ctx)
	if err != nil {
		cancel()
		return err
	}
	downloading := false
	for _, sl := range snap.Slots {
		if sl.Status == "Downloading" {
			downloading = true
		}
	}
	if !downloading || snap.Paused || snap.KBPerSec <= 0 {
		cancel()
		return userErr("Start a download first: the helper measures real downloads. A big film or season works best.")
	}
	t := &ConnTuner{Running: true, Server: name, Label: label, Original: best, Started: time.Now(), Steps: []ConnStep{}, cancel: cancel}
	a.mu.Lock()
	a.conn = t
	a.mu.Unlock()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer cancel()
		a.runConnTuner(ctx, t, by)
	}()
	return nil
}

func (a *App) connUpdate(fn func(t *ConnTuner)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		fn(a.conn)
	}
}

func (a *App) runConnTuner(ctx context.Context, t *ConnTuner, by string) {
	c := a.SAB()
	restore := func(why string) {
		bg, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = c.SetConfig(bg, "servers", t.Server, "connections", fmt.Sprint(t.Original))
		a.connUpdate(func(t *ConnTuner) {
			t.Running, t.Trying, t.Error, t.Finished = false, 0, why, time.Now()
		})
	}
	for _, n := range connCandidates(t.Original) {
		a.connUpdate(func(t *ConnTuner) { t.Trying = n })
		if err := c.SetConfig(ctx, "servers", t.Server, "connections", fmt.Sprint(n)); err != nil {
			restore("Couldn't change SABnzbd: " + err.Error() + ". Put the original back.")
			return
		}
		if !sleepCtx(ctx, connSettle) {
			restore("Stopped. Put the original number of connections back.")
			return
		}
		var sum float64
		samples := 0
		for elapsed := time.Duration(0); elapsed < connSample; elapsed += connTick {
			snap, err := c.Snapshot(ctx)
			if err != nil {
				restore("Lost touch with SABnzbd. Put the original back.")
				return
			}
			busy := false
			for _, sl := range snap.Slots {
				if sl.Status == "Downloading" {
					busy = true
				}
			}
			if !busy || snap.Paused {
				restore("The downloads finished or paused before the test was done, so the original was put back. Try again with something bigger downloading.")
				return
			}
			sum += snap.KBPerSec / 1024
			samples++
			if !sleepCtx(ctx, connTick) {
				restore("Stopped. Put the original number of connections back.")
				return
			}
		}
		step := ConnStep{Connections: n, MBs: sum / float64(samples)}
		a.connUpdate(func(t *ConnTuner) { t.Steps = append(t.Steps, step) })
	}
	var steps []ConnStep
	a.mu.Lock()
	steps = append(steps, t.Steps...)
	a.mu.Unlock()
	best := pickBest(steps)
	if best == 0 {
		restore("No measurements, so nothing changed.")
		return
	}
	bg, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := c.SetConfig(bg, "servers", t.Server, "connections", fmt.Sprint(best)); err != nil {
		restore("Couldn't save the result: " + err.Error())
		return
	}
	top := 0.0
	for _, s := range steps {
		if s.MBs > top {
			top = s.MBs
		}
	}
	msg := fmt.Sprintf("%d connections reach top speed (about %.0f MB/s), so %s now uses %d (was %d).", best-2, top, t.Label, best, t.Original)
	if best == t.Original {
		msg = fmt.Sprintf("Your current %d connections are already right (about %.0f MB/s).", best, top)
	}
	a.connUpdate(func(t *ConnTuner) {
		t.Running, t.Trying, t.Best, t.Message, t.Finished = false, 0, best, msg, time.Now()
	})
	if best != t.Original {
		a.recordTune(&store.TuneChange{At: time.Now(), Rule: "sab-connections-helper", App: "sabnzbd",
			Key: "server." + t.Server + ".connections", Title: "Connections for " + t.Label, Old: t.Original, New: best, By: by, CanUndo: true})
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// ---------- HTTP ----------

func (a *App) hConnTuner(w http.ResponseWriter, r *http.Request, u *store.User) {
	a.mu.Lock()
	var t ConnTuner
	if a.conn != nil {
		t = *a.conn
		t.Steps = append([]ConnStep{}, a.conn.Steps...)
	}
	a.mu.Unlock()
	if t.Steps == nil {
		t.Steps = []ConnStep{}
	}
	t.cancel = nil
	writeJSON(w, 200, t)
}

func (a *App) hConnTunerStart(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := a.StartConnTuner(u.Username); err != nil {
		var ue *UserError
		if errors.As(err, &ue) {
			writeErr(w, 400, ue.Msg)
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hConnTunerStop(w http.ResponseWriter, r *http.Request, u *store.User) {
	a.mu.Lock()
	t := a.conn
	a.mu.Unlock()
	if t == nil || !t.Running || t.cancel == nil {
		writeErr(w, 400, "It isn't running.")
		return
	}
	t.cancel()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
