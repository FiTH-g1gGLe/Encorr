package store

import "time"

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
}

// LibEntry records that an item is in Plex. Seasons holds episode counts per
// season for shows that have been checked in detail.
type LibEntry struct {
	Key     string      `json:"key"`
	Seasons map[int]int `json:"seasons,omitempty"`
}

type Settings struct {
	SetupComplete bool           `json:"setupComplete"`
	ClientID      string         `json:"clientId"` // X-Plex-Client-Identifier for this install
	AppURL        string         `json:"appUrl"`
	Region        string         `json:"region"`   // GB
	Language      string         `json:"language"` // en-GB
	Accent        string         `json:"accent"`   // violet | coral
	Plex          PlexSettings   `json:"plex"`
	TMDBKey       Secret         `json:"tmdbKey"`
	Radarr        ArrSettings    `json:"radarr"`
	Sonarr        ArrSettings    `json:"sonarr"`
	Discord       DiscordSetting `json:"discord"`
	SABnzbd       SabSettings    `json:"sabnzbd"`
	Quotas        QuotaSettings  `json:"quotas"`
}

type PlexSettings struct {
	ServerName string        `json:"serverName"`
	MachineID  string        `json:"machineId"`
	URL        string        `json:"url"`
	Token      Secret        `json:"token"` // the owner's token, used for library scans
	Libraries  []PlexLibrary `json:"libraries"`
}

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
	ID            int       `json:"id"`
	PlexID        int       `json:"plexId"`
	Username      string    `json:"username"`
	Email         string    `json:"email"`
	Thumb         string    `json:"thumb"`
	Role          string    `json:"role"`
	PlexToken     Secret    `json:"plexToken"`
	DiscordID     string    `json:"discordId"`
	WatchlistSync bool      `json:"watchlistSync"`
	Created       time.Time `json:"created"`
	LastSeen      time.Time `json:"lastSeen"`
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
