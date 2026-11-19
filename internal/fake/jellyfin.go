package fake

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Simulated Jellyfin server under /jellyfin, following the real API's paths
// and shapes (Jellyfin.Api controllers, 10.10/10.11).
//
// People: jfadmin/jfpass (administrator), jfmate/matepass, jfgone/gonepass
// (disabled). API keys made with POST /Auth/Keys are "jf-api-key-N".

type jfUser struct {
	ID, Name, Password string
	Admin, Disabled    bool
}

type jfQuick struct {
	Secret, Code string
	User         string // set once approved
}

var jfUsers = []jfUser{
	{ID: "u-admin", Name: "jfadmin", Password: "jfpass", Admin: true},
	{ID: "u-mate", Name: "jfmate", Password: "matepass"},
	{ID: "u-gone", Name: "jfgone", Password: "gonepass", Disabled: true},
}

func (s *Stack) initJellyfin() {
	s.JFMovies = map[int]time.Time{}
	s.JFShows = map[int]map[int]int{}
	s.JFAdded = map[int]time.Time{}
	s.JFQuickOn = true
	s.jfQuick = map[string]*jfQuick{}
	s.JFLibOpts = map[string]map[string]any{
		"lib-films": {"EnableRealtimeMonitor": false, "ExtractTrickplayImagesDuringLibraryScan": true, "ExtractChapterImagesDuringLibraryScan": false, "EnableTrickplayImageExtraction": true, "PathInfos": []any{map[string]any{"Path": `P:\Media\Films`}}},
		"lib-tv":    {"EnableRealtimeMonitor": true, "ExtractTrickplayImagesDuringLibraryScan": false, "ExtractChapterImagesDuringLibraryScan": false, "PathInfos": []any{map[string]any{"Path": `P:\Media\TV`}}},
		"lib-music": {"EnableRealtimeMonitor": true},
	}
	s.JFConfig = map[string]any{"QuickConnectAvailable": true, "LibraryMonitorDelay": 60, "ServerName": "Jelly Box"}
}

// AddToJellyfin puts a film or episodes in the fake Jellyfin library.
func (s *Stack) AddToJellyfin(typ string, tmdbID int, seasons map[int]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if typ == "movie" {
		s.JFMovies[tmdbID] = time.Now()
		s.Queue["radarr"] = nil
		return
	}
	if s.JFShows[tmdbID] == nil {
		s.JFShows[tmdbID] = map[int]int{}
	}
	for k, v := range seasons {
		s.JFShows[tmdbID][k] = v
	}
	s.JFAdded[tmdbID] = time.Now()
	s.Queue["sonarr"] = nil
}

// RemoveFromJellyfin takes a film or show out again.
func (s *Stack) RemoveFromJellyfin(typ string, tmdbID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if typ == "movie" {
		delete(s.JFMovies, tmdbID)
	} else {
		delete(s.JFShows, tmdbID)
	}
}

// ApproveQuickConnect is what a person does in their Jellyfin app.
func (s *Stack) ApproveQuickConnect(code, userName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.jfQuick {
		if q.Code == code {
			for _, u := range jfUsers {
				if u.Name == userName {
					q.User = u.ID
					return true
				}
			}
		}
	}
	return false
}

var jfTokenRe = regexp.MustCompile(`Token="([^"]*)"`)

func jfToken(r *http.Request) string {
	if m := jfTokenRe.FindStringSubmatch(r.Header.Get("Authorization")); m != nil {
		return m[1]
	}
	return ""
}

func jfUserByID(id string) *jfUser {
	for i := range jfUsers {
		if jfUsers[i].ID == id {
			return &jfUsers[i]
		}
	}
	return nil
}

func jfUserDTO(u *jfUser) map[string]any {
	return map[string]any{"Id": u.ID, "Name": u.Name, "ServerId": "jf-server-1",
		"Policy": map[string]any{"IsAdministrator": u.Admin, "IsDisabled": u.Disabled}}
}

// who returns the caller: "key" for an API key, a user for a user token.
func (s *Stack) jfWho(r *http.Request) (isKey bool, u *jfUser) {
	t := jfToken(r)
	if t == "" {
		return false, nil
	}
	for _, k := range s.JFKeys {
		if k == t {
			return true, nil
		}
	}
	if strings.HasPrefix(t, "jt-") {
		return false, jfUserByID(strings.TrimPrefix(t, "jt-"))
	}
	return false, nil
}

func (s *Stack) jfShowName(tmdbID int) (string, int) {
	for _, sh := range s.Shows {
		if sh.ID == tmdbID {
			return sh.Title, sh.TVDB
		}
	}
	return fmt.Sprintf("Show %d", tmdbID), 0
}

func (s *Stack) jfMovieName(tmdbID int) (string, string, int) {
	for _, m := range s.Movies {
		if m.ID == tmdbID {
			return m.Title, m.IMDB, m.Year
		}
	}
	return fmt.Sprintf("Film %d", tmdbID), "", 0
}

func (s *Stack) jfMovieItem(id int, added time.Time) map[string]any {
	title, imdb, year := s.jfMovieName(id)
	prov := map[string]any{"Tmdb": strconv.Itoa(id)}
	if imdb != "" {
		prov["Imdb"] = imdb
	}
	return map[string]any{"Id": fmt.Sprintf("jm%d", id), "Name": title, "Type": "Movie", "ProductionYear": year,
		"ProviderIds": prov, "Path": fmt.Sprintf(`P:\Media\Films\%s (%d)\%s.mkv`, title, year, title),
		"DateCreated": added.UTC().Format(time.RFC3339), "LocationType": "FileSystem",
		"MediaStreams": []any{map[string]any{"Type": "Video", "Width": 3840, "Height": 2160, "VideoRange": "HDR", "VideoRangeType": "DOVI"},
			map[string]any{"Type": "Audio", "Channels": 6, "IsDefault": true}}}
}

func (s *Stack) jfSeriesItem(id int) map[string]any {
	title, tvdb := s.jfShowName(id)
	// Shows identified only by TVDB (common in Jellyfin) test the fallback.
	prov := map[string]any{"Tvdb": strconv.Itoa(tvdb)}
	if tvdb == 0 || id%2 == 0 {
		prov["Tmdb"] = strconv.Itoa(id)
	}
	return map[string]any{"Id": fmt.Sprintf("js%d", id), "Name": title, "Type": "Series", "ProviderIds": prov,
		"Path": `P:\Media\TV\` + title, "DateCreated": s.JFAdded[id].UTC().Format(time.RFC3339), "LocationType": "FileSystem"}
}

func (s *Stack) jfEpisodes(id int) []map[string]any {
	var out []map[string]any
	var seasons []int
	for sn := range s.JFShows[id] {
		seasons = append(seasons, sn)
	}
	sort.Ints(seasons)
	for _, sn := range seasons {
		n := s.JFShows[id][sn]
		for e := 1; e <= n; e++ {
			ep := map[string]any{"Id": fmt.Sprintf("je%d-%d-%d", id, sn, e), "Name": fmt.Sprintf("Episode %d", e), "Type": "Episode",
				"SeriesId": fmt.Sprintf("js%d", id), "ParentIndexNumber": sn, "IndexNumber": e, "LocationType": "FileSystem",
				"DateCreated": s.JFAdded[id].UTC().Format(time.RFC3339)}
			// Episodes 1 and 2 arrive as one double-episode file, plus a
			// second copy of episode 2: both must count once each.
			if e == 1 && n >= 2 {
				ep["IndexNumberEnd"] = 2
			}
			out = append(out, ep)
		}
		// Jellyfin lists missing episodes as "Virtual" items.
		out = append(out, map[string]any{"Id": fmt.Sprintf("je%d-%d-v", id, sn), "Type": "Episode", "SeriesId": fmt.Sprintf("js%d", id),
			"ParentIndexNumber": sn, "IndexNumber": n + 1, "LocationType": "Virtual"})
	}
	return out
}

func (s *Stack) jellyfin(w http.ResponseWriter, r *http.Request, p string) {
	isKey, who := s.jfWho(r)
	authed := isKey || (who != nil && !who.Disabled)
	admin := isKey || (who != nil && who.Admin && !who.Disabled)
	q := r.URL.Query()
	need := func(ok bool) bool {
		if !ok {
			if !authed {
				w.WriteHeader(401)
			} else {
				w.WriteHeader(403)
			}
		}
		return ok
	}
	switch {
	case p == "/System/Info/Public":
		writeJSON(w, map[string]any{"ServerName": "Jelly Box", "Version": "10.11.3", "Id": "jf-server-1", "ProductName": "Jellyfin Server", "StartupWizardCompleted": true})
	case p == "/System/Info":
		if need(authed) {
			writeJSON(w, map[string]any{"ServerName": "Jelly Box", "Version": "10.11.3", "Id": "jf-server-1"})
		}
	case p == "/Users/AuthenticateByName" && r.Method == http.MethodPost:
		b := readBody(r)
		for i := range jfUsers {
			u := &jfUsers[i]
			if strings.EqualFold(u.Name, fmt.Sprint(b["Username"])) && u.Password == fmt.Sprint(b["Pw"]) && !u.Disabled {
				writeJSON(w, map[string]any{"User": jfUserDTO(u), "AccessToken": "jt-" + u.ID, "ServerId": "jf-server-1"})
				return
			}
		}
		w.WriteHeader(401)
	case p == "/QuickConnect/Enabled":
		writeJSON(w, s.JFQuickOn)
	case p == "/QuickConnect/Initiate" && r.Method == http.MethodPost:
		if !s.JFQuickOn {
			w.WriteHeader(401)
			return
		}
		if !strings.Contains(r.Header.Get("Authorization"), `DeviceId="`) {
			w.WriteHeader(400) // Jellyfin needs to know the device
			return
		}
		n := len(s.jfQuick) + 1
		qc := &jfQuick{Secret: fmt.Sprintf("qs-%d", n), Code: fmt.Sprintf("%06d", 482910+n)}
		s.jfQuick[qc.Secret] = qc
		writeJSON(w, map[string]any{"Secret": qc.Secret, "Code": qc.Code, "Authenticated": false})
	case p == "/QuickConnect/Connect":
		qc := s.jfQuick[q.Get("secret")]
		if qc == nil {
			w.WriteHeader(404)
			return
		}
		writeJSON(w, map[string]any{"Secret": qc.Secret, "Code": qc.Code, "Authenticated": qc.User != ""})
	case p == "/Users/AuthenticateWithQuickConnect" && r.Method == http.MethodPost:
		qc := s.jfQuick[fmt.Sprint(readBody(r)["Secret"])]
		if qc == nil || qc.User == "" {
			w.WriteHeader(401)
			return
		}
		delete(s.jfQuick, qc.Secret) // a secret works once
		u := jfUserByID(qc.User)
		writeJSON(w, map[string]any{"User": jfUserDTO(u), "AccessToken": "jt-" + u.ID, "ServerId": "jf-server-1"})
	case p == "/Users/Me":
		if need(who != nil) {
			writeJSON(w, jfUserDTO(who))
		}
	case p == "/Users":
		if need(admin) {
			var out []any
			for i := range jfUsers {
				out = append(out, jfUserDTO(&jfUsers[i]))
			}
			writeJSON(w, out)
		}
	case strings.HasPrefix(p, "/Users/") && r.Method == http.MethodGet:
		if need(admin) {
			if u := jfUserByID(strings.TrimPrefix(p, "/Users/")); u != nil {
				writeJSON(w, jfUserDTO(u))
			} else {
				w.WriteHeader(404)
			}
		}
	case p == "/Auth/Keys" && r.Method == http.MethodPost:
		if need(who != nil && who.Admin) { // only a signed-in administrator can make keys
			k := fmt.Sprintf("jf-api-key-%d", len(s.JFKeys)+1)
			s.JFKeys = append(s.JFKeys, k)
			s.jfKeyApps = append(s.jfKeyApps, q.Get("app"))
			if s.JFOldKeys { // before 10.11 nothing came back
				w.WriteHeader(204)
				return
			}
			writeJSON(w, map[string]any{"AccessToken": k, "AppName": q.Get("app")})
		}
	case p == "/Auth/Keys":
		if need(admin) {
			var items []any
			for i, k := range s.JFKeys {
				items = append(items, map[string]any{"AccessToken": k, "AppName": s.jfKeyApps[i], "DateCreated": time.Now().Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339)})
			}
			writeJSON(w, map[string]any{"Items": items})
		}
	case p == "/Library/VirtualFolders":
		if need(admin) {
			writeJSON(w, []any{
				map[string]any{"Name": "Films", "ItemId": "lib-films", "CollectionType": "movies", "Locations": []any{`P:\Media\Films`}, "LibraryOptions": s.JFLibOpts["lib-films"]},
				map[string]any{"Name": "TV", "ItemId": "lib-tv", "CollectionType": "tvshows", "Locations": []any{`P:\Media\TV`}, "LibraryOptions": s.JFLibOpts["lib-tv"]},
				map[string]any{"Name": "Music", "ItemId": "lib-music", "CollectionType": "music", "Locations": []any{`P:\Media\Music`}, "LibraryOptions": s.JFLibOpts["lib-music"]},
			})
		}
	case p == "/Library/VirtualFolders/LibraryOptions" && r.Method == http.MethodPost:
		if need(admin) {
			b := readBody(r)
			id := fmt.Sprint(b["Id"])
			opts, _ := b["LibraryOptions"].(map[string]any)
			if s.JFLibOpts[id] == nil || opts == nil {
				w.WriteHeader(404)
				return
			}
			s.JFLibOpts[id] = opts // Jellyfin replaces the whole object
			w.WriteHeader(204)
		}
	case p == "/System/Configuration":
		if need(admin) {
			writeJSON(w, s.JFConfig)
		}
	case p == "/Library/Media/Updated" && r.Method == http.MethodPost:
		if need(authed) {
			if ups, ok := readBody(r)["Updates"].([]any); ok {
				for _, u := range ups {
					if m, ok := u.(map[string]any); ok {
						s.JFScans = append(s.JFScans, fmt.Sprint(m["Path"]))
					}
				}
			}
			w.WriteHeader(204)
		}
	case strings.HasPrefix(p, "/Shows/") && strings.HasSuffix(p, "/Episodes"):
		if need(authed) {
			idStr := strings.TrimSuffix(strings.TrimPrefix(p, "/Shows/js"), "/Episodes")
			id, _ := strconv.Atoi(idStr)
			if s.JFShows[id] == nil {
				w.WriteHeader(404)
				return
			}
			eps := s.jfEpisodes(id)
			if q.Get("IsMissing") == "false" && !s.JFIgnoreMissing {
				var keep []map[string]any
				for _, e := range eps {
					if e["LocationType"] != "Virtual" {
						keep = append(keep, e)
					}
				}
				eps = keep
			}
			writeJSON(w, map[string]any{"Items": eps, "TotalRecordCount": len(eps)})
		}
	case p == "/Items":
		if need(authed) {
			s.jfItems(w, q)
		}
	default:
		http.NotFound(w, r)
	}
}

func (s *Stack) jfItems(w http.ResponseWriter, q map[string][]string) {
	get := func(k string) string {
		for kk, v := range q {
			if strings.EqualFold(kk, k) && len(v) > 0 {
				return v[0]
			}
		}
		return ""
	}
	type row struct {
		item  map[string]any
		added time.Time
		name  string
	}
	var rows []row
	parent, types, term := get("ParentId"), get("IncludeItemTypes"), strings.ToLower(get("SearchTerm"))
	if ids := get("Ids"); ids != "" {
		for _, id := range strings.Split(ids, ",") {
			var n int
			if _, err := fmt.Sscanf(id, "jm%d", &n); err == nil {
				if t, ok := s.JFMovies[n]; ok {
					rows = append(rows, row{item: s.jfMovieItem(n, t)})
				}
			} else if _, err := fmt.Sscanf(id, "js%d", &n); err == nil && s.JFShows[n] != nil {
				rows = append(rows, row{item: s.jfSeriesItem(n)})
			}
		}
	} else {
		for _, typ := range strings.Split(types, ",") {
			switch typ {
			case "Movie":
				if parent != "" && parent != "lib-films" {
					continue
				}
				for id, t := range s.JFMovies {
					it := s.jfMovieItem(id, t)
					rows = append(rows, row{it, t, fmt.Sprint(it["Name"])})
				}
			case "Series":
				if parent != "" && parent != "lib-tv" {
					continue
				}
				for id := range s.JFShows {
					it := s.jfSeriesItem(id)
					rows = append(rows, row{it, s.JFAdded[id], fmt.Sprint(it["Name"])})
				}
			case "Episode":
				if parent != "" && parent != "lib-tv" {
					continue
				}
				for id := range s.JFShows {
					for _, e := range s.jfEpisodes(id) {
						if e["LocationType"] != "Virtual" {
							rows = append(rows, row{e, s.JFAdded[id], ""})
						}
					}
				}
			}
		}
	}
	if term != "" {
		keep := rows[:0]
		for _, r := range rows {
			if strings.Contains(strings.ToLower(r.name), term) {
				keep = append(keep, r)
			}
		}
		rows = keep
	}
	if strings.EqualFold(get("SortBy"), "DateCreated") {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].added.After(rows[j].added) })
	} else {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	}
	total := len(rows)
	start, _ := strconv.Atoi(get("StartIndex"))
	if start > len(rows) {
		start = len(rows)
	}
	rows = rows[start:]
	if lim, _ := strconv.Atoi(get("Limit")); lim > 0 && lim < len(rows) {
		rows = rows[:lim]
	}
	items := make([]any, len(rows))
	for i, r := range rows {
		items[i] = r.item
	}
	writeJSON(w, map[string]any{"Items": items, "TotalRecordCount": total})
}
