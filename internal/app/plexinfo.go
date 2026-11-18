package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/plex"
)

// plexRating is one score shown on the title page.
type plexRating struct {
	Source string `json:"source"` // imdb | rt-critic | rt-audience | tmdb
	Label  string `json:"label"`  // "IMDb", "Rotten Tomatoes", "Audience"
	Value  string `json:"value"`  // "7.9", "92%"
	Fresh  *bool  `json:"fresh,omitempty"`
}

// plexInfo is what Plex adds to a title that's in your library.
type plexInfo struct {
	URL     string       `json:"url,omitempty"` // opens it in Plex
	Ratings []plexRating `json:"ratings,omitempty"`
	Quality string       `json:"quality,omitempty"` // e.g. "4K HDR", "1080p"
	Audio   string       `json:"audio,omitempty"`   // e.g. "5.1"
}

// plexDetails looks a library item up in Plex for the title page. It's a
// nicety, so it gives up quickly.
func (a *App) plexDetails(ctx context.Context, ratingKey string) *plexInfo {
	srv := a.PlexServer()
	if srv == nil || ratingKey == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	it, err := srv.Metadata(ctx, ratingKey)
	if err != nil {
		return nil
	}
	info := &plexInfo{Ratings: ratingsOf(it)}
	if mid := a.settings().Plex.MachineID; mid != "" {
		info.URL = fmt.Sprintf("https://app.plex.tv/desktop/#!/server/%s/details?key=%s", mid, url.QueryEscape("/library/metadata/"+ratingKey))
	}
	if len(it.Media) > 0 {
		m := it.Media[0]
		switch strings.ToLower(m.VideoResolution) {
		case "4k", "2160":
			info.Quality = "4K"
		case "":
		case "sd", "480", "576":
			info.Quality = "SD"
		default:
			info.Quality = m.VideoResolution + "p"
		}
		if d := strings.ToUpper(m.VideoDynamic); d != "" && d != "SDR" {
			info.Quality += " " + strings.ReplaceAll(d, "DOLBY VISION", "DV")
		}
		switch m.AudioChannels {
		case 0:
		case 1:
			info.Audio = "Mono"
		case 2:
			info.Audio = "Stereo"
		default:
			info.Audio = fmt.Sprintf("%d.1", m.AudioChannels-1)
		}
	}
	return info
}

func ratingsOf(it *plex.Item) []plexRating {
	var out []plexRating
	seen := map[string]bool{}
	add := func(image string, v float64, typ string) {
		if v <= 0 {
			return
		}
		var r plexRating
		switch {
		case strings.HasPrefix(image, "imdb://"):
			r = plexRating{Source: "imdb", Label: "IMDb", Value: fmt.Sprintf("%.1f", v)}
		case strings.HasPrefix(image, "rottentomatoes://") && typ == "critic":
			fresh := !strings.Contains(image, "rotten")
			if strings.Contains(image, "ripe") {
				fresh = true
			}
			r = plexRating{Source: "rt-critic", Label: "Rotten Tomatoes", Value: fmt.Sprintf("%.0f%%", v*10), Fresh: &fresh}
		case strings.HasPrefix(image, "rottentomatoes://"):
			fresh := strings.Contains(image, "upright")
			r = plexRating{Source: "rt-audience", Label: "RT audience", Value: fmt.Sprintf("%.0f%%", v*10), Fresh: &fresh}
		case strings.HasPrefix(image, "themoviedb://"):
			r = plexRating{Source: "tmdb", Label: "TMDB", Value: fmt.Sprintf("%.0f%%", v*10)}
		default:
			return
		}
		if !seen[r.Source] {
			seen[r.Source] = true
			out = append(out, r)
		}
	}
	for _, x := range it.Ratings {
		add(x.Image, x.Value, x.Type)
	}
	add(it.RatingImage, it.Rating, "critic")
	add(it.AudienceRatingImage, it.AudienceRating, "audience")
	return out
}
