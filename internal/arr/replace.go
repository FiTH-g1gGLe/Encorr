package arr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// markFailed blocklists the release behind a history "grabbed" record, so
// Radarr/Sonarr won't pick the same copy again.
func (c *Client) markFailed(ctx context.Context, historyID int) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/history/failed/%d", historyID), nil, nil, nil)
}

// ReplaceMovie swaps a bad copy of a film for a different one: the release it
// came from is blocklisted, the file is deleted and a new search starts.
// It reports whether a release could be blocklisted.
func (c *Client) ReplaceMovie(ctx context.Context, movieID int) (blocklisted bool, err error) {
	if hist, herr := c.History(ctx, "movie", movieID); herr == nil {
		var last *HistoryItem
		for i, h := range hist {
			if h.EventType == "grabbed" && h.ID > 0 && (last == nil || h.Date.After(last.Date)) {
				last = &hist[i]
			}
		}
		if last != nil {
			blocklisted = c.markFailed(ctx, last.ID) == nil
		}
	}
	var files []struct {
		ID int `json:"id"`
	}
	if err := c.do(ctx, http.MethodGet, "/moviefile", url.Values{"movieId": {strconv.Itoa(movieID)}}, nil, &files); err != nil {
		return blocklisted, err
	}
	for _, f := range files {
		if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/moviefile/%d", f.ID), nil, nil, nil); err != nil {
			return blocklisted, err
		}
	}
	return blocklisted, c.SearchMovie(ctx, movieID)
}

// ReplaceEpisodes does the same for some episodes of a show: each release is
// blocklisted once, the files are deleted and the episodes searched again.
func (c *Client) ReplaceEpisodes(ctx context.Context, seriesID int, episodeIDs []int) (blocklisted int, err error) {
	want := map[int]bool{}
	for _, id := range episodeIDs {
		want[id] = true
	}
	if hist, herr := c.History(ctx, "series", seriesID); herr == nil {
		done := map[string]bool{}
		for _, h := range hist {
			if h.EventType != "grabbed" || h.ID == 0 || !want[h.EpisodeID] {
				continue
			}
			key := h.DownloadID
			if key == "" {
				key = h.SourceTitle
			}
			if done[key] {
				continue // a season pack covers several episodes
			}
			done[key] = true
			if c.markFailed(ctx, h.ID) == nil {
				blocklisted++
			}
		}
	}
	eps, err := c.EpisodeDetails(ctx, seriesID)
	if err != nil {
		return blocklisted, err
	}
	gone := map[int]bool{}
	for _, e := range eps {
		if want[e.ID] && e.EpisodeFileID > 0 && !gone[e.EpisodeFileID] {
			gone[e.EpisodeFileID] = true
			if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/episodefile/%d", e.EpisodeFileID), nil, nil, nil); err != nil {
				return blocklisted, err
			}
		}
	}
	return blocklisted, c.SearchEpisodes(ctx, episodeIDs)
}
