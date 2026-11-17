// Package arr talks to Radarr and Sonarr through their v3 API.
//
// The add-or-update logic follows the approach proven in Seerr (MIT): it
// handles every "already in Radarr/Sonarr" case instead of failing.
package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

type Client struct {
	Name   string // "Radarr" or "Sonarr", used in messages
	URL    string // base, e.g. http://localhost:7878 (may include a URL base)
	APIKey string
}

func (c *Client) endpoint(path string, q url.Values) string {
	u := strings.TrimRight(c.URL, "/") + "/api/v3" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// APIError carries the HTTP status so callers can react to 404 etc.
type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string { return e.Msg }

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path, q), rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return c.friendly(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode == 401 {
		return &APIError{401, c.Name + " rejected the API key: copy it again from " + c.Name + " → Settings → General"}
	}
	if resp.StatusCode >= 300 {
		return &APIError{resp.StatusCode, fmt.Sprintf("%s answered %d: %s", c.Name, resp.StatusCode, shortBody(data))}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s sent something unexpected (is this the right address?): %v", c.Name, err)
	}
	return nil
}

func shortBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	// Radarr/Sonarr validation errors are arrays of {errorMessage}
	var ve []struct {
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(b, &ve) == nil && len(ve) > 0 && ve[0].ErrorMessage != "" {
		return ve[0].ErrorMessage
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func (c *Client) friendly(err error) error {
	s := err.Error()
	host := c.URL
	if u, e := url.Parse(c.URL); e == nil {
		host = u.Host
	}
	switch {
	case strings.Contains(s, "connection refused"):
		return fmt.Errorf("%s isn't answering at %s: is it running?", c.Name, host)
	case strings.Contains(s, "Client.Timeout") || strings.Contains(s, "deadline exceeded"):
		return fmt.Errorf("%s took too long to answer at %s", c.Name, host)
	case strings.Contains(s, "no such host"):
		return fmt.Errorf("couldn't find %s: check the address", host)
	}
	return fmt.Errorf("couldn't reach %s: %v", c.Name, err)
}

// ---------- shared calls ----------

type SystemStatus struct {
	AppName string `json:"appName"`
	Version string `json:"version"`
	URLBase string `json:"urlBase"`
}

func (c *Client) Status(ctx context.Context) (*SystemStatus, error) {
	var s SystemStatus
	err := c.do(ctx, http.MethodGet, "/system/status", nil, nil, &s)
	return &s, err
}

type Profile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (c *Client) QualityProfiles(ctx context.Context) ([]Profile, error) {
	var p []Profile
	err := c.do(ctx, http.MethodGet, "/qualityprofile", nil, nil, &p)
	return p, err
}

// LanguageProfiles exists in Sonarr v3 only; v4 returns 404 (not an error).
func (c *Client) LanguageProfiles(ctx context.Context) ([]Profile, error) {
	var p []Profile
	err := c.do(ctx, http.MethodGet, "/languageprofile", nil, nil, &p)
	var ae *APIError
	if errors.As(err, &ae) && (ae.Status == 404 || ae.Status == 405) {
		return nil, nil
	}
	return p, err
}

type RootFolder struct {
	ID         int    `json:"id"`
	Path       string `json:"path"`
	Accessible bool   `json:"accessible"`
	FreeSpace  int64  `json:"freeSpace"`
}

func (c *Client) RootFolders(ctx context.Context) ([]RootFolder, error) {
	var r []RootFolder
	err := c.do(ctx, http.MethodGet, "/rootfolder", nil, nil, &r)
	return r, err
}

type Tag struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

func (c *Client) Tags(ctx context.Context) ([]Tag, error) {
	var t []Tag
	err := c.do(ctx, http.MethodGet, "/tag", nil, nil, &t)
	return t, err
}

var tagClean = regexp.MustCompile(`[^a-z0-9-]+`)

// TagLabel makes a Radarr/Sonarr-safe label for a requester.
func TagLabel(username string) string {
	l := tagClean.ReplaceAllString(strings.ToLower(username), "-")
	l = strings.Trim(l, "-")
	if l == "" {
		l = "user"
	}
	return "encorr-" + l
}

// EnsureTag returns the id of the tag with label, creating it if needed.
func (c *Client) EnsureTag(ctx context.Context, label string) (int, error) {
	tags, err := c.Tags(ctx)
	if err != nil {
		return 0, err
	}
	for _, t := range tags {
		if strings.EqualFold(t.Label, label) {
			return t.ID, nil
		}
	}
	var t Tag
	err = c.do(ctx, http.MethodPost, "/tag", nil, map[string]string{"label": label}, &t)
	return t.ID, err
}

func (c *Client) Command(ctx context.Context, body map[string]any) error {
	return c.do(ctx, http.MethodPost, "/command", nil, body, nil)
}

// ItemPath returns the folder of a Radarr movie or Sonarr series by its
// Radarr/Sonarr id (so Plex can be told to scan just that folder).
func (c *Client) ItemPath(ctx context.Context, kind string, id int) (string, error) {
	var it struct {
		Path string `json:"path"`
	}
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/%s/%d", kind, id), nil, nil, &it)
	return it.Path, err
}

// QueueItem is one entry in the download queue.
type QueueItem struct {
	MovieID                 int       `json:"movieId"`
	SeriesID                int       `json:"seriesId"`
	EpisodeID               int       `json:"episodeId"`
	SeasonNumber            int       `json:"seasonNumber"`
	Title                   string    `json:"title"`
	DownloadID              string    `json:"downloadId"`
	DownloadClient          string    `json:"downloadClient"`
	Protocol                string    `json:"protocol"`
	Size                    float64   `json:"size"`
	SizeLeft                float64   `json:"sizeleft"`
	TimeLeft                string    `json:"timeleft"`
	Status                  string    `json:"status"`
	TrackedDownloadState    string    `json:"trackedDownloadState"`
	EstimatedCompletionTime time.Time `json:"estimatedCompletionTime"`
	Episode                 *struct {
		SeasonNumber  int `json:"seasonNumber"`
		EpisodeNumber int `json:"episodeNumber"`
	} `json:"episode,omitempty"`
}

func (c *Client) Queue(ctx context.Context) ([]QueueItem, error) {
	var page struct {
		Records []QueueItem `json:"records"`
	}
	q := url.Values{"page": {"1"}, "pageSize": {"250"}, "includeEpisode": {"true"}}
	err := c.do(ctx, http.MethodGet, "/queue", q, nil, &page)
	return page.Records, err
}

// TestResult is what the settings Test button returns.
type TestResult struct {
	OK               bool         `json:"ok"`
	Version          string       `json:"version,omitempty"`
	Profiles         []Profile    `json:"profiles,omitempty"`
	LanguageProfiles []Profile    `json:"languageProfiles,omitempty"`
	RootFolders      []RootFolder `json:"rootFolders,omitempty"`
	Error            string       `json:"error,omitempty"`
	URLBase          string       `json:"urlBase,omitempty"`
}

// Test proves the address and key work and fetches what the dropdowns need.
func (c *Client) Test(ctx context.Context) TestResult {
	st, err := c.Status(ctx)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	if c.Name != "" && st.AppName != "" && !strings.EqualFold(st.AppName, c.Name) {
		return TestResult{Error: fmt.Sprintf("that address is %s, not %s", st.AppName, c.Name)}
	}
	res := TestResult{OK: true, Version: st.Version, URLBase: st.URLBase}
	if res.Profiles, err = c.QualityProfiles(ctx); err != nil {
		return TestResult{Error: err.Error()}
	}
	if res.RootFolders, err = c.RootFolders(ctx); err != nil {
		return TestResult{Error: err.Error()}
	}
	if strings.EqualFold(c.Name, "Sonarr") {
		res.LanguageProfiles, _ = c.LanguageProfiles(ctx)
	}
	if len(res.RootFolders) == 0 {
		res.Error = "connected, but " + c.Name + " has no root folders yet: add one in " + c.Name + " → Settings → Media Management"
	}
	return res
}
