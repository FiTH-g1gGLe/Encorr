package app

import (
	"net/http"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// Quota is how many requests of one kind someone has used in the current
// period, and their limit (0 = no limit).
type Quota struct {
	Limit    int        `json:"limit"`
	Used     int        `json:"used"`
	Left     int        `json:"left"`
	Days     int        `json:"days"`
	Custom   bool       `json:"custom,omitempty"` // set for this person, not the default
	NextFree *time.Time `json:"nextFree,omitempty"`
}

// quotaFor works out a person's limit for films or shows. Owners never have
// one. A limit set on the person wins; otherwise Friends get the default and
// Trusted people none. Declined requests don't count.
func quotaFor(d *store.Data, u *store.User, typ string) Quota {
	q := d.Settings.Quotas
	days := q.Days
	if days <= 0 {
		days = 7
	}
	out := Quota{Days: days}
	if u.Role == store.RoleAdmin {
		return out
	}
	own := u.MovieLimit
	def := q.FriendMovieLimit
	if typ == store.TypeTV {
		own, def = u.TVLimit, q.FriendTVLimit
	}
	switch {
	case own != nil:
		out.Limit, out.Custom = *own, true
	case u.Role == store.RoleFriend && q.Days > 0:
		out.Limit = def
	}
	if out.Limit <= 0 {
		out.Limit = 0
		return out
	}
	since := time.Now().AddDate(0, 0, -days)
	var oldest time.Time
	for _, r := range d.Requests {
		if r.RequestedBy == u.ID && r.Type == typ && r.Created.After(since) && r.Status != store.ReqDeclined {
			out.Used++
			if oldest.IsZero() || r.Created.Before(oldest) {
				oldest = r.Created
			}
		}
	}
	out.Left = out.Limit - out.Used
	if out.Left < 0 {
		out.Left = 0
	}
	if out.Left == 0 && !oldest.IsZero() {
		t := oldest.AddDate(0, 0, days)
		out.NextFree = &t
	}
	return out
}

// hMyQuota tells someone how many requests they have left.
func (a *App) hMyQuota(w http.ResponseWriter, r *http.Request, u *store.User) {
	var mv, tv Quota
	a.Store.Read(func(d *store.Data) {
		if me := d.UserByID(u.ID); me != nil {
			mv, tv = quotaFor(d, me, store.TypeMovie), quotaFor(d, me, store.TypeTV)
		}
	})
	writeJSON(w, 200, map[string]Quota{"movie": mv, "tv": tv})
}
