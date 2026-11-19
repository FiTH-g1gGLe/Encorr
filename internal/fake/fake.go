// Package fake simulates plex.tv, a Plex Media Server, TMDB, Radarr, Sonarr
// and a Discord webhook in one HTTP server, for tests and UI previews.
package fake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Account struct {
	ID       int
	Username string
	Email    string
	Token    string
}

type Movie struct {
	ID       int
	Title    string
	Year     int
	Overview string
	Poster   string
	Backdrop string
	IMDB     string
}

type Show struct {
	ID       int
	Title    string
	Year     int
	Overview string
	Poster   string
	Backdrop string
	TVDB     int
	Seasons  []int // episode counts for seasons 1..n
}

// Stack is the fake world.
type Stack struct {
	mu        sync.Mutex
	URL       string // set by the caller after starting the server
	Owner     Account
	Friend    Account
	NextLogin *Account // whose token the next PIN returns
	Movies    []Movie
	Shows     []Show

	OwnerWatchlist int // how many items are on the owner's Plex watchlist

	// Plex library: what's "downloaded".
	PlexMovies map[int]time.Time   // tmdb id -> added
	PlexShows  map[int]map[int]int // tmdb id -> season -> episodes
	PlexAdded  map[int]time.Time   // show tmdb id -> last episode added

	// Radarr/Sonarr state.
	RadarrMovies map[int]map[string]any // radarr id -> movie
	SonarrSeries map[int]map[string]any
	SonarrEps    map[int][]map[string]any // series id -> episodes
	Tags         map[string][]map[string]any
	Commands     []map[string]any
	Queue        map[string][]map[string]any
	Discord      []map[string]any
	Calls        []string
	Scans        []string                    // folders Plex was asked to scan
	Removed      []map[string]any            // queue items removed (DELETE /queue/{id})
	Grabbed      []map[string]any            // releases picked by hand
	History      map[string][]map[string]any // Radarr/Sonarr history
	ArrHealth    map[string][]map[string]any
	SABCalls     []string // SABnzbd control calls: "pause:nzo", "switch:nzo"...
	SABOrphans   []string
	// Tune-up: settings the fake apps report and accept.
	ArrConfig     map[string]map[string]map[string]any // app -> resource -> object
	CustomFormats map[string][]map[string]any
	ProfilesPut   []map[string]any
	QProfiles     map[string][]map[string]any
	ReleaseProfs  []map[string]any
	PlexPrefs     map[string]any
	SABMisc       map[string]any
	SABServers    []map[string]any
	ProwlarrVIP   string // vipExpiration for the fake indexer
	nextID        int
}

func New() *Stack {
	s := &Stack{
		Owner:        Account{ID: 1001, Username: "g1ggle", Email: "owner@example.com", Token: "tok-owner"},
		Friend:       Account{ID: 2002, Username: "matey", Email: "friend@example.com", Token: "tok-friend"},
		PlexMovies:   map[int]time.Time{},
		PlexShows:    map[int]map[int]int{},
		PlexAdded:    map[int]time.Time{},
		RadarrMovies: map[int]map[string]any{},
		SonarrSeries: map[int]map[string]any{},
		SonarrEps:    map[int][]map[string]any{},
		Tags:         map[string][]map[string]any{"radarr": {}, "sonarr": {}},
		Queue:        map[string][]map[string]any{"radarr": {}, "sonarr": {}},
		History:      map[string][]map[string]any{"radarr": {}, "sonarr": {}},
		ArrHealth:    map[string][]map[string]any{"radarr": {}, "sonarr": {}},
		ArrConfig: map[string]map[string]map[string]any{
			"radarr": {
				"mediamanagement": {"id": 1, "fileDate": "cinemas", "enableMediaInfo": true, "minimumFreeSpaceWhenImporting": 100, "downloadPropersAndRepacks": "preferAndUpgrade", "recycleBin": ""},
				"downloadclient":  {"id": 1, "enableCompletedDownloadHandling": true, "autoRedownloadFailed": false},
				"indexer":         {"id": 1, "rssSyncInterval": 15, "allowHardcodedSubs": true},
				"naming":          {"id": 1, "replaceIllegalCharacters": true, "renameMovies": true},
				"host":            {"id": 1, "logLevel": "trace", "apiKey": "radarr-key", "password": "hunter2"},
			},
			"sonarr": {
				"mediamanagement": {"id": 1, "fileDate": "none", "enableMediaInfo": true, "minimumFreeSpaceWhenImporting": 2048, "downloadPropersAndRepacks": "doNotPrefer", "recycleBin": `D:\Recycle`},
				"downloadclient":  {"id": 1, "enableCompletedDownloadHandling": true, "autoRedownloadFailed": true},
				"indexer":         {"id": 1, "rssSyncInterval": 15},
				"naming":          {"id": 1, "replaceIllegalCharacters": false, "renameEpisodes": false},
				"host":            {"id": 1, "logLevel": "info"},
			},
		},
		QProfiles: map[string][]map[string]any{
			"radarr": {{"id": 1, "name": "Any", "minFormatScore": 0, "formatItems": []any{}}, {"id": 4, "name": "HD-1080p", "minFormatScore": -10000, "formatItems": []any{map[string]any{"format": 1, "score": -10000}}}, {"id": 5, "name": "Ultra-HD", "minFormatScore": 0, "formatItems": []any{}}},
			"sonarr": {{"id": 1, "name": "Any", "minFormatScore": 0, "formatItems": []any{}}, {"id": 4, "name": "HD-1080p", "minFormatScore": 0, "formatItems": []any{}}, {"id": 5, "name": "Ultra-HD", "minFormatScore": 0, "formatItems": []any{}}},
		},
		CustomFormats: map[string][]map[string]any{"radarr": {{"id": 1, "name": "Repack/Proper", "specifications": []any{}}}, "sonarr": {}},
		PlexPrefs: map[string]any{"FSEventLibraryUpdatesEnabled": false, "FSEventLibraryPartialScanEnabled": true, "ScannerLowPriority": false,
			"GenerateBIFBehavior": "asap", "ScheduledLibraryUpdatesEnabled": true, "secureConnections": 1},
		SABMisc: map[string]any{"pause_on_pwrar": 1, "action_on_unwanted_extensions": 0, "unwanted_extensions_mode": 0, "unwanted_extensions": []any{},
			"fail_hopeless_jobs": 1, "fast_fail": 1, "safe_postproc": 1, "ignore_unrar_dates": 0, "deobfuscate_final_filenames": 1,
			"enable_par_cleanup": 1, "cache_limit": "450M", "bandwidth_max": "", "host": "127.0.0.1", "api_key": "secret",
			"pause_on_post_processing": 0, "propagation_delay": 0, "complete_dir": `D:\complete`},
		SABServers: []map[string]any{{"name": "news.example.com", "displayname": "Example News", "enable": 1, "priority": 0, "connections": 60, "pipelining_requests": 1, "ssl": 1, "ssl_ciphers": "", "password": "*****"},
			{"name": "block.example.net", "displayname": "Example Block", "enable": 1, "priority": 1, "connections": 20, "quota": "2T", "usage_at_start": 1e11, "expire_date": "", "ssl": 1}},
		ProwlarrVIP: time.Now().AddDate(0, 0, 10).Format("2006-01-02"),
		nextID:      100,
	}
	s.Movies = []Movie{
		{101, "The Last Lighthouse", 2026, "A keeper on a remote island discovers the light has been signalling someone.", "/p101.jpg", "/b101.jpg", "tt0000101"},
		{102, "Neon Harbour", 2025, "A detective chases a ghost through a city that never switches off.", "/p102.jpg", "/b102.jpg", "tt0000102"},
		{103, "Paper Kingdoms", 2024, "Two rival mapmakers race to chart a coastline that keeps changing.", "/p103.jpg", "/b103.jpg", "tt0000103"},
		{104, "Static Bloom", 2026, "A radio engineer hears tomorrow's news tonight.", "/p104.jpg", "/b104.jpg", "tt0000104"},
		{105, "The Orchard Line", 2023, "A sleepy railway town hides an extraordinary secret.", "/p105.jpg", "/b105.jpg", "tt0000105"},
		{106, "Glass Meridian", 2026, "Explorers cross a frozen sea that reflects the past.", "/p106.jpg", "/b106.jpg", "tt0000106"},
	}
	s.Shows = []Show{
		{201, "Northbound", 2024, "A family drama aboard the last overnight train in Scotland.", "/p201.jpg", "/b201.jpg", 9201, []int{8, 8}},
		{202, "Low Orbit", 2025, "A ground crew keeps a failing space station alive.", "/p202.jpg", "/b202.jpg", 9202, []int{10, 10, 6}},
		{203, "The Quiet Parish", 2022, "A new vicar uncovers a village's oldest grudge.", "/p203.jpg", "/b203.jpg", 9203, []int{6}},
	}
	// Already in Plex before we start: one film and season 1 of one show.
	s.PlexMovies[105] = time.Now().Add(-48 * time.Hour)
	s.PlexShows[203] = map[int]int{1: 6}
	s.PlexAdded[203] = time.Now().Add(-48 * time.Hour)
	return s
}

func (s *Stack) id() int { s.nextID++; return s.nextID }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func readBody(r *http.Request) map[string]any {
	var m map[string]any
	_ = json.NewDecoder(r.Body).Decode(&m)
	return m
}

func (s *Stack) account(token string) *Account {
	switch token {
	case s.Owner.Token:
		return &s.Owner
	case s.Friend.Token:
		return &s.Friend
	}
	return nil
}

// ServeHTTP routes to the right fake service by path prefix.
func (s *Stack) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Calls = append(s.Calls, r.Method+" "+r.URL.Path)
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/plextv/"):
		s.plexTV(w, r, strings.TrimPrefix(p, "/plextv"))
	case strings.HasPrefix(p, "/discover/"):
		s.discover(w, r, strings.TrimPrefix(p, "/discover"))
	case strings.HasPrefix(p, "/pms/"):
		s.pms(w, r, strings.TrimPrefix(p, "/pms"))
	case strings.HasPrefix(p, "/tmdb/"):
		s.tmdb(w, r, strings.TrimPrefix(p, "/tmdb"))
	case strings.HasPrefix(p, "/radarr/api/v3/"):
		s.radarr(w, r, strings.TrimPrefix(p, "/radarr/api/v3"))
	case strings.HasPrefix(p, "/prowlarr/api/v1/"):
		s.prowlarr(w, r, strings.TrimPrefix(p, "/prowlarr/api/v1"))
	case strings.HasPrefix(p, "/speedtest"):
		w.Write(make([]byte, 2_000_000))
	case strings.HasPrefix(p, "/sonarr/api/v3/"):
		s.sonarr(w, r, strings.TrimPrefix(p, "/sonarr/api/v3"))
	case strings.HasPrefix(p, "/_test/plex/movie/") && r.Method == http.MethodPost:
		// Preview helper: "finish" a film download and put it in Plex.
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/_test/plex/movie/"))
		s.PlexMovies[id] = time.Now()
		s.Queue["radarr"] = nil
		writeJSON(w, map[string]any{"ok": true})
	case p == "/sabnzbd/api":
		s.sab(w, r)
	case strings.HasPrefix(p, "/discord/"):
		s.Discord = append(s.Discord, readBody(r))
		w.WriteHeader(204)
	case strings.HasPrefix(p, "/img/"):
		s.image(w, r)
	default:
		http.NotFound(w, r)
	}
}

// ---------- plex.tv ----------

func (s *Stack) plexTV(w http.ResponseWriter, r *http.Request, p string) {
	tok := r.Header.Get("X-Plex-Token")
	switch {
	case p == "/api/v2/pins" && r.Method == http.MethodPost:
		writeJSON(w, map[string]any{"id": 77, "code": "ENCR"})
	case strings.HasPrefix(p, "/api/v2/pins/"):
		acct := s.NextLogin
		if acct == nil {
			acct = &s.Owner
		}
		writeJSON(w, map[string]any{"id": 77, "code": "ENCR", "authToken": acct.Token})
	case p == "/api/v2/user":
		a := s.account(tok)
		if a == nil {
			w.WriteHeader(401)
			return
		}
		writeJSON(w, map[string]any{"id": a.ID, "username": a.Username, "email": a.Email, "thumb": ""})
	case p == "/api/v2/resources":
		a := s.account(tok)
		if a == nil {
			w.WriteHeader(401)
			return
		}
		writeJSON(w, []any{map[string]any{
			"name": "HomeServer", "product": "Plex Media Server", "clientIdentifier": "machine-1",
			"provides": "server", "owned": a.ID == s.Owner.ID, "accessToken": a.Token,
			"connections": []any{map[string]any{"protocol": "http", "uri": s.URL + "/pms", "local": true, "relay": false}},
		}})
	case p == "/api/users":
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<MediaContainer><User id="%d" title="%s" username="%s" email="%s" thumb=""><Server machineIdentifier="machine-1"/></User><User id="3003" title="neighbour" username="neighbour" email="n@example.com" thumb=""><Server machineIdentifier="other"/></User></MediaContainer>`,
			s.Friend.ID, s.Friend.Username, s.Friend.Username, s.Friend.Email)
	default:
		http.NotFound(w, r)
	}
}

func (s *Stack) discover(w http.ResponseWriter, r *http.Request, p string) {
	if strings.HasPrefix(p, "/library/sections/watchlist/all") {
		var items []any
		if r.Header.Get("X-Plex-Token") == s.Friend.Token {
			items = append(items, map[string]any{"ratingKey": "w1", "type": "movie", "title": "Static Bloom", "guid": "plex://item/fake", "Guid": []any{map[string]any{"id": "tmdb://104"}}})
		}
		if r.Header.Get("X-Plex-Token") == s.Owner.Token {
			for i := 0; i < s.OwnerWatchlist; i++ {
				items = append(items, map[string]any{"ratingKey": fmt.Sprintf("o%d", i), "type": "movie", "title": fmt.Sprintf("Watch %d", i), "guid": "plex://item/o", "Guid": []any{map[string]any{"id": fmt.Sprintf("tmdb://%d", 9000+i)}}})
			}
		}
		// Like the real service: big pages are refused.
		start, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Start"))
		size, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Size"))
		if size > 20 {
			w.WriteHeader(400)
			writeJSON(w, map[string]any{"Error": map[string]any{"message": "Invalid value provided for x-plex-container-size!"}})
			return
		}
		total := len(items)
		if start > total {
			start = total
		}
		end := start + size
		if size == 0 || end > total {
			end = total
		}
		writeJSON(w, map[string]any{"MediaContainer": map[string]any{"totalSize": total, "size": end - start, "Metadata": items[start:end]}})
		return
	}
	http.NotFound(w, r)
}

// ---------- Plex Media Server ----------

func (s *Stack) pms(w http.ResponseWriter, r *http.Request, p string) {
	if r.Header.Get("X-Plex-Token") != s.Owner.Token {
		w.WriteHeader(401)
		return
	}
	mc := func(field string, items []any) {
		writeJSON(w, map[string]any{"MediaContainer": map[string]any{"size": len(items), field: items}})
	}
	q := r.URL.Query()
	switch {
	case p == "/:/prefs" && r.Method == http.MethodPut:
		for k, v := range q {
			if strings.HasPrefix(k, "X-Plex") {
				continue
			}
			if old, ok := s.PlexPrefs[k]; ok {
				if _, isBool := old.(bool); isBool {
					s.PlexPrefs[k] = v[0] == "1" || v[0] == "true"
					continue
				}
			}
			s.PlexPrefs[k] = v[0]
		}
		w.WriteHeader(200)
		return
	case p == "/:/prefs":
		var set []any
		for k, v := range s.PlexPrefs {
			typ := "text"
			if _, ok := v.(bool); ok {
				typ = "bool"
			}
			enum := ""
			if strings.HasSuffix(k, "Behavior") {
				enum = "never:never|scheduled:as a scheduled task|asap:as soon as possible"
			}
			set = append(set, map[string]any{"id": k, "type": typ, "value": v, "enumValues": enum})
		}
		writeJSON(w, map[string]any{"MediaContainer": map[string]any{"Setting": set}})
		return
	case strings.HasSuffix(p, "/all") && strings.HasPrefix(p, "/library/sections/") && r.Method == http.MethodPut:
		id, _ := strconv.Atoi(strings.TrimPrefix(q.Get("id"), "m"))
		ts, _ := strconv.ParseInt(q.Get("addedAt.value"), 10, 64)
		if _, ok := s.PlexMovies[id]; ok && ts > 0 {
			s.PlexMovies[id] = time.Unix(ts, 0)
		}
		s.Calls = append(s.Calls, "addedAt:"+q.Get("id"))
		w.WriteHeader(200)
		return
	case p == "/identity":
		writeJSON(w, map[string]any{"MediaContainer": map[string]any{"machineIdentifier": "machine-1"}})
	case p == "/library/sections":
		mc("Directory", []any{
			map[string]any{"key": "1", "title": "Films", "type": "movie", "Location": []any{map[string]any{"path": `P:\Media\Films`}}},
			map[string]any{"key": "2", "title": "TV Shows", "type": "show", "Location": []any{map[string]any{"path": `P:\Media\TV`}}},
		})
	case strings.HasSuffix(p, "/refresh") && strings.HasPrefix(p, "/library/sections/"):
		s.Scans = append(s.Scans, q.Get("path"))
		w.WriteHeader(200)
	case p == "/library/sections/1/all":
		var items []any
		since := sinceParam(r)
		for id, at := range s.PlexMovies {
			if at.Before(since) {
				continue
			}
			if t := q.Get("title"); t != "" && !strings.Contains(strings.ToLower(s.movie(id).Title), strings.ToLower(t)) {
				continue
			}
			items = append(items, map[string]any{"ratingKey": fmt.Sprintf("m%d", id), "type": "movie", "title": s.movie(id).Title, "addedAt": at.Unix(),
				"guid": "plex://item/fake", "Guid": []any{map[string]any{"id": fmt.Sprintf("tmdb://%d", id)}}})
		}
		mc("Metadata", items)
	case p == "/library/sections/2/all":
		var items []any
		since := sinceParam(r)
		if q.Get("type") == "4" { // episodes
			for id, at := range s.PlexAdded {
				if at.Before(since) {
					continue
				}
				items = append(items, map[string]any{"ratingKey": fmt.Sprintf("e%d", id), "type": "episode", "grandparentRatingKey": fmt.Sprintf("s%d", id), "addedAt": at.Unix()})
			}
		} else {
			for id := range s.PlexShows {
				if t := q.Get("title"); t != "" && !strings.Contains(strings.ToLower(s.show(id).Title), strings.ToLower(t)) {
					continue
				}
				items = append(items, map[string]any{"ratingKey": fmt.Sprintf("s%d", id), "type": "show", "title": s.show(id).Title,
					"guid": "plex://item/fake", "Guid": []any{map[string]any{"id": fmt.Sprintf("tmdb://%d", id)}}})
			}
		}
		mc("Metadata", items)
	case strings.HasPrefix(p, "/library/metadata/m"):
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/library/metadata/m"))
		mc("Metadata", []any{map[string]any{"ratingKey": fmt.Sprintf("m%d", id), "type": "movie", "title": s.movie(id).Title,
			"guid": "plex://item/fake", "Guid": []any{map[string]any{"id": fmt.Sprintf("tmdb://%d", id)}},
			"Rating": []any{map[string]any{"image": "imdb://image.rating", "value": 7.8, "type": "audience"},
				map[string]any{"image": "rottentomatoes://image.rating.ripe", "value": 9.1, "type": "critic"},
				map[string]any{"image": "rottentomatoes://image.rating.upright", "value": 8.4, "type": "audience"}},
			"Media": []any{map[string]any{"videoResolution": "4k", "videoDynamicRangeType": "HDR10", "audioChannels": 6}}}})
	case strings.HasPrefix(p, "/library/metadata/s"):
		rest := strings.TrimPrefix(p, "/library/metadata/s")
		children := strings.HasSuffix(rest, "/children")
		id, _ := strconv.Atoi(strings.TrimSuffix(rest, "/children"))
		if children {
			var items []any
			for season, n := range s.PlexShows[id] {
				items = append(items, map[string]any{"ratingKey": fmt.Sprintf("s%d-%d", id, season), "type": "season", "index": season, "leafCount": n})
			}
			mc("Metadata", items)
			return
		}
		mc("Metadata", []any{map[string]any{"ratingKey": fmt.Sprintf("s%d", id), "type": "show", "title": s.show(id).Title,
			"guid": "plex://item/fake", "Guid": []any{map[string]any{"id": fmt.Sprintf("tmdb://%d", id)}}}})
	default:
		http.NotFound(w, r)
	}
}

func sinceParam(r *http.Request) time.Time {
	for k, v := range r.URL.Query() {
		if strings.HasPrefix(k, "addedAt>") {
			n, _ := strconv.ParseInt(strings.TrimPrefix(v[0], "="), 10, 64)
			if n == 0 {
				n, _ = strconv.ParseInt(v[0], 10, 64)
			}
			return time.Unix(n, 0)
		}
	}
	return time.Time{}
}

func (s *Stack) movie(id int) Movie {
	for _, m := range s.Movies {
		if m.ID == id {
			return m
		}
	}
	return Movie{}
}

func (s *Stack) show(id int) Show {
	for _, m := range s.Shows {
		if m.ID == id {
			return m
		}
	}
	return Show{}
}

// ---------- TMDB ----------

func (s *Stack) movieResult(m Movie) map[string]any {
	return map[string]any{"id": m.ID, "media_type": "movie", "title": m.Title, "overview": m.Overview, "poster_path": m.Poster,
		"backdrop_path": m.Backdrop, "release_date": fmt.Sprintf("%d-05-01", m.Year), "vote_average": 7.4}
}

func (s *Stack) showResult(m Show) map[string]any {
	return map[string]any{"id": m.ID, "media_type": "tv", "name": m.Title, "overview": m.Overview, "poster_path": m.Poster,
		"backdrop_path": m.Backdrop, "first_air_date": fmt.Sprintf("%d-09-10", m.Year), "vote_average": 8.1}
}

func (s *Stack) tmdb(w http.ResponseWriter, r *http.Request, p string) {
	if r.URL.Query().Get("api_key") != "good-key" && r.Header.Get("Authorization") == "" {
		w.WriteHeader(401)
		return
	}
	page := func(items []any) {
		writeJSON(w, map[string]any{"page": 1, "total_pages": 1, "total_results": len(items), "results": items})
	}
	var movies, shows []any
	for _, m := range s.Movies {
		movies = append(movies, s.movieResult(m))
	}
	for _, m := range s.Shows {
		shows = append(shows, s.showResult(m))
	}
	switch {
	case p == "/configuration":
		writeJSON(w, map[string]any{"images": map[string]any{"secure_base_url": "https://image.tmdb.org/t/p/"}})
	case p == "/trending/all/week":
		var mixed []any
		for i := 0; i < len(movies) || i < len(shows); i++ {
			if i < len(movies) {
				mixed = append(mixed, movies[i])
			}
			if i < len(shows) {
				mixed = append(mixed, shows[i])
			}
		}
		page(mixed)
	case p == "/movie/popular", p == "/movie/upcoming":
		page(movies)
	case p == "/tv/popular":
		page(shows)
	case p == "/movie/top_rated", p == "/movie/now_playing", p == "/discover/movie":
		page(movies)
	case p == "/tv/top_rated", p == "/tv/on_the_air", p == "/discover/tv":
		page(shows)
	case p == "/collection/900":
		var parts []any
		for i, m := range movies {
			if i < 3 {
				parts = append(parts, m)
			}
		}
		parts = append(parts, map[string]any{"id": 199, "title": "The Last Lighthouse 4", "release_date": "", "poster_path": "/p199.jpg"})
		writeJSON(w, map[string]any{"id": 900, "name": "The Lighthouse Collection", "overview": "Every light in the series.", "poster_path": "/c900.jpg", "backdrop_path": "/b900.jpg", "parts": parts})
	case p == "/person/popular":
		writeJSON(w, map[string]any{"results": []any{map[string]any{"id": 31, "name": "Tom Hanks", "known_for_department": "Acting"}}})
	case p == "/person/31":
		writeJSON(w, map[string]any{"id": 31, "name": "Tom Hanks"})
	case p == "/search/movie", p == "/search/tv":
		q := strings.ToLower(r.URL.Query().Get("query"))
		var out []any
		if p == "/search/movie" {
			for _, m := range s.Movies {
				if strings.Contains(strings.ToLower(m.Title), q) {
					out = append(out, s.movieResult(m))
				}
			}
		} else {
			for _, m := range s.Shows {
				if strings.Contains(strings.ToLower(m.Title), q) {
					out = append(out, s.showResult(m))
				}
			}
		}
		page(out)
	case p == "/search/multi":
		q := strings.ToLower(r.URL.Query().Get("query"))
		var out []any
		for _, m := range s.Movies {
			if strings.Contains(strings.ToLower(m.Title), q) {
				out = append(out, s.movieResult(m))
			}
		}
		for _, m := range s.Shows {
			if strings.Contains(strings.ToLower(m.Title), q) {
				out = append(out, s.showResult(m))
			}
		}
		page(out)
	case strings.HasPrefix(p, "/movie/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/movie/"))
		m := s.movie(id)
		if m.ID == 0 {
			w.WriteHeader(404)
			return
		}
		res := s.movieResult(m)
		res["runtime"] = 118
		res["tagline"] = "Some lights are meant to be found."
		res["imdb_id"] = m.IMDB
		res["budget"], res["revenue"], res["vote_count"], res["original_language"] = 40000000, 145603415, 2381, "en"
		res["production_countries"] = []any{map[string]any{"name": "United Kingdom"}}
		res["production_companies"] = []any{map[string]any{"name": "Northlight Pictures"}, map[string]any{"name": "Harbour Films"}}
		res["credits"] = fakeCredits()
		res["keywords"] = map[string]any{"keywords": []any{map[string]any{"id": 1, "name": "lighthouse"}, map[string]any{"id": 2, "name": "island"}, map[string]any{"id": 3, "name": "mystery signal"}}}
		res["watch/providers"] = map[string]any{"results": map[string]any{"GB": map[string]any{"link": "https://www.themoviedb.org/", "flatrate": []any{map[string]any{"provider_name": "Netflix", "logo_path": "/prov1.jpg"}}}}}
		res["genres"] = []any{map[string]any{"id": 18, "name": "Drama"}, map[string]any{"id": 9648, "name": "Mystery"}}
		res["videos"] = map[string]any{"results": []any{map[string]any{"key": "abc123", "site": "YouTube", "type": "Trailer"}}}
		res["release_dates"] = map[string]any{"results": []any{map[string]any{"iso_3166_1": "GB", "release_dates": []any{
			map[string]any{"certification": "12A", "type": 3, "release_date": fmt.Sprintf("%d-03-14T00:00:00.000Z", m.Year)},
			map[string]any{"type": 4, "release_date": fmt.Sprintf("%d-06-02T00:00:00.000Z", m.Year)}}}}}
		res["recommendations"] = map[string]any{"results": movies[:3]}
		if id == 101 || id == 102 {
			res["belongs_to_collection"] = map[string]any{"id": 900, "name": "The Lighthouse Collection"}
		}
		writeJSON(w, res)
	case strings.HasPrefix(p, "/tv/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/tv/"))
		m := s.show(id)
		if m.ID == 0 {
			w.WriteHeader(404)
			return
		}
		res := s.showResult(m)
		var seasons []any
		seasons = append(seasons, map[string]any{"season_number": 0, "name": "Specials", "episode_count": 2})
		for i, n := range m.Seasons {
			seasons = append(seasons, map[string]any{"season_number": i + 1, "name": fmt.Sprintf("Season %d", i+1), "episode_count": n, "air_date": fmt.Sprintf("%d-09-10", m.Year+i)})
		}
		res["seasons"] = seasons
		res["episode_run_time"] = []int{52}
		res["networks"] = []any{map[string]any{"name": "BBC One"}}
		res["genres"] = []any{map[string]any{"name": "Drama"}}
		res["external_ids"] = map[string]any{"tvdb_id": m.TVDB}
		res["content_ratings"] = map[string]any{"results": []any{map[string]any{"iso_3166_1": "GB", "rating": "15"}}}
		res["recommendations"] = map[string]any{"results": shows}
		res["credits"] = fakeCredits()
		res["created_by"] = []any{map[string]any{"id": 77, "name": "Ruth Calloway"}}
		res["number_of_seasons"], res["number_of_episodes"] = len(m.Seasons), 0
		writeJSON(w, res)
	case strings.HasPrefix(p, "/find/"):
		writeJSON(w, map[string]any{"movie_results": []any{}, "tv_results": []any{}})
	default:
		http.NotFound(w, r)
	}
}

// ---------- Radarr ----------

func (s *Stack) arrCommon(w http.ResponseWriter, r *http.Request, app, p string) bool {
	if r.Header.Get("X-Api-Key") != app+"-key" {
		w.WriteHeader(401)
		return true
	}
	switch {
	case p == "/system/status":
		writeJSON(w, map[string]any{"appName": strings.ToUpper(app[:1]) + app[1:], "version": "5.9.1", "urlBase": "/" + app})
	case p == "/qualityprofile":
		writeJSON(w, s.QProfiles[app])
	case strings.HasPrefix(p, "/qualityprofile/") && r.Method == http.MethodGet:
		for _, qp := range s.QProfiles[app] {
			if fmt.Sprint(qp["id"]) == strings.TrimPrefix(p, "/qualityprofile/") {
				writeJSON(w, qp)
				return true
			}
		}
		http.NotFound(w, r)
	case p == "/indexer":
		writeJSON(w, map[string][]any{"radarr": {map[string]any{"name": "NZBgeek (Prowlarr)"}, map[string]any{"name": "Hand Added"}}, "sonarr": {}}[app])
	case p == "/system/backup":
		writeJSON(w, map[string][]any{"radarr": {}, "sonarr": {map[string]any{"time": time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)}}}[app])
	case p == "/rootfolder":
		path := map[string]string{"radarr": `P:\Media\Films`, "sonarr": `P:\Media\TV`}[app]
		writeJSON(w, []any{map[string]any{"id": 1, "path": path, "accessible": true, "freeSpace": 41.3e12}})
	case p == "/tag" && r.Method == http.MethodGet:
		writeJSON(w, s.Tags[app])
	case p == "/tag" && r.Method == http.MethodPost:
		b := readBody(r)
		t := map[string]any{"id": s.id(), "label": b["label"]}
		s.Tags[app] = append(s.Tags[app], t)
		writeJSON(w, t)
	case p == "/manualimport":
		f := r.URL.Query().Get("folder")
		writeJSON(w, []any{map[string]any{"path": f + `\release.mkv`, "size": 4e9, "quality": map[string]any{"quality": map[string]any{"id": 7}}, "languages": []any{}, "releaseGroup": "GRP", "episodes": []any{}},
			map[string]any{"path": f + `\sample.mkv`, "size": 4e7, "quality": map[string]any{}, "languages": []any{}}})
	case p == "/command":
		s.Commands = append(s.Commands, readBody(r))
		writeJSON(w, map[string]any{"id": s.id()})
	case strings.HasPrefix(p, "/config/"):
		res := strings.Split(strings.TrimPrefix(p, "/config/"), "/")[0]
		obj := s.ArrConfig[app][res]
		if obj == nil {
			http.NotFound(w, r)
			return true
		}
		if r.Method == http.MethodPut {
			b := readBody(r)
			for k, v := range b {
				obj[k] = v
			}
		}
		writeJSON(w, obj)
	case p == "/customformat" && r.Method == http.MethodGet:
		writeJSON(w, s.CustomFormats[app])
	case p == "/customformat" && r.Method == http.MethodPost:
		b := readBody(r)
		b["id"] = s.id()
		s.CustomFormats[app] = append(s.CustomFormats[app], b)
		writeJSON(w, b)
	case strings.HasPrefix(p, "/customformat/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(p, "/customformat/")
		kept := s.CustomFormats[app][:0]
		for _, cf := range s.CustomFormats[app] {
			if fmt.Sprint(cf["id"]) != id {
				kept = append(kept, cf)
			}
		}
		s.CustomFormats[app] = kept
		w.WriteHeader(200)
	case strings.HasPrefix(p, "/qualityprofile/") && r.Method == http.MethodPut:
		b := readBody(r)
		s.ProfilesPut = append(s.ProfilesPut, b)
		for i, qp := range s.QProfiles[app] {
			if fmt.Sprint(qp["id"]) == strings.TrimPrefix(p, "/qualityprofile/") {
				b["id"] = qp["id"]
				s.QProfiles[app][i] = b
			}
		}
		writeJSON(w, b)
	case p == "/releaseprofile" && r.Method == http.MethodGet:
		writeJSON(w, s.ReleaseProfs)
	case p == "/releaseprofile" && r.Method == http.MethodPost:
		b := readBody(r)
		b["id"] = s.id()
		s.ReleaseProfs = append(s.ReleaseProfs, b)
		writeJSON(w, b)
	case strings.HasPrefix(p, "/releaseprofile/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(p, "/releaseprofile/")
		kept := s.ReleaseProfs[:0]
		for _, rp := range s.ReleaseProfs {
			if fmt.Sprint(rp["id"]) != id {
				kept = append(kept, rp)
			}
		}
		s.ReleaseProfs = kept
		w.WriteHeader(200)
	case p == "/downloadclient" && r.Method == http.MethodGet:
		cat := map[string][]any{"radarr": {map[string]any{"name": "movieCategory", "value": "movies"}}, "sonarr": {map[string]any{"name": "tvCategory", "value": ""}}}[app]
		writeJSON(w, []any{map[string]any{"id": 1, "name": "SABnzbd", "implementation": "Sabnzbd", "enable": true, "removeCompletedDownloads": app == "sonarr", "removeFailedDownloads": true, "fields": cat}})
	case p == "/queue":
		writeJSON(w, map[string]any{"records": s.Queue[app]})
	case strings.HasPrefix(p, "/queue/") && r.Method == http.MethodDelete:
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/queue/"))
		kept := s.Queue[app][:0]
		for _, it := range s.Queue[app] {
			if n, _ := it["id"].(int); n == id {
				continue
			}
			kept = append(kept, it)
		}
		s.Queue[app] = kept
		s.Removed = append(s.Removed, map[string]any{"app": app, "id": id, "blocklist": r.URL.Query().Get("blocklist"), "skipRedownload": r.URL.Query().Get("skipRedownload")})
		w.WriteHeader(200)
	case p == "/history/movie" || p == "/history/series":
		writeJSON(w, s.History[app])
	case p == "/health":
		writeJSON(w, s.ArrHealth[app])
	case p == "/release" && r.Method == http.MethodGet:
		writeJSON(w, []any{
			map[string]any{"guid": "g1", "indexerId": 1, "indexer": "NZBgeek", "title": "Good.Copy.1080p.WEB-DL", "size": 4.2e9, "ageHours": 30.0, "approved": true, "quality": map[string]any{"quality": map[string]any{"name": "WEBDL-1080p"}}},
			map[string]any{"guid": "g2", "indexerId": 2, "indexer": "Nzb.su", "title": "Bad.Copy.CAM", "size": 1.1e9, "ageHours": 400.0, "approved": false, "rejected": true, "rejections": []any{"Quality not wanted"}, "quality": map[string]any{"quality": map[string]any{"name": "CAM"}}},
		})
	case p == "/release" && r.Method == http.MethodPost:
		s.Grabbed = append(s.Grabbed, readBody(r))
		writeJSON(w, map[string]any{})
	default:
		return false
	}
	return true
}

func (s *Stack) radarr(w http.ResponseWriter, r *http.Request, p string) {
	if s.arrCommon(w, r, "radarr", p) {
		return
	}
	switch {
	case p == "/movie" && r.Method == http.MethodGet:
		tm, _ := strconv.Atoi(r.URL.Query().Get("tmdbId"))
		var out []any
		for _, m := range s.RadarrMovies {
			if tm == 0 || int(m["tmdbId"].(float64)) == tm {
				out = append(out, m)
			}
		}
		writeJSON(w, out)
	case p == "/movie/lookup/tmdb":
		tm, _ := strconv.Atoi(r.URL.Query().Get("tmdbId"))
		m := s.movie(tm)
		writeJSON(w, map[string]any{"title": m.Title, "year": m.Year, "tmdbId": m.ID, "titleSlug": fmt.Sprintf("slug-%d", m.ID), "images": []any{}})
	case p == "/movie" && r.Method == http.MethodPost:
		b := readBody(r)
		id := s.id()
		b["id"] = float64(id)
		b["hasFile"] = false
		b["isAvailable"], b["status"] = true, "released"
		if int(b["tmdbId"].(float64)) == 102 { // one "still in cinemas" film for the preview
			b["isAvailable"], b["status"] = false, "inCinemas"
			b["digitalRelease"] = time.Now().AddDate(0, 0, 13).Format(time.RFC3339)
		}
		b["path"] = fmt.Sprintf(`%v\%v (%v)`, b["rootFolderPath"], b["title"], b["year"])
		s.RadarrMovies[id] = b
		// Simulate a download starting.
		s.Queue["radarr"] = append(s.Queue["radarr"], map[string]any{"movieId": id, "title": b["title"], "size": 8e9, "sizeleft": 2e9, "timeleft": "00:04:10", "status": "downloading", "downloadId": fmt.Sprintf("SABnzbd_nzo_m%d", id), "trackedDownloadState": "downloading",
			"estimatedCompletionTime": time.Now().Add(4 * time.Minute).Format(time.RFC3339)})
		writeJSON(w, b)
	case strings.HasPrefix(p, "/movie/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/movie/"))
		if r.Method == http.MethodPut {
			b := readBody(r)
			s.RadarrMovies[id] = b
			writeJSON(w, b)
			return
		}
		writeJSON(w, s.RadarrMovies[id])
	default:
		http.NotFound(w, r)
	}
}

// ---------- Sonarr ----------

func (s *Stack) sonarr(w http.ResponseWriter, r *http.Request, p string) {
	if s.arrCommon(w, r, "sonarr", p) {
		return
	}
	switch {
	case p == "/languageprofile":
		w.WriteHeader(404) // Sonarr v4
	case p == "/series" && r.Method == http.MethodGet:
		tv, _ := strconv.Atoi(r.URL.Query().Get("tvdbId"))
		var out []any
		for _, m := range s.SonarrSeries {
			if int(m["tvdbId"].(float64)) == tv {
				out = append(out, m)
			}
		}
		writeJSON(w, out)
	case p == "/series/lookup":
		term := strings.TrimPrefix(r.URL.Query().Get("term"), "tvdb:")
		tv, _ := strconv.Atoi(term)
		for _, m := range s.Shows {
			if m.TVDB == tv {
				var seasons []any
				for i := range m.Seasons {
					seasons = append(seasons, map[string]any{"seasonNumber": i + 1, "monitored": false})
				}
				writeJSON(w, []any{map[string]any{"title": m.Title, "tvdbId": m.TVDB, "titleSlug": "slug", "seasons": seasons, "images": []any{}}})
				return
			}
		}
		writeJSON(w, []any{})
	case p == "/series" && r.Method == http.MethodPost:
		b := readBody(r)
		id := s.id()
		b["id"] = float64(id)
		s.SonarrSeries[id] = b
		s.Queue["sonarr"] = append(s.Queue["sonarr"], map[string]any{"seriesId": id, "title": b["title"], "size": 2e9, "sizeleft": 1.5e9, "timeleft": "00:12:00", "status": "downloading", "downloadId": fmt.Sprintf("SABnzbd_nzo_s%d", id), "trackedDownloadState": "downloading",
			"estimatedCompletionTime": time.Now().Add(12 * time.Minute).Format(time.RFC3339)})
		writeJSON(w, b)
	case strings.HasPrefix(p, "/series/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/series/"))
		if r.Method == http.MethodPut {
			b := readBody(r)
			s.SonarrSeries[id] = b
			writeJSON(w, b)
			return
		}
		writeJSON(w, s.SonarrSeries[id])
	case p == "/episode" && r.Method == http.MethodGet:
		id, _ := strconv.Atoi(r.URL.Query().Get("seriesId"))
		if s.SonarrEps[id] == nil {
			// Build episodes from the show's seasons.
			tv := int(s.SonarrSeries[id]["tvdbId"].(float64))
			for _, m := range s.Shows {
				if m.TVDB == tv {
					for si, n := range m.Seasons {
						for e := 1; e <= n; e++ {
							air := time.Now().AddDate(0, 0, -100+si*30+e).UTC().Format(time.RFC3339)
							s.SonarrEps[id] = append(s.SonarrEps[id], map[string]any{"id": id*1000 + (si+1)*100 + e, "seasonNumber": si + 1, "episodeNumber": e, "monitored": false,
								"title": fmt.Sprintf("Episode %d", e), "airDateUtc": air, "hasFile": false})
						}
					}
				}
			}
		}
		writeJSON(w, s.SonarrEps[id])
	case p == "/episodefile":
		writeJSON(w, []any{map[string]any{"id": 1, "size": 1.5e9, "quality": map[string]any{"quality": map[string]any{"name": "WEBDL-1080p"}}}})
	case p == "/episode/monitor":
		b := readBody(r)
		s.Commands = append(s.Commands, map[string]any{"name": "EpisodeMonitor", "body": b})
		ids := map[int]bool{}
		if list, ok := b["episodeIds"].([]any); ok {
			for _, x := range list {
				if f, ok := x.(float64); ok {
					ids[int(f)] = true
				}
			}
		}
		for _, eps := range s.SonarrEps {
			for _, e := range eps {
				if id, ok := e["id"].(int); ok && ids[id] {
					e["monitored"] = b["monitored"]
				}
			}
		}
		w.WriteHeader(202)
	default:
		http.NotFound(w, r)
	}
}

// ---------- posters for previews ----------

var palette = [][2]string{{"#3b2a7a", "#c084fc"}, {"#0f4c5c", "#5eead4"}, {"#7a2a3b", "#fb7185"}, {"#2a4a7a", "#93c5fd"}, {"#5c3d0f", "#fbbf24"}, {"#1f5c3d", "#86efac"}}

func (s *Stack) image(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	id, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimLeft(name, "pb"), ".jpg"))
	title := s.movie(id).Title
	if title == "" {
		title = s.show(id).Title
	}
	c := palette[id%len(palette)]
	wide := strings.HasPrefix(name, "b")
	vw, vh := 500, 750
	if wide {
		vw, vh = 1280, 720
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="%s"/><stop offset="1" stop-color="#070b1a"/></linearGradient><radialGradient id="r" cx=".7" cy=".3" r=".6"><stop offset="0" stop-color="%s" stop-opacity=".7"/><stop offset="1" stop-color="%s" stop-opacity="0"/></radialGradient></defs><rect width="100%%" height="100%%" fill="url(#g)"/><rect width="100%%" height="100%%" fill="url(#r)"/>`, vw, vh, c[0], c[1], c[1])
	if !wide {
		fmt.Fprintf(w, `<text x="40" y="640" font-family="Segoe UI,Arial" font-size="46" font-weight="800" fill="#fff">%s</text>`, escapeXML(title))
	}
	fmt.Fprint(w, `</svg>`)
}

func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// ---------- helpers for tests ----------

// AddToPlex simulates a download finishing and Plex picking it up.
// SABKey is the fake SABnzbd's API key.
const SABKey = "sab-key"

// sab serves SABnzbd's API: it reports every Radarr/Sonarr queue item as
// 90% done, so tests can tell live numbers from Radarr's (75%).
func (s *Stack) sab(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("mode") == "version" {
		writeJSON(w, map[string]any{"version": "4.3.3"})
		return
	}
	if q.Get("apikey") != SABKey {
		writeJSON(w, map[string]any{"status": false, "error": "API Key Incorrect"})
		return
	}
	switch q.Get("mode") {
	case "queue":
		if n := q.Get("name"); n == "pause" || n == "resume" {
			s.SABCalls = append(s.SABCalls, n+":"+q.Get("value"))
			writeJSON(w, map[string]any{"status": true})
			return
		}
		var slots []any
		for _, app := range []string{"radarr", "sonarr"} {
			for _, it := range s.Queue[app] {
				slots = append(slots, map[string]any{"nzo_id": it["downloadId"], "status": "Downloading", "percentage": "90", "mb": "1000", "mbleft": "100", "timeleft": "0:00:09"})
			}
		}
		kb := "190000"
		if len(s.SABServers) > 0 {
			// Faster with more connections, up to 20.
			c := s.SABServers[0]["connections"]
			n, _ := c.(int)
			if f, ok := c.(float64); ok {
				n = int(f)
			}
			if n > 20 {
				n = 20
			}
			kb = strconv.Itoa(n * 10 * 1024)
		}
		writeJSON(w, map[string]any{"queue": map[string]any{"kbpersec": kb, "paused": false, "slots": slots}})
	case "history":
		writeJSON(w, map[string]any{"history": map[string]any{"slots": []any{}}})
	case "pause", "resume", "restart":
		s.SABCalls = append(s.SABCalls, q.Get("mode"))
		writeJSON(w, map[string]any{"status": true})
	case "switch":
		s.SABCalls = append(s.SABCalls, "switch:"+q.Get("value"))
		writeJSON(w, map[string]any{"result": map[string]any{"position": 0}})
	case "get_config":
		writeJSON(w, map[string]any{"config": map[string]any{"misc": s.SABMisc, "servers": s.SABServers}})
	case "set_config":
		sec, kw, val := q.Get("section"), q.Get("keyword"), q.Get("value")
		s.SABCalls = append(s.SABCalls, "set:"+sec+"."+kw+"="+val)
		if sec == "servers" {
			for _, sv := range s.SABServers {
				if sv["name"] == q.Get("name") {
					if n, err := strconv.Atoi(val); err == nil {
						sv[kw] = n
					} else {
						sv[kw] = val
					}
				}
			}
		} else if kw == "unwanted_extensions" {
			parts := []any{}
			for _, x := range strings.Split(val, ",") {
				parts = append(parts, x)
			}
			s.SABMisc[kw] = parts
		} else if n, err := strconv.Atoi(val); err == nil {
			s.SABMisc[kw] = n
		} else {
			s.SABMisc[kw] = val
		}
		writeJSON(w, map[string]any{"status": true})
	case "server_stats":
		day := time.Now().Format("2006-01-02")
		writeJSON(w, map[string]any{"total": 1, "servers": map[string]any{"news.example.com": map[string]any{
			"total": 5e12, "articles_tried": map[string]any{day: 200000}, "articles_success": map[string]any{day: 120000}},
			"block.example.net": map[string]any{"total": 1.7e12}}})
	case "status":
		if q.Get("calculate_performance") == "1" {
			writeJSON(w, map[string]any{"status": map[string]any{"pystone": 250000, "downloaddirspeed": 60.5, "completedirspeed": 300.0, "internetbandwidth": 120.0, "downloaddir": `D:\incomplete`, "completedir": `D:\complete`}})
			return
		}
		if q.Get("name") == "delete_all_orphan" {
			s.SABCalls = append(s.SABCalls, "orphans")
			s.SABOrphans = nil
			writeJSON(w, map[string]any{"status": true})
			return
		}
		writeJSON(w, map[string]any{"status": map[string]any{"folders": s.SABOrphans}})
	case "warnings":
		writeJSON(w, map[string]any{"warnings": []any{}})
	default:
		writeJSON(w, map[string]any{"status": false, "error": "not implemented"})
	}
}

// AddToPlexQuietly adds a film with an old "added" date, so Plex's
// recently-added list won't show it (like a slow Plex scan).
func (s *Stack) AddToPlexQuietly(tmdbID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PlexMovies[tmdbID] = time.Now().Add(-48 * time.Hour)
	s.Queue["radarr"] = nil
}

func (s *Stack) AddToPlex(typ string, tmdbID int, seasons map[int]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if typ == "movie" {
		s.PlexMovies[tmdbID] = time.Now()
		s.Queue["radarr"] = nil
		return
	}
	if s.PlexShows[tmdbID] == nil {
		s.PlexShows[tmdbID] = map[int]int{}
	}
	for k, v := range seasons {
		s.PlexShows[tmdbID][k] = v
	}
	s.PlexAdded[tmdbID] = time.Now()
	s.Queue["sonarr"] = nil
}

func (s *Stack) Lock()   { s.mu.Lock() }
func (s *Stack) Unlock() { s.mu.Unlock() }

func fakeCredits() map[string]any {
	cast := []any{}
	for i, n := range []string{"Ada Morrow", "Ben Okafor", "Clara Voss", "Dev Patel-Shaw", "Elin Hart", "Finn Rook"} {
		cast = append(cast, map[string]any{"id": 300 + i, "name": n, "character": []string{"Keeper", "Harbourmaster", "Signal officer", "Fisherman", "Doctor", "Boy"}[i], "profile_path": fmt.Sprintf("/cast%d.jpg", i)})
	}
	return map[string]any{"cast": cast, "crew": []any{
		map[string]any{"id": 400, "name": "Iris Lowe", "job": "Director"},
		map[string]any{"id": 401, "name": "Iris Lowe", "job": "Screenplay"},
		map[string]any{"id": 402, "name": "Sam Hale", "job": "Producer"},
		map[string]any{"id": 403, "name": "Noor Quinn", "job": "Original Music Composer"},
	}}
}

// prowlarr serves just enough of Prowlarr for Tune-up's indexer checks.
func (s *Stack) prowlarr(w http.ResponseWriter, r *http.Request, p string) {
	if r.Header.Get("X-Api-Key") != "prowlarr-key" {
		w.WriteHeader(401)
		return
	}
	switch p {
	case "/system/status":
		writeJSON(w, map[string]any{"appName": "Prowlarr", "version": "2.7.0"})
	case "/applications":
		writeJSON(w, []any{map[string]any{"name": "Radarr", "syncLevel": "fullSync"}, map[string]any{"name": "Sonarr", "syncLevel": "addOnly"}})
	case "/indexer":
		writeJSON(w, []any{map[string]any{"id": 1, "name": "GeekFake", "enable": true, "protocol": "usenet", "fields": []any{map[string]any{"name": "vipExpiration", "value": s.ProwlarrVIP}}},
			map[string]any{"id": 2, "name": "NinjaFake", "enable": true, "protocol": "usenet", "fields": []any{map[string]any{"name": "vipExpiration", "value": "2135-01-01"}}}})
	case "/indexerstatus":
		writeJSON(w, []any{map[string]any{"indexerId": 1, "disabledTill": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}})
	default:
		http.NotFound(w, r)
	}
}
