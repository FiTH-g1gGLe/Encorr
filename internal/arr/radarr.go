package arr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type Movie struct {
	ID                  int            `json:"id,omitempty"`
	Title               string         `json:"title"`
	TitleSlug           string         `json:"titleSlug,omitempty"`
	Year                int            `json:"year"`
	TMDBID              int            `json:"tmdbId"`
	QualityProfileID    int            `json:"qualityProfileId"`
	RootFolderPath      string         `json:"rootFolderPath,omitempty"`
	Path                string         `json:"path,omitempty"`
	Monitored           bool           `json:"monitored"`
	MinimumAvailability string         `json:"minimumAvailability,omitempty"`
	HasFile             bool           `json:"hasFile"`
	IsAvailable         bool           `json:"isAvailable"`
	Tags                []int          `json:"tags"`
	Images              []any          `json:"images,omitempty"`
	AddOptions          map[string]any `json:"addOptions,omitempty"`
}

type AddMovieOptions struct {
	TMDBID              int
	Title               string
	Year                int
	QualityProfileID    int
	RootFolder          string
	MinimumAvailability string
	Tags                []int
	SearchNow           bool
}

func (c *Client) MovieByTMDB(ctx context.Context, tmdbID int) (*Movie, error) {
	var list []Movie
	if err := c.do(ctx, http.MethodGet, "/movie", url.Values{"tmdbId": {strconv.Itoa(tmdbID)}}, nil, &list); err != nil {
		return nil, err
	}
	if len(list) > 0 {
		return &list[0], nil
	}
	return nil, nil
}

func (c *Client) LookupMovie(ctx context.Context, tmdbID int) (*Movie, error) {
	var m Movie
	err := c.do(ctx, http.MethodGet, "/movie/lookup/tmdb", url.Values{"tmdbId": {strconv.Itoa(tmdbID)}}, nil, &m)
	if err != nil {
		return nil, err
	}
	if m.TMDBID == 0 {
		return nil, errors.New("Radarr couldn't find this film")
	}
	return &m, nil
}

func (c *Client) SearchMovie(ctx context.Context, id int) error {
	return c.Command(ctx, map[string]any{"name": "MoviesSearch", "movieIds": []int{id}})
}

func mergeTags(a, b []int) []int {
	seen := map[int]bool{}
	out := []int{}
	for _, t := range append(append([]int{}, a...), b...) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// AddMovie adds the film, or brings an existing entry back into line:
//   - already downloaded: nothing to do
//   - in Radarr but unmonitored: monitor it, merge tags, search
//   - monitored but missing: just search
//   - not in Radarr: add it with the chosen profile, folder and tags
func (c *Client) AddMovie(ctx context.Context, o AddMovieOptions) (*Movie, error) {
	existing, err := c.MovieByTMDB(ctx, o.TMDBID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.ID != 0 {
		if existing.HasFile {
			return existing, nil
		}
		if !existing.Monitored {
			// PUT the full object back so nothing Radarr stores is lost.
			raw := map[string]any{}
			if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/movie/%d", existing.ID), nil, nil, &raw); err != nil {
				return nil, err
			}
			raw["monitored"] = true
			raw["qualityProfileId"] = o.QualityProfileID
			raw["minimumAvailability"] = o.MinimumAvailability
			raw["tags"] = mergeTags(existing.Tags, o.Tags)
			var updated Movie
			if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/movie/%d", existing.ID), nil, raw, &updated); err != nil {
				return nil, err
			}
			if o.SearchNow {
				_ = c.SearchMovie(ctx, existing.ID)
			}
			if updated.ID == 0 {
				updated = *existing
			}
			return &updated, nil
		}
		if o.SearchNow {
			_ = c.SearchMovie(ctx, existing.ID)
		}
		return existing, nil
	}

	body := map[string]any{
		"title":               o.Title,
		"year":                o.Year,
		"tmdbId":              o.TMDBID,
		"titleSlug":           strconv.Itoa(o.TMDBID),
		"qualityProfileId":    o.QualityProfileID,
		"rootFolderPath":      o.RootFolder,
		"minimumAvailability": o.MinimumAvailability,
		"monitored":           true,
		"tags":                mergeTags(nil, o.Tags),
		"addOptions":          map[string]any{"searchForMovie": o.SearchNow},
	}
	// Radarr is happier with the full lookup object (images etc.) when available.
	if lk, err := c.LookupMovie(ctx, o.TMDBID); err == nil && lk != nil {
		if lk.Title != "" {
			body["title"] = lk.Title
		}
		if lk.Year != 0 {
			body["year"] = lk.Year
		}
		if lk.TitleSlug != "" {
			body["titleSlug"] = lk.TitleSlug
		}
		if lk.Images != nil {
			body["images"] = lk.Images
		}
	}
	var m Movie
	if err := c.do(ctx, http.MethodPost, "/movie", nil, body, &m); err != nil {
		return nil, err
	}
	if m.ID == 0 {
		return nil, errors.New("Radarr didn't accept the film")
	}
	return &m, nil
}
