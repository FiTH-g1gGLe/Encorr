package app

// Subscriptions: everything the owner pays for, in one place. Indexers and
// their VIP expiry come from Prowlarr; news servers, block sizes and usage
// come from SABnzbd; anything else is added by hand. Reminders go out before
// things run out.

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/notify"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// SubView is one card on the Subscriptions page.
type SubView struct {
	ID       int        `json:"id,omitempty"` // stored entry (hand-added, or details added to a detected one)
	Key      string     `json:"key,omitempty"`
	Name     string     `json:"name"`
	Kind     string     `json:"kind"`
	Plan     string     `json:"plan"`
	Cost     float64    `json:"cost,omitempty"`
	Currency string     `json:"currency,omitempty"`
	Renews   *time.Time `json:"renews,omitempty"`
	DaysLeft *int       `json:"daysLeft,omitempty"`
	Status   string     `json:"status"` // ok | soon | urgent | expired | lifetime | unknown | off
	Source   string     `json:"source"` // Prowlarr | SABnzbd | You
	Note     string     `json:"note,omitempty"`
	URL      string     `json:"url,omitempty"`
	Notes    string     `json:"notes,omitempty"`
	// Blocks: size and how much is used, in bytes.
	BlockSize float64 `json:"blockSize,omitempty"`
	BlockUsed float64 `json:"blockUsed,omitempty"`
	Enabled   bool    `json:"enabled"`
}

// detectedSubs reads indexers from Prowlarr and news servers from SABnzbd.
func (a *App) detectedSubs(ctx context.Context) []SubView {
	var mu sync.Mutex
	var out []SubView
	var wg sync.WaitGroup
	a.autoDetectProwlarr(ctx)
	if c := a.Prowlarr(); c != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			var idx []map[string]any
			if c.GetJSON(cctx, "/indexer", &idx) != nil {
				return
			}
			var status []map[string]any
			_ = c.GetJSON(cctx, "/indexerstatus", &status)
			paused := map[string]bool{}
			for _, st := range status {
				if t, err := time.Parse(time.RFC3339, canon(st["disabledTill"])); err == nil && t.After(time.Now()) {
					paused[canon(st["indexerId"])] = true
				}
			}
			for _, ix := range idx {
				if canon(ix["protocol"]) == "torrent" {
					continue
				}
				name := strings.TrimSuffix(canon(ix["name"]), " (Prowlarr)")
				v := SubView{Key: "prowlarr:" + name, Name: name, Kind: "indexer", Source: "Prowlarr", Enabled: canon(ix["enable"]) == "1"}
				if vip, ok := fieldValue(ix, "vipExpiration"); ok {
					if d, err := time.Parse("2006-01-02", canon(vip)); err == nil {
						v.Renews = &d
						if d.Year() > 2100 {
							v.Plan = "lifetime"
						}
					}
				}
				if paused[canon(ix["id"])] {
					v.Note = "Paused in Prowlarr after failures"
				}
				mu.Lock()
				out = append(out, v)
				mu.Unlock()
			}
		}()
	}
	if c := a.SAB(); c != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			cfg, err := c.GetConfig(cctx)
			if err != nil {
				return
			}
			totals := map[string]float64{}
			var stats struct {
				Servers map[string]struct {
					Total float64 `json:"total"`
				} `json:"servers"`
			}
			if c.CallRaw(cctx, "server_stats", &stats) == nil {
				for k, v := range stats.Servers {
					totals[k] = v.Total
				}
			}
			for _, s := range cfg.Servers {
				if isStraySABServer(s) {
					continue
				}
				name := canon(s["name"])
				if name == "" {
					continue
				}
				label := name
				if dn := canon(s["displayname"]); dn != "" {
					label = dn
				}
				v := SubView{Key: "sab:" + name, Name: label, Kind: "provider", Source: "SABnzbd", Enabled: canon(s["enable"]) != "0"}
				if d, err := time.Parse("2006-01-02", canon(s["expire_date"])); err == nil {
					v.Renews = &d
				}
				if q := sizeBytes(s["quota"]); q > 0 {
					v.Plan = "block"
					v.BlockSize = q
					used := totals[name] - num(s["usage_at_start"])
					if used < 0 {
						used = 0
					}
					v.BlockUsed = used
				}
				if num(s["priority"]) > 0 && v.Plan == "" {
					v.Note = fmt.Sprintf("Backup server (priority %s)", canon(s["priority"]))
				}
				mu.Lock()
				out = append(out, v)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

// subscriptions merges detected services with the owner's own entries.
func (a *App) subscriptions(ctx context.Context) []SubView {
	detected := a.detectedSubs(ctx)
	var stored []store.Subscription
	a.Store.Read(func(d *store.Data) { stored = append(stored, d.Subscriptions...) })
	byKey := map[string]store.Subscription{}
	for _, s := range stored {
		if s.Key != "" {
			byKey[s.Key] = s
		}
	}
	var out []SubView
	seen := map[string]bool{}
	for _, v := range detected {
		if s, ok := byKey[v.Key]; ok {
			v.ID, v.Cost, v.Currency, v.URL, v.Notes = s.ID, s.Cost, s.Currency, s.URL, s.Notes
			if s.Name != "" {
				v.Name = s.Name
			}
			if s.Plan != "" && v.Plan != "block" && v.Plan != "lifetime" {
				v.Plan = s.Plan
			}
			if v.Renews == nil && s.Renews != nil {
				v.Renews = s.Renews
			}
			seen[v.Key] = true
		}
		out = append(out, v)
	}
	for _, s := range stored {
		if s.Key != "" && !seen[s.Key] && (strings.HasPrefix(s.Key, "prowlarr:") || strings.HasPrefix(s.Key, "sab:")) {
			continue // details for something no longer detected
		}
		if s.Key != "" && seen[s.Key] {
			continue
		}
		out = append(out, SubView{ID: s.ID, Key: s.Key, Name: s.Name, Kind: s.Kind, Plan: s.Plan, Cost: s.Cost, Currency: s.Currency,
			Renews: s.Renews, Source: "You", URL: s.URL, Notes: s.Notes, Enabled: true})
	}
	now := time.Now()
	for i := range out {
		v := &out[i]
		switch {
		case !v.Enabled:
			v.Status = "off"
		case v.Plan == "lifetime":
			v.Status = "lifetime"
		case v.Renews == nil:
			v.Status = "unknown"
			if v.Plan == "block" {
				v.Status = "ok"
			}
		default:
			days := int(v.Renews.Sub(now.Truncate(24*time.Hour)).Hours() / 24)
			v.DaysLeft = &days
			switch {
			case days < 0:
				v.Status = "expired"
			case days <= 7:
				v.Status = "urgent"
			case days <= 30:
				v.Status = "soon"
			default:
				v.Status = "ok"
			}
		}
		if v.Plan == "block" && v.BlockSize > 0 && v.Status == "ok" {
			if pct := v.BlockUsed / v.BlockSize; pct >= 0.9 {
				v.Status = "urgent"
			} else if pct >= 0.75 {
				v.Status = "soon"
			}
		}
	}
	rank := map[string]int{"expired": 0, "urgent": 1, "soon": 2, "unknown": 3, "ok": 4, "lifetime": 5, "off": 6}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Status] != rank[out[j].Status] {
			return rank[out[i].Status] < rank[out[j].Status]
		}
		if out[i].DaysLeft != nil && out[j].DaysLeft != nil {
			return *out[i].DaysLeft < *out[j].DaysLeft
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// yearlyCost totals what renews each year, per currency, plus one-off costs.
func yearlyCost(list []SubView) (map[string]float64, map[string]float64) {
	yearly, oneOff := map[string]float64{}, map[string]float64{}
	for _, v := range list {
		if v.Cost <= 0 || v.Status == "off" {
			continue
		}
		cur := v.Currency
		if cur == "" {
			cur = "GBP"
		}
		switch v.Plan {
		case "monthly":
			yearly[cur] += v.Cost * 12
		case "yearly":
			yearly[cur] += v.Cost
		default:
			oneOff[cur] += v.Cost
		}
	}
	return yearly, oneOff
}

// ---------- reminders ----------

// jobSubscriptions sends one reminder per milestone: 14, 7 and 1 days before
// something runs out, on the day, and when a block passes 75% and 90%.
func (a *App) jobSubscriptions(ctx context.Context) (string, error) {
	list := a.subscriptions(ctx)
	var sent []string
	for _, v := range list {
		if v.Status == "off" {
			continue
		}
		var key, msg string
		if v.DaysLeft != nil && v.Plan != "lifetime" {
			d := *v.DaysLeft
			date := v.Renews.Format("2006-01-02")
			for _, m := range []int{0, 1, 7, 14} {
				if d <= m && d >= 0 {
					key = fmt.Sprintf("%s|%s|%d", subRef(v), date, m)
					switch d {
					case 0:
						msg = v.Name + " runs out today"
					case 1:
						msg = v.Name + " runs out tomorrow"
					default:
						msg = fmt.Sprintf("%s runs out in %d days (%s)", v.Name, d, v.Renews.Format("2 Jan"))
					}
					break
				}
			}
		}
		if key == "" && v.Plan == "block" && v.BlockSize > 0 {
			pct := v.BlockUsed / v.BlockSize * 100
			for _, m := range []int{90, 75} {
				if pct >= float64(m) {
					key = fmt.Sprintf("%s|block|%d", subRef(v), m)
					msg = fmt.Sprintf("%s block is %.0f%% used (%s left)", v.Name, pct, humanBytes(v.BlockSize-v.BlockUsed))
					break
				}
			}
		}
		if key == "" {
			continue
		}
		already := false
		_ = a.Store.Update(func(d *store.Data) error {
			if d.SubReminders == nil {
				d.SubReminders = map[string]bool{}
			}
			already = d.SubReminders[key]
			d.SubReminders[key] = true
			return nil
		})
		if already {
			continue
		}
		sent = append(sent, msg)
		a.activity("warn", msg, "Settings → Subscriptions")
		a.Notify(notify.Message{Event: notify.EventAttention, Title: msg, Detail: "Renew or top it up so downloads keep working. Details in Encorr → Settings → Subscriptions."})
	}
	if len(sent) == 0 {
		return "nothing due", nil
	}
	return fmt.Sprintf("%d reminder(s)", len(sent)), nil
}

func subRef(v SubView) string {
	if v.Key != "" {
		return v.Key
	}
	return fmt.Sprintf("id:%d", v.ID)
}

func humanBytes(b float64) string {
	switch {
	case b >= 1e12:
		return fmt.Sprintf("%.1f TB", b/1e12)
	case b >= 1e9:
		return fmt.Sprintf("%.0f GB", b/1e9)
	}
	return fmt.Sprintf("%.0f MB", b/1e6)
}

// ---------- HTTP ----------

func (a *App) hSubscriptions(w http.ResponseWriter, r *http.Request, u *store.User) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	list := a.subscriptions(ctx)
	if list == nil {
		list = []SubView{}
	}
	yearly, oneOff := yearlyCost(list)
	writeJSON(w, 200, map[string]any{"items": list, "yearly": yearly, "oneOff": oneOff,
		"prowlarr": a.Prowlarr() != nil, "sabnzbd": a.SAB() != nil})
}

type subBody struct {
	Key      string  `json:"key"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Plan     string  `json:"plan"`
	Cost     float64 `json:"cost"`
	Currency string  `json:"currency"`
	Renews   string  `json:"renews"` // YYYY-MM-DD or empty
	URL      string  `json:"url"`
	Notes    string  `json:"notes"`
}

var subPlans = map[string]bool{"monthly": true, "yearly": true, "lifetime": true, "block": true, "other": true}
var subKinds = map[string]bool{"provider": true, "indexer": true, "other": true}

func (b subBody) toStore() (store.Subscription, error) {
	s := store.Subscription{Key: strings.TrimSpace(b.Key), Name: strings.TrimSpace(b.Name), Kind: b.Kind, Plan: b.Plan,
		Cost: b.Cost, Currency: strings.ToUpper(strings.TrimSpace(b.Currency)), URL: strings.TrimSpace(b.URL), Notes: strings.TrimSpace(b.Notes)}
	if s.Name == "" && s.Key == "" {
		return s, userErr("Give it a name.")
	}
	if !subKinds[s.Kind] {
		s.Kind = "other"
	}
	if s.Plan != "" && !subPlans[s.Plan] {
		s.Plan = "other"
	}
	if s.Cost < 0 || s.Cost > 100000 {
		return s, userErr("That cost doesn't look right.")
	}
	if s.Currency != "" && s.Currency != "GBP" && s.Currency != "USD" && s.Currency != "EUR" {
		s.Currency = "GBP"
	}
	if s.URL != "" && !strings.HasPrefix(s.URL, "https://") && !strings.HasPrefix(s.URL, "http://") {
		s.URL = "https://" + s.URL
	}
	if b.Renews != "" {
		t, err := time.Parse("2006-01-02", b.Renews)
		if err != nil {
			return s, userErr("Pick a renewal date, or leave it empty.")
		}
		s.Renews = &t
	}
	return s, nil
}

// hSaveSubscription adds an entry, or saves details for a detected one.
func (a *App) hSaveSubscription(w http.ResponseWriter, r *http.Request, u *store.User) {
	var b subBody
	if err := readJSON(r, &b); err != nil {
		fail(w, err)
		return
	}
	s, err := b.toStore()
	if err != nil {
		fail(w, err)
		return
	}
	id := pathInt(r, "id")
	err = a.Store.Update(func(d *store.Data) error {
		for i := range d.Subscriptions {
			x := &d.Subscriptions[i]
			if (id > 0 && x.ID == id) || (id == 0 && s.Key != "" && x.Key == s.Key) {
				s.ID = x.ID
				*x = s
				return nil
			}
		}
		if id > 0 {
			return userErr("That subscription has gone.")
		}
		s.ID = d.NewID("subscription")
		d.Subscriptions = append(d.Subscriptions, s)
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, s)
}

func (a *App) hDeleteSubscription(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := pathInt(r, "id")
	_ = a.Store.Update(func(d *store.Data) error {
		kept := d.Subscriptions[:0]
		for _, x := range d.Subscriptions {
			if x.ID != id {
				kept = append(kept, x)
			}
		}
		d.Subscriptions = kept
		return nil
	})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
