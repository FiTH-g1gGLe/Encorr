// Package store keeps all of Encorr's data in one JSON file (encorr.json) in
// the config folder. Writes are atomic: data goes to a temp file which then
// replaces the old one, and the previous copy is kept as encorr.json.bak.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const currentVersion = 1

type Store struct {
	mu   sync.RWMutex
	dir  string
	path string
	data *Data
}

// Open loads (or creates) the data file in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create config folder %s: %w", dir, err)
	}
	if err := LoadOrCreateKey(dir); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, path: filepath.Join(dir, "encorr.json")}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.data = newData()
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		d := newData()
		if err := json.Unmarshal(b, d); err != nil {
			return nil, fmt.Errorf("encorr.json is damaged (%v): restore encorr.json.bak", err)
		}
		fillDefaults(d)
		s.data = d
	}
	return s, nil
}

func newData() *Data {
	d := &Data{Version: currentVersion}
	fillDefaults(d)
	return d
}

func fillDefaults(d *Data) {
	if d.Jobs == nil {
		d.Jobs = map[string]JobInfo{}
	}
	if d.NextID == nil {
		d.NextID = map[string]int{}
	}
	if d.Library == nil {
		d.Library = map[string]LibEntry{}
	}
	st := &d.Settings
	if st.ClientID == "" {
		st.ClientID = "encorr-" + RandomToken(12)
	}
	if st.Region == "" {
		st.Region = "GB"
	}
	if st.Language == "" {
		st.Language = "en-GB"
	}
	if st.Accent == "" {
		st.Accent = "violet"
	}
	if st.Quotas.Days == 0 {
		st.Quotas = QuotaSettings{FriendMovieLimit: 10, FriendTVLimit: 3, Days: 7}
	}
	if st.Radarr.MinimumAvailability == "" {
		st.Radarr.MinimumAvailability = "released"
	}
}

func (s *Store) Dir() string { return s.dir }

// Read runs fn with a read lock. fn must not keep references after returning.
func (s *Store) Read(fn func(d *Data)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(s.data)
}

// Update runs fn with a write lock and saves if fn returns nil.
func (s *Store) Update(fn func(d *Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(s.data); err != nil {
		return err
	}
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if _, err := os.Stat(s.path); err == nil {
		_ = copyFile(s.path, s.path+".bak")
	}
	return os.Rename(tmp, s.path)
}

// Backup writes a timestamped copy of the data file and key into dir/backups.
func (s *Store) Backup() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bdir := filepath.Join(s.dir, "backups")
	if err := os.MkdirAll(bdir, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().Format("2006-01-02_150405")
	dst := filepath.Join(bdir, "encorr_"+stamp+".json")
	if err := copyFile(s.path, dst); err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(s.dir, "secret.key"), filepath.Join(bdir, "secret_"+stamp+".key")); err != nil {
		return "", err
	}
	return dst, nil
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

// NextID hands out increasing ids per collection. Call inside Update.
func (d *Data) NewID(kind string) int {
	d.NextID[kind]++
	return d.NextID[kind]
}

func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// ---- lookups (call while holding a lock) ----

func (d *Data) UserByID(id int) *User {
	for _, u := range d.Users {
		if u.ID == id {
			return u
		}
	}
	return nil
}

// UserByName finds a user by username, ignoring capitals.
func (d *Data) UserByName(name string) *User {
	name = strings.TrimSpace(name)
	for _, u := range d.Users {
		if strings.EqualFold(u.Username, name) {
			return u
		}
	}
	return nil
}

// UserByJellyfinID finds the person linked to a Jellyfin account.
func (d *Data) UserByJellyfinID(id string) *User {
	if id == "" {
		return nil
	}
	for _, u := range d.Users {
		if strings.EqualFold(u.JellyfinID, id) {
			return u
		}
	}
	return nil
}

func (d *Data) UserByPlexID(id int) *User {
	if id == 0 {
		return nil // Encorr-only accounts have no Plex id
	}
	for _, u := range d.Users {
		if u.PlexID == id {
			return u
		}
	}
	return nil
}

func (d *Data) MediaByTMDB(typ string, tmdbID int) *Media {
	for _, m := range d.Media {
		if m.Type == typ && m.TMDBID == tmdbID {
			return m
		}
	}
	return nil
}

func (d *Data) MediaByID(id int) *Media {
	for _, m := range d.Media {
		if m.ID == id {
			return m
		}
	}
	return nil
}

func (d *Data) RequestByID(id int) *Request {
	for _, r := range d.Requests {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (d *Data) RequestsForMedia(mediaID int) []*Request {
	var out []*Request
	for _, r := range d.Requests {
		if r.MediaID == mediaID {
			out = append(out, r)
		}
	}
	return out
}

func (d *Data) HasAdmin() bool {
	for _, u := range d.Users {
		if u.Role == RoleAdmin {
			return true
		}
	}
	return false
}

// LibKey builds the key used in Data.Library.
func LibKey(typ string, tmdbID int) string { return fmt.Sprintf("%s:%d", typ, tmdbID) }
