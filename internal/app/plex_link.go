package app

import (
	"net/http"

	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// hPlexLink lets an owner who set Encorr up with Jellyfin (or a password)
// connect their Plex account afterwards: it finishes the Plex PIN sign-in
// and links that account to the owner instead of making a new person.
func (a *App) hPlexLink(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		ID int `json:"id"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	ctx := r.Context()
	pin, err := a.plexClient("").CheckPin(ctx, body.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if pin.AuthToken == "" {
		writeJSON(w, 200, map[string]any{"done": false})
		return
	}
	acct, err := a.plexClient(pin.AuthToken).Account(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	err = a.Store.Update(func(d *store.Data) error {
		me := d.UserByID(u.ID)
		if me == nil {
			return userErr("Please sign in again.")
		}
		if other := d.UserByPlexID(acct.ID); other != nil && other.ID != me.ID {
			if other.Role == store.RoleAdmin {
				return userErr("That Plex account already belongs to another owner here.")
			}
			// They'd signed in with Plex before as a separate person: that
			// account is now the owner's.
			other.PlexID, other.PlexToken = 0, ""
		}
		me.PlexID, me.PlexToken = acct.ID, store.Secret(pin.AuthToken)
		if me.Email == "" {
			me.Email = acct.Email
		}
		if me.Thumb == "" {
			me.Thumb = acct.Thumb
		}
		d.Settings.Plex.Token = store.Secret(pin.AuthToken)
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.activity("action", "Linked Plex", acct.Username)
	writeJSON(w, 200, map[string]any{"done": true})
}
