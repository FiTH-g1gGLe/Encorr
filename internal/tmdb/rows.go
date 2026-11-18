package tmdb

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Row is one shelf on the home screen.
type Row struct {
	Key   string `json:"key"`   // what to ask /discover/{key} for
	Title string `json:"title"` // shown as the heading
}

// Fixed rows always show, in this order.
var fixedRows = []Row{
	{"trending", "🔥 Trending this week"},
	{"movies", "🎬 Popular films"},
	{"tv", "📺 Popular TV"},
	{"upcoming", "🎟️ Coming soon to cinemas"},
}

// The pool the rotating rows are picked from. Ids are TMDB company, network
// and genre ids.
var rotatingPool = []Row{
	{"company:420:movie", "🦸 Marvel Studios"},
	{"company:2:movie", "🏰 Disney"},
	{"company:3:movie", "🧸 Pixar"},
	{"company:521:movie", "🐉 DreamWorks Animation"},
	{"company:1:movie", "🌌 Lucasfilm"},
	{"company:9993:movie", "🦇 DC"},
	{"company:41077:movie", "🎞️ A24"},
	{"company:10342:movie", "🍃 Studio Ghibli"},
	{"network:213", "🟥 Netflix originals"},
	{"network:49", "🎭 HBO"},
	{"network:2552", "🍎 Apple TV+"},
	{"network:2739", "✨ Disney+ originals"},
	{"network:1024", "📦 Prime Video originals"},
	{"network:4", "🇬🇧 BBC One"},
	{"top-movies", "🏆 Top rated films of all time"},
	{"top-tv", "🏆 Top rated TV of all time"},
	{"now-playing", "🍿 In cinemas now"},
	{"on-air", "📡 New episodes this week"},
	{"genre:27:movie", "👻 Horror"},
	{"genre:35:movie", "😂 Comedy"},
	{"genre:878:movie", "🚀 Sci-fi"},
	{"genre:28:movie", "💥 Action"},
	{"genre:53:movie", "🔪 Thrillers"},
	{"genre:16:movie", "🎨 Animated films"},
	{"genre:10751:movie", "👨‍👩‍👧 Family night"},
	{"genre:99:movie", "🌍 Documentaries"},
	{"genre:80:tv", "🕵️ Crime drama"},
	{"genre:10765:tv", "🧙 Sci-fi & fantasy TV"},
	{"genre:16:tv", "📺 Animated series"},
	{"genre:35:tv", "😆 Comedy series"},
	{"decade:1980", "📼 Best of the 80s"},
	{"decade:1990", "💿 Best of the 90s"},
	{"decade:2000", "📀 Best of the 2000s"},
	{"gems:movie", "💎 Hidden gems"},
}

// rotating is how many pool rows show each day (plus one actor spotlight).
const rotating = 6

// daySeed changes once a day, so everyone sees the same rows today and new
// ones tomorrow.
func daySeed(day time.Time) uint32 {
	h := fnv.New32a()
	h.Write([]byte(day.Format("2006-01-02")))
	return h.Sum32()
}

// pickRotating chooses today's rows from the pool without repeats.
func pickRotating(day time.Time, n int) []Row {
	pool := append([]Row(nil), rotatingPool...)
	s := daySeed(day)
	var out []Row
	for i := 0; i < n && len(pool) > 0; i++ {
		s = s*1664525 + 1013904223 // tiny LCG: deterministic shuffle
		j := int(s>>8) % len(pool)
		out = append(out, pool[j])
		pool = append(pool[:j], pool[j+1:]...)
	}
	return out
}

// Rows returns today's home-screen shelves: the fixed ones, an actor
// spotlight, and a rotating handful from the pool.
func (c *Client) Rows(ctx context.Context, day time.Time) []Row {
	rows := append([]Row(nil), fixedRows...)
	picks := pickRotating(day, rotating)
	if p, ok := c.spotlight(ctx, day); ok {
		// Slot the actor in after the first couple of rotating rows.
		at := min(2, len(picks))
		picks = append(picks[:at], append([]Row{p}, picks[at:]...)...)
	}
	return append(rows, picks...)
}

// spotlight picks today's actor from TMDB's popular people.
func (c *Client) spotlight(ctx context.Context, day time.Time) (Row, bool) {
	var p struct {
		Results []struct {
			ID         int     `json:"id"`
			Name       string  `json:"name"`
			Department string  `json:"known_for_department"`
			Adult      bool    `json:"adult"`
			Popularity float64 `json:"popularity"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/person/popular", url.Values{"page": {"1"}}, 24*time.Hour, &p); err != nil {
		return Row{}, false
	}
	var actors []int
	for i, r := range p.Results {
		if r.Department == "Acting" && !r.Adult {
			actors = append(actors, i)
		}
	}
	if len(actors) == 0 {
		return Row{}, false
	}
	r := p.Results[actors[int(daySeed(day)%uint32(len(actors)))]]
	return Row{Key: "person:" + strconv.Itoa(r.ID), Title: "⭐ Starring " + r.Name}, true
}

// rowQuery turns a row key into a TMDB request. Only known shapes with
// numeric ids are accepted.
func (c *Client) rowQuery(key string) (path string, q url.Values, def string, err error) {
	q = url.Values{}
	parts := strings.Split(key, ":")
	num := func(i int) (int, bool) {
		if i >= len(parts) {
			return 0, false
		}
		n, e := strconv.Atoi(parts[i])
		return n, e == nil && n > 0
	}
	kind := func(i int) (string, bool) {
		if i >= len(parts) {
			return "movie", true
		}
		return parts[i], parts[i] == "movie" || parts[i] == "tv"
	}
	popular := func(t string) {
		q.Set("sort_by", "popularity.desc")
		q.Set("include_adult", "false")
		if t == "movie" {
			q.Set("vote_count.gte", "50")
		}
	}
	switch parts[0] {
	case "trending":
		return "/trending/all/week", q, "", nil
	case "movies":
		q.Set("region", c.Region)
		return "/movie/popular", q, "movie", nil
	case "tv":
		return "/tv/popular", q, "tv", nil
	case "upcoming":
		q.Set("region", c.Region)
		return "/movie/upcoming", q, "movie", nil
	case "top-movies":
		return "/movie/top_rated", q, "movie", nil
	case "top-tv":
		return "/tv/top_rated", q, "tv", nil
	case "now-playing":
		q.Set("region", c.Region)
		return "/movie/now_playing", q, "movie", nil
	case "on-air":
		return "/tv/on_the_air", q, "tv", nil
	case "company":
		id, ok := num(1)
		t, ok2 := kind(2)
		if ok && ok2 {
			popular(t)
			q.Set("with_companies", strconv.Itoa(id))
			return "/discover/" + t, q, t, nil
		}
	case "network":
		if id, ok := num(1); ok {
			popular("tv")
			q.Set("with_networks", strconv.Itoa(id))
			return "/discover/tv", q, "tv", nil
		}
	case "genre":
		id, ok := num(1)
		t, ok2 := kind(2)
		if ok && ok2 {
			popular(t)
			q.Set("with_genres", strconv.Itoa(id))
			return "/discover/" + t, q, t, nil
		}
	case "person":
		if id, ok := num(1); ok {
			popular("movie")
			q.Del("vote_count.gte")
			q.Set("with_cast", strconv.Itoa(id))
			return "/discover/movie", q, "movie", nil
		}
	case "decade":
		if y, ok := num(1); ok && y >= 1920 && y <= 2100 && y%10 == 0 {
			q.Set("primary_release_date.gte", fmt.Sprintf("%d-01-01", y))
			q.Set("primary_release_date.lte", fmt.Sprintf("%d-12-31", y+9))
			q.Set("sort_by", "vote_count.desc")
			return "/discover/movie", q, "movie", nil
		}
	case "gems":
		if t, ok := kind(1); ok {
			q.Set("sort_by", "vote_average.desc")
			q.Set("vote_average.gte", "7.5")
			q.Set("vote_count.gte", "300")
			q.Set("vote_count.lte", "3000")
			return "/discover/" + t, q, t, nil
		}
	}
	return "", nil, "", errors.New("unknown list")
}

// RowTitle names a row key for its "see all" page.
func (c *Client) RowTitle(ctx context.Context, key string) string {
	for _, r := range append(append([]Row(nil), fixedRows...), rotatingPool...) {
		if r.Key == key {
			return r.Title
		}
	}
	if strings.HasPrefix(key, "person:") {
		id := strings.TrimPrefix(key, "person:")
		var p struct {
			Name string `json:"name"`
		}
		if c.get(ctx, "/person/"+id, nil, 24*time.Hour, &p) == nil && p.Name != "" {
			return "⭐ Starring " + p.Name
		}
	}
	return ""
}
