package arr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// EpisodeDetail is a Sonarr episode with what the episode view needs.
type EpisodeDetail struct {
	ID            int       `json:"id"`
	SeasonNumber  int       `json:"seasonNumber"`
	EpisodeNumber int       `json:"episodeNumber"`
	Title         string    `json:"title"`
	AirDateUTC    time.Time `json:"airDateUtc"`
	Monitored     bool      `json:"monitored"`
	HasFile       bool      `json:"hasFile"`
	EpisodeFileID int       `json:"episodeFileId"`
}

func (c *Client) EpisodeDetails(ctx context.Context, seriesID int) ([]EpisodeDetail, error) {
	var eps []EpisodeDetail
	err := c.do(ctx, http.MethodGet, "/episode", url.Values{"seriesId": {strconv.Itoa(seriesID)}}, nil, &eps)
	return eps, err
}

// EpisodeFile is a downloaded episode's file.
type EpisodeFile struct {
	ID      int     `json:"id"`
	Size    float64 `json:"size"`
	Quality struct {
		Quality struct {
			Name string `json:"name"`
		} `json:"quality"`
	} `json:"quality"`
}

func (c *Client) EpisodeFiles(ctx context.Context, seriesID int) ([]EpisodeFile, error) {
	var f []EpisodeFile
	err := c.do(ctx, http.MethodGet, "/episodefile", url.Values{"seriesId": {strconv.Itoa(seriesID)}}, nil, &f)
	return f, err
}

// HistoryItem is one line of Radarr/Sonarr's history for an item.
type HistoryItem struct {
	ID          int               `json:"id"`
	Date        time.Time         `json:"date"`
	EventType   string            `json:"eventType"`
	SourceTitle string            `json:"sourceTitle"`
	EpisodeID   int               `json:"episodeId"`
	DownloadID  string            `json:"downloadId"`
	Data        map[string]string `json:"data"`
	Episode     *struct {
		SeasonNumber  int `json:"seasonNumber"`
		EpisodeNumber int `json:"episodeNumber"`
	} `json:"episode,omitempty"`
}

// History returns the newest history for one film (kind "movie") or show.
func (c *Client) History(ctx context.Context, kind string, id int) ([]HistoryItem, error) {
	var out []HistoryItem
	var err error
	if kind == "movie" {
		err = c.do(ctx, http.MethodGet, "/history/movie", url.Values{"movieId": {strconv.Itoa(id)}}, nil, &out)
	} else {
		err = c.do(ctx, http.MethodGet, "/history/series", url.Values{"seriesId": {strconv.Itoa(id)}, "includeEpisode": {"true"}}, nil, &out)
	}
	return out, err
}

// RemoveFromQueue cancels a download. blocklist marks that copy as bad;
// research asks Radarr/Sonarr to look for another one straight away.
func (c *Client) RemoveFromQueue(ctx context.Context, queueID int, blocklist, research bool) error {
	q := url.Values{
		"removeFromClient": {"true"},
		"blocklist":        {strconv.FormatBool(blocklist)},
		"skipRedownload":   {strconv.FormatBool(!research)},
	}
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/queue/%d", queueID), q, nil, nil)
}

// Release is one copy an indexer offers, from an interactive search.
type Release struct {
	GUID       string   `json:"guid"`
	IndexerID  int      `json:"indexerId"`
	Indexer    string   `json:"indexer"`
	Title      string   `json:"title"`
	Size       float64  `json:"size"`
	AgeHours   float64  `json:"ageHours"`
	Approved   bool     `json:"approved"`
	Rejected   bool     `json:"rejected"`
	Rejections []string `json:"rejections"`
	FullSeason bool     `json:"fullSeason"`
	Score      int      `json:"customFormatScore"`
	Quality    struct {
		Quality struct {
			Name string `json:"name"`
		} `json:"quality"`
	} `json:"quality"`
	Languages []struct {
		Name string `json:"name"`
	} `json:"languages"`
}

// releaseClient waits longer: an interactive search asks every indexer.
var releaseClient = &http.Client{Timeout: 90 * time.Second}

// Releases runs an interactive search for a film, or a show's season.
func (c *Client) Releases(ctx context.Context, kind string, id, season int) ([]Release, error) {
	return c.ReleasesFor(ctx, kind, id, season, 0)
}

// ReleasesFor searches every indexer for a film, a season, or one episode
// (episodeID > 0, Sonarr's own episode id).
func (c *Client) ReleasesFor(ctx context.Context, kind string, id, season, episodeID int) ([]Release, error) {
	q := url.Values{}
	switch {
	case kind == "movie":
		q.Set("movieId", strconv.Itoa(id))
	case episodeID > 0:
		q.Set("episodeId", strconv.Itoa(episodeID))
	default:
		q.Set("seriesId", strconv.Itoa(id))
		q.Set("seasonNumber", strconv.Itoa(season))
	}
	var out []Release
	err := c.doWith(ctx, releaseClient, http.MethodGet, "/release", q, nil, &out)
	return out, err
}

// Grab downloads one release from an interactive search.
func (c *Client) Grab(ctx context.Context, guid string, indexerID int) error {
	return c.doWith(ctx, releaseClient, http.MethodPost, "/release", nil, map[string]any{"guid": guid, "indexerId": indexerID}, nil)
}

// HealthItem is one warning from Radarr/Sonarr's own health check.
type HealthItem struct {
	Source  string `json:"source"`
	Type    string `json:"type"` // ok | notice | warning | error
	Message string `json:"message"`
}

func (c *Client) Health(ctx context.Context) ([]HealthItem, error) {
	var out []HealthItem
	err := c.do(ctx, http.MethodGet, "/health", nil, nil, &out)
	return out, err
}

// SearchSeason / SearchEpisodes look for part of a show.
func (c *Client) SearchSeason(ctx context.Context, seriesID, season int) error {
	return c.Command(ctx, map[string]any{"name": "SeasonSearch", "seriesId": seriesID, "seasonNumber": season})
}

func (c *Client) SearchEpisodes(ctx context.Context, ids []int) error {
	return c.Command(ctx, map[string]any{"name": "EpisodeSearch", "episodeIds": ids})
}

// ---------- raw resources (Tune-up) ----------

// GetJSON / PutJSON / PostJSON / DeleteJSON work on any API resource, for
// settings Encorr reads and writes as whole objects.
func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, nil, out)
}

func (c *Client) PutJSON(ctx context.Context, path string, body any) error {
	return c.do(ctx, http.MethodPut, path, nil, body, nil)
}

func (c *Client) PostJSON(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, nil, body, out)
}

func (c *Client) DeleteJSON(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodDelete, path, nil, nil, nil)
}

// GetJSONQuery is GetJSON with query parameters.
func (c *Client) GetJSONQuery(ctx context.Context, path string, q url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, q, nil, out)
}
