// Package jellyfin talks to a Jellyfin server: signing people in (password
// or Quick Connect), reading libraries, asking for folder scans and reading
// a few settings for Tune-up.
//
// Everything here follows Jellyfin's own API (Jellyfin.Api controllers):
// the token always travels in the Authorization header, never in the URL.
package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Product and Version name Encorr in Jellyfin's device list.
var (
	Product    = "Encorr"
	Version    = "0.1.0"
	httpClient = &http.Client{Timeout: 25 * time.Second}
)

// Client talks to one Jellyfin server. Token is an API key (for the owner's
// background work) or a user's access token. DeviceID should stay the same
// for an install so Jellyfin doesn't list a new device every time.
type Client struct {
	URL      string
	Token    string
	DeviceID string
}

// ErrUnauthorized means Jellyfin turned the credentials down.
var ErrUnauthorized = errors.New("Jellyfin didn't accept those details")

func (c *Client) base() string { return strings.TrimRight(c.URL, "/") }

func quote(s string) string {
	return strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(s)
}

func (c *Client) authHeader() string {
	dev := c.DeviceID
	if dev == "" {
		dev = "encorr"
	}
	h := fmt.Sprintf(`MediaBrowser Client="%s", Device="%s", DeviceId="%s", Version="%s"`, quote(Product), quote(Product), quote(dev), quote(Version))
	if c.Token != "" {
		h += fmt.Sprintf(`, Token="%s"`, quote(c.Token))
	}
	return h
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	if c.URL == "" {
		return errors.New("no Jellyfin address set")
	}
	u := c.base() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	switch {
	case resp.StatusCode == 401:
		return ErrUnauthorized
	case resp.StatusCode == 403:
		return errors.New("Jellyfin says this account isn't allowed to do that")
	case resp.StatusCode == 404:
		return fmt.Errorf("Jellyfin couldn't find %s: is this a Jellyfin server, and is it up to date?", path)
	case resp.StatusCode >= 300:
		return fmt.Errorf("Jellyfin answered %d for %s", resp.StatusCode, path)
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("Jellyfin sent something unexpected for %s", path)
	}
	return nil
}

func friendlyNetErr(err error) error {
	s := err.Error()
	switch {
	case strings.Contains(s, "connection refused"):
		return errors.New("Jellyfin isn't answering at that address: is it running?")
	case strings.Contains(s, "Client.Timeout") || strings.Contains(s, "deadline exceeded"):
		return errors.New("Jellyfin took too long to answer: check the address and your network")
	case strings.Contains(s, "no such host"):
		return errors.New("couldn't find that Jellyfin address: check the spelling")
	case strings.Contains(s, "certificate"):
		return errors.New("Jellyfin's HTTPS certificate isn't trusted: try its http:// address on your network")
	}
	return err
}

// ---------- server ----------

// PublicInfo is what any Jellyfin server says about itself without signing in.
type PublicInfo struct {
	ServerName  string `json:"ServerName"`
	Version     string `json:"Version"`
	ID          string `json:"Id"`
	ProductName string `json:"ProductName"`
	// StartupWizardCompleted is false on a brand-new server.
	StartupWizardCompleted bool `json:"StartupWizardCompleted"`
}

// PublicInfo checks an address really is a Jellyfin server.
func (c *Client) PublicInfo(ctx context.Context) (*PublicInfo, error) {
	var p PublicInfo
	if err := c.do(ctx, "GET", "/System/Info/Public", nil, nil, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, errors.New("that address answered, but it isn't a Jellyfin server")
	}
	if strings.Contains(strings.ToLower(p.ProductName), "emby") {
		return nil, errors.New("that's an Emby server: Encorr works with Jellyfin and Plex")
	}
	return &p, nil
}

// Ping checks the token still works.
func (c *Client) Ping(ctx context.Context) (*PublicInfo, error) {
	var p PublicInfo
	if err := c.do(ctx, "GET", "/System/Info", nil, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ---------- people ----------

type Policy struct {
	IsAdministrator bool `json:"IsAdministrator"`
	IsDisabled      bool `json:"IsDisabled"`
}

type User struct {
	ID              string `json:"Id"`
	Name            string `json:"Name"`
	ServerID        string `json:"ServerId"`
	PrimaryImageTag string `json:"PrimaryImageTag"`
	Policy          Policy `json:"Policy"`
}

type AuthResult struct {
	User        User   `json:"User"`
	AccessToken string `json:"AccessToken"`
	ServerID    string `json:"ServerId"`
}

// SignIn checks a username and password.
func (c *Client) SignIn(ctx context.Context, username, password string) (*AuthResult, error) {
	var r AuthResult
	cc := *c
	cc.Token = ""
	if err := cc.do(ctx, "POST", "/Users/AuthenticateByName", nil, map[string]string{"Username": username, "Pw": password}, &r); err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return nil, errors.New("wrong Jellyfin username or password")
		}
		return nil, err
	}
	if r.AccessToken == "" || r.User.ID == "" {
		return nil, errors.New("Jellyfin didn't sign you in")
	}
	return &r, nil
}

// QuickConnect is a pending Quick Connect sign-in: the person types Code
// into a Jellyfin app they're already signed in to.
type QuickConnect struct {
	Secret        string `json:"Secret"`
	Code          string `json:"Code"`
	Authenticated bool   `json:"Authenticated"`
}

// QuickConnectEnabled says whether the server allows Quick Connect.
func (c *Client) QuickConnectEnabled(ctx context.Context) (bool, error) {
	var on bool
	cc := *c
	cc.Token = ""
	err := cc.do(ctx, "GET", "/QuickConnect/Enabled", nil, nil, &on)
	return on, err
}

// QuickConnectStart asks for a new code.
func (c *Client) QuickConnectStart(ctx context.Context) (*QuickConnect, error) {
	var q QuickConnect
	cc := *c
	cc.Token = ""
	if err := cc.do(ctx, "POST", "/QuickConnect/Initiate", nil, nil, &q); err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return nil, errors.New("Quick Connect is turned off on this Jellyfin server")
		}
		return nil, err
	}
	return &q, nil
}

// QuickConnectState checks whether the code has been approved yet.
func (c *Client) QuickConnectState(ctx context.Context, secret string) (*QuickConnect, error) {
	var q QuickConnect
	cc := *c
	cc.Token = ""
	if err := cc.do(ctx, "GET", "/QuickConnect/Connect", url.Values{"secret": {secret}}, nil, &q); err != nil {
		return nil, err
	}
	return &q, nil
}

// QuickConnectFinish swaps an approved secret for a signed-in user.
func (c *Client) QuickConnectFinish(ctx context.Context, secret string) (*AuthResult, error) {
	var r AuthResult
	cc := *c
	cc.Token = ""
	if err := cc.do(ctx, "POST", "/Users/AuthenticateWithQuickConnect", nil, map[string]string{"Secret": secret}, &r); err != nil {
		return nil, err
	}
	if r.AccessToken == "" || r.User.ID == "" {
		return nil, errors.New("Jellyfin didn't sign you in")
	}
	return &r, nil
}

// Me is the signed-in user (with a user token).
func (c *Client) Me(ctx context.Context) (*User, error) {
	var u User
	if err := c.do(ctx, "GET", "/Users/Me", nil, nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// UserByID looks up one person (needs an admin token or API key).
func (c *Client) UserByID(ctx context.Context, id string) (*User, error) {
	var u User
	if err := c.do(ctx, "GET", "/Users/"+url.PathEscape(id), nil, nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Users lists everyone on the server (needs an admin token or API key).
func (c *Client) Users(ctx context.Context) ([]User, error) {
	var us []User
	err := c.do(ctx, "GET", "/Users", nil, nil, &us)
	return us, err
}

// CreateAPIKey makes an API key named app (needs an admin user token). Jellyfin
// 10.11+ returns the new key; older versions don't, so it's read back by
// name and the newest one wins.
func (c *Client) CreateAPIKey(ctx context.Context, app string) (string, error) {
	var made struct {
		AccessToken string `json:"AccessToken"`
	}
	if err := c.do(ctx, "POST", "/Auth/Keys", url.Values{"app": {app}}, nil, &made); err != nil {
		return "", err
	}
	if made.AccessToken != "" { // Jellyfin 10.11 and later hand it straight back
		return made.AccessToken, nil
	}
	var keys struct {
		Items []struct {
			AccessToken string    `json:"AccessToken"`
			AppName     string    `json:"AppName"`
			DateCreated time.Time `json:"DateCreated"`
		} `json:"Items"`
	}
	if err := c.do(ctx, "GET", "/Auth/Keys", nil, nil, &keys); err != nil {
		return "", err
	}
	best, at := "", time.Time{}
	for _, k := range keys.Items {
		if k.AppName == app && k.AccessToken != "" && (best == "" || !k.DateCreated.Before(at)) {
			best, at = k.AccessToken, k.DateCreated
		}
	}
	if best == "" {
		return "", errors.New("Jellyfin made the API key but didn't hand it back")
	}
	return best, nil
}

// ---------- libraries ----------

// Library is one Jellyfin library ("virtual folder").
type Library struct {
	Name           string         `json:"Name"`
	ItemID         string         `json:"ItemId"`
	CollectionType string         `json:"CollectionType"` // movies | tvshows | music | ... | "" (mixed)
	Locations      []string       `json:"Locations"`
	LibraryOptions map[string]any `json:"LibraryOptions"`
}

// Kind is "movie", "show" or "" for libraries Encorr doesn't use.
func (l Library) Kind() string {
	switch strings.ToLower(l.CollectionType) {
	case "movies":
		return "movie"
	case "tvshows":
		return "show"
	}
	return ""
}

// Libraries lists every library with its folders and options.
func (c *Client) Libraries(ctx context.Context) ([]Library, error) {
	var ls []Library
	err := c.do(ctx, "GET", "/Library/VirtualFolders", nil, nil, &ls)
	return ls, err
}

// SetLibraryOptions saves a library's whole options object (Jellyfin replaces
// it, so always send back what Libraries returned, with your change made).
func (c *Client) SetLibraryOptions(ctx context.Context, id string, opts map[string]any) error {
	return c.do(ctx, "POST", "/Library/VirtualFolders/LibraryOptions", nil, map[string]any{"Id": id, "LibraryOptions": opts}, nil)
}

// Item is a film, show, season or episode.
type Item struct {
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	Type              string            `json:"Type"` // Movie | Series | Season | Episode
	ProductionYear    int               `json:"ProductionYear"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
	Path              string            `json:"Path"`
	DateCreated       time.Time         `json:"DateCreated"`
	LocationType      string            `json:"LocationType"` // FileSystem | Virtual (= missing)
	SeriesID          string            `json:"SeriesId"`
	ParentIndexNumber *int              `json:"ParentIndexNumber"` // season number, for episodes
	IndexNumber       *int              `json:"IndexNumber"`
	IndexNumberEnd    *int              `json:"IndexNumberEnd"` // multi-episode files
	Width             int               `json:"Width"`
	MediaStreams      []MediaStream     `json:"MediaStreams"`
}

type MediaStream struct {
	Type          string `json:"Type"` // Video | Audio | Subtitle
	Width         int    `json:"Width"`
	Height        int    `json:"Height"`
	Channels      int    `json:"Channels"`
	VideoRange    string `json:"VideoRange"`     // SDR | HDR
	VideoRangeTyp string `json:"VideoRangeType"` // SDR | HDR10 | DOVI | HLG ...
	IsDefault     bool   `json:"IsDefault"`
}

// provider finds a provider id whatever case Jellyfin used for the name.
func (it Item) provider(names ...string) string {
	for k, v := range it.ProviderIDs {
		for _, n := range names {
			if strings.EqualFold(k, n) && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// IDs returns the TMDB, IMDb and TVDB ids Jellyfin knows for an item.
func (it Item) IDs() (tmdb int, imdb string, tvdb int) {
	tmdb, _ = strconv.Atoi(it.provider("Tmdb", "TheMovieDb"))
	imdb = it.provider("Imdb")
	tvdb, _ = strconv.Atoi(it.provider("Tvdb"))
	return
}

// Missing is true for episodes Jellyfin lists but hasn't got a file for.
func (it Item) Missing() bool { return strings.EqualFold(it.LocationType, "Virtual") }

type itemPage struct {
	Items            []Item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
}

const pageSize = 500

// AllItems lists every film ("Movie") or show ("Series") in a library.
func (c *Client) AllItems(ctx context.Context, libraryID, itemType string) ([]Item, error) {
	var out []Item
	for start := 0; ; start += pageSize {
		q := url.Values{
			"ParentId": {libraryID}, "IncludeItemTypes": {itemType}, "Recursive": {"true"},
			"Fields": {"ProviderIds,Path,DateCreated"}, "EnableImages": {"false"}, "EnableUserData": {"false"},
			"StartIndex": {strconv.Itoa(start)}, "Limit": {strconv.Itoa(pageSize)}, "SortBy": {"SortName"},
		}
		var p itemPage
		if err := c.do(ctx, "GET", "/Items", q, nil, &p); err != nil {
			return nil, err
		}
		for _, it := range p.Items {
			if !it.Missing() {
				out = append(out, it)
			}
		}
		if len(p.Items) < pageSize || start+len(p.Items) >= p.TotalRecordCount {
			return out, nil
		}
		if start > 200000 {
			return out, errors.New("Jellyfin library is too big to read")
		}
	}
}

// Newest lists the most recently added items of a type ("Movie" or
// "Episode") in a library, newest first.
func (c *Client) Newest(ctx context.Context, libraryID, itemType string, n int) ([]Item, error) {
	q := url.Values{
		"ParentId": {libraryID}, "IncludeItemTypes": {itemType}, "Recursive": {"true"},
		"Fields": {"ProviderIds,DateCreated"}, "EnableImages": {"false"}, "EnableUserData": {"false"},
		"SortBy": {"DateCreated"}, "SortOrder": {"Descending"}, "Limit": {strconv.Itoa(n)},
		"IsMissing": {"false"},
	}
	var p itemPage
	if err := c.do(ctx, "GET", "/Items", q, nil, &p); err != nil {
		return nil, err
	}
	out := p.Items[:0]
	for _, it := range p.Items {
		if !it.Missing() {
			out = append(out, it)
		}
	}
	return out, nil
}

// Search looks for films ("Movie") or shows ("Series") by name.
func (c *Client) Search(ctx context.Context, itemType, term string) ([]Item, error) {
	q := url.Values{
		"SearchTerm": {term}, "IncludeItemTypes": {itemType}, "Recursive": {"true"},
		"Fields": {"ProviderIds,Path"}, "EnableImages": {"false"}, "EnableUserData": {"false"}, "Limit": {"25"},
	}
	var p itemPage
	if err := c.do(ctx, "GET", "/Items", q, nil, &p); err != nil {
		return nil, err
	}
	out := p.Items[:0]
	for _, it := range p.Items {
		if !it.Missing() {
			out = append(out, it)
		}
	}
	return out, nil
}

// Item fetches one item with its media details (for the title page).
func (c *Client) Item(ctx context.Context, id string) (*Item, error) {
	q := url.Values{"Ids": {id}, "Fields": {"ProviderIds,MediaStreams,Path"}, "EnableImages": {"false"}}
	var p itemPage
	if err := c.do(ctx, "GET", "/Items", q, nil, &p); err != nil {
		return nil, err
	}
	if len(p.Items) == 0 {
		return nil, errors.New("Jellyfin doesn't have that item any more")
	}
	return &p.Items[0], nil
}

// SeasonCounts maps season number -> episodes with files, for a show.
// Multi-episode files count every episode they hold. Specials (season 0)
// are left out, as with Plex.
func (c *Client) SeasonCounts(ctx context.Context, seriesID string) (map[int]int, error) {
	q := url.Values{"IsMissing": {"false"}, "Fields": {"Path"}, "EnableImages": {"false"}, "EnableUserData": {"false"}}
	var p itemPage
	if err := c.do(ctx, "GET", "/Shows/"+url.PathEscape(seriesID)+"/Episodes", q, nil, &p); err != nil {
		return nil, err
	}
	out := map[int]int{}
	seen := map[[2]int]bool{}
	for _, e := range p.Items {
		if e.Missing() || e.ParentIndexNumber == nil || e.IndexNumber == nil || *e.ParentIndexNumber <= 0 {
			continue
		}
		s, from, to := *e.ParentIndexNumber, *e.IndexNumber, *e.IndexNumber
		if e.IndexNumberEnd != nil && *e.IndexNumberEnd > to && *e.IndexNumberEnd-to < 50 {
			to = *e.IndexNumberEnd
		}
		for n := from; n <= to; n++ {
			if !seen[[2]int{s, n}] { // two copies of one episode count once
				seen[[2]int{s, n}] = true
				out[s]++
			}
		}
	}
	return out, nil
}

// ScanPath tells Jellyfin a folder has new files, so it picks them up now
// rather than at the next scheduled scan.
func (c *Client) ScanPath(ctx context.Context, path string) error {
	return c.do(ctx, "POST", "/Library/Media/Updated", nil,
		map[string]any{"Updates": []map[string]string{{"Path": path, "UpdateType": "Created"}}}, nil)
}

// Config is the server's main settings (System/Configuration).
func (c *Client) Config(ctx context.Context) (map[string]any, error) {
	var m map[string]any
	err := c.do(ctx, "GET", "/System/Configuration", nil, nil, &m)
	return m, err
}

// WebURL opens an item in Jellyfin's web app. base is the address people
// use to reach Jellyfin (the public one if set).
func WebURL(base, itemID, serverID string) string {
	if base == "" || itemID == "" {
		return ""
	}
	u := strings.TrimRight(base, "/") + "/web/index.html#!/details?id=" + url.QueryEscape(itemID)
	if serverID != "" {
		u += "&serverId=" + url.QueryEscape(serverID)
	}
	return u
}
