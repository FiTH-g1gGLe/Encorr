package arr

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Detected is what the setup wizard pre-fills when Radarr/Sonarr live on the
// same machine.
type Detected struct {
	Found      bool   `json:"found"`
	URL        string `json:"url,omitempty"`
	APIKey     string `json:"apiKey,omitempty"`
	ConfigPath string `json:"configPath,omitempty"`
	Note       string `json:"note,omitempty"`
}

type arrConfig struct {
	Port        int    `xml:"Port"`
	SslPort     int    `xml:"SslPort"`
	EnableSsl   string `xml:"EnableSsl"`
	URLBase     string `xml:"UrlBase"`
	APIKey      string `xml:"ApiKey"`
	BindAddress string `xml:"BindAddress"`
}

// ConfigCandidates lists where Radarr/Sonarr normally keep config.xml.
// app is "Radarr" or "Sonarr".
func ConfigCandidates(app string) []string {
	lower := strings.ToLower(app)
	var c []string
	if extra := os.Getenv("ENCORR_" + strings.ToUpper(app) + "_CONFIG"); extra != "" {
		c = append(c, extra)
	}
	switch runtime.GOOS {
	case "windows":
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		c = append(c, filepath.Join(pd, app, "config.xml"))
		if app == "Sonarr" {
			c = append(c, filepath.Join(pd, "NzbDrone", "config.xml")) // very old Sonarr
		}
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			c = append(c, filepath.Join(la, app, "config.xml"))
		}
	default:
		if home, err := os.UserHomeDir(); err == nil {
			c = append(c, filepath.Join(home, ".config", app, "config.xml"))
		}
		c = append(c,
			filepath.Join("/var/lib", lower, "config.xml"),
			filepath.Join("/var/lib", lower, ".config", app, "config.xml"),
			filepath.Join("/config", "config.xml"),
			// Docker: Radarr's config folder mounted read-only at /radarr.
			filepath.Join("/", lower, "config.xml"),
		)
	}
	return c
}

// ParseConfig reads a Radarr/Sonarr config.xml and builds the local address.
func ParseConfig(data []byte) (Detected, error) {
	var cfg arrConfig
	if err := xml.Unmarshal(data, &cfg); err != nil {
		return Detected{}, err
	}
	if cfg.APIKey == "" || cfg.Port == 0 {
		return Detected{}, fmt.Errorf("config.xml has no port or API key")
	}
	// Radarr/Sonarr keep serving plain HTTP on Port even when SSL is enabled,
	// so the local address always uses it.
	scheme, port := "http", cfg.Port
	host := "localhost"
	if b := strings.TrimSpace(cfg.BindAddress); b != "" && b != "*" && b != "0.0.0.0" {
		host = b
	}
	base := strings.TrimRight(cfg.URLBase, "/")
	if base != "" && !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return Detected{Found: true, URL: scheme + "://" + host + ":" + strconv.Itoa(port) + base, APIKey: cfg.APIKey}, nil
}

// Detect looks for a local Radarr/Sonarr install.
func Detect(app string) Detected {
	for _, p := range ConfigCandidates(app) {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		d, err := ParseConfig(b)
		if err != nil {
			return Detected{Note: fmt.Sprintf("found %s but couldn't read it: %v", p, err)}
		}
		d.ConfigPath = p
		return d
	}
	def := map[string]string{"Radarr": "http://localhost:7878", "Sonarr": "http://localhost:8989"}[app]
	return Detected{URL: def, Note: app + " wasn't found on this machine. If it's elsewhere, enter its address and API key (" + app + " → Settings → General)."}
}
