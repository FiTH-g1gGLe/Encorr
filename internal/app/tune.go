package app

// Tune-up: checks the settings of Plex, SABnzbd, Radarr, Sonarr and Prowlarr
// against recommendations, explains each one in plain English, and changes
// them on request with a snapshot for Undo.
//
// Safety rules, in order of importance:
//   - Read live settings first. A rule only applies to a setting the app
//     actually reported, so different versions never get a bad write.
//   - Never touch addresses, ports, passwords, keys or remote-access settings.
//   - After every write, read it back. If it didn't take, put the old value
//     back and say so.
//   - Keep the old value of every change, so it can be undone, and warn if
//     someone changed it again since.

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/plex"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// TuneFinding is one line on the Tune-up page.
type TuneFinding struct {
	ID          string `json:"id"`
	App         string `json:"app"`  // sabnzbd | radarr | sonarr | prowlarr | plex | jellyfin
	Area        string `json:"area"` // Reliability | Speed | Library | Playback | Housekeeping | Indexers
	Title       string `json:"title"`
	Why         string `json:"why"`
	Level       string `json:"level"`  // safe | review | info | warn
	Status      string `json:"status"` // ok | change | notice
	Current     string `json:"current,omitempty"`
	Recommended string `json:"recommended,omitempty"`
	CanApply    bool   `json:"canApply"`
	CanUndo     bool   `json:"canUndo"`
	Fix         string `json:"fix,omitempty"` // what to do by hand, when Encorr can't
	// Failed is why the last Apply didn't work, until one does.
	Failed string `json:"failed,omitempty"`
}

// TuneApp is how Tune-up got on with one app.
type TuneApp struct {
	App       string `json:"app"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// tuneEnv is everything read live for one check.
type tuneEnv struct {
	mu        sync.Mutex
	vals      map[string]map[string]any            // app -> key -> value
	raw       map[string]map[string]map[string]any // app -> resource -> object (Radarr/Sonarr config)
	enums     map[string]string                    // Plex pref id -> enumValues
	cfNames   map[string][]string                  // app -> custom format names
	cfRegex   map[string][]string                  // app -> release title regexes in custom formats
	relProf   []map[string]any                     // Sonarr release profiles
	clients   map[string][]map[string]any          // app -> download clients
	roots     map[string][]string                  // app -> library root folders
	profiles  map[string][]map[string]any          // app -> quality profiles
	arrIdx    map[string][]string                  // app -> indexer names set up in Radarr/Sonarr
	backups   map[string][]time.Time               // app -> backup times
	logLevel  map[string]string                    // app -> log level
	apps      map[string]*TuneApp
	sabSrv    []map[string]any
	orphans   int
	prowlarr  *prowlarrInfo
	future    []futureItem
	speed     *store.SpeedTest
	region    string
	libraries []store.PlexLibrary
	jfNames   map[string]string // Jellyfin library id -> name
	plexPass  bool              // the Plex owner has Plex Pass (hardware transcoding needs it)
}

type prowlarrInfo struct {
	apps    []map[string]any
	indexes []map[string]any
	status  []map[string]any
}

type futureItem struct {
	lib       string
	plexType  int
	ratingKey string
	title     string
	file      string
	addedAt   time.Time
}

func newEnv() *tuneEnv {
	return &tuneEnv{vals: map[string]map[string]any{}, raw: map[string]map[string]map[string]any{},
		enums: map[string]string{}, cfNames: map[string][]string{}, cfRegex: map[string][]string{},
		clients: map[string][]map[string]any{}, roots: map[string][]string{}, apps: map[string]*TuneApp{},
		profiles: map[string][]map[string]any{}, arrIdx: map[string][]string{}, backups: map[string][]time.Time{}, logLevel: map[string]string{}}
}

func (e *tuneEnv) set(app, key string, v any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.vals[app] == nil {
		e.vals[app] = map[string]any{}
	}
	e.vals[app][key] = v
}

func (e *tuneEnv) get(app, key string) (any, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.vals[app][key]
	return v, ok
}

var tuneAppNames = map[string]string{"sabnzbd": "SABnzbd", "radarr": "Radarr", "sonarr": "Sonarr", "prowlarr": "Prowlarr", "plex": "Plex", "jellyfin": "Jellyfin"}

// secretKey guards against ever writing credentials or connection settings.
var secretKey = regexp.MustCompile(`(?i)(pass(word)?|api_?key|nzb_key|token|secret|username|^user$|host|port$|^url|url_base|baseurl|cert|https|email|whitelist|local_ranges|inet_exposure|bind|remote|secureconnections|allowednetworks|relay|customconnections|manualportmapping|publishserver)`)

// ---------- reading ----------

// readTune reads the live settings of every connected app (or only the
// given ones) in parallel.
func (a *App) readTune(ctx context.Context, only ...string) *tuneEnv {
	e := newEnv()
	s := a.settings()
	e.region = s.Region
	e.libraries = a.enabledLibraries()
	want := func(app string) bool {
		if len(only) == 0 {
			return true
		}
		for _, o := range only {
			if o == app {
				return true
			}
		}
		return false
	}
	a.Store.Read(func(d *store.Data) {
		if n := len(d.SpeedTests); n > 0 {
			t := d.SpeedTests[n-1]
			e.speed = &t
		}
	})
	if want("prowlarr") {
		a.autoDetectProwlarr(ctx)
	}
	var wg sync.WaitGroup
	run := func(app string, fn func(context.Context, *tuneEnv) error) {
		if !want(app) {
			return
		}
		ta := &TuneApp{App: app, Name: tuneAppNames[app]}
		e.mu.Lock() // apps already started read this map
		e.apps[app] = ta
		e.mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()
			if err := fn(cctx, e); err != nil {
				e.mu.Lock()
				ta.Error = err.Error()
				e.mu.Unlock()
			}
		}()
	}
	if c := a.SAB(); c != nil {
		run("sabnzbd", a.readSAB)
	}
	if c := a.Radarr(); c != nil {
		run("radarr", func(ctx context.Context, e *tuneEnv) error { return a.readArr(ctx, e, "radarr", c) })
	}
	if c := a.Sonarr(); c != nil {
		run("sonarr", func(ctx context.Context, e *tuneEnv) error { return a.readArr(ctx, e, "sonarr", c) })
	}
	if c := a.Prowlarr(); c != nil {
		run("prowlarr", func(ctx context.Context, e *tuneEnv) error { return a.readProwlarr(ctx, e, c) })
	}
	if srv := a.PlexServer(); srv != nil {
		run("plex", func(ctx context.Context, e *tuneEnv) error { return a.readPlex(ctx, e, srv) })
	}
	if jf := a.Jellyfin(); jf != nil {
		run("jellyfin", func(ctx context.Context, e *tuneEnv) error { return a.readJellyfin(ctx, e) })
	}
	wg.Wait()
	// Apps that aren't set up still show, so people know Tune-up can check them.
	for app, name := range tuneAppNames {
		if e.apps[app] == nil && want(app) {
			e.apps[app] = &TuneApp{App: app, Name: name}
		}
	}
	return e
}

func (a *App) readSAB(ctx context.Context, e *tuneEnv) error {
	c := a.SAB()
	cfg, err := c.GetConfig(ctx)
	if err != nil {
		return err
	}
	for k, v := range cfg.Misc {
		e.set("sabnzbd", "misc."+k, v)
	}
	for _, srv := range cfg.Servers {
		name, _ := srv["name"].(string)
		if name == "" || isStraySABServer(srv) {
			continue
		}
		for k, v := range srv {
			e.set("sabnzbd", "server."+name+"."+k, v)
		}
	}
	e.mu.Lock()
	for _, sv := range cfg.Servers {
		if !isStraySABServer(sv) { // empty leftovers aren't real servers
			e.sabSrv = append(e.sabSrv, sv)
		}
	}
	e.mu.Unlock()
	if in, err := c.Info(ctx); err == nil {
		e.mu.Lock()
		e.orphans = len(in.Orphans)
		e.apps["sabnzbd"].Version = in.Version
		e.mu.Unlock()
	}
	e.mu.Lock()
	e.apps["sabnzbd"].Connected = true
	e.mu.Unlock()
	return nil
}

// arrResources are the Radarr/Sonarr settings pages Tune-up reads.
var arrResources = []string{"mediamanagement", "downloadclient", "indexer", "naming"}

func (a *App) readArr(ctx context.Context, e *tuneEnv, app string, c *arr.Client) error {
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.apps[app].Connected = true
	e.apps[app].Version = st.Version
	e.raw[app] = map[string]map[string]any{}
	e.mu.Unlock()
	for _, res := range arrResources {
		var obj map[string]any
		if err := c.GetJSON(ctx, "/config/"+res, &obj); err != nil {
			continue
		}
		e.mu.Lock()
		e.raw[app][res] = obj
		e.mu.Unlock()
		for k, v := range obj {
			if k != "id" {
				e.set(app, res+"."+k, v)
			}
		}
	}
	var cfs []map[string]any
	if c.GetJSON(ctx, "/customformat", &cfs) == nil {
		var names, rx []string
		for _, cf := range cfs {
			if n, _ := cf["name"].(string); n != "" {
				names = append(names, n)
			}
			specs, _ := cf["specifications"].([]any)
			for _, sp := range specs {
				spm, _ := sp.(map[string]any)
				fields, _ := spm["fields"].([]any)
				for _, f := range fields {
					fm, _ := f.(map[string]any)
					if v, ok := fm["value"].(string); ok && fm["name"] == "value" {
						rx = append(rx, v)
					}
				}
			}
		}
		e.mu.Lock()
		e.cfNames[app], e.cfRegex[app] = names, rx
		e.mu.Unlock()
	}
	var profs []map[string]any
	if c.GetJSON(ctx, "/qualityprofile", &profs) == nil {
		e.mu.Lock()
		e.profiles[app] = profs
		e.mu.Unlock()
	}
	var idx []map[string]any
	if c.GetJSON(ctx, "/indexer", &idx) == nil {
		var names []string
		for _, ix := range idx {
			names = append(names, canon(ix["name"]))
		}
		e.mu.Lock()
		e.arrIdx[app] = names
		e.mu.Unlock()
	}
	var bks []map[string]any
	if c.GetJSON(ctx, "/system/backup", &bks) == nil {
		var times []time.Time
		for _, b := range bks {
			if t, err := time.Parse(time.RFC3339, canon(b["time"])); err == nil {
				times = append(times, t)
			}
		}
		e.mu.Lock()
		e.backups[app] = times
		if times == nil {
			e.backups[app] = []time.Time{}
		}
		e.mu.Unlock()
	}
	// Host settings hold the API key and password: only the log level is kept.
	var host map[string]any
	if c.GetJSON(ctx, "/config/host", &host) == nil {
		e.mu.Lock()
		e.logLevel[app] = strings.ToLower(canon(host["logLevel"]))
		e.mu.Unlock()
	}
	if rf, err := c.RootFolders(ctx); err == nil {
		var paths []string
		for _, r := range rf {
			paths = append(paths, r.Path)
		}
		e.mu.Lock()
		e.roots[app] = paths
		e.mu.Unlock()
	}
	var clients []map[string]any
	if c.GetJSON(ctx, "/downloadclient", &clients) == nil {
		e.mu.Lock()
		e.clients[app] = clients
		e.mu.Unlock()
	}
	if app == "sonarr" {
		var rp []map[string]any
		if c.GetJSON(ctx, "/releaseprofile", &rp) == nil {
			e.mu.Lock()
			e.relProf = rp
			e.mu.Unlock()
		}
	}
	return nil
}

func (a *App) readProwlarr(ctx context.Context, e *tuneEnv, c *arr.Client) error {
	var st map[string]any
	if err := c.GetJSON(ctx, "/system/status", &st); err != nil {
		return err
	}
	p := &prowlarrInfo{}
	_ = c.GetJSON(ctx, "/applications", &p.apps)
	_ = c.GetJSON(ctx, "/indexer", &p.indexes)
	_ = c.GetJSON(ctx, "/indexerstatus", &p.status)
	e.mu.Lock()
	e.prowlarr = p
	e.apps["prowlarr"].Connected = true
	e.apps["prowlarr"].Version, _ = st["version"].(string)
	e.mu.Unlock()
	return nil
}

func (a *App) readPlex(ctx context.Context, e *tuneEnv, srv *plex.Server) error {
	prefs, err := srv.Prefs(ctx)
	if err != nil {
		return err
	}
	for _, p := range prefs {
		e.set("plex", "pref."+p.ID, p.Value)
		e.mu.Lock()
		e.enums[p.ID] = p.EnumValues
		e.mu.Unlock()
	}
	pass, _ := srv.PlexPass(ctx)
	e.mu.Lock()
	e.apps["plex"].Connected = true
	e.plexPass = pass
	e.mu.Unlock()
	var fut []futureItem
	for _, lib := range e.libraries {
		typ := 1
		if lib.Type == "show" {
			typ = 4
		}
		items, err := srv.FutureItems(ctx, lib.Key, typ)
		if err != nil {
			continue
		}
		for _, it := range items {
			fi := futureItem{lib: lib.Key, plexType: typ, ratingKey: it.RatingKey, title: it.Title, addedAt: time.Unix(it.AddedAt, 0)}
			if len(it.Media) > 0 && len(it.Media[0].Part) > 0 {
				fi.file = it.Media[0].Part[0].File
			}
			fut = append(fut, fi)
		}
	}
	e.mu.Lock()
	e.future = fut
	e.mu.Unlock()
	return nil
}

// ---------- values ----------

// canon turns any setting value into a comparable string: true/1 -> "1",
// 5.0 -> "5", lists -> "a,b".
func canon(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if x {
			return "1"
		}
		return "0"
	case float64:
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case string:
		s := strings.TrimSpace(x)
		switch strings.ToLower(s) {
		case "true":
			return "1"
		case "false":
			return "0"
		}
		return s
	case []any:
		parts := make([]string, len(x))
		for i, p := range x {
			parts[i] = canon(p)
		}
		return strings.Join(parts, ",")
	case []string:
		return strings.Join(x, ",")
	}
	return fmt.Sprint(v)
}

func sameVal(a, b any) bool { return strings.EqualFold(canon(a), canon(b)) }

func num(v any) float64 {
	f, _ := strconv.ParseFloat(canon(v), 64)
	return f
}

// sizeBytes reads SABnzbd sizes like "1G", "512M", "350M".
func sizeBytes(v any) float64 {
	s := strings.ToUpper(strings.TrimSpace(canon(v)))
	if s == "" {
		return 0
	}
	mult := 1.0
	switch s[len(s)-1] {
	case 'K':
		mult = 1e3
	case 'M':
		mult = 1e6
	case 'G':
		mult = 1e9
	case 'T':
		mult = 1e12
	}
	if mult > 1 {
		s = s[:len(s)-1]
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f * mult
}

func onOff(v any) string {
	if canon(v) == "1" {
		return "On"
	}
	return "Off"
}

func choice(m map[string]string) func(any) string {
	return func(v any) string {
		if s, ok := m[canon(v)]; ok {
			return s
		}
		return canon(v)
	}
}

// ---------- rules ----------

// tuneRule is one recommendation for one setting (key) or, with check, a
// custom finding.
type tuneRule struct {
	id, app, area, level string
	title, why, fix      string
	key                  string
	want                 func(e *tuneEnv, cur any) (any, bool) // recommended value; false = doesn't apply here
	show                 func(any) string
	// check builds custom findings (no single setting behind them).
	check func(e *tuneEnv) []TuneFinding
	// action applies a custom finding; returns undo data (nil = can't undo).
	action func(ctx context.Context, a *App, e *tuneEnv) (map[string]any, string, error)
	undo   func(ctx context.Context, a *App, data map[string]any) error
}

func always(v any) func(*tuneEnv, any) (any, bool) {
	return func(*tuneEnv, any) (any, bool) { return v, true }
}

var defaultUnwanted = []string{"exe", "bat", "cmd", "com", "scr", "vbs", "js", "ps1", "msi", "lnk", "hta", "jar", "pif", "reg", "dll"}

// englishRegions get the foreign-subtitle blocker; elsewhere those subtitles
// are usually wanted.
var englishRegions = map[string]bool{"GB": true, "US": true, "IE": true, "AU": true, "NZ": true, "CA": true}

const foreignSubsRegex = `\b(NL|DK|SE|SWE|NO|NOR|FI|FIN|DE|GER|FR|FRE|ES|SPA|IT|ITA|PL|POL|NORDiC)[ ._-]?SUBS?\b`
const foreignSubsName = "Encorr: foreign subtitles"

func tuneRules() []tuneRule {
	rules := []tuneRule{
		// ---- SABnzbd: failures that should be failures ----
		{id: "sab-pwrar", app: "sabnzbd", area: "Reliability", level: "safe", key: "misc.pause_on_pwrar",
			title: "Abort password-protected downloads",
			why:   "They can never be unpacked. Pausing leaves them stuck in the queue (and can freeze unpacking); aborting fails them, so Radarr/Sonarr block that copy and grab another.",
			want:  always(2), show: choice(map[string]string{"0": "Ignore", "1": "Pause", "2": "Abort"})},
		{id: "sab-unwanted-action", app: "sabnzbd", area: "Reliability", level: "safe", key: "misc.action_on_unwanted_extensions",
			title: "Abort downloads containing programs",
			why:   "Films and episodes never contain .exe or .lnk files. Downloads that do are fakes or malware; aborting them gets a real copy instead.",
			want: func(e *tuneEnv, _ any) (any, bool) {
				mode, _ := e.get("sabnzbd", "misc.unwanted_extensions_mode")
				return 2, canon(mode) == "0" || canon(mode) == ""
			}, show: choice(map[string]string{"0": "Off", "1": "Pause", "2": "Abort"})},
		{id: "sab-unwanted-list", app: "sabnzbd", area: "Reliability", level: "safe", key: "misc.unwanted_extensions",
			title: "List program file types as unwanted",
			why:   "Gives the rule above something to catch.",
			want: func(_ *tuneEnv, cur any) (any, bool) {
				return defaultUnwanted, canon(cur) == ""
			}, show: func(v any) string {
				if c := canon(v); c != "" {
					return c
				}
				return "(none)"
			}},
		{id: "sab-hopeless", app: "sabnzbd", area: "Reliability", level: "safe", key: "misc.fail_hopeless_jobs",
			title: "Give up early on downloads that can't finish",
			why:   "If too much is missing to ever repair it, stop straight away instead of downloading the rest, so another copy can be tried sooner.",
			want:  always(1), show: onOff},
		{id: "sab-fastfail", app: "sabnzbd", area: "Reliability", level: "safe", key: "misc.fast_fail",
			title: "Fail fast on missing articles",
			why:   "Spots downloads your provider doesn't have within seconds rather than minutes.",
			want:  always(1), show: onOff},
		{id: "sab-safepp", app: "sabnzbd", area: "Reliability", level: "safe", key: "misc.safe_postproc",
			title: "Only unpack downloads that checked out",
			why:   "Unpacking a broken download wastes time and can hang. Failing it lets Radarr/Sonarr fetch a good copy.",
			want:  always(1), show: onOff},
		{id: "sab-unrar-dates", app: "sabnzbd", area: "Library", level: "safe", key: "misc.ignore_unrar_dates",
			title: "Give unpacked files today's date",
			why:   "Some releases carry odd dates, even years in the future, which pin them to the top of Plex's Recently Added. Using today's date keeps Recently Added honest.",
			want:  always(1), show: onOff},
		{id: "sab-deobfuscate", app: "sabnzbd", area: "Library", level: "safe", key: "misc.deobfuscate_final_filenames",
			title: "Rename scrambled file names",
			why:   "Some uploads use random file names. Renaming them to the release name helps Radarr/Sonarr import them.",
			want:  always(1), show: onOff},
		{id: "sab-par-cleanup", app: "sabnzbd", area: "Housekeeping", level: "safe", key: "misc.enable_par_cleanup",
			title: "Delete repair files after a successful download",
			why:   "They're only needed while downloading.",
			want:  always(1), show: onOff},
		{id: "sab-cache", app: "sabnzbd", area: "Speed", level: "review", key: "misc.cache_limit",
			title: "Article cache of 1 GB",
			why:   "A bigger memory cache means fewer small disk writes, which helps fast lines. 1 GB is comfortable on any machine with 4 GB of memory or more.",
			want: func(_ *tuneEnv, cur any) (any, bool) {
				return "1G", sizeBytes(cur) < 500e6
			}, show: func(v any) string {
				if canon(v) == "" || canon(v) == "0" {
					return "Off"
				}
				return canon(v)
			}},
		{id: "sab-bandwidth", app: "sabnzbd", area: "Speed", level: "review", key: "misc.bandwidth_max",
			title: "Tell SABnzbd how fast your line is",
			why:   "SABnzbd tunes itself for fast lines (150 MB/s and up) only when it knows the line speed. This uses your last speed test. SABnzbd may need a restart to use it fully.",
			want: func(e *tuneEnv, cur any) (any, bool) {
				if e.speed == nil || e.speed.Internet < 5 {
					return nil, false
				}
				target := int(e.speed.Internet * 0.95)
				have := sizeBytes(cur) / 1e6
				if have == 0 || have < float64(target)*0.7 {
					return fmt.Sprintf("%dM", target), true
				}
				return nil, false
			}, show: func(v any) string {
				if canon(v) == "" {
					return "Not set"
				}
				return canon(v) + "B/s"
			}},

		{id: "sab-pause-pp", app: "sabnzbd", area: "Speed", level: "review", key: "misc.pause_on_post_processing",
			title: "Pause downloading while unpacking",
			why:   "Your speed test decides this one. On a slow drive, downloading and unpacking at the same time fight over the disk and both crawl, so pausing helps. On a fast SSD it's quicker to do both at once.",
			want: func(e *tuneEnv, cur any) (any, bool) {
				if e.speed == nil || e.speed.DownloadDir <= 0 {
					return nil, false
				}
				switch {
				case e.speed.DownloadDir < 100:
					return 1, true
				case e.speed.DownloadDir >= 150 && canon(cur) == "1":
					return 0, true
				}
				return nil, false
			}, show: onOff},
		{id: "sab-propagation", app: "sabnzbd", area: "Reliability", level: "review", key: "misc.propagation_delay",
			title: "Wait 5 minutes before grabbing brand-new uploads",
			why:   "A new upload takes a few minutes to reach every news server. Grabbing it straight away can fail with \"missing articles\" that would have been there minutes later. The trade-off: new episodes start downloading 5 minutes later.",
			want: func(_ *tuneEnv, cur any) (any, bool) {
				return 5, num(cur) == 0
			}, show: func(v any) string {
				if num(v) == 0 {
					return "Off"
				}
				return canon(v) + " min"
			}},
		{id: "sab-same-drive", app: "sabnzbd", check: checkSameDrive},

		// ---- custom SABnzbd checks ----
		{id: "sab-servers", app: "sabnzbd", check: checkSABServers},
		{id: "sab-orphans", app: "sabnzbd", area: "Housekeeping", level: "safe",
			title: "Clear leftover folders from old downloads",
			check: func(e *tuneEnv) []TuneFinding {
				if e.orphans == 0 {
					return nil
				}
				return []TuneFinding{{Status: "change", Current: fmt.Sprintf("%d leftover folder(s)", e.orphans), Recommended: "None",
					Why: "Scraps of downloads that failed or were removed. They're not finished files, just clutter SABnzbd keeps warning about."}}
			},
			action: func(ctx context.Context, a *App, e *tuneEnv) (map[string]any, string, error) {
				return nil, fmt.Sprintf("%d folders", e.orphans), a.SAB().DeleteOrphans(ctx)
			}},
	}

	// ---- Radarr and Sonarr ----
	for _, app := range []string{"radarr", "sonarr"} {
		app := app
		name := tuneAppNames[app]
		rules = append(rules,
			tuneRule{id: app + "-cdh", app: app, area: "Reliability", level: "safe", key: "downloadclient.enableCompletedDownloadHandling",
				title: "Import finished downloads automatically",
				why:   name + " picks up finished downloads from SABnzbd by itself. Without it, nothing reaches Plex.",
				want:  always(true), show: onOff},
			tuneRule{id: app + "-redownload", app: app, area: "Reliability", level: "safe", key: "downloadclient.autoRedownloadFailed",
				title: "Grab another copy when a download fails",
				why:   "Blocks the broken copy and searches for a different one straight away, instead of waiting for someone to notice.",
				want:  always(true), show: onOff},
			tuneRule{id: app + "-propers", app: app, area: "Library", level: "safe", key: "mediamanagement.downloadPropersAndRepacks",
				title: "Let custom formats decide on repacks",
				why:   "You use TRaSH-style custom formats, which score repacks themselves. TRaSH recommends \"Do not prefer\" so " + name + " doesn't double count them and re-download needlessly.",
				want: func(e *tuneEnv, _ any) (any, bool) {
					for _, n := range e.cfNames[app] {
						if strings.EqualFold(n, "Repack/Proper") {
							return "doNotPrefer", true
						}
					}
					return nil, false
				}, show: choice(map[string]string{"preferAndUpgrade": "Prefer and upgrade", "doNotUpgrade": "Don't upgrade", "doNotPrefer": "Do not prefer"})},
			tuneRule{id: app + "-filedate", app: app, area: "Library", level: "review", key: "mediamanagement.fileDate",
				title: "Leave file dates alone",
				why:   "Changing file dates to the air or release date can give files wrong dates when the online info is wrong, which confuses Plex's Recently Added. Leaving them alone is safest.",
				want:  always("none"), show: choice(map[string]string{"none": "Leave alone", "localAirDate": "Local air date", "utcAirDate": "UTC air date", "cinemas": "Cinema release", "release": "Physical release"})},
			tuneRule{id: app + "-freespace", app: app, area: "Reliability", level: "review", key: "mediamanagement.minimumFreeSpaceWhenImporting",
				title: "Keep at least 1 GB free when importing",
				why:   "The default stops at 100 MB, which can fill a drive to the brim. 1 GB leaves room for Windows and Plex.",
				want: func(_ *tuneEnv, cur any) (any, bool) {
					return 1024, num(cur) < 1024
				}, show: func(v any) string { return canon(v) + " MB" }},
			tuneRule{id: app + "-mediainfo", app: app, area: "Library", level: "safe", key: "mediamanagement.enableMediaInfo",
				title: "Read video details from files",
				why:   "Lets " + name + " see resolution, HDR and audio, which quality decisions rely on.",
				want:  always(true), show: onOff},
			tuneRule{id: app + "-illegal", app: app, area: "Library", level: "safe", key: "naming.replaceIllegalCharacters",
				title: "Replace characters Windows can't use in names",
				why:   "Titles with : or ? would otherwise fail to import on Windows.",
				want:  always(true), show: onOff},
			tuneRule{id: app + "-extras", app: app, check: func(e *tuneEnv) []TuneFinding { return checkArrExtras(e, app) }},
			tuneRule{id: app + "-minscore", app: app, area: "Reliability", level: "safe",
				title: "Make \"never want\" scores actually block releases",
				check: func(e *tuneEnv) []TuneFinding { return checkMinScore(e, app) },
				action: func(ctx context.Context, a *App, e *tuneEnv) (map[string]any, string, error) {
					return a.fixMinScore(ctx, e, app)
				},
				undo: func(ctx context.Context, a *App, data map[string]any) error { return a.undoMinScore(ctx, app, data) }},
			tuneRule{id: app + "-foreign-subs", app: app, area: "Reliability", level: "review",
				title: "Skip releases with foreign hard-coded subtitles",
				check: func(e *tuneEnv) []TuneFinding { return checkForeignSubs(e, app) },
				action: func(ctx context.Context, a *App, e *tuneEnv) (map[string]any, string, error) {
					return a.addForeignSubsBlock(ctx, app)
				},
				undo: func(ctx context.Context, a *App, data map[string]any) error {
					c := a.Radarr()
					if app == "sonarr" {
						c = a.Sonarr()
					}
					if c == nil {
						return fmt.Errorf("%s isn't connected", name)
					}
					p, _ := data["delete"].(string)
					if p == "" {
						return nil
					}
					return c.DeleteJSON(ctx, p)
				}},
		)
	}
	rules = append(rules,
		tuneRule{id: "radarr-rename", app: "radarr", area: "Library", level: "review", key: "naming.renameMovies",
			title: "Rename films when importing",
			why:   "Gives every file a clean, consistent name with the title and year, so Plex matches it first time instead of guessing from a release name.",
			want:  always(true), show: onOff},
		tuneRule{id: "sonarr-rename", app: "sonarr", area: "Library", level: "review", key: "naming.renameEpisodes",
			title: "Rename episodes when importing",
			why:   "Gives every file a clean, consistent name with the show, season and episode, so Plex matches it first time.",
			want:  always(true), show: onOff},
		tuneRule{id: "arr-categories", app: "radarr", check: checkCategories},
	)
	rules = append(rules, tuneRule{id: "radarr-hardsubs", app: "radarr", area: "Reliability", level: "safe", key: "indexer.allowHardcodedSubs",
		title: "Skip releases with burned-in subtitles",
		why:   "Releases flagged as having hard-coded subtitles can't have them turned off.",
		want:  always(false), show: func(v any) string {
			if canon(v) == "1" {
				return "Allowed"
			}
			return "Skipped"
		}})

	// ---- Prowlarr ----
	rules = append(rules, tuneRule{id: "prowlarr-indexers", app: "prowlarr", check: checkProwlarr})

	// ---- Plex ----
	behave := choice(map[string]string{"never": "Never", "scheduled": "During maintenance", "asap": "As soon as possible"})
	scheduledIfASAP := func(id string) func(*tuneEnv, any) (any, bool) {
		return func(e *tuneEnv, cur any) (any, bool) {
			return "scheduled", canon(cur) == "asap" && strings.Contains(e.enums[id], "scheduled")
		}
	}
	rules = append(rules,
		tuneRule{id: "plex-fsevents", app: "plex", area: "Library", level: "safe", key: "pref.FSEventLibraryUpdatesEnabled",
			title: "Scan automatically when files change",
			why:   "New films and episodes appear in Plex within moments of being imported.",
			want:  always(true), show: onOff},
		tuneRule{id: "plex-partial", app: "plex", area: "Library", level: "safe", key: "pref.FSEventLibraryPartialScanEnabled",
			title: "Only scan the folder that changed",
			why:   "Much quicker than rescanning the whole library every time one file arrives.",
			want:  always(true), show: onOff},
		tuneRule{id: "plex-scheduled", app: "plex", area: "Library", level: "review", key: "pref.ScheduledLibraryUpdatesEnabled",
			title: "Also scan on a schedule",
			why:   "Automatic scanning relies on Windows noticing changes, which doesn't always work on network or pooled drives. A regular scan catches anything it missed.",
			want:  always(true), show: onOff},
		tuneRule{id: "plex-lowprio", app: "plex", area: "Library", level: "safe", key: "pref.ScannerLowPriority",
			title: "Run scans at a lower priority",
			why:   "Keeps playback smooth while Plex scans.",
			want:  always(true), show: onOff},
		tuneRule{id: "plex-bif", app: "plex", area: "Housekeeping", level: "review", key: "pref.GenerateBIFBehavior",
			title: "Make preview thumbnails overnight",
			why:   "Generating scrubbing thumbnails is heavy on the disk and CPU. Doing it during maintenance hours keeps the server responsive.",
			want:  scheduledIfASAP("GenerateBIFBehavior"), show: behave},
		tuneRule{id: "plex-chapters", app: "plex", area: "Housekeeping", level: "review", key: "pref.GenerateChapterThumbBehavior",
			title: "Make chapter thumbnails overnight",
			why:   "Same idea: heavy work moves to maintenance hours.",
			want:  scheduledIfASAP("GenerateChapterThumbBehavior"), show: behave},
		tuneRule{id: "plex-loudness", app: "plex", area: "Housekeeping", level: "review", key: "pref.LoudnessAnalysisBehavior",
			title: "Analyse loudness overnight",
			why:   "Same idea: heavy work moves to maintenance hours.",
			want:  scheduledIfASAP("LoudnessAnalysisBehavior"), show: behave},
		tuneRule{id: "plex-hwaccel", app: "plex", area: "Playback", level: "review", key: "pref.HardwareAcceleratedCodecs",
			title: "Use the graphics chip for transcoding",
			why:   "When a device can't play a file as it is, Plex converts it on the fly. The graphics chip (Intel Quick Sync, NVIDIA or AMD) does this many times faster than the processor, so several people can stream at once without stutter. If this computer has no usable graphics chip, Plex just carries on using the processor.",
			want: func(e *tuneEnv, cur any) (any, bool) {
				return true, e.plexPass && !truthy(cur)
			}, show: onOff},
		tuneRule{id: "plex-hwencode", app: "plex", area: "Playback", level: "review", key: "pref.HardwareAcceleratedEncoders",
			title: "Let the graphics chip do the encoding too",
			why:   "Without this, the graphics chip only reads the original and the processor still does the heavy half of the work.",
			want: func(e *tuneEnv, cur any) (any, bool) {
				return true, e.plexPass && !truthy(cur)
			}, show: onOff},
		tuneRule{id: "plex-tonemap", app: "plex", area: "Playback", level: "safe", key: "pref.TranscoderToneMapping",
			title: "Fix washed-out colours on HDR films",
			why:   "When an HDR film is converted for a screen that isn't HDR, it looks grey and dull unless Plex adjusts the colours (tone mapping).",
			want:  always(true), show: onOff},
		tuneRule{id: "plex-nopass", app: "plex", area: "Playback",
			check: func(e *tuneEnv) []TuneFinding {
				v, ok := e.get("plex", "pref.HardwareAcceleratedCodecs")
				if !ok || e.plexPass || truthy(v) {
					return nil
				}
				return []TuneFinding{{ID: "plex-nopass", App: "plex", Area: "Playback", Level: "info", Status: "notice",
					Title: "Hardware transcoding needs Plex Pass",
					Why:   "Without it, Plex converts video using the processor only, which limits how many people can stream something that needs converting at the same time. Everything else works as normal.",
					Fix:   "Optional: Plex Pass from plex.tv, then turn on hardware acceleration in Plex → Settings → Transcoder"}}
			}},
		tuneRule{id: "plex-lan", app: "plex", area: "Playback",
			check: func(e *tuneEnv) []TuneFinding {
				v, ok := e.get("plex", "pref.LanNetworksBandwidth")
				if !ok || strings.TrimSpace(canon(v)) != "" {
					return nil
				}
				return []TuneFinding{{ID: "plex-lan", App: "plex", Area: "Playback", Level: "info", Status: "notice",
					Title: "Tell Plex which addresses are your home network",
					Why:   "If Plex can't tell a device is at home, it may treat it as remote and lower the quality. Listing your home network fixes that. Encorr leaves this to you because it's part of Plex's network settings.",
					Fix:   "Plex → Settings → Network → Show Advanced → LAN Networks: your network, e.g. 192.168.1.0/24 if your devices' addresses start 192.168.1"}}
			}},
		tuneRule{id: "plex-future", app: "plex", area: "Library", level: "safe",
			title: "Fix items dated in the future",
			check: func(e *tuneEnv) []TuneFinding {
				if len(e.future) == 0 {
					return nil
				}
				names := []string{}
				seen := map[string]bool{}
				for _, f := range e.future {
					if !seen[f.title] && len(names) < 4 {
						seen[f.title] = true
						names = append(names, f.title)
					}
				}
				return []TuneFinding{{Status: "change", Current: fmt.Sprintf("%d item(s): %s", len(e.future), strings.Join(names, ", ")), Recommended: "Dated today",
					Why: "Files with dates in the future stay stuck at the top of Recently Added. Encorr resets their date in Plex (and on the file, when it's on this computer)."}}
			},
			action: func(ctx context.Context, a *App, e *tuneEnv) (map[string]any, string, error) {
				n, err := a.fixFutureItems(ctx, e)
				return nil, fmt.Sprintf("%d items", n), err
			}},
	)
	return append(rules, jellyfinRules()...)
}

// checkSABServers looks at each news server: connections, pipelining and,
// with a speed test, how many articles it actually had.
func checkSABServers(e *tuneEnv) []TuneFinding {
	var out []TuneFinding
	for _, srv := range e.sabSrv {
		name, _ := srv["name"].(string)
		label := name
		if dn, _ := srv["displayname"].(string); dn != "" {
			label = dn
		}
		if canon(srv["enable"]) == "0" {
			continue
		}
		if v, ok := srv["pipelining_requests"]; ok && num(v) < 3 {
			out = append(out, TuneFinding{ID: "sab-pipelining:" + name, App: "sabnzbd", Area: "Speed", Level: "review", Status: "change",
				Title: "Ask " + label + " for more articles at once", Current: canon(v), Recommended: "4", CanApply: true, CanUndo: true,
				Why: "Asking for several articles per request keeps each connection busy. SABnzbd suggests 4 or more on fast lines; servers set up before SABnzbd 5 keep a low number."})
		}
		if e.speed != nil && e.speed.CPU > 0 && e.speed.CPU < 100000 && canon(srv["ssl"]) == "1" && canon(srv["ssl_ciphers"]) == "" {
			out = append(out, TuneFinding{ID: "sab-ciphers:" + name, App: "sabnzbd", Area: "Speed", Level: "review", Status: "change",
				Title: "Use a lighter encryption for " + label, Current: "Automatic", Recommended: "AES128", CanApply: true, CanUndo: true,
				Why: "Your speed test shows a modest CPU. AES128 is just as private for this and much lighter, which can lift download speed."})
		}
		if c := num(srv["connections"]); c > 50 {
			out = append(out, TuneFinding{ID: "sab-connections:" + name, App: "sabnzbd", Area: "Speed", Level: "info", Status: "notice",
				Title: "Check how many connections " + label + " allows", Current: canon(srv["connections"]),
				Why: "More connections than your provider allows causes errors and slows things down. Most allow 30 to 60; check your account page, then try a few values with the speed test.",
				Fix: "SABnzbd → Config → Servers → Connections"})
		}
	}
	if e.speed != nil {
		for _, s := range e.speed.Servers {
			if !s.Backup && s.Tried > 50000 && s.Rate < 80 {
				out = append(out, TuneFinding{ID: "sab-completion:" + s.Name, App: "sabnzbd", Area: "Speed", Level: "warn", Status: "notice",
					Title: fmt.Sprintf("Your news server only had %.0f%% of what was asked for", s.Rate), Current: fmt.Sprintf("%.0f%% over the last two weeks", s.Rate),
					Why: "When a provider is missing articles, downloads crawl and fail. Older shows are often the worst. A backup \"block\" account on a different backbone fills the gaps cheaply.",
					Fix: "Add a block account from a provider on a different backbone in SABnzbd → Config → Servers, with priority 1 so it's only used for what your main server is missing."})
			}
		}
	}
	return out
}

// checkArrExtras covers things Encorr can explain but won't change itself.
func checkArrExtras(e *tuneEnv, app string) []TuneFinding {
	name := tuneAppNames[app]
	var out []TuneFinding
	if v, ok := e.get(app, "mediamanagement.recycleBin"); ok && canon(v) == "" {
		out = append(out, TuneFinding{ID: app + "-recycle", App: app, Area: "Housekeeping", Level: "info", Status: "notice",
			Title: "Turn on a recycle bin", Current: "Off (deleted files are gone for good)",
			Why: "Upgrades and deletes go to a folder you can restore from for a week, instead of disappearing.",
			Fix: name + " → Settings → Media Management → Recycling Bin: pick a folder on the same drive as your media."})
	}
	if e.prowlarr != nil {
		var manual []string
		for _, n := range e.arrIdx[app] {
			if !strings.HasSuffix(n, "(Prowlarr)") {
				manual = append(manual, n)
			}
		}
		if len(manual) > 0 {
			out = append(out, TuneFinding{ID: app + "-manual-indexers", App: app, Area: "Indexers", Level: "info", Status: "notice",
				Title: "Indexers set up outside Prowlarr", Current: strings.Join(manual, ", "),
				Why: "These were added straight into " + name + ", so they miss Prowlarr's syncing, limits and health checks, and may be searched twice.",
				Fix: "Add them in Prowlarr instead, then delete them from " + name + " → Settings → Indexers."})
		}
	}
	if times, ok := e.backups[app]; ok {
		newest := time.Time{}
		for _, t := range times {
			if t.After(newest) {
				newest = t
			}
		}
		if newest.IsZero() || time.Since(newest) > 14*24*time.Hour {
			cur := "None found"
			if !newest.IsZero() {
				cur = "Last one " + newest.Format("2 Jan 2006")
			}
			out = append(out, TuneFinding{ID: app + "-backups", App: app, Area: "Housekeeping", Level: "warn", Status: "notice",
				Title: name + " hasn't backed up recently", Current: cur,
				Why: "A backup holds your whole setup (profiles, custom formats, history). Without one, a broken database means starting again.",
				Fix: name + " → System → Backup → Backup now, and check Settings → General → Backups (every 7 days is the default)."})
		}
	}
	if lv := e.logLevel[app]; lv == "debug" || lv == "trace" {
		out = append(out, TuneFinding{ID: app + "-loglevel", App: app, Area: "Housekeeping", Level: "info", Status: "notice",
			Title: name + " is keeping very detailed logs", Current: map[string]string{"debug": "Debug", "trace": "Trace"}[lv],
			Why: "Debug and Trace logging is for chasing a problem. Left on, it writes a lot to disk and slows things down.",
			Fix: name + " → Settings → General → Log Level: Info."})
	}
	for _, c := range e.clients[app] {
		if !strings.EqualFold(fmt.Sprint(c["implementation"]), "Sabnzbd") {
			continue
		}
		if v, ok := c["removeCompletedDownloads"]; ok && canon(v) == "0" {
			out = append(out, TuneFinding{ID: app + "-removecompleted", App: app, Area: "Housekeeping", Level: "info", Status: "notice",
				Title: "Tidy SABnzbd's history after imports", Current: "Off",
				Why: "Finished downloads pile up in SABnzbd's history and folders.",
				Fix: name + " → Settings → Download Clients → SABnzbd → Remove Completed."})
		}
	}
	return out
}

func checkForeignSubs(e *tuneEnv, app string) []TuneFinding {
	if !englishRegions[strings.ToUpper(e.region)] {
		return nil // elsewhere, subtitled releases are often what people want
	}
	re := regexp.MustCompile(`(?i)subs`)
	has := false
	if app == "sonarr" {
		for _, rp := range e.relProf {
			ign, _ := rp["ignored"].([]any)
			for _, x := range ign {
				if re.MatchString(canon(x)) {
					has = true
				}
			}
		}
	}
	for _, rx := range e.cfRegex[app] {
		if re.MatchString(rx) {
			has = true
		}
	}
	for _, n := range e.cfNames[app] {
		if n == foreignSubsName {
			has = true
		}
	}
	f := TuneFinding{Current: "Not blocked", Recommended: "Blocked",
		Why: "Releases like \"NL Subs\" or \"NORDiC Subs\" have subtitles burned in, and some are password-protected traps that freeze unpacking. Skipping them by name means they're never downloaded."}
	if has {
		f.Status, f.Current, f.Recommended = "ok", "Blocked", ""
	} else {
		f.Status = "change"
	}
	return []TuneFinding{f}
}

func checkProwlarr(e *tuneEnv) []TuneFinding {
	p := e.prowlarr
	if p == nil {
		return nil
	}
	var out []TuneFinding
	enabled := 0
	now := time.Now()
	for _, ix := range p.indexes {
		if canon(ix["enable"]) != "1" {
			continue
		}
		enabled++
		name := fmt.Sprint(ix["name"])
		fields, _ := ix["fields"].([]any)
		for _, f := range fields {
			fm, _ := f.(map[string]any)
			if fm["name"] != "vipExpiration" {
				continue
			}
			d, err := time.Parse("2006-01-02", canon(fm["value"]))
			if err != nil {
				continue
			}
			days := int(d.Sub(now).Hours() / 24)
			switch {
			case days < 0:
				out = append(out, TuneFinding{ID: "prowlarr-vip:" + name, App: "prowlarr", Area: "Indexers", Level: "warn", Status: "notice",
					Title: name + " VIP has ended", Current: "Ended " + d.Format("2 Jan 2006"),
					Why: "Without VIP the indexer may stop answering searches, or only answer a few a day.", Fix: "Renew it on the indexer's website."})
			case days <= 30:
				out = append(out, TuneFinding{ID: "prowlarr-vip:" + name, App: "prowlarr", Area: "Indexers", Level: "warn", Status: "notice",
					Title: fmt.Sprintf("%s VIP ends in %d days", name, days), Current: "Ends " + d.Format("2 Jan 2006"),
					Why: "When it lapses, searches through it stop or are heavily limited.", Fix: "Renew it on the indexer's website before then."})
			}
		}
	}
	for _, st := range p.status {
		until, _ := time.Parse(time.RFC3339, canon(st["disabledTill"]))
		if until.After(now) {
			name := fmt.Sprint(st["indexerId"])
			for _, ix := range p.indexes {
				if canon(ix["id"]) == canon(st["indexerId"]) {
					name = fmt.Sprint(ix["name"])
				}
			}
			out = append(out, TuneFinding{ID: "prowlarr-down:" + name, App: "prowlarr", Area: "Indexers", Level: "warn", Status: "notice",
				Title: name + " is paused after failures", Current: "Until " + until.Local().Format("15:04 2 Jan"),
				Why: "Prowlarr stops using an indexer for a while after errors. Often it's down, or its daily limit ran out.",
				Fix: "Wait for it to come back, or test it in Prowlarr → Indexers. If it happens daily, set a query limit just under your account's allowance on the indexer in Prowlarr."})
		}
	}
	switch {
	case enabled == 0:
		out = append(out, TuneFinding{ID: "prowlarr-none", App: "prowlarr", Area: "Indexers", Level: "warn", Status: "notice",
			Title: "No indexers are switched on", Why: "Radarr and Sonarr can't find anything without one.", Fix: "Add an indexer in Prowlarr → Indexers."})
	case enabled == 1:
		out = append(out, TuneFinding{ID: "prowlarr-one", App: "prowlarr", Area: "Indexers", Level: "info", Status: "notice",
			Title: "Only one indexer", Current: "1 switched on",
			Why: "Each indexer lists different uploads. Two or three find far more, and keep things going when one is down.", Fix: "Add another in Prowlarr → Indexers."})
	default:
		out = append(out, TuneFinding{ID: "prowlarr-count", App: "prowlarr", Area: "Indexers", Level: "info", Status: "ok",
			Title: fmt.Sprintf("%d indexers switched on", enabled), Why: "More than one indexer means more to choose from."})
	}
	for _, ap := range p.apps {
		if lvl := canon(ap["syncLevel"]); lvl != "" && lvl != "fullSync" {
			out = append(out, TuneFinding{ID: "prowlarr-sync:" + fmt.Sprint(ap["name"]), App: "prowlarr", Area: "Indexers", Level: "info", Status: "notice",
				Title: fmt.Sprint(ap["name"]) + " doesn't get indexer changes", Current: map[string]string{"addOnly": "Add and remove only", "disabled": "Off"}[lvl],
				Why: "With Full Sync, indexers you add or change in Prowlarr reach " + fmt.Sprint(ap["name"]) + " automatically.",
				Fix: "Prowlarr → Settings → Apps → " + fmt.Sprint(ap["name"]) + " → Sync Level: Full Sync."})
		}
	}
	return out
}

// ---------- checking ----------

// TuneCheck reads everything and lists every recommendation.
func (a *App) TuneCheck(ctx context.Context) ([]TuneFinding, []TuneApp) {
	e := a.readTune(ctx)
	fs := a.evaluate(e)
	a.mu.Lock()
	for i := range fs {
		if f, ok := a.tuneFails[fs[i].ID]; ok && fs[i].Status == "change" && time.Since(f.at) < 7*24*time.Hour {
			fs[i].Failed = f.msg
		}
	}
	a.mu.Unlock()
	return fs, appList(e)
}

func appList(e *tuneEnv) []TuneApp {
	var out []TuneApp
	for _, k := range []string{"sabnzbd", "radarr", "sonarr", "prowlarr", "plex", "jellyfin"} {
		if ta := e.apps[k]; ta != nil {
			out = append(out, *ta)
		}
	}
	return out
}

func (a *App) evaluate(e *tuneEnv) []TuneFinding {
	var out []TuneFinding
	for _, r := range tuneRules() {
		if ta := e.apps[r.app]; ta == nil || !ta.Connected {
			continue
		}
		if r.check != nil {
			for _, f := range r.check(e) {
				if f.ID == "" {
					f.ID = r.id
				}
				if f.App == "" {
					f.App = r.app
				}
				if f.Area == "" {
					f.Area = r.area
				}
				if f.Level == "" {
					f.Level = r.level
				}
				if f.Title == "" {
					f.Title = r.title
				}
				if f.Why == "" {
					f.Why = r.why
				}
				if f.Status == "change" && r.action != nil && f.ID == r.id {
					f.CanApply = true
					f.CanUndo = r.undo != nil
				}
				out = append(out, f)
			}
			continue
		}
		cur, ok := e.get(r.app, r.key)
		if !ok {
			continue // this version doesn't have the setting
		}
		want, applies := r.want(e, cur)
		if !applies {
			continue
		}
		f := TuneFinding{ID: r.id, App: r.app, Area: r.area, Title: r.title, Why: r.why, Level: r.level,
			Current: r.show(cur), Recommended: r.show(want), CanUndo: true}
		if sameVal(cur, want) {
			f.Status, f.Recommended = "ok", ""
		} else {
			f.Status, f.CanApply = "change", true
		}
		out = append(out, f)
	}
	order := map[string]int{"warn": 0, "safe": 1, "review": 2, "info": 3}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := out[i].Status == "ok", out[j].Status == "ok"
		if si != sj {
			return !si
		}
		return order[out[i].Level] < order[out[j].Level]
	})
	return out
}

// ---------- applying ----------

// ruleFor finds the rule behind a finding id ("sab-pipelining:server" maps
// to a dynamic server rule).
func ruleFor(id string) (tuneRule, string, bool) {
	if k, srv, ok := strings.Cut(id, ":"); ok {
		switch k {
		case "sab-pipelining":
			return tuneRule{id: id, app: "sabnzbd", key: "server." + srv + ".pipelining_requests", title: "More articles per request",
				want: func(_ *tuneEnv, cur any) (any, bool) { return 4, num(cur) < 3 }, show: canon}, srv, true
		case "sab-ciphers":
			return tuneRule{id: id, app: "sabnzbd", key: "server." + srv + ".ssl_ciphers", title: "Lighter encryption",
				want: func(_ *tuneEnv, cur any) (any, bool) { return "AES128", canon(cur) == "" }, show: canon}, srv, true
		}
		return tuneRule{}, "", false
	}
	for _, r := range tuneRules() {
		if r.id == id && (r.key != "" || r.action != nil) {
			return r, "", true
		}
	}
	return tuneRule{}, "", false
}

// TuneApply makes one recommended change. It reads the setting fresh,
// writes it, reads it back, and records the old value for Undo.
func (a *App) TuneApply(ctx context.Context, id, by string) (*store.TuneChange, error) {
	r, _, ok := ruleFor(id)
	if !ok {
		return nil, userErr("Tune-up doesn't know that one.")
	}
	e := a.readTune(ctx, r.app)
	if ta := e.apps[r.app]; ta == nil || !ta.Connected {
		msg := tuneAppNames[r.app] + " isn't connected"
		if ta != nil && ta.Error != "" {
			msg += ": " + ta.Error
		}
		return nil, userErr(msg + ".")
	}
	ch := store.TuneChange{At: time.Now(), Rule: id, App: r.app, Key: r.key, Title: r.title, By: by}
	if r.action != nil {
		undo, what, err := r.action(ctx, a, e)
		if err != nil {
			return nil, err
		}
		ch.Undo, ch.CanUndo, ch.New = undo, undo != nil, what
		a.recordTune(&ch)
		return &ch, nil
	}
	if secretKey.MatchString(strings.TrimPrefix(strings.TrimPrefix(r.key, "misc."), "pref.")) && !strings.HasPrefix(r.key, "server.") {
		return nil, userErr("Tune-up never changes connection or security settings.")
	}
	cur, ok := e.get(r.app, r.key)
	if !ok {
		return nil, userErr(tuneAppNames[r.app] + " doesn't have that setting in this version.")
	}
	want, applies := r.want(e, cur)
	if !applies || sameVal(cur, want) {
		return nil, userErr("Nothing to change: it's already fine.")
	}
	if err := a.writeSetting(ctx, e, r.app, r.key, want); err != nil {
		return nil, err
	}
	if err := a.verifySetting(ctx, r.app, r.key, want); err != nil {
		_ = a.writeSetting(ctx, a.readTune(ctx, r.app), r.app, r.key, cur)
		return nil, err
	}
	ch.Old, ch.New, ch.CanUndo = cur, want, true
	a.recordTune(&ch)
	return &ch, nil
}

func (a *App) recordTune(ch *store.TuneChange) {
	_ = a.Store.Update(func(d *store.Data) error {
		ch.ID = d.NewID("tune")
		d.TuneChanges = append(d.TuneChanges, *ch)
		if len(d.TuneChanges) > 200 {
			d.TuneChanges = d.TuneChanges[len(d.TuneChanges)-200:]
		}
		return nil
	})
	a.activity("action", "Tune-up: "+ch.Title, tuneAppNames[ch.App]+": "+canon(ch.Old)+" → "+canon(ch.New))
}

// verifySetting reads a setting back after a write.
func (a *App) verifySetting(ctx context.Context, app, key string, want any) error {
	e := a.readTune(ctx, app)
	got, ok := e.get(app, key)
	if !ok || !sameVal(got, want) {
		return fmt.Errorf("%s didn't keep the change (it still says %q), so Encorr put it back", tuneAppNames[app], canon(got))
	}
	return nil
}

// writeSetting writes one value the way each app expects.
func (a *App) writeSetting(ctx context.Context, e *tuneEnv, app, key string, v any) error {
	switch app {
	case "sabnzbd":
		c := a.SAB()
		if c == nil {
			return userErr("SABnzbd isn't connected.")
		}
		val := canon(v)
		if strings.HasPrefix(key, "server.") {
			rest := strings.TrimPrefix(key, "server.")
			i := strings.LastIndex(rest, ".")
			if i < 0 {
				return userErr("Bad server setting.")
			}
			return a.setSABServer(ctx, c, rest[:i], rest[i+1:], val)
		}
		section, kw, _ := strings.Cut(key, ".")
		return c.SetConfig(ctx, section, kw, val)
	case "radarr", "sonarr":
		c := a.Radarr()
		if app == "sonarr" {
			c = a.Sonarr()
		}
		if c == nil {
			return userErr(tuneAppNames[app] + " isn't connected.")
		}
		res, field, _ := strings.Cut(key, ".")
		obj := map[string]any{}
		for k, x := range e.raw[app][res] {
			obj[k] = x
		}
		if len(obj) == 0 {
			return userErr("Couldn't read " + tuneAppNames[app] + "'s " + res + " settings.")
		}
		obj[field] = coerceLike(obj[field], v)
		return c.PutJSON(ctx, fmt.Sprintf("/config/%s/%s", res, canon(obj["id"])), obj)
	case "plex":
		srv := a.PlexServer()
		if srv == nil {
			return userErr("Plex isn't connected.")
		}
		id := strings.TrimPrefix(key, "pref.")
		cur, _ := e.get("plex", key)
		val := canon(v)
		if _, isBool := cur.(bool); isBool {
			if err := srv.SetPref(ctx, id, val); err != nil {
				return err
			}
			// Some Plex versions want true/false rather than 1/0.
			if got, ok := a.readTune(ctx, "plex").get("plex", key); ok && !sameVal(got, v) {
				return srv.SetPref(ctx, id, map[string]string{"1": "true", "0": "false"}[val])
			}
			return nil
		}
		return srv.SetPref(ctx, id, val)
	}
	return userErr("Tune-up can't change " + app + ".")
}

// coerceLike gives a new value the same JSON type as the old one.
func coerceLike(old, v any) any {
	switch old.(type) {
	case bool:
		return canon(v) == "1"
	case float64:
		return num(v)
	case string:
		return canon(v)
	}
	return v
}

// TuneUndo puts a change back. Without force it refuses if the setting was
// changed again since, so it never overwrites someone's later choice.
func (a *App) TuneUndo(ctx context.Context, changeID int, force bool, by string) error {
	var ch *store.TuneChange
	a.Store.Read(func(d *store.Data) {
		for i := range d.TuneChanges {
			if d.TuneChanges[i].ID == changeID {
				c := d.TuneChanges[i]
				ch = &c
			}
		}
	})
	if ch == nil {
		return userErr("That change isn't in the history any more.")
	}
	if ch.Undone {
		return userErr("That change was already undone.")
	}
	if !ch.CanUndo {
		return userErr("That one can't be undone.")
	}
	if ch.Key == "" {
		r, _, ok := ruleFor(ch.Rule)
		if !ok || r.undo == nil {
			return userErr("That one can't be undone.")
		}
		if err := r.undo(ctx, a, ch.Undo); err != nil {
			return err
		}
	} else {
		e := a.readTune(ctx, ch.App)
		cur, ok := e.get(ch.App, ch.Key)
		if !ok {
			return userErr(tuneAppNames[ch.App] + " isn't reachable right now, or no longer has that setting.")
		}
		if !force && !sameVal(cur, ch.New) {
			return &conflictErr{fmt.Sprintf("It's been changed again since (it's now %q). Undo anyway?", canon(cur))}
		}
		if err := a.writeSetting(ctx, e, ch.App, ch.Key, ch.Old); err != nil {
			return err
		}
		if err := a.verifySetting(ctx, ch.App, ch.Key, ch.Old); err != nil {
			return err
		}
	}
	_ = a.Store.Update(func(d *store.Data) error {
		for i := range d.TuneChanges {
			if d.TuneChanges[i].ID == changeID {
				d.TuneChanges[i].Undone = true
			}
		}
		return nil
	})
	a.activity("action", "Tune-up undone: "+ch.Title, "by "+by)
	return nil
}

type conflictErr struct{ msg string }

func (e *conflictErr) Error() string { return e.msg }

// ---------- actions ----------

// addForeignSubsBlock adds a rule that skips foreign hard-coded-subtitle
// releases: a release profile in Sonarr, a custom format scored -10000 in
// every Radarr profile.
func (a *App) addForeignSubsBlock(ctx context.Context, app string) (map[string]any, string, error) {
	c := a.Radarr()
	if app == "sonarr" {
		c = a.Sonarr()
	}
	if c == nil {
		return nil, "", userErr(tuneAppNames[app] + " isn't connected.")
	}
	if app == "sonarr" {
		var made map[string]any
		body := map[string]any{"name": foreignSubsName, "enabled": true, "required": []string{}, "ignored": []string{"/" + foreignSubsRegex + "/i"}, "indexerId": 0, "tags": []int{}}
		if err := c.PostJSON(ctx, "/releaseprofile", body, &made); err != nil {
			return nil, "", err
		}
		return map[string]any{"delete": "/releaseprofile/" + canon(made["id"])}, "release profile added", nil
	}
	var made map[string]any
	cf := map[string]any{"name": foreignSubsName, "includeCustomFormatWhenRenaming": false, "specifications": []any{map[string]any{
		"name": "Foreign subtitles in the name", "implementation": "ReleaseTitleSpecification", "negate": false, "required": true,
		"fields": []any{map[string]any{"name": "value", "value": foreignSubsRegex}}}}}
	if err := c.PostJSON(ctx, "/customformat", cf, &made); err != nil {
		return nil, "", err
	}
	cfID := num(made["id"])
	undo := map[string]any{"delete": "/customformat/" + canon(made["id"])}
	var profiles []map[string]any
	if err := c.GetJSON(ctx, "/qualityprofile", &profiles); err != nil {
		return undo, "custom format added", nil
	}
	n := 0
	for _, p := range profiles {
		items, _ := p["formatItems"].([]any)
		found := false
		for _, it := range items {
			im, _ := it.(map[string]any)
			if num(im["format"]) == cfID {
				im["score"] = -10000
				found = true
			}
		}
		if !found {
			items = append(items, map[string]any{"format": cfID, "name": foreignSubsName, "score": -10000})
			p["formatItems"] = items
		}
		if err := c.PutJSON(ctx, "/qualityprofile/"+canon(p["id"]), p); err == nil {
			n++
		}
	}
	return undo, fmt.Sprintf("custom format added to %d profile(s)", n), nil
}

// driveOf returns the Windows drive letter ("D:") or UNC share of a path, or
// "" when it can't tell (Linux paths, relative paths).
func driveOf(p string) string {
	p = strings.TrimSpace(p)
	if len(p) >= 2 && p[1] == ':' {
		return strings.ToUpper(p[:2])
	}
	if strings.HasPrefix(p, `\\`) {
		parts := strings.SplitN(strings.TrimPrefix(p, `\\`), `\`, 3)
		if len(parts) >= 2 {
			return strings.ToLower(`\\` + parts[0] + `\` + parts[1])
		}
	}
	return ""
}

// checkSameDrive spots finished downloads being copied to the library
// rather than moved, which happens when they're on different drives.
func checkSameDrive(e *tuneEnv) []TuneFinding {
	cv, ok := e.get("sabnzbd", "misc.complete_dir")
	if !ok {
		return nil
	}
	complete := driveOf(canon(cv))
	if complete == "" {
		return nil
	}
	libDrives := map[string]bool{}
	for _, app := range []string{"radarr", "sonarr"} {
		for _, r := range e.roots[app] {
			if d := driveOf(r); d != "" {
				libDrives[d] = true
			}
		}
	}
	if len(libDrives) == 0 || libDrives[complete] {
		return nil
	}
	var list []string
	for d := range libDrives {
		list = append(list, d)
	}
	sort.Strings(list)
	return []TuneFinding{{ID: "sab-same-drive", App: "sabnzbd", Area: "Speed", Level: "info", Status: "notice",
		Title:   "Finished downloads are copied, not moved",
		Current: "Completed folder on " + complete + ", library on " + strings.Join(list, ", "),
		Why:     "Moving a file within the same drive is instant. Between drives, every film is copied and then deleted, which takes minutes and wears the disks. If you can, put SABnzbd's Completed folder on the same drive as your library, and keep the temporary download folder on your fastest drive. With pooled drives (like DrivePool) this may not be possible, which is fine.",
		Fix:     "SABnzbd → Config → Folders → Completed Download Folder."}}
}

// fieldValue reads a named field from a Radarr/Sonarr "fields" list.
func fieldValue(obj map[string]any, name string) (any, bool) {
	fields, _ := obj["fields"].([]any)
	for _, f := range fields {
		fm, _ := f.(map[string]any)
		if fm["name"] == name {
			return fm["value"], true
		}
	}
	return nil, false
}

// checkCategories makes sure Radarr and Sonarr each send SABnzbd their own
// category, so downloads don't collide or land in the wrong folder.
func checkCategories(e *tuneEnv) []TuneFinding {
	cats := map[string]string{}
	for app, field := range map[string]string{"radarr": "movieCategory", "sonarr": "tvCategory"} {
		for _, c := range e.clients[app] {
			if !strings.EqualFold(canon(c["implementation"]), "Sabnzbd") || canon(c["enable"]) == "0" {
				continue
			}
			if v, ok := fieldValue(c, field); ok {
				cats[app] = strings.ToLower(canon(v))
			}
		}
	}
	var out []TuneFinding
	for _, app := range []string{"radarr", "sonarr"} {
		v, ok := cats[app]
		if ok && v == "" {
			out = append(out, TuneFinding{ID: app + "-category", App: app, Area: "Reliability", Level: "warn", Status: "notice",
				Title: tuneAppNames[app] + " doesn't give SABnzbd a category", Current: "No category",
				Why: "Without its own category, " + tuneAppNames[app] + "'s downloads mix with everything else in SABnzbd and can end up in the wrong folder or be picked up by the wrong app.",
				Fix: tuneAppNames[app] + " → Settings → Download Clients → SABnzbd → Category (for example \"" + map[string]string{"radarr": "movies", "sonarr": "tv"}[app] + "\")."})
		}
	}
	if r, s := cats["radarr"], cats["sonarr"]; r != "" && r == s {
		out = append(out, TuneFinding{ID: "arr-category-clash", App: "sonarr", Area: "Reliability", Level: "warn", Status: "notice",
			Title: "Radarr and Sonarr share a SABnzbd category", Current: "Both use \"" + r + "\"",
			Why: "Each app then sees the other's downloads, which leads to failed or wrong imports.",
			Fix: "Give each its own category in Settings → Download Clients → SABnzbd (for example \"movies\" and \"tv\")."})
	}
	return out
}

// lowestScore is the most negative custom format score in a profile.
func lowestScore(p map[string]any) float64 {
	low := 0.0
	items, _ := p["formatItems"].([]any)
	for _, it := range items {
		im, _ := it.(map[string]any)
		if sc := num(im["score"]); sc < low {
			low = sc
		}
	}
	return low
}

// badMinScore lists profiles whose minimum score lets "never want" formats
// (scored -10000 or so) through anyway.
func badMinScore(e *tuneEnv, app string) []map[string]any {
	var bad []map[string]any
	for _, p := range e.profiles[app] {
		low := lowestScore(p)
		if low <= -1000 && num(p["minFormatScore"]) <= low {
			bad = append(bad, p)
		}
	}
	return bad
}

func checkMinScore(e *tuneEnv, app string) []TuneFinding {
	if len(e.profiles[app]) == 0 {
		return nil
	}
	bad := badMinScore(e, app)
	if len(bad) == 0 {
		return []TuneFinding{{Status: "ok", Why: "Releases with a big negative custom format score are rejected."}}
	}
	var names []string
	for _, p := range bad {
		names = append(names, fmt.Sprintf("%s (%s)", canon(p["name"]), canon(p["minFormatScore"])))
	}
	return []TuneFinding{{Status: "change", Current: strings.Join(names, ", "), Recommended: "0",
		Why: "Custom formats scored -10000 mean \"never download this\", but only if the profile's minimum score is above that. Here it's so low that those releases still get through."}}
}

func (a *App) fixMinScore(ctx context.Context, e *tuneEnv, app string) (map[string]any, string, error) {
	c := a.Radarr()
	if app == "sonarr" {
		c = a.Sonarr()
	}
	var undo []any
	for _, p := range badMinScore(e, app) {
		old := num(p["minFormatScore"])
		p["minFormatScore"] = 0
		if err := c.PutJSON(ctx, "/qualityprofile/"+canon(p["id"]), p); err != nil {
			if len(undo) == 0 {
				return nil, "", err
			}
			break
		}
		undo = append(undo, map[string]any{"id": canon(p["id"]), "old": old})
	}
	return map[string]any{"profiles": undo}, fmt.Sprintf("%d profile(s) set to 0", len(undo)), nil
}

func (a *App) undoMinScore(ctx context.Context, app string, data map[string]any) error {
	c := a.Radarr()
	if app == "sonarr" {
		c = a.Sonarr()
	}
	if c == nil {
		return fmt.Errorf("%s isn't connected", tuneAppNames[app])
	}
	list, _ := data["profiles"].([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		var p map[string]any
		if err := c.GetJSON(ctx, "/qualityprofile/"+canon(m["id"]), &p); err != nil {
			return err
		}
		p["minFormatScore"] = num(m["old"])
		if err := c.PutJSON(ctx, "/qualityprofile/"+canon(m["id"]), p); err != nil {
			return err
		}
	}
	return nil
}

// truthy reads an on/off value that may arrive as a bool, 1/0 or true/false.
func truthy(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	c := strings.ToLower(canon(v))
	return c == "1" || c == "true"
}
