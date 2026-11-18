// Package tmdb fetches film and TV information, posters and discover lists
// from The Movie Database (https://www.themoviedb.org). Results are cached in
// memory for a short while so browsing stays fast and light on TMDB.
package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BuiltInKey is Encorr's own TMDB API key, so nobody has to get one.
// It can also be set at build time:
//
//	go build -ldflags "-X github.com/FiTH-g1gGLe/encorr/internal/tmdb.BuiltInKey=KEY"
var BuiltInKey = ""

var (
	BaseURL    = "https://api.themoviedb.org/3"
	ImageBase  = "https://image.tmdb.org/t/p/"
	httpClient = &http.Client{Timeout: 15 * time.Second}
)

type Client struct {
	Key      string // v3 API key or v4 read access token
	Language string // en-GB
	Region   string // GB

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	at   time.Time
	body []byte
}

func New(key, language, region string) *Client {
	return &Client{Key: key, Language: language, Region: region, cache: map[string]cacheEntry{}}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, ttl time.Duration, out any) error {
	if c.Key == "" {
		return errors.New("no TMDB key set: add one in Settings")
	}
	if q == nil {
		q = url.Values{}
	}
	if c.Language != "" && q.Get("language") == "" {
		q.Set("language", c.Language)
	}
	bearer := strings.Count(c.Key, ".") >= 2 // v4 tokens are JWTs
	if !bearer {
		q.Set("api_key", c.Key)
	}
	u := BaseURL + path + "?" + q.Encode()
	cacheKey := path + "?" + q.Encode()

	c.mu.Lock()
	if e, ok := c.cache[cacheKey]; ok && time.Since(e.at) < ttl {
		c.mu.Unlock()
		return json.Unmarshal(e.body, out)
	}
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if bearer {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach TMDB: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == 401 {
		return errors.New("TMDB rejected the key: check it in Settings")
	}
	if resp.StatusCode == 404 {
		return errors.New("not found on TMDB")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("TMDB answered %d", resp.StatusCode)
	}
	c.mu.Lock()
	if len(c.cache) > 2000 {
		c.cache = map[string]cacheEntry{}
	}
	c.cache[cacheKey] = cacheEntry{at: time.Now(), body: body}
	c.mu.Unlock()
	return json.Unmarshal(body, out)
}

// Validate checks the key works.
func (c *Client) Validate(ctx context.Context) error {
	var v struct {
		Images struct {
			SecureBaseURL string `json:"secure_base_url"`
		} `json:"images"`
	}
	return c.get(ctx, "/configuration", nil, time.Minute, &v)
}

// Result is one card in a list: a film or a show.
type Result struct {
	ID           int     `json:"id"`
	MediaType    string  `json:"media_type"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	VoteAverage  float64 `json:"vote_average"`
	ProfilePath  string  `json:"profile_path"`
}

type Page struct {
	Page         int      `json:"page"`
	TotalPages   int      `json:"total_pages"`
	TotalResults int      `json:"total_results"`
	Results      []Result `json:"results"`
}

// Card is the simplified shape the UI uses.
type Card struct {
	ID       int     `json:"id"`
	Type     string  `json:"type"` // movie | tv
	Title    string  `json:"title"`
	Year     int     `json:"year,omitempty"`
	Date     string  `json:"date,omitempty"`
	Overview string  `json:"overview,omitempty"`
	Poster   string  `json:"poster,omitempty"`
	Backdrop string  `json:"backdrop,omitempty"`
	Rating   float64 `json:"rating,omitempty"`
}

func (r Result) Card(defaultType string) (Card, bool) {
	t := r.MediaType
	if t == "" {
		t = defaultType
	}
	if t != "movie" && t != "tv" {
		return Card{}, false
	}
	title, date := r.Title, r.ReleaseDate
	if t == "tv" {
		title, date = r.Name, r.FirstAirDate
	}
	return Card{ID: r.ID, Type: t, Title: title, Year: YearOf(date), Date: date, Overview: r.Overview,
		Poster: r.PosterPath, Backdrop: r.BackdropPath, Rating: r.VoteAverage}, true
}

func YearOf(date string) int {
	if len(date) >= 4 {
		y, _ := strconv.Atoi(date[:4])
		return y
	}
	return 0
}

func cards(p Page, defaultType string) []Card {
	out := make([]Card, 0, len(p.Results))
	for _, r := range p.Results {
		if c, ok := r.Card(defaultType); ok {
			out = append(out, c)
		}
	}
	return out
}

// List fetches one page of a home-screen row (see rows.go for the keys).
func (c *Client) List(ctx context.Context, key string, page int) ([]Card, int, error) {
	path, q, def, err := c.rowQuery(key)
	if err != nil {
		return nil, 0, err
	}
	q.Set("page", strconv.Itoa(max(page, 1)))
	var p Page
	if err := c.get(ctx, path, q, 30*time.Minute, &p); err != nil {
		return nil, 0, err
	}
	// TMDB caps paging at 500.
	return cards(p, def), min(p.TotalPages, 500), nil
}

func (c *Client) Search(ctx context.Context, query string, page int) ([]Card, int, error) {
	q := url.Values{"query": {query}, "page": {strconv.Itoa(max(page, 1))}, "include_adult": {"false"}}
	var p Page
	if err := c.get(ctx, "/search/multi", q, 10*time.Minute, &p); err != nil {
		return nil, 0, err
	}
	return cards(p, ""), p.TotalPages, nil
}

// SearchType searches only films ("movie") or only TV ("tv").
func (c *Client) SearchType(ctx context.Context, typ, query string, page int) ([]Card, int, error) {
	if typ != "movie" && typ != "tv" {
		return nil, 0, errors.New("unknown type")
	}
	q := url.Values{"query": {query}, "page": {strconv.Itoa(max(page, 1))}, "include_adult": {"false"}}
	var p Page
	if err := c.get(ctx, "/search/"+typ, q, 10*time.Minute, &p); err != nil {
		return nil, 0, err
	}
	return cards(p, typ), p.TotalPages, nil
}

type Video struct {
	Key  string `json:"key"`
	Site string `json:"site"`
	Type string `json:"type"`
	Name string `json:"name"`
}

type Season struct {
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	EpisodeCount int    `json:"episode_count"`
	AirDate      string `json:"air_date"`
	PosterPath   string `json:"poster_path"`
	// Aired is how many of the season's episodes have been shown so far
	// (EpisodeCount includes announced episodes that haven't aired yet).
	Aired int `json:"aired"`
}

// episodeRef is TMDB's last_episode_to_air.
type episodeRef struct {
	SeasonNumber  int `json:"season_number"`
	EpisodeNumber int `json:"episode_number"`
}

// airedCounts fills in Season.Aired from the show's last aired episode.
func airedCounts(seasons []Season, last *episodeRef, today string) {
	for i := range seasons {
		s := &seasons[i]
		switch {
		case s.AirDate != "" && s.AirDate > today:
			s.Aired = 0 // not started yet
		case last == nil || s.SeasonNumber < last.SeasonNumber:
			s.Aired = s.EpisodeCount
		case s.SeasonNumber == last.SeasonNumber:
			s.Aired = min(last.EpisodeNumber, s.EpisodeCount)
		default:
			s.Aired = 0
		}
	}
}

type Details struct {
	Card
	Genres     []string `json:"genres"`
	Runtime    int      `json:"runtime,omitempty"`
	Tagline    string   `json:"tagline,omitempty"`
	IMDBID     string   `json:"imdbId,omitempty"`
	TVDBID     int      `json:"tvdbId,omitempty"`
	Trailer    string   `json:"trailer,omitempty"` // YouTube key
	Seasons    []Season `json:"seasons,omitempty"`
	Status     string   `json:"tmdbStatus,omitempty"`
	Network    string   `json:"network,omitempty"`
	Cert       string   `json:"certification,omitempty"`
	Collection *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"collection,omitempty"`
	Recommended []Card `json:"recommended,omitempty"`

	// The richer facts for the title page.
	GenreList     []Named     `json:"genreList,omitempty"` // with ids, for links
	VoteCount     int         `json:"voteCount,omitempty"`
	OriginalTitle string      `json:"originalTitle,omitempty"`
	Language      string      `json:"language,omitempty"` // original language, in English
	Countries     []string    `json:"countries,omitempty"`
	Studios       []string    `json:"studios,omitempty"`
	Budget        int64       `json:"budget,omitempty"`
	Revenue       int64       `json:"revenue,omitempty"`
	Releases      []Release   `json:"releases,omitempty"` // this region's cinema/digital/disc dates
	FirstAir      string      `json:"firstAir,omitempty"`
	LastAir       string      `json:"lastAir,omitempty"`
	NumSeasons    int         `json:"numSeasons,omitempty"`
	NumEpisodes   int         `json:"numEpisodes,omitempty"`
	Cast          []Person    `json:"cast,omitempty"`
	Crew          []CrewGroup `json:"crew,omitempty"`
	Keywords      []Named     `json:"keywords,omitempty"`
	Providers     []Provider  `json:"providers,omitempty"` // where to stream it in this region
	ProvidersURL  string      `json:"providersUrl,omitempty"`
	Homepage      string      `json:"homepage,omitempty"`
}

// Named is anything TMDB gives an id and a name (genres, keywords).
type Named struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Person is one cast member.
type Person struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Character string `json:"character,omitempty"`
	Photo     string `json:"photo,omitempty"`
}

// CrewGroup is a job and who did it (e.g. Director: Christopher Nolan).
type CrewGroup struct {
	Job    string   `json:"job"`
	People []Person `json:"people"`
}

// Release is one dated release: "cinema", "digital", "disc" or "tv".
type Release struct {
	Kind string `json:"kind"`
	Date string `json:"date"`
}

// Provider is a streaming service (logo from TMDB, data by JustWatch).
type Provider struct {
	Name string `json:"name"`
	Logo string `json:"logo"`
}

type rawDetails struct {
	Result
	Genres []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
	Runtime        int         `json:"runtime"`
	EpisodeRunTime []int       `json:"episode_run_time"`
	Tagline        string      `json:"tagline"`
	IMDBID         string      `json:"imdb_id"`
	Status         string      `json:"status"`
	Seasons        []Season    `json:"seasons"`
	LastEpisode    *episodeRef `json:"last_episode_to_air"`
	Networks       []struct {
		Name string `json:"name"`
	} `json:"networks"`
	ExternalIDs struct {
		IMDBID string `json:"imdb_id"`
		TVDBID int    `json:"tvdb_id"`
	} `json:"external_ids"`
	Videos struct {
		Results []Video `json:"results"`
	} `json:"videos"`
	ReleaseDates struct {
		Results []struct {
			ISO          string `json:"iso_3166_1"`
			ReleaseDates []struct {
				Certification string `json:"certification"`
				Type          int    `json:"type"`
				Date          string `json:"release_date"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	ContentRatings struct {
		Results []struct {
			ISO    string `json:"iso_3166_1"`
			Rating string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
	BelongsTo *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"belongs_to_collection"`
	Recommendations Page `json:"recommendations"`

	VoteCount        int    `json:"vote_count"`
	OriginalTitle    string `json:"original_title"`
	OriginalName     string `json:"original_name"`
	OriginalLanguage string `json:"original_language"`
	SpokenLanguages  []struct {
		ISO         string `json:"iso_639_1"`
		EnglishName string `json:"english_name"`
	} `json:"spoken_languages"`
	ProductionCountries []struct {
		Name string `json:"name"`
	} `json:"production_countries"`
	ProductionCompanies []struct {
		Name string `json:"name"`
	} `json:"production_companies"`
	Budget      int64  `json:"budget"`
	Revenue     int64  `json:"revenue"`
	Homepage    string `json:"homepage"`
	LastAirDate string `json:"last_air_date"`
	NumSeasons  int    `json:"number_of_seasons"`
	NumEpisodes int    `json:"number_of_episodes"`
	CreatedBy   []struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		ProfilePath string `json:"profile_path"`
	} `json:"created_by"`
	Credits struct {
		Cast []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Character   string `json:"character"`
			ProfilePath string `json:"profile_path"`
		} `json:"cast"`
		Crew []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Job         string `json:"job"`
			ProfilePath string `json:"profile_path"`
		} `json:"crew"`
	} `json:"credits"`
	Keywords struct {
		Keywords []Named `json:"keywords"` // films
		Results  []Named `json:"results"`  // TV
	} `json:"keywords"`
	WatchProviders struct {
		Results map[string]struct {
			Link     string `json:"link"`
			Flatrate []struct {
				Name string `json:"provider_name"`
				Logo string `json:"logo_path"`
			} `json:"flatrate"`
		} `json:"results"`
	} `json:"watch/providers"`
}

func (c *Client) details(ctx context.Context, typ string, id int) (*Details, error) {
	extra := "external_ids,videos,recommendations,credits,keywords,watch/providers"
	if typ == "movie" {
		extra += ",release_dates"
	} else {
		extra += ",content_ratings"
	}
	q := url.Values{"append_to_response": {extra}}
	var r rawDetails
	if err := c.get(ctx, fmt.Sprintf("/%s/%d", typ, id), q, 6*time.Hour, &r); err != nil {
		return nil, err
	}
	card, _ := r.Result.Card(typ)
	d := &Details{Card: card, Runtime: r.Runtime, Tagline: r.Tagline, Status: r.Status}
	for _, g := range r.Genres {
		d.Genres = append(d.Genres, g.Name)
	}
	d.IMDBID = r.IMDBID
	if d.IMDBID == "" {
		d.IMDBID = r.ExternalIDs.IMDBID
	}
	d.TVDBID = r.ExternalIDs.TVDBID
	if d.Runtime == 0 && len(r.EpisodeRunTime) > 0 {
		d.Runtime = r.EpisodeRunTime[0]
	}
	for _, v := range r.Videos.Results {
		if v.Site == "YouTube" && (v.Type == "Trailer" || d.Trailer == "") {
			d.Trailer = v.Key
			if v.Type == "Trailer" {
				break
			}
		}
	}
	airedCounts(r.Seasons, r.LastEpisode, time.Now().Format("2006-01-02"))
	for _, s := range r.Seasons {
		if s.SeasonNumber > 0 { // specials (season 0) left out
			d.Seasons = append(d.Seasons, s)
		}
	}
	if len(r.Networks) > 0 {
		d.Network = r.Networks[0].Name
	}
	for _, rd := range r.ReleaseDates.Results {
		if rd.ISO == c.Region {
			for _, x := range rd.ReleaseDates {
				if x.Certification != "" {
					d.Cert = x.Certification
					break
				}
			}
		}
	}
	for _, cr := range r.ContentRatings.Results {
		if cr.ISO == c.Region {
			d.Cert = cr.Rating
		}
	}
	d.Collection = r.BelongsTo
	c.enrich(d, &r, typ)
	recs := cards(r.Recommendations, typ)
	if len(recs) > 12 {
		recs = recs[:12]
	}
	d.Recommended = recs
	return d, nil
}

func (c *Client) Movie(ctx context.Context, id int) (*Details, error) {
	return c.details(ctx, "movie", id)
}
func (c *Client) TV(ctx context.Context, id int) (*Details, error) { return c.details(ctx, "tv", id) }

// FindByIMDB maps an IMDb id to a TMDB id (used for watchlist items that only
// carry an IMDb id).
func (c *Client) FindByIMDB(ctx context.Context, imdb string) (movieID, tvID int, err error) {
	return c.find(ctx, imdb, "imdb_id")
}

// FindByTVDB turns a TVDB id (old Plex TV agent) into a TMDB show id.
func (c *Client) FindByTVDB(ctx context.Context, tvdb int) (int, error) {
	_, tv, err := c.find(ctx, strconv.Itoa(tvdb), "tvdb_id")
	return tv, err
}

func (c *Client) find(ctx context.Context, id, source string) (movieID, tvID int, err error) {
	var r struct {
		Movie []Result `json:"movie_results"`
		TV    []Result `json:"tv_results"`
	}
	if err = c.get(ctx, "/find/"+url.PathEscape(id), url.Values{"external_source": {source}}, 24*time.Hour, &r); err != nil {
		return
	}
	if len(r.Movie) > 0 {
		movieID = r.Movie[0].ID
	}
	if len(r.TV) > 0 {
		tvID = r.TV[0].ID
	}
	return
}
