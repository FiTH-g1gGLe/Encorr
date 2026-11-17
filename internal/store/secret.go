package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Secret is a string that is encrypted when the store is written to disk
// (API keys, Plex tokens, webhook URLs). In memory it is plain text.
type Secret string

const secretPrefix = "enc:v1:"

var (
	keyMu  sync.RWMutex
	aesKey []byte
)

// LoadOrCreateKey reads the 32-byte key from dir/secret.key, creating it on
// first run. The key never leaves the config folder.
func LoadOrCreateKey(dir string) error {
	path := filepath.Join(dir, "secret.key")
	b, err := os.ReadFile(path)
	if err == nil {
		k, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if derr != nil || len(k) != 32 {
			return errors.New("secret.key is damaged: restore it from a backup or delete it (saved keys would then need re-entering)")
		}
		setKey(k)
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(k)), 0o600); err != nil {
		return err
	}
	setKey(k)
	return nil
}

func setKey(k []byte) {
	keyMu.Lock()
	aesKey = k
	keyMu.Unlock()
}

func gcm() (cipher.AEAD, error) {
	keyMu.RLock()
	k := aesKey
	keyMu.RUnlock()
	if k == nil {
		return nil, errors.New("encryption key not loaded")
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s Secret) MarshalJSON() ([]byte, error) {
	if s == "" {
		return json.Marshal("")
	}
	g, err := gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := g.Seal(nonce, nonce, []byte(s), nil)
	return json.Marshal(secretPrefix + base64.StdEncoding.EncodeToString(sealed))
}

func (s *Secret) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw == "" || !strings.HasPrefix(raw, secretPrefix) {
		*s = Secret(raw) // plain value (e.g. hand-edited); re-encrypted on next save
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(raw, secretPrefix))
	if err != nil {
		return err
	}
	g, err := gcm()
	if err != nil {
		return err
	}
	if len(data) < g.NonceSize() {
		return errors.New("secret too short")
	}
	plain, err := g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], nil)
	if err != nil {
		return errors.New("could not decrypt a saved secret: secret.key does not match this data file")
	}
	*s = Secret(plain)
	return nil
}

// Masked returns a short hint safe to show in the UI.
func (s Secret) Masked() string {
	if len(s) <= 4 {
		if s == "" {
			return ""
		}
		return "••••"
	}
	return "••••" + string(s[len(s)-4:])
}
