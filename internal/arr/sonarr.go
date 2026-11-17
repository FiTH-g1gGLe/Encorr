package arr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type SeasonInfo struct {
	SeasonNumber int  `json:"seasonNumber"`
	Monitored    bool `json:"monitored"`
}

type Series struct {
	ID               int          `json:"id,omitempty"`
	Title            string       `json:"title"`
	TitleSlug        string       `json:"titleSlug,omitempty"`
	TVDBID           int          `json:"tvdbId"`
	Year             int          `json:"year"`
	Monitored        bool         `json:"monitored"`
	QualityProfileID int          `json:"qualityProfileId"`
	Seasons          []SeasonInfo `json:"seasons"`
	Path             string       `json:"path,omitempty"`
	Tags             []int        `json:"tags"`
	Images           []any        `json:"images,omitempty"`
}

type Episode struct {
	ID            int  `json:"id"`
	SeasonNumber  int  `json:"seasonNumber"`
	EpisodeNumber int  `json:"episodeNumber"`
	Monitored     bool `json:"monitored"`
	HasFile       bool `json:"hasFile"`
}

type AddSeriesOptions struct {
	TVDBID            int
	Title             string
	QualityProfileID  int
	LanguageProfileID int
	RootFolder        string
	Seasons           []int
	SeasonFolders     bool
	Tags              []int
	SearchNow         bool
}

// retry settings are variables so tests can shorten them.
var (
	episodeRetries = 5
	episodeWait    = 3 * time.Second
)

func (c *Client) SeriesByTVDB(ctx context.Context, tvdbID int) (*Series, error) {
	var list []Series
	if err := c.do(ctx, http.MethodGet, "/series", url.Values{"tvdbId": {strconv.Itoa(tvdbID)}}, nil, &list); err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].TVDBID == tvdbID {
			return &list[i], nil
		}
	}
	return nil, nil
}

func (c *Client) LookupSeries(ctx context.Context, tvdbID int) (*Series, error) {
	var list []Series
	if err := c.do(ctx, http.MethodGet, "/series/lookup", url.Values{"term": {"tvdb:" + strconv.Itoa(tvdbID)}}, nil, &list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("Sonarr couldn't find this show (no TVDB match)")
	}
	return &list[0], nil
}

func (c *Client) Episodes(ctx context.Context, seriesID int) ([]Episode, error) {
	var eps []Episode
	err := c.do(ctx, http.MethodGet, "/episode", url.Values{"seriesId": {strconv.Itoa(seriesID)}}, nil, &eps)
	return eps, err
}

func (c *Client) MonitorEpisodes(ctx context.Context, ids []int, monitored bool) error {
	if len(ids) == 0 {
		return nil
	}
	return c.do(ctx, http.MethodPut, "/episode/monitor", nil, map[string]any{"episodeIds": ids, "monitored": monitored}, nil)
}

func contains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// AddSeries adds a show with only the requested seasons monitored, or
// updates an existing show to monitor the newly requested seasons.
func (c *Client) AddSeries(ctx context.Context, o AddSeriesOptions) (*Series, error) {
	if o.TVDBID == 0 {
		return nil, errors.New("this show has no TVDB id on TMDB, so Sonarr can't add it")
	}
	existing, err := c.SeriesByTVDB(ctx, o.TVDBID)
	if err != nil {
		return nil, err
	}

	if existing != nil && existing.ID != 0 {
		raw := map[string]any{}
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/series/%d", existing.ID), nil, nil, &raw); err != nil {
			return nil, err
		}
		seasons := make([]map[string]any, 0, len(existing.Seasons))
		for _, s := range existing.Seasons {
			seasons = append(seasons, map[string]any{
				"seasonNumber": s.SeasonNumber,
				"monitored":    s.Monitored || contains(o.Seasons, s.SeasonNumber),
			})
		}
		raw["seasons"] = seasons
		raw["monitored"] = true
		raw["tags"] = mergeTags(existing.Tags, o.Tags)
		var updated Series
		if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/series/%d", existing.ID), nil, raw, &updated); err != nil {
			return nil, err
		}
		if err := c.monitorRequested(ctx, existing.ID, o.Seasons); err != nil {
			return nil, err
		}
		if o.SearchNow {
			c.searchSeasons(ctx, existing.ID, o.Seasons)
		}
		if updated.ID == 0 {
			updated = *existing
		}
		return &updated, nil
	}

	lk, err := c.LookupSeries(ctx, o.TVDBID)
	if err != nil {
		return nil, err
	}
	seasons := make([]map[string]any, 0, len(lk.Seasons))
	for _, s := range lk.Seasons {
		seasons = append(seasons, map[string]any{
			"seasonNumber": s.SeasonNumber,
			"monitored":    contains(o.Seasons, s.SeasonNumber), // first request: only what was asked for
		})
	}
	title := lk.Title
	if title == "" {
		title = o.Title
	}
	body := map[string]any{
		"tvdbId":           o.TVDBID,
		"title":            title,
		"titleSlug":        lk.TitleSlug,
		"images":           lk.Images,
		"qualityProfileId": o.QualityProfileID,
		"rootFolderPath":   o.RootFolder,
		"seasons":          seasons,
		"seasonFolder":     o.SeasonFolders,
		"monitored":        true,
		"seriesType":       "standard",
		"tags":             mergeTags(nil, o.Tags),
		"addOptions": map[string]any{
			"ignoreEpisodesWithFiles":  true,
			"searchForMissingEpisodes": o.SearchNow,
		},
	}
	if o.LanguageProfileID > 0 {
		body["languageProfileId"] = o.LanguageProfileID
	}
	var s Series
	if err := c.do(ctx, http.MethodPost, "/series", nil, body, &s); err != nil {
		return nil, err
	}
	if s.ID == 0 {
		return nil, errors.New("Sonarr didn't accept the show")
	}
	return &s, nil
}

// monitorRequested switches on episodes of the requested seasons. Sonarr can
// take a moment to load episode details for a show, so it retries (an idea
// borrowed from Ombi, written fresh here).
func (c *Client) monitorRequested(ctx context.Context, seriesID int, seasons []int) error {
	var eps []Episode
	var err error
	for attempt := 0; attempt < episodeRetries; attempt++ {
		eps, err = c.Episodes(ctx, seriesID)
		if err == nil && hasAllSeasons(eps, seasons) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(episodeWait):
		}
	}
	if err != nil {
		return err
	}
	var ids []int
	for _, e := range eps {
		if contains(seasons, e.SeasonNumber) && !e.Monitored {
			ids = append(ids, e.ID)
		}
	}
	return c.MonitorEpisodes(ctx, ids, true)
}

func hasAllSeasons(eps []Episode, seasons []int) bool {
	have := map[int]bool{}
	for _, e := range eps {
		have[e.SeasonNumber] = true
	}
	for _, s := range seasons {
		if !have[s] {
			return false
		}
	}
	return true
}

func (c *Client) searchSeasons(ctx context.Context, seriesID int, seasons []int) {
	for _, s := range seasons {
		_ = c.Command(ctx, map[string]any{"name": "SeasonSearch", "seriesId": seriesID, "seasonNumber": s})
	}
}
