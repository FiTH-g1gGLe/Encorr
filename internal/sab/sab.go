// Package sab reads live download progress straight from SABnzbd. Radarr and
// Sonarr only refresh their queue about once a minute, so their numbers lag;
// SABnzbd's own API is up to the second.
package sab

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 8 * time.Second}

type Client struct {
	URL    string // e.g. http://localhost:8080/sabnzbd
	APIKey string
}

// Slot is one download in SABnzbd's queue or post-processing list.
type Slot struct {
	ID       string  // nzo_id, the same as Radarr/Sonarr's downloadId
	Status   string  // Downloading, Queued, Paused, Fetching, Extracting, Verifying, Repairing, Moving, Running, Completed, Failed
	Percent  float64 // 0-100
	MB       float64
	MBLeft   float64
	TimeLeft string // h:mm:ss
	PostProc bool   // finished downloading, SABnzbd is unpacking/checking it
}

// Snapshot is the whole picture from one poll.
type Snapshot struct {
	Slots    map[string]Slot // by lower-case nzo_id
	KBPerSec float64
	Paused   bool
	// Working is the finished download SABnzbd is unpacking/checking right
	// now (it does one at a time), and Waiting how many are queued behind it.
	Working *Working
	Waiting int
}

// Working describes the job in SABnzbd's post-processing step.
type Working struct {
	Name   string // release name
	Status string // Extracting, Verifying, Repairing, Moving, Running
	Action string // SABnzbd's own progress line, e.g. "Unpacking: 52/101"
}

func num(s string) float64 { f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64); return f }

func (c *Client) call(ctx context.Context, mode string, extra url.Values, out any) error {
	q := url.Values{"mode": {mode}, "output": {"json"}, "apikey": {c.APIKey}}
	for k, v := range extra {
		q[k] = v
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.URL, "/")+"/api?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("can't reach SABnzbd at %s", c.URL)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("SABnzbd answered %d", resp.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return errors.New("SABnzbd sent something unexpected (is that the right address?)")
	}
	var e struct {
		Status *bool  `json:"status"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error != "" {
		if strings.Contains(strings.ToLower(e.Error), "api key") {
			return errors.New("SABnzbd rejected the API key")
		}
		return errors.New("SABnzbd: " + e.Error)
	}
	return json.Unmarshal(raw, out)
}

// Version proves the address works (no key needed) and Test checks the key too.
func (c *Client) Test(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.call(ctx, "version", nil, &v); err != nil {
		return "", err
	}
	if v.Version == "" {
		return "", errors.New("that address isn't SABnzbd")
	}
	if _, err := c.Snapshot(ctx); err != nil {
		return "", err
	}
	return v.Version, nil
}

// Snapshot reads the queue plus anything still being unpacked.
func (c *Client) Snapshot(ctx context.Context) (*Snapshot, error) {
	var q struct {
		Queue struct {
			Status   string `json:"status"`
			Paused   bool   `json:"paused"`
			KBPerSec string `json:"kbpersec"`
			Slots    []struct {
				ID         string `json:"nzo_id"`
				Status     string `json:"status"`
				Percentage string `json:"percentage"`
				MB         string `json:"mb"`
				MBLeft     string `json:"mbleft"`
				TimeLeft   string `json:"timeleft"`
			} `json:"slots"`
		} `json:"queue"`
	}
	if err := c.call(ctx, "queue", url.Values{"limit": {"100"}}, &q); err != nil {
		return nil, err
	}
	s := &Snapshot{Slots: map[string]Slot{}, KBPerSec: num(q.Queue.KBPerSec), Paused: q.Queue.Paused}
	for _, x := range q.Queue.Slots {
		s.Slots[strings.ToLower(x.ID)] = Slot{ID: x.ID, Status: x.Status, Percent: num(x.Percentage),
			MB: num(x.MB), MBLeft: num(x.MBLeft), TimeLeft: x.TimeLeft}
	}
	var h struct {
		History struct {
			Slots []struct {
				ID     string  `json:"nzo_id"`
				Name   string  `json:"name"`
				Status string  `json:"status"`
				Bytes  float64 `json:"bytes"`
				Action string  `json:"action_line"`
			} `json:"slots"`
		} `json:"history"`
	}
	if err := c.call(ctx, "history", url.Values{"limit": {"200"}}, &h); err == nil {
		for _, x := range h.History.Slots {
			switch x.Status {
			case "Queued":
				s.Waiting++
			case "Extracting", "Verifying", "Repairing", "Moving", "Running", "Fetching":
				if s.Working == nil {
					s.Working = &Working{Name: x.Name, Status: x.Status, Action: x.Action}
				}
			}
			k := strings.ToLower(x.ID)
			if _, inQueue := s.Slots[k]; inQueue {
				continue
			}
			s.Slots[k] = Slot{ID: x.ID, Status: x.Status, Percent: 100, MB: x.Bytes / 1e6,
				PostProc: x.Status != "Completed" && x.Status != "Failed"}
		}
	}
	return s, nil
}

// Detected mirrors arr.Detected so the settings page can treat them alike.
type Detected struct {
	Found      bool   `json:"found"`
	URL        string `json:"url,omitempty"`
	APIKey     string `json:"apiKey,omitempty"`
	ConfigPath string `json:"configPath,omitempty"`
	Note       string `json:"note,omitempty"`
}

// ConfigCandidates lists where sabnzbd.ini normally lives.
func ConfigCandidates() []string {
	var c []string
	if p := os.Getenv("ENCORR_SABNZBD_CONFIG"); p != "" {
		c = append(c, p)
	}
	if runtime.GOOS == "windows" {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			c = append(c, filepath.Join(la, "sabnzbd", "sabnzbd.ini"))
		}
		if pd := os.Getenv("ProgramData"); pd != "" {
			c = append(c, filepath.Join(pd, "sabnzbd", "sabnzbd.ini"))
		}
	} else {
		if home, err := os.UserHomeDir(); err == nil {
			c = append(c, filepath.Join(home, ".sabnzbd", "sabnzbd.ini"), filepath.Join(home, ".config", "sabnzbd", "sabnzbd.ini"))
		}
		c = append(c, "/var/lib/sabnzbd/sabnzbd.ini", "/config/sabnzbd.ini")
	}
	return c
}

// ParseINI reads the [misc] settings Encorr needs from sabnzbd.ini.
func ParseINI(path string) (Detected, error) {
	f, err := os.Open(path)
	if err != nil {
		return Detected{}, err
	}
	defer f.Close()
	v := map[string]string{}
	section := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[] ")
			continue
		}
		if section != "misc" {
			continue
		}
		if k, val, ok := strings.Cut(line, "="); ok {
			v[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(val), `"`)
		}
	}
	if v["api_key"] == "" {
		return Detected{}, errors.New("no API key in " + path)
	}
	host := v["host"]
	if host == "" || host == "0.0.0.0" || host == "::" || host == "::0" {
		host = "localhost"
	}
	scheme, port := "http", v["port"]
	if v["enable_https"] == "1" && v["https_port"] != "" && v["https_port"] != v["port"] {
		// plain http still works on the normal port; prefer it
	} else if v["enable_https"] == "1" {
		scheme = "https"
	}
	if port == "" {
		port = "8080"
	}
	base := strings.Trim(v["url_base"], "/")
	if base == "" {
		base = "sabnzbd"
	}
	return Detected{Found: true, URL: fmt.Sprintf("%s://%s:%s/%s", scheme, host, port, base), APIKey: v["api_key"], ConfigPath: path}, nil
}

// Detect looks for a local SABnzbd install.
func Detect() Detected {
	for _, p := range ConfigCandidates() {
		if d, err := ParseINI(p); err == nil {
			return d
		}
	}
	return Detected{Note: "Couldn't find SABnzbd's settings file on this machine. Copy the API key from SABnzbd → Config → General."}
}

// ---------- controls ----------

func (c *Client) simple(ctx context.Context, mode string, extra url.Values) error {
	var out map[string]any
	return c.call(ctx, mode, extra, &out)
}

// Pause / Resume one download by its nzo_id.
func (c *Client) Pause(ctx context.Context, id string) error {
	return c.simple(ctx, "queue", url.Values{"name": {"pause"}, "value": {id}})
}

func (c *Client) Resume(ctx context.Context, id string) error {
	return c.simple(ctx, "queue", url.Values{"name": {"resume"}, "value": {id}})
}

// MoveToTop puts a download first in the queue.
func (c *Client) MoveToTop(ctx context.Context, id string) error {
	return c.simple(ctx, "switch", url.Values{"value": {id}, "value2": {"0"}})
}

// PauseAll / ResumeAll pause or resume the whole queue.
func (c *Client) PauseAll(ctx context.Context) error  { return c.simple(ctx, "pause", nil) }
func (c *Client) ResumeAll(ctx context.Context) error { return c.simple(ctx, "resume", nil) }

// Restart restarts SABnzbd (it comes back by itself within a minute).
func (c *Client) Restart(ctx context.Context) error { return c.simple(ctx, "restart", nil) }

// Info is SABnzbd's overall state for the health page.
type Info struct {
	Version   string   `json:"version"`
	Paused    bool     `json:"paused"`
	Speed     string   `json:"speed"`
	Status    string   `json:"status"`
	Queue     int      `json:"queue"`
	DiskFree  string   `json:"diskFree"` // GB free on the download drive
	Orphans   []string `json:"orphans"`
	Warnings  []string `json:"warnings"`
	PostProc  int      `json:"postProc"`
	SizeLeft  string   `json:"sizeLeft"`
	TimeLeft  string   `json:"timeLeft"`
	Completed string   `json:"completeDiskFree"`
}

// Info reads version, queue totals, free space, orphaned jobs and warnings.
func (c *Client) Info(ctx context.Context) (*Info, error) {
	var q struct {
		Queue struct {
			Version   string `json:"version"`
			Status    string `json:"status"`
			Paused    bool   `json:"paused"`
			Speed     string `json:"speed"`
			NoOfSlots int    `json:"noofslots_total"`
			DiskFree1 string `json:"diskspace1"`
			DiskFree2 string `json:"diskspace2"`
			SizeLeft  string `json:"sizeleft"`
			TimeLeft  string `json:"timeleft"`
		} `json:"queue"`
	}
	if err := c.call(ctx, "queue", url.Values{"limit": {"1"}}, &q); err != nil {
		return nil, err
	}
	in := &Info{Version: q.Queue.Version, Paused: q.Queue.Paused, Speed: q.Queue.Speed, Status: q.Queue.Status,
		Queue: q.Queue.NoOfSlots, DiskFree: q.Queue.DiskFree1, Completed: q.Queue.DiskFree2,
		SizeLeft: q.Queue.SizeLeft, TimeLeft: q.Queue.TimeLeft, Orphans: []string{}, Warnings: []string{}}
	var st struct {
		Status struct {
			Folders []string `json:"folders"`
		} `json:"status"`
	}
	if err := c.call(ctx, "status", url.Values{"skip_dashboard": {"1"}}, &st); err == nil && st.Status.Folders != nil {
		in.Orphans = st.Status.Folders
	}
	var w struct {
		Warnings []any `json:"warnings"`
	}
	if err := c.call(ctx, "warnings", nil, &w); err == nil {
		for _, x := range w.Warnings {
			switch v := x.(type) {
			case string:
				in.Warnings = append(in.Warnings, v)
			case map[string]any:
				if t, ok := v["text"].(string); ok {
					in.Warnings = append(in.Warnings, t)
				}
			}
		}
		if len(in.Warnings) > 5 {
			in.Warnings = in.Warnings[len(in.Warnings)-5:]
		}
	}
	return in, nil
}

// DeleteOrphans removes leftover folders of jobs SABnzbd no longer tracks.
func (c *Client) DeleteOrphans(ctx context.Context) error {
	return c.simple(ctx, "status", url.Values{"name": {"delete_all_orphan"}})
}

// ClearWarnings empties SABnzbd's warnings list.
func (c *Client) ClearWarnings(ctx context.Context) error {
	return c.simple(ctx, "warnings", url.Values{"name": {"clear"}})
}
