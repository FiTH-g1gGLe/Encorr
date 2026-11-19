package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// ---------- future-dated items ----------

// fixFutureItems resets items dated in the future: the file's own date
// (when it's on this computer and really in the future) and Plex's "added"
// date, so they drop out of the top of Recently Added.
func (a *App) fixFutureItems(ctx context.Context, e *tuneEnv) (int, error) {
	srv := a.PlexServer()
	if srv == nil {
		return 0, userErr("Plex isn't connected.")
	}
	now := time.Now()
	fixed := 0
	var lastErr error
	for _, f := range e.future {
		if f.file != "" {
			if st, err := os.Stat(f.file); err == nil && st.ModTime().After(now.Add(time.Hour)) {
				_ = os.Chtimes(f.file, now, now)
			}
		}
		if err := srv.SetAddedAt(ctx, f.lib, f.plexType, f.ratingKey, now); err != nil {
			lastErr = err
			continue
		}
		fixed++
	}
	if fixed == 0 && lastErr != nil {
		return 0, lastErr
	}
	return fixed, nil
}

// ---------- Prowlarr ----------

// autoDetectProwlarr fills in Prowlarr the first time it's found on this
// machine, like SABnzbd. Tune-up only reads from it.
func (a *App) autoDetectProwlarr(ctx context.Context) {
	if a.settings().Prowlarr.URL != "" {
		return
	}
	a.mu.Lock()
	tried := a.prowlarrTried
	a.prowlarrTried = true
	a.mu.Unlock()
	if tried {
		return
	}
	d := arr.Detect("Prowlarr")
	if !d.Found {
		return
	}
	c := &arr.Client{Name: "Prowlarr", URL: d.URL, APIKey: d.APIKey, APIVersion: "v1"}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var st map[string]any
	if err := c.GetJSON(cctx, "/system/status", &st); err != nil {
		return
	}
	_ = a.Store.Update(func(dd *store.Data) error {
		dd.Settings.Prowlarr = store.ArrSettings{Enabled: true, URL: d.URL, APIKey: store.Secret(d.APIKey)}
		return nil
	})
	a.logf("found Prowlarr at %s: Tune-up can check your indexers", d.URL)
}

// ---------- speed test ----------

// cloudflareURL is overridable in tests and the developer preview.
var cloudflareURL = "https://speed.cloudflare.com/__down?bytes=25000000"

// SpeedTestURL points the internet speed test somewhere else (preview only).
func SpeedTestURL(u string) { cloudflareURL = u }

// internetSpeed downloads from Cloudflare on 4 connections for up to 12
// seconds and returns MB/s. It measures this computer's line.
func internetSpeed(ctx context.Context) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var total int64
	var wg sync.WaitGroup
	start := time.Now()
	var firstErr error
	var once sync.Once
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cloudflareURL, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				once.Do(func() { firstErr = err })
				return
			}
			defer resp.Body.Close()
			buf := make([]byte, 256<<10)
			for {
				n, err := resp.Body.Read(buf)
				atomic.AddInt64(&total, int64(n))
				if err != nil {
					if err != io.EOF && !errors.Is(err, context.DeadlineExceeded) {
						once.Do(func() { firstErr = err })
					}
					return
				}
			}
		}()
	}
	wg.Wait()
	secs := time.Since(start).Seconds()
	if total == 0 {
		if firstErr == nil {
			firstErr = errors.New("no data")
		}
		return 0, firstErr
	}
	return float64(total) / secs / 1e6, nil
}

// RunSpeedTest measures the internet line, SABnzbd's disks and CPU, and how
// complete each news server is, then works out what's holding downloads
// back. Results are saved, so Tune-up can size settings to this machine.
func (a *App) RunSpeedTest(ctx context.Context) (*store.SpeedTest, error) {
	res := &store.SpeedTest{At: time.Now()}
	if c := a.SAB(); c != nil {
		if p, err := c.RunPerformance(ctx); err == nil {
			res.CPU, res.DownloadDir, res.CompleteDir = p.Pystone, p.DownloadDir, p.CompleteDir
			if p.Internet > 0 {
				res.Internet, res.InternetBy = p.Internet, "SABnzbd"
			}
		}
		conns := map[string]int{}
		if cfg, err := c.GetConfig(ctx); err == nil {
			for _, s := range cfg.Servers {
				if n, ok := s["name"].(string); ok {
					conns[n] = int(num(s["connections"]))
				}
			}
		}
		if hs, err := c.ArticleSuccess(ctx, 14); err == nil {
			for _, h := range hs {
				res.Servers = append(res.Servers, store.ServerResult{Name: h.Name, Connections: conns[h.Name], Tried: h.Tried, Rate: h.Rate})
			}
		}
	}
	if mbs, err := internetSpeed(ctx); err == nil && mbs > res.Internet {
		res.Internet, res.InternetBy = mbs, "Cloudflare"
	}
	if res.Internet == 0 && res.CPU == 0 {
		return nil, userErr("Couldn't run the speed test: connect SABnzbd, or check this computer can reach the internet.")
	}
	res.Bottleneck, res.Advice = diagnose(res)
	_ = a.Store.Update(func(d *store.Data) error {
		d.SpeedTests = append(d.SpeedTests, *res)
		if len(d.SpeedTests) > 50 {
			d.SpeedTests = d.SpeedTests[len(d.SpeedTests)-50:]
		}
		return nil
	})
	a.activity("action", "Speed test", fmt.Sprintf("Internet %.0f MB/s, download folder %.0f MB/s, bottleneck: %s", res.Internet, res.DownloadDir, res.Bottleneck))
	return res, nil
}

// diagnose names the slowest link, using SABnzbd's own guidance: a CPU score
// of 100,000+, folders writing at 100+ MB/s, and a provider that actually has
// the articles.
func diagnose(t *store.SpeedTest) (string, []string) {
	var adv []string
	worst := 100.0
	for _, s := range t.Servers {
		if s.Tried > 50000 && s.Rate < worst {
			worst = s.Rate
		}
	}
	bottleneck := "settings"
	switch {
	case t.Internet > 0 && t.Internet < 15:
		bottleneck = "internet"
		adv = append(adv, fmt.Sprintf("Your internet measured %.0f MB/s, so that's the ceiling. SABnzbd can't download faster than the line.", t.Internet))
	case t.CPU > 0 && t.CPU < 100000:
		bottleneck = "cpu"
		adv = append(adv, "The CPU score is under SABnzbd's suggested 100,000. Lighter encryption (AES128) and fewer connections help; Tune-up offers the first.")
	case t.DownloadDir > 0 && t.DownloadDir < 100:
		bottleneck = "disk"
		adv = append(adv, fmt.Sprintf("The download folder writes at only %.0f MB/s. Point SABnzbd's temporary download folder at an SSD (not a pooled or network drive).", t.DownloadDir))
	case worst < 80:
		bottleneck = "provider"
		adv = append(adv, fmt.Sprintf("Your news server only had %.0f%% of the articles asked for. Missing articles slow everything down. Add a backup block account on a different backbone at priority 1.", worst))
	default:
		adv = append(adv, "Your line, disks and CPU are all quick. If downloads are still slow, it's the provider or SABnzbd's settings: try more articles per request and the recommended cache, then run this again.")
	}
	if t.CompleteDir > 0 && t.CompleteDir < 60 {
		adv = append(adv, fmt.Sprintf("The completed folder writes at %.0f MB/s, which slows unpacking.", t.CompleteDir))
	}
	if bottleneck != "provider" && worst < 80 {
		adv = append(adv, fmt.Sprintf("Also: your news server only had %.0f%% of the articles asked for over two weeks.", worst))
	}
	return bottleneck, adv
}

// ---------- HTTP ----------

func (a *App) hTune(w http.ResponseWriter, r *http.Request, u *store.User) {
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	findings, apps := a.TuneCheck(ctx)
	if findings == nil {
		findings = []TuneFinding{}
	}
	var last *store.SpeedTest
	var changes []store.TuneChange
	a.Store.Read(func(d *store.Data) {
		if n := len(d.SpeedTests); n > 0 {
			t := d.SpeedTests[n-1]
			last = &t
		}
		for i := len(d.TuneChanges) - 1; i >= 0 && len(changes) < 30; i-- {
			changes = append(changes, d.TuneChanges[i])
		}
	})
	if changes == nil {
		changes = []store.TuneChange{}
	}
	writeJSON(w, 200, map[string]any{"findings": findings, "apps": apps, "speed": last, "changes": changes})
}

func (a *App) hTuneApply(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := readJSON(r, &body); err != nil || len(body.IDs) == 0 {
		writeErr(w, 400, "Pick something to change.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	type result struct {
		ID    string `json:"id"`
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	var out []result
	ok := 0
	for _, id := range body.IDs {
		if _, err := a.TuneApply(ctx, id, u.Username); err != nil {
			out = append(out, result{ID: id, Error: err.Error()})
			continue
		}
		ok++
		out = append(out, result{ID: id, OK: true})
	}
	writeJSON(w, 200, map[string]any{"results": out, "changed": ok})
}

func (a *App) hTuneUndo(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Force bool `json:"force"`
	}
	_ = readJSON(r, &body)
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := a.TuneUndo(ctx, pathInt(r, "id"), body.Force, u.Username); err != nil {
		var ce *conflictErr
		if errors.As(err, &ce) {
			writeErr(w, http.StatusConflict, ce.msg)
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hSpeedTest(w http.ResponseWriter, r *http.Request, u *store.User) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	res, err := a.RunSpeedTest(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	var hist []store.SpeedTest
	a.Store.Read(func(d *store.Data) { hist = append(hist, d.SpeedTests...) })
	writeJSON(w, 200, map[string]any{"result": res, "history": hist})
}

func (a *App) hSpeedHistory(w http.ResponseWriter, r *http.Request, u *store.User) {
	var hist []store.SpeedTest
	a.Store.Read(func(d *store.Data) { hist = append(hist, d.SpeedTests...) })
	if hist == nil {
		hist = []store.SpeedTest{}
	}
	writeJSON(w, 200, map[string]any{"history": hist})
}

// hSaveProwlarr connects (or disconnects) Prowlarr after testing it.
func (a *App) hSaveProwlarr(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Enabled bool   `json:"enabled"`
		URL     string `json:"url"`
		APIKey  string `json:"apiKey"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	key := body.APIKey
	if key == "" {
		key = string(a.settings().Prowlarr.APIKey)
	}
	if body.Enabled {
		c := &arr.Client{Name: "Prowlarr", URL: normaliseURL(body.URL), APIKey: key, APIVersion: "v1"}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		var st map[string]any
		if err := c.GetJSON(ctx, "/system/status", &st); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if n, _ := st["appName"].(string); n != "" && n != "Prowlarr" {
			writeErr(w, 400, "That address is "+n+", not Prowlarr.")
			return
		}
	}
	err := a.Store.Update(func(d *store.Data) error {
		d.Settings.Prowlarr = store.ArrSettings{Enabled: body.Enabled, URL: normaliseURL(body.URL), APIKey: store.Secret(key)}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
