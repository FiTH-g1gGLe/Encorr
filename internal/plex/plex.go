// Package plex talks to plex.tv (sign-in, servers, friends, watchlists) and to
// the owner's Plex Media Server (libraries and scans).
package plex

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	Product = "Encorr"
)

// Endpoints are variables so tests can point them at fake servers.
var (
	PlexTVURL   = "https://plex.tv"
	DiscoverURL = "https://discover.provider.plex.tv"
	AppAuthURL  = "https://app.plex.tv/auth"
	Version     = "0.1.0"
	httpClient  = &http.Client{Timeout: 20 * time.Second}
	quickClient = &http.Client{Timeout: 5 * time.Second}
)

type Client struct {
	ClientID string
	Token    string
}

func (c *Client) headers(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Product", Product)
	req.Header.Set("X-Plex-Version", Version)
	req.Header.Set("X-Plex-Client-Identifier", c.ClientID)
	req.Header.Set("X-Plex-Platform", "Web")
	req.Header.Set("X-Plex-Device-Name", Product)
	if c.Token != "" {
		req.Header.Set("X-Plex-Token", c.Token)
	}
}

func (c *Client) do(ctx context.Context, hc *http.Client, method, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	c.headers(req)
	resp, err := hc.Do(req)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode == 401 {
		return errors.New("Plex rejected the sign-in token (it may have expired): sign in again")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Plex answered %d for %s", resp.StatusCode, redact(u))
	}
	if out == nil {
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "<") {
		return xml.Unmarshal(body, out)
	}
	return json.Unmarshal(body, out)
}

func friendlyNetErr(err error) error {
	s := err.Error()
	switch {
	case strings.Contains(s, "connection refused"):
		return errors.New("Plex isn't answering at that address: is Plex running?")
	case strings.Contains(s, "Client.Timeout") || strings.Contains(s, "deadline exceeded"):
		return errors.New("Plex took too long to answer: check the address and your network")
	case strings.Contains(s, "no such host"):
		return errors.New("couldn't find that Plex address: check the spelling")
	}
	return err
}

func redact(u string) string {
	if i := strings.Index(u, "X-Plex-Token="); i >= 0 {
		return u[:i] + "X-Plex-Token=…"
	}
	return u
}

// ---------- Sign-in (PIN flow) ----------

type Pin struct {
	ID        int    `json:"id"`
	Code      string `json:"code"`
	AuthToken string `json:"authToken"`
}

func (c *Client) CreatePin(ctx context.Context) (*Pin, error) {
	var p Pin
	err := c.do(ctx, httpClient, http.MethodPost, PlexTVURL+"/api/v2/pins?strong=true", &p)
	return &p, err
}

func (c *Client) CheckPin(ctx context.Context, id int) (*Pin, error) {
	var p Pin
	err := c.do(ctx, httpClient, http.MethodGet, fmt.Sprintf("%s/api/v2/pins/%d", PlexTVURL, id), &p)
	return &p, err
}

// AuthURL is the Plex page the user signs in on.
func (c *Client) AuthURL(code, forward string) string {
	v := url.Values{}
	v.Set("clientID", c.ClientID)
	v.Set("code", code)
	v.Set("context[device][product]", Product)
	if forward != "" {
		v.Set("forwardUrl", forward)
	}
	return AppAuthURL + "#?" + v.Encode()
}

type Account struct {
	ID       int    `json:"id"`
	UUID     string `json:"uuid"`
	Username string `json:"username"`
	Title    string `json:"title"`
	Email    string `json:"email"`
	Thumb    string `json:"thumb"`
}

func (c *Client) Account(ctx context.Context) (*Account, error) {
	var a Account
	err := c.do(ctx, httpClient, http.MethodGet, PlexTVURL+"/api/v2/user", &a)
	if a.Username == "" {
		a.Username = a.Title
	}
	return &a, err
}

// ---------- Servers ----------

type Connection struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	URI      string `json:"uri"`
	Local    bool   `json:"local"`
	Relay    bool   `json:"relay"`
}

type Resource struct {
	Name             string       `json:"name"`
	Product          string       `json:"product"`
	ClientIdentifier string       `json:"clientIdentifier"`
	Provides         string       `json:"provides"`
	Owned            bool         `json:"owned"`
	AccessToken      string       `json:"accessToken"`
	Connections      []Connection `json:"connections"`
}

// Servers lists Plex Media Servers this account can see.
func (c *Client) Servers(ctx context.Context) ([]Resource, error) {
	var all []Resource
	if err := c.do(ctx, httpClient, http.MethodGet, PlexTVURL+"/api/v2/resources?includeHttps=1&includeRelay=1", &all); err != nil {
		return nil, err
	}
	var out []Resource
	for _, r := range all {
		if strings.Contains(r.Provides, "server") {
			out = append(out, r)
		}
	}
	return out, nil
}

// HasServer reports whether this account can reach the server with machineID
// (owner or shared with them). Used to decide who may sign in.
func (c *Client) HasServer(ctx context.Context, machineID string) (bool, error) {
	srv, err := c.Servers(ctx)
	if err != nil {
		return false, err
	}
	for _, r := range srv {
		if r.ClientIdentifier == machineID {
			return true, nil
		}
	}
	return false, nil
}

// BestConnection tries a server's addresses (local first, relay last) and
// returns the first that answers.
func (c *Client) BestConnection(ctx context.Context, r Resource) (string, error) {
	order := func(cn Connection) int {
		switch {
		case cn.Local && !cn.Relay:
			return 0
		case !cn.Relay:
			return 1
		default:
			return 2
		}
	}
	conns := append([]Connection(nil), r.Connections...)
	for i := range conns {
		for j := i + 1; j < len(conns); j++ {
			if order(conns[j]) < order(conns[i]) {
				conns[i], conns[j] = conns[j], conns[i]
			}
		}
	}
	tok := r.AccessToken
	if tok == "" {
		tok = c.Token
	}
	sc := &Client{ClientID: c.ClientID, Token: tok}
	var lastErr error
	for _, cn := range conns {
		if err := sc.do(ctx, quickClient, http.MethodGet, strings.TrimRight(cn.URI, "/")+"/identity", nil); err != nil {
			lastErr = err
			continue
		}
		return strings.TrimRight(cn.URI, "/"), nil
	}
	if lastErr == nil {
		lastErr = errors.New("this server has no addresses listed")
	}
	return "", fmt.Errorf("couldn't reach %s on any address: %w", r.Name, lastErr)
}

// ---------- Friends ----------

type Friend struct {
	ID       int    `xml:"id,attr"`
	Title    string `xml:"title,attr"`
	Username string `xml:"username,attr"`
	Email    string `xml:"email,attr"`
	Thumb    string `xml:"thumb,attr"`
	Servers  []struct {
		MachineIdentifier string `xml:"machineIdentifier,attr"`
	} `xml:"Server"`
}

// Friends lists people the owner shares servers with.
func (c *Client) Friends(ctx context.Context) ([]Friend, error) {
	var mc struct {
		Users []Friend `xml:"User"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, PlexTVURL+"/api/users", nil)
	if err != nil {
		return nil, err
	}
	c.headers(req)
	req.Header.Set("Accept", "application/xml")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, friendlyNetErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Plex answered %d when listing friends", resp.StatusCode)
	}
	if err := xml.NewDecoder(resp.Body).Decode(&mc); err != nil {
		return nil, err
	}
	return mc.Users, nil
}

// ---------- Library ----------

type Guid struct {
	ID string `json:"id"`
}

type Item struct {
	RatingKey            string `json:"ratingKey"`
	Key                  string `json:"key"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	Year                 int    `json:"year"`
	Index                int    `json:"index"`
	LeafCount            int    `json:"leafCount"`
	ParentIndex          int    `json:"parentIndex"`
	GrandparentRatingKey string `json:"grandparentRatingKey"`
	AddedAt              int64  `json:"addedAt"`
	// Real Plex sends both "guid" (a string, e.g. plex://movie/... or a legacy
	// agent id) and "Guid" (the list of tmdb/imdb/tvdb ids). Both need their
	// own field or decoding fails.
	Location []struct {
		Path string `json:"path"`
	} `json:"Location,omitempty"`
	GuidString string `json:"guid"`
	Guids      []Guid `json:"Guid"`
}

type container struct {
	MediaContainer struct {
		TotalSize int    `json:"totalSize"`
		Size      int    `json:"size"`
		Directory []Item `json:"Directory"`
		Metadata  []Item `json:"Metadata"`
	} `json:"MediaContainer"`
}

// IDs pulls TMDB/IMDb/TVDB ids out of an item's Guid list, falling back to
// the old-style agent id (com.plexapp.agents.themoviedb://603?lang=en).
func (it Item) IDs() (tmdb int, imdb string, tvdb int) {
	defer func() {
		if tmdb != 0 || imdb != "" || tvdb != 0 {
			return
		}
		g := it.GuidString
		k := strings.Index(g, "://")
		if k < 0 {
			return
		}
		rest := g[k+3:]
		if j := strings.IndexAny(rest, "?/"); j >= 0 {
			rest = rest[:j]
		}
		switch {
		case strings.Contains(g, "agents.themoviedb"):
			tmdb, _ = strconv.Atoi(rest)
		case strings.Contains(g, "agents.imdb"):
			imdb = rest
		case strings.Contains(g, "agents.thetvdb"), strings.Contains(g, "agents.hama"):
			tvdb, _ = strconv.Atoi(strings.TrimPrefix(rest, "tvdb-"))
		}
	}()
	for _, g := range it.Guids {
		switch {
		case strings.HasPrefix(g.ID, "tmdb://"):
			tmdb, _ = strconv.Atoi(strings.TrimPrefix(g.ID, "tmdb://"))
		case strings.HasPrefix(g.ID, "imdb://"):
			imdb = strings.TrimPrefix(g.ID, "imdb://")
		case strings.HasPrefix(g.ID, "tvdb://"):
			tvdb, _ = strconv.Atoi(strings.TrimPrefix(g.ID, "tvdb://"))
		}
	}
	return
}

type Server struct {
	URL      string
	ClientID string
	Token    string
}

func (s *Server) client() *Client { return &Client{ClientID: s.ClientID, Token: s.Token} }

func (s *Server) get(ctx context.Context, path string, start, size int, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.URL, "/")+path, nil)
	if err != nil {
		return err
	}
	s.client().headers(req)
	if size > 0 {
		req.Header.Set("X-Plex-Container-Start", strconv.Itoa(start))
		req.Header.Set("X-Plex-Container-Size", strconv.Itoa(size))
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return errors.New("Plex rejected Encorr's token: sign in again as the server owner in Settings")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Plex answered %d for %s", resp.StatusCode, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Server) Ping(ctx context.Context) error {
	var c container
	return s.get(ctx, "/identity", 0, 0, &c)
}

func (s *Server) Libraries(ctx context.Context) ([]Item, error) {
	var c container
	if err := s.get(ctx, "/library/sections", 0, 0, &c); err != nil {
		return nil, err
	}
	var out []Item
	for _, d := range c.MediaContainer.Directory {
		if d.Type == "movie" || d.Type == "show" {
			out = append(out, d)
		}
	}
	return out, nil
}

// ScanPath asks Plex to look at one folder now (a quick partial scan),
// instead of waiting for its own schedule.
func (s *Server) ScanPath(ctx context.Context, libKey, path string) error {
	var c container
	err := s.get(ctx, fmt.Sprintf("/library/sections/%s/refresh?path=%s", url.PathEscape(libKey), url.QueryEscape(path)), 0, 0, &c)
	if err != nil && strings.Contains(err.Error(), "EOF") {
		return nil // Plex answers this one with an empty body
	}
	return err
}

// AllItems pages through every movie (type 1) or show (type 2) in a library.
func (s *Server) AllItems(ctx context.Context, libKey string, plexType int) ([]Item, error) {
	var out []Item
	const page = 500
	for start := 0; ; start += page {
		var c container
		p := fmt.Sprintf("/library/sections/%s/all?type=%d&includeGuids=1", libKey, plexType)
		if err := s.get(ctx, p, start, page, &c); err != nil {
			return nil, err
		}
		out = append(out, c.MediaContainer.Metadata...)
		if len(c.MediaContainer.Metadata) < page {
			break
		}
	}
	return out, nil
}

// RecentlyAdded returns movies (type 1) or episodes (type 4) added since t.
func (s *Server) RecentlyAdded(ctx context.Context, libKey string, plexType int, since time.Time) ([]Item, error) {
	var c container
	p := fmt.Sprintf("/library/sections/%s/all?type=%d&sort=addedAt:desc&addedAt>>=%d", libKey, plexType, since.Unix())
	if plexType == 1 {
		p += "&includeGuids=1"
	}
	if err := s.get(ctx, p, 0, 500, &c); err != nil {
		return nil, err
	}
	return c.MediaContainer.Metadata, nil
}

// FindByTitle searches one library by title (Plex matches part of the title).
func (s *Server) FindByTitle(ctx context.Context, libKey string, plexType int, title string) ([]Item, error) {
	var c container
	p := fmt.Sprintf("/library/sections/%s/all?type=%d&includeGuids=1&title=%s", libKey, plexType, url.QueryEscape(title))
	if err := s.get(ctx, p, 0, 50, &c); err != nil {
		return nil, err
	}
	return c.MediaContainer.Metadata, nil
}

func (s *Server) Metadata(ctx context.Context, ratingKey string) (*Item, error) {
	var c container
	if err := s.get(ctx, "/library/metadata/"+url.PathEscape(ratingKey)+"?includeGuids=1", 0, 0, &c); err != nil {
		return nil, err
	}
	if len(c.MediaContainer.Metadata) == 0 {
		return nil, errors.New("item not found in Plex")
	}
	return &c.MediaContainer.Metadata[0], nil
}

// Seasons returns a show's seasons with their episode counts (leafCount).
func (s *Server) Seasons(ctx context.Context, ratingKey string) ([]Item, error) {
	var c container
	if err := s.get(ctx, "/library/metadata/"+url.PathEscape(ratingKey)+"/children", 0, 0, &c); err != nil {
		return nil, err
	}
	return c.MediaContainer.Metadata, nil
}

// ---------- Watchlist ----------

// Watchlist returns the items on this account's Plex watchlist with their ids.
//
// Plex's watchlist service refuses big pages ("Invalid value provided for
// x-plex-container-size", HTTP 400), so it is read a small page at a time,
// dropping to an even smaller page if Plex still says no.
func (c *Client) Watchlist(ctx context.Context) ([]Item, error) {
	const most = 2000 // a sane ceiling, in case Plex never reports a total
	size := 20
	var items []Item
	for start := 0; start < most; {
		var mc container
		u := fmt.Sprintf("%s/library/sections/watchlist/all?includeGuids=1&X-Plex-Container-Start=%d&X-Plex-Container-Size=%d", DiscoverURL, start, size)
		if err := c.do(ctx, httpClient, http.MethodGet, u, &mc); err != nil {
			if size > 10 && strings.Contains(err.Error(), "answered 400") {
				size = 10
				continue
			}
			return nil, err
		}
		page := mc.MediaContainer.Metadata
		items = append(items, page...)
		start += len(page)
		if len(page) == 0 || len(page) < size || (mc.MediaContainer.TotalSize > 0 && start >= mc.MediaContainer.TotalSize) {
			break
		}
	}
	for i := range items {
		if len(items[i].Guids) > 0 {
			continue
		}
		var md container
		if err := c.do(ctx, httpClient, http.MethodGet, DiscoverURL+"/library/metadata/"+url.PathEscape(items[i].RatingKey), &md); err == nil && len(md.MediaContainer.Metadata) > 0 {
			items[i].Guids = md.MediaContainer.Metadata[0].Guids
		}
	}
	return items, nil
}
