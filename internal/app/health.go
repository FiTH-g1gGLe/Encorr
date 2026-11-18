package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok | warn | error | off
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

func gb(b int64) string { return fmt.Sprintf("%.1f TB", float64(b)/1e12) }

// Health runs every check in parallel and returns plain-English results.
func (a *App) Health(ctx context.Context) []Check {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s := a.settings()

	checks := make([]Check, 5)
	var wg sync.WaitGroup
	wg.Add(4)

	go func() { // Plex
		defer wg.Done()
		c := Check{Name: "Plex"}
		srv := a.PlexServer()
		switch {
		case srv == nil:
			c.Status, c.Message, c.Fix = "error", "No Plex server chosen yet.", "Open Settings → Plex and pick your server."
		default:
			if err := srv.Ping(ctx); err != nil {
				c.Status, c.Message, c.Fix = "error", err.Error(), "Make sure Plex is running, then press Re-check. If its address changed, pick the server again in Settings → Plex."
			} else {
				n := 0
				for _, l := range s.Plex.Libraries {
					if l.Enabled {
						n++
					}
				}
				c.Status, c.Message = "ok", fmt.Sprintf("Connected to %s, watching %d libraries.", s.Plex.ServerName, n)
				if n == 0 {
					c.Status, c.Fix = "warn", "Tick at least one library in Settings → Plex."
				}
			}
		}
		checks[0] = c
	}()

	go func() { // TMDB
		defer wg.Done()
		c := Check{Name: "TMDB (film info and posters)"}
		if tmdbKey(s) == "" {
			c.Status, c.Message, c.Fix = "error", "No TMDB key saved.", "Get a free key at themoviedb.org → Settings → API, then add it in Settings → General."
		} else if err := a.TMDB().Validate(ctx); err != nil {
			c.Status, c.Message, c.Fix = "error", err.Error(), "Check the key in Settings → General."
		} else {
			c.Status, c.Message = "ok", "Working."
			if s.TMDBKey == "" {
				c.Message = "Working (using Encorr's built-in key)."
			}
		}
		checks[1] = c
	}()

	arrCheck := func(name string, set store.ArrSettings, idx int) {
		defer wg.Done()
		c := Check{Name: name}
		cl := arrClient(name, set)
		if cl == nil {
			c.Status, c.Message, c.Fix = "off", name+" isn't set up.", "Add it in Settings → "+name+" so requests can be sent automatically."
			checks[idx] = c
			return
		}
		st, err := cl.Status(ctx)
		if err != nil {
			c.Status, c.Message, c.Fix = "error", err.Error(), "Start "+name+", or correct its address and API key in Settings → "+name+"."
			checks[idx] = c
			return
		}
		folders, err := cl.RootFolders(ctx)
		if err != nil {
			c.Status, c.Message = "warn", "Connected, but couldn't read root folders: "+err.Error()
			checks[idx] = c
			return
		}
		var match *arr.RootFolder
		for i := range folders {
			if folders[i].Path == set.RootFolder {
				match = &folders[i]
			}
		}
		switch {
		case match == nil:
			c.Status, c.Message, c.Fix = "error", fmt.Sprintf("Connected (v%s), but the folder %q no longer exists in %s.", st.Version, set.RootFolder, name), "Pick a root folder again in Settings → "+name+"."
		case !match.Accessible:
			c.Status, c.Message, c.Fix = "error", fmt.Sprintf("%s can't reach %s.", name, match.Path), "Check the drive (DrivePool) is mounted and "+name+" has permission to write to it."
		case match.FreeSpace > 0 && match.FreeSpace < 200e9:
			c.Status, c.Message, c.Fix = "warn", fmt.Sprintf("Connected (v%s). Only %s free on %s.", st.Version, gb(match.FreeSpace), match.Path), "Free up space or add a drive soon."
		default:
			msg := fmt.Sprintf("Connected (v%s), saving to %s", st.Version, match.Path)
			if match.FreeSpace > 0 {
				msg += fmt.Sprintf(" (%s free)", gb(match.FreeSpace))
			}
			c.Status, c.Message = "ok", msg+"."
		}
		checks[idx] = c
	}
	go arrCheck("Radarr", s.Radarr, 2)
	go arrCheck("Sonarr", s.Sonarr, 3)
	wg.Wait()

	d := Check{Name: "Discord notifications"}
	if s.Discord.Enabled && s.Discord.WebhookURL != "" {
		d.Status, d.Message = "ok", "Turned on. Use Send test in Settings → Discord to check the channel."
	} else {
		d.Status, d.Message, d.Fix = "off", "Turned off.", "Add a channel webhook in Settings → Discord so people hear when their encore is ready."
	}
	checks[4] = d

	z := Check{Name: "SABnzbd (live progress)"}
	if c := a.SAB(); c != nil {
		zctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		if v, err := c.Test(zctx); err != nil {
			z.Status, z.Message, z.Fix = "warn", err.Error(), "Check the address and API key in Settings → SABnzbd. Progress falls back to Radarr/Sonarr meanwhile."
		} else {
			z.Status, z.Message = "ok", "Connected (SABnzbd "+v+"): progress bars are live."
			if st := a.SABStalled(); st != nil {
				z.Status = "error"
				z.Message = fmt.Sprintf("Unpacking looks stuck: %s has been on \"%s\" for %s, and %d finished downloads are waiting behind it.",
					st.Name, st.Action, time.Since(st.Since).Round(time.Minute), st.Waiting)
				z.Fix = "Usually a broken or password-protected download has frozen SABnzbd's unpacking tool. Open SABnzbd → History, delete that job (tick \"delete files\"), and if it won't go, end UnRAR.exe in Task Manager or restart SABnzbd. Radarr/Sonarr then grab another copy."
			}
		}
		cancel()
	} else {
		z.Status, z.Message, z.Fix = "off", "Not connected: progress bars update about once a minute.", "Connect SABnzbd in Settings → SABnzbd for live progress."
	}
	checks = append(checks, z)

	// Config folder writable.
	cf := Check{Name: "Data folder"}
	test := filepath.Join(a.Store.Dir(), ".write-test")
	if err := os.WriteFile(test, []byte("ok"), 0o600); err != nil {
		cf.Status, cf.Message, cf.Fix = "error", "Encorr can't write to "+a.Store.Dir(), "Run Encorr as a user that can write to that folder."
	} else {
		_ = os.Remove(test)
		cf.Status, cf.Message = "ok", "Saving data to "+a.Store.Dir()
	}
	checks = append(checks, cf)

	// Background jobs that failed last time.
	for _, j := range a.Jobs() {
		if !j.Info.LastRun.IsZero() && !j.Info.LastOK {
			checks = append(checks, Check{Name: j.Label, Status: "warn", Message: "Last run failed: " + j.Info.Message, Fix: "It retries automatically; fix the cause above and press Run now in Settings → Jobs."})
		}
	}
	return checks
}
