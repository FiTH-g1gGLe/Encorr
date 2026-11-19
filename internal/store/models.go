package store

import (
	"strconv"
	"time"
)

// Roles keep permissions simple: three roles instead of thirty switches.
const (
	RoleAdmin   = "admin"   // everything, including settings and approvals
	RoleTrusted = "trusted" // requests are approved automatically
	RoleFriend  = "friend"  // requests wait for an admin
)

// Request statuses.
const (
	ReqPending   = "pending"
	ReqApproved  = "approved"
	ReqDeclined  = "declined"
	ReqFailed    = "failed"
	ReqCompleted = "completed"
)

// Media statuses.
const (
	MediaUnknown    = "unknown"
	MediaPending    = "pending"
	MediaProcessing = "processing"
	MediaPartial    = "partial"
	MediaAvailable  = "available"
)

const (
	TypeMovie = "movie"
	TypeTV    = "tv"
)

type Data struct {
	Version  int                 `json:"version"`
	Settings Settings            `json:"settings"`
	Users    []*User             `json:"users"`
	Media    []*Media            `json:"media"`
	Requests []*Request          `json:"requests"`
	Sessions []*Session          `json:"sessions"`
	Jobs     map[string]JobInfo  `json:"jobs"`
	NextID   map[string]int      `json:"nextId"`
	Library  map[string]LibEntry `json:"library"` // "movie:603" -> what Plex has
	// WatchlistSkip lists watchlist titles someone removed from Encorr, so the
	// watchlist sync doesn't keep adding them back: "userID:movie:603".
	WatchlistSkip map[string]bool `json:"watchlistSkip,omitempty"`
	// Activity is the auto-fix log, newest last, capped at 200.
	Activity []Activity `json:"activity,omitempty"`
	// TuneChanges are settings Tune-up changed, with the old value for Undo.
	TuneChanges []TuneChange `json:"tuneChanges,omitempty"`
	// SpeedTests are saved speed test results, newest last, capped at 50.
	SpeedTests []SpeedTest `json:"speedTests,omitempty"`
	// Subscriptions are things the owner pays for: entries they added, and
	// extra details (cost, plan) for ones Encorr finds itself.
	Subscriptions []Subscription `json:"subscriptions,omitempty"`
	// SubReminders remembers which expiry/usage reminders were already sent.
	SubReminders map[string]bool `json:"subReminders,omitempty"`
	// Issues are problems people reported with something in the library.
	Issues []*Issue `json:"issues,omitempty"`
}

// Issue statuses.
const (
	IssueOpen      = "open"
	IssueReplacing = "replacing" // a new copy has been asked for
	IssueResolved  = "resolved"
)

// Issue is a problem someone reported with a film or show in the library:
// it won't play, the wrong audio, missing subtitles...
type Issue struct {
	ID         int       `json:"id"`
	Type       string    `json:"type"`
	TMDBID     int       `json:"tmdbId"`
	TVDBID     int       `json:"tvdbId,omitempty"`
	Title      string    `json:"title"`
	Year       int       `json:"year,omitempty"`
	Poster     string    `json:"poster,omitempty"`
	Season     int       `json:"season,omitempty"`  // TV: 0 = the whole show
	Episode    int       `json:"episode,omitempty"` // 0 = the whole season
	Kind       string    `json:"kind"`              // play | video | audio | subtitles | wrong | other
	Note       string    `json:"note,omitempty"`
	Status     string    `json:"status"`
	ReportedBy int       `json:"reportedBy"`
	Created    time.Time `json:"created"`
	Updated    time.Time `json:"updated"`
	// ArrApp and ArrID say which Radarr film or Sonarr show is being
	// replaced, so the issue closes itself when the new copy arrives.
	ArrApp string  `json:"arrApp,omitempty"`
	ArrID  int     `json:"arrId,omitempty"`
	Events []Event `json:"events,omitempty"`
}

// Log adds a line to the issue's story.
func (i *Issue) Log(kind, text string) {
	i.Events = append(i.Events, Event{At: time.Now(), Kind: kind, Text: text})
	i.Updated = time.Now()
}

// Subscription is one paid service: a news server, an indexer, a VPN...
// Key links it to something Encorr detects ("prowlarr:NZBgeek",
// "sab:news.example.com"); empty for entries added by hand.
type Subscription struct {
	ID       int        `json:"id"`
	Key      string     `json:"key,omitempty"`
	Name     string     `json:"name"`
	Kind     string     `json:"kind"` // provider | indexer | other
	Plan     string     `json:"plan"` // monthly | yearly | lifetime | block | other
	Cost     float64    `json:"cost,omitempty"`
	Currency string     `json:"currency,omitempty"` // GBP | USD | EUR
	Renews   *time.Time `json:"renews,omitempty"`   // expiry or renewal date
	URL      string     `json:"url,omitempty"`
	Notes    string     `json:"notes,omitempty"`
}

// TuneChange is one setting Tune-up changed in another app.
type TuneChange struct {
	ID     int       `json:"id"`
	At     time.Time `json:"at"`
	Rule   string    `json:"rule"`
	App    string    `json:"app"`
	Key    string    `json:"key"`
	Title  string    `json:"title"`
	Old    any       `json:"old"`
	New    any       `json:"new"`
	By     string    `json:"by"`
	Undone bool      `json:"undone,omitempty"`
	// Undo describes how to reverse actions that aren't a single setting
	// (e.g. "delete customformat 12"). Empty for plain settings.
	Undo    map[string]any `json:"undo,omitempty"`
	CanUndo bool           `json:"canUndo"`
}

// SpeedTest is one run of the Tune-up speed test.
type SpeedTest struct {
	At          time.Time      `json:"at"`
	CPU         float64        `json:"cpu"`            // SABnzbd pystone score
	DownloadDir float64        `json:"downloadDirMBs"` // MB/s
	CompleteDir float64        `json:"completeDirMBs"`
	Internet    float64        `json:"internetMBs"` // best of SABnzbd's and Encorr's own test
	InternetBy  string         `json:"internetBy,omitempty"`
	Servers     []ServerResult `json:"servers,omitempty"`
	Bottleneck  string         `json:"bottleneck"` // internet | provider | disk | cpu | settings | unknown
	Advice      []string       `json:"advice,omitempty"`
}

// ServerResult is how a news server did over the last two weeks.
type ServerResult struct {
	Name        string  `json:"name"`
	Connections int     `json:"connections"`
	Tried       int64   `json:"tried"`
	Rate        float64 `json:"rate"`
	Label       string  `json:"label,omitempty"`  // the name shown in SABnzbd
	Backup      bool    `json:"backup,omitempty"` // lower priority: only asked for what the main server lacks
}

// SkipKey is the WatchlistSkip key for one user's title.
func SkipKey(userID int, typ string, tmdbID int) string {
	return strconv.Itoa(userID) + ":" + LibKey(typ, tmdbID)
}

// LibEntry records that an item is in Plex. Seasons holds episode counts per
// season for shows that have been checked in detail.
// LibEntry is one film or show in the library index. A title can be on
// Plex, on Jellyfin, or both; each server keeps its own details.
type LibEntry struct {
	Key     string      `json:"key,omitempty"` // Plex rating key
	Seasons map[int]int `json:"seasons,omitempty"`
	// Jellyfin item id and episodes per season there.
	JF        string      `json:"jf,omitempty"`
	JFSeasons map[int]int `json:"jfSeasons,omitempty"`
}

// Empty is true when neither server has the title any more.
func (e LibEntry) Empty() bool { return e.Key == "" && e.JF == "" }

// Have merges both servers: episodes available per season, whichever has
// more. nil when no season detail is known yet.
func (e LibEntry) Have() map[int]int {
	if e.Seasons == nil && e.JFSeasons == nil {
		return nil
	}
	out := map[int]int{}
	for s, n := range e.Seasons {
		out[s] = n
	}
	for s, n := range e.JFSeasons {
		if n > out[s] {
			out[s] = n
		}
	}
	return out
}

// KnowsSeasons is true when every server that has the show has had its
// seasons read.
func (e LibEntry) KnowsSeasons() bool {
	return (e.Key == "" || e.Seasons != nil) && (e.JF == "" || e.JFSeasons != nil)
}

type Settings struct {
	SetupComplete bool             `json:"setupComplete"`
	ClientID      string           `json:"clientId"` // X-Plex-Client-Identifier for this install
	AppURL        string           `json:"appUrl"`
	Region        string           `json:"region"`   // GB
	Language      string           `json:"language"` // en-GB
	Accent        string           `json:"accent"`   // violet | blue | teal | green | yellow | coral | red | pink
	Theme         string           `json:"theme"`    // black (default) | navy
	Plex          PlexSettings     `json:"plex"`
	Jellyfin      JellyfinSettings `json:"jellyfin"`
	TMDBKey       Secret           `json:"tmdbKey"`
	Radarr        ArrSettings      `json:"radarr"`
	Sonarr        ArrSettings      `json:"sonarr"`
	Discord       DiscordSetting   `json:"discord"`
	SABnzbd       SabSettings      `json:"sabnzbd"`
	Prowlarr      ArrSettings      `json:"prowlarr"`
	Quotas        QuotaSettings    `json:"quotas"`
	// AutoFix lets Encorr sort out stuck downloads by itself. nil means on.
	AutoFix *bool `json:"autoFix,omitempty"`
}

// AutoFixOn reports whether self-healing is switched on (the default).
func (s Settings) AutoFixOn() bool { return s.AutoFix == nil || *s.AutoFix }

type PlexSettings struct {
	ServerName string        `json:"serverName"`
	MachineID  string        `json:"machineId"`
	URL        string        `json:"url"`
	Token      Secret        `json:"token"` // the owner's token, used for library scans
	Libraries  []PlexLibrary `json:"libraries"`
}

// JellyfinSettings is the owner's Jellyfin server. APIKey is made by Encorr
// when the owner signs in; their password is never kept.
type JellyfinSettings struct {
	ServerName string `json:"serverName"`
	ServerID   string `json:"serverId"`
	URL        string `json:"url"`       // how Encorr reaches it, e.g. http://localhost:8096
	PublicURL  string `json:"publicUrl"` // optional: the address friends use, for Play buttons
	Version    string `json:"version"`
	APIKey     Secret `json:"apiKey"`
	// Libraries uses the same shape as Plex: Key is the Jellyfin library id.
	Libraries []PlexLibrary `json:"libraries"`
}

// Ready is true once a server is connected.
func (j JellyfinSettings) Ready() bool { return j.URL != "" && j.APIKey != "" }

type PlexLibrary struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Type    string `json:"type"` // movie | show
	Enabled bool   `json:"enabled"`
}

type ArrSettings struct {
	Enabled             bool   `json:"enabled"`
	URL                 string `json:"url"` // e.g. http://localhost:7878
	APIKey              Secret `json:"apiKey"`
	QualityProfileID    int    `json:"qualityProfileId"`
	QualityProfileName  string `json:"qualityProfileName"`
	RootFolder          string `json:"rootFolder"`
	LanguageProfileID   int    `json:"languageProfileId,omitempty"`   // Sonarr v3 only
	MinimumAvailability string `json:"minimumAvailability,omitempty"` // Radarr
	SeasonFolders       bool   `json:"seasonFolders,omitempty"`       // Sonarr
	TagRequests         bool   `json:"tagRequests"`
	SearchOnAdd         bool   `json:"searchOnAdd"`
}

// SabSettings is optional: with it, progress bars come straight from
// SABnzbd (live) instead of Radarr/Sonarr's once-a-minute copy.
type SabSettings struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"` // e.g. http://localhost:8080/sabnzbd
	APIKey  Secret `json:"apiKey"`
	// Detected is true when Encorr found and filled this in by itself.
	Detected bool `json:"detected,omitempty"`
}

type DiscordSetting struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL Secret `json:"webhookUrl"`
}

type QuotaSettings struct {
	FriendMovieLimit int `json:"friendMovieLimit"` // 0 = unlimited
	FriendTVLimit    int `json:"friendTvLimit"`
	Days             int `json:"days"`
}

type User struct {
	ID        int    `json:"id"`
	PlexID    int    `json:"plexId"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Thumb     string `json:"thumb"`
	Role      string `json:"role"`
	PlexToken Secret `json:"plexToken"`
	// JellyfinID links a Jellyfin account (they sign in with it).
	JellyfinID string `json:"jellyfinId,omitempty"`
	// Encorr's own sign-in (username + password). Only a salted, slow hash
	// is kept, never the password itself.
	PasswordHash       string `json:"passwordHash,omitempty"`
	MustChangePassword bool   `json:"mustChangePassword,omitempty"`
	DiscordID          string `json:"discordId"`
	WatchlistSync      bool   `json:"watchlistSync"`
	// MovieLimit and TVLimit override the Friend limits for this person.
	// nil uses the default for their role; 0 means no limit.
	MovieLimit *int      `json:"movieLimit,omitempty"`
	TVLimit    *int      `json:"tvLimit,omitempty"`
	Created    time.Time `json:"created"`
	LastSeen   time.Time `json:"lastSeen"`
	// ReadySeen is when they last looked at their ready requests, for the
	// "new" badge.
	ReadySeen time.Time `json:"readySeen,omitempty"`
}

type Media struct {
	ID          int            `json:"id"`
	Type        string         `json:"type"`
	TMDBID      int            `json:"tmdbId"`
	TVDBID      int            `json:"tvdbId,omitempty"`
	IMDBID      string         `json:"imdbId,omitempty"`
	Title       string         `json:"title"`
	Year        int            `json:"year,omitempty"`
	Poster      string         `json:"poster,omitempty"`
	Status      string         `json:"status"`
	PlexKey     string         `json:"plexKey,omitempty"`
	JFKey       string         `json:"jfKey,omitempty"`
	ExternalID  int            `json:"externalId,omitempty"` // Radarr movie id / Sonarr series id
	SeasonEps   map[int]int    `json:"seasonEps,omitempty"`  // season -> episodes TMDB expects
	SeasonHave  map[int]int    `json:"seasonHave,omitempty"` // season -> episodes in Plex
	Created     time.Time      `json:"created"`
	Updated     time.Time      `json:"updated"`
	AvailableAt *time.Time     `json:"availableAt,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
}

type Request struct {
	ID          int       `json:"id"`
	MediaID     int       `json:"mediaId"`
	Type        string    `json:"type"`
	TMDBID      int       `json:"tmdbId"`
	Title       string    `json:"title"`
	Year        int       `json:"year,omitempty"`
	Poster      string    `json:"poster,omitempty"`
	Seasons     []int     `json:"seasons,omitempty"`
	Status      string    `json:"status"`
	RequestedBy int       `json:"requestedBy"`
	ModifiedBy  int       `json:"modifiedBy,omitempty"`
	Source      string    `json:"source"` // web | watchlist | app | discord
	Note        string    `json:"note,omitempty"`
	Error       string    `json:"error,omitempty"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
	// DoneAt is when everything asked for reached Plex.
	DoneAt *time.Time `json:"doneAt,omitempty"`
	// Events is the request's own story: requested, approved, sent, fixed...
	// Radarr/Sonarr's history is merged in when the timeline is shown.
	Events []Event `json:"events,omitempty"`
}

// Event is one line in a request's timeline.
type Event struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"` // requested | approved | declined | sent | failed | ready | retry | search | fix | action
	Text string    `json:"text"`
}

// Log adds a line to the request's timeline (kept to the last 60).
func (r *Request) Log(kind, text string) {
	r.Events = append(r.Events, Event{At: time.Now(), Kind: kind, Text: text})
	if len(r.Events) > 60 {
		r.Events = r.Events[len(r.Events)-60:]
	}
}

// Activity is one thing Encorr did by itself (or an admin did through it)
// to keep downloads moving. Shown on the Health page.
type Activity struct {
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"` // fix | warn | action
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	RequestID int       `json:"requestId,omitempty"`
}

type Session struct {
	Token   string    `json:"token"`
	UserID  int       `json:"userId"`
	Expires time.Time `json:"expires"`
}

type JobInfo struct {
	LastRun  time.Time `json:"lastRun"`
	LastOK   bool      `json:"lastOk"`
	Message  string    `json:"message"`
	Duration string    `json:"duration"`
}
