package arr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"

	"strings"
	"time"
)

// Warning is Radarr/Sonarr's own explanation for a stuck queue item, in
// one line.
func (q QueueItem) Warning() string {
	for _, m := range q.StatusMessages {
		for _, line := range m.Messages {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
	}
	return ""
}

// Blocked says the download finished but Radarr/Sonarr won't import it
// without a person confirming (usually a name that doesn't quite match).
func (q QueueItem) Blocked() bool {
	s := strings.ToLower(q.TrackedDownloadState)
	return s == "importblocked" || (s == "importpending" && strings.EqualFold(q.Status, "warning"))
}

// MovieInfo is the release info Encorr shows for films still to come out.
type MovieInfo struct {
	ID              int       `json:"id"`
	Status          string    `json:"status"` // announced | inCinemas | released
	IsAvailable     bool      `json:"isAvailable"`
	HasFile         bool      `json:"hasFile"`
	InCinemas       time.Time `json:"inCinemas"`
	DigitalRelease  time.Time `json:"digitalRelease"`
	PhysicalRelease time.Time `json:"physicalRelease"`
}

// Movies lists every film in Radarr (one call; Encorr keeps the ones it
// cares about).
func (c *Client) Movies(ctx context.Context) ([]MovieInfo, error) {
	var out []MovieInfo
	err := c.do(ctx, http.MethodGet, "/movie", nil, nil, &out)
	return out, err
}

// SeriesInfo is what Encorr shows about a show's next episode.
type SeriesInfo struct {
	ID         int       `json:"id"`
	Status     string    `json:"status"` // continuing | ended | upcoming
	NextAiring time.Time `json:"nextAiring"`
	Statistics struct {
		EpisodeFileCount int `json:"episodeFileCount"`
		EpisodeCount     int `json:"episodeCount"`
	} `json:"statistics"`
}

// AllSeries lists every show in Sonarr.
func (c *Client) AllSeries(ctx context.Context) ([]SeriesInfo, error) {
	var out []SeriesInfo
	err := c.do(ctx, http.MethodGet, "/series", nil, nil, &out)
	return out, err
}

// Search asks Radarr ("movie") or Sonarr ("tv") to search for everything
// still missing for one item.
func (c *Client) Search(ctx context.Context, kind string, id int) error {
	if kind == "tv" {
		return c.Command(ctx, map[string]any{"name": "SeriesSearch", "seriesId": id})
	}
	return c.Command(ctx, map[string]any{"name": "MoviesSearch", "movieIds": []int{id}})
}

type manualItem struct {
	Path         string          `json:"path"`
	Size         float64         `json:"size"`
	Quality      json.RawMessage `json:"quality"`
	Languages    json.RawMessage `json:"languages"`
	ReleaseGroup string          `json:"releaseGroup"`
	IndexerFlags int             `json:"indexerFlags"`
	Episodes     []struct {
		ID int `json:"id"`
	} `json:"episodes"`
}

var videoExt = regexp.MustCompile(`(?i)\.(mkv|mp4|avi|m4v|ts|wmv)$`)

// ImportBlocked imports the finished downloads for one film or show that
// Radarr/Sonarr wouldn't import by themselves: the same as pressing
// "Manual Import" in their Activity page and confirming. kind is "movie" or
// "tv"; id the Radarr movie / Sonarr series id. It returns how many files
// were sent for import.
func (c *Client) ImportBlocked(ctx context.Context, kind string, id int) (int, error) {
	queue, err := c.Queue(ctx)
	if err != nil {
		return 0, err
	}
	type dl struct {
		folder   string
		episodes []int
	}
	downloads := map[string]*dl{}
	var order []string
	for _, q := range queue {
		if !q.Blocked() || q.DownloadID == "" || q.OutputPath == "" {
			continue
		}
		if (kind == "tv" && q.SeriesID != id) || (kind != "tv" && q.MovieID != id) {
			continue
		}
		d := downloads[q.DownloadID]
		if d == nil {
			d = &dl{folder: q.OutputPath}
			downloads[q.DownloadID] = d
			order = append(order, q.DownloadID)
		}
		if q.EpisodeID != 0 {
			d.episodes = append(d.episodes, q.EpisodeID)
		}
	}
	if len(order) == 0 {
		return 0, errors.New("nothing is waiting to be imported")
	}
	var files []map[string]any
	for _, dlID := range order {
		d := downloads[dlID]
		var items []manualItem
		q := url.Values{"folder": {d.folder}, "downloadId": {dlID}, "filterExistingFiles": {"false"}}
		if err := c.do(ctx, http.MethodGet, "/manualimport", q, nil, &items); err != nil {
			continue
		}
		var vids []manualItem
		for _, it := range items {
			if videoExt.MatchString(it.Path) && !strings.Contains(strings.ToLower(it.Path), "sample") {
				vids = append(vids, it)
			}
		}
		if len(vids) == 0 {
			continue
		}
		sort.Slice(vids, func(i, j int) bool { return vids[i].Size > vids[j].Size })
		base := func(v manualItem) map[string]any {
			f := map[string]any{"path": v.Path, "quality": v.Quality, "releaseGroup": v.ReleaseGroup,
				"downloadId": dlID, "indexerFlags": v.IndexerFlags}
			if len(v.Languages) > 2 { // not "[]" or null
				f["languages"] = v.Languages
			} else {
				f["languages"] = []map[string]any{{"id": 1, "name": "English"}}
			}
			return f
		}
		if kind != "tv" {
			f := base(vids[0]) // the film is the biggest file; the rest are extras or duplicates
			f["movieId"] = id
			files = append(files, f)
			continue
		}
		if len(d.episodes) > 0 && (len(vids) == 1 || allSameSize(vids)) {
			// One episode (or one copy of it): use the episodes the queue says it's for.
			f := base(vids[0])
			f["seriesId"], f["episodeIds"] = id, d.episodes
			files = append(files, f)
			continue
		}
		for _, v := range vids { // a pack: each file says which episode it is
			if len(v.Episodes) == 0 {
				continue
			}
			ids := make([]int, len(v.Episodes))
			for i, e := range v.Episodes {
				ids[i] = e.ID
			}
			f := base(v)
			f["seriesId"], f["episodeIds"] = id, ids
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return 0, errors.New("couldn't work out which files to import. Use Manual Import in " + c.Name)
	}
	if err := c.Command(ctx, map[string]any{"name": "ManualImport", "files": files, "importMode": "auto"}); err != nil {
		return 0, err
	}
	return len(files), nil
}

// allSameSize spots the "file.mkv" + "file(1).mkv" duplicates SABnzbd sometimes leaves.
func allSameSize(v []manualItem) bool {
	for _, x := range v[1:] {
		if x.Size != v[0].Size {
			return false
		}
	}
	return true
}
