package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/FiTH-g1gGLe/encorr/internal/sab"
)

// setSABServer changes one setting on an existing SABnzbd news server.
// SABnzbd creates a new server for a name it doesn't know, so Encorr checks
// the server is there first, and sends its quota so a block's usage counter
// isn't reset.
func (a *App) setSABServer(ctx context.Context, c *sab.Client, server, field, value string) error {
	cfg, err := c.GetConfig(ctx)
	if err != nil {
		return err
	}
	for _, sv := range cfg.Servers {
		if fmt.Sprint(sv["name"]) == server {
			quota := ""
			if q, ok := sv["quota"]; ok && q != nil {
				quota = strings.TrimSpace(fmt.Sprint(q))
			}
			return c.SetServer(ctx, server, field, value, quota)
		}
	}
	return userErr("SABnzbd has no server called " + server + " any more.")
}

// sabServerFields are the server settings Encorr has ever written. An old
// Encorr version sent them the wrong way round, and SABnzbd made a new,
// empty server named after the setting (it was never used: SABnzbd skips
// servers without an address).
var sabServerFields = map[string]bool{
	"pipelining_requests": true, "connections": true, "ssl_ciphers": true, "priority": true,
	"quota": true, "expire_date": true, "usage_at_start": true, "timeout": true, "enable": true,
	"optional": true, "required": true, "retention": true, "ssl": true, "ssl_verify": true,
	"displayname": true, "value": true,
}

// isStraySABServer is true only for those empty leftovers: named exactly like
// a setting, with no address.
func isStraySABServer(sv map[string]any) bool {
	name := strings.TrimSpace(fmt.Sprint(sv["name"]))
	host := ""
	if h, ok := sv["host"]; ok && h != nil {
		host = strings.TrimSpace(fmt.Sprint(h))
	}
	return host == "" && sabServerFields[name]
}

// cleanStraySABServers removes those leftovers. Returns how many it removed.
func (a *App) cleanStraySABServers(ctx context.Context) int {
	c := a.SAB()
	if c == nil {
		return 0
	}
	cfg, err := c.GetConfig(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, sv := range cfg.Servers {
		if !isStraySABServer(sv) {
			continue
		}
		name := fmt.Sprint(sv["name"])
		if err := c.DeleteServer(ctx, name); err != nil {
			a.logf("couldn't remove the empty SABnzbd server %q: %v", name, err)
			continue
		}
		n++
		a.activity("fix", "Removed an empty SABnzbd server", fmt.Sprintf("%q had no address and was never used: an earlier version of Encorr made it by mistake.", name))
	}
	return n
}
