package app

import (
	"net/http"

	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// BackgroundStatus describes running Encorr in the background (as a Windows
// service that starts with the computer).
type BackgroundStatus struct {
	Supported bool   `json:"supported"` // false on Linux/macOS builds for now
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	IsService bool   `json:"isService"` // this copy is the background one
	Note      string `json:"note,omitempty"`
}

// Background is provided by the platform layer (cmd/encorr).
type Background interface {
	Status() BackgroundStatus
	// Install sets up the background service. quit is called when this
	// (window) copy should close so the service can take over.
	Install(quit func()) error
	// Remove takes Encorr out of the background; quit is called after.
	Remove(quit func()) error
}

// Quit is closed when Encorr should shut down by itself (for example,
// handing over to the background service).
func (a *App) Quit() <-chan struct{} { return a.quit }

func (a *App) requestQuit() { a.quitOnce.Do(func() { close(a.quit) }) }

func (a *App) hBackground(w http.ResponseWriter, r *http.Request, u *store.User) {
	if a.Background == nil {
		writeJSON(w, 200, BackgroundStatus{})
		return
	}
	writeJSON(w, 200, a.Background.Status())
}

func (a *App) hBackgroundSet(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		On bool `json:"on"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, err)
		return
	}
	if a.Background == nil || !a.Background.Status().Supported {
		fail(w, userErr("Running in the background isn't available on this system yet."))
		return
	}
	a.upd.mu.Lock()
	updating := a.upd.updating
	a.upd.mu.Unlock()
	if updating {
		fail(w, userErr("Encorr is updating. Wait for it to restart, then try again."))
		return
	}
	var err error
	if body.On {
		err = a.Background.Install(a.requestQuit)
	} else {
		err = a.Background.Remove(a.requestQuit)
	}
	if err != nil {
		fail(w, userErr(err.Error()))
		return
	}
	a.logf("%s turned background mode %s", u.Username, map[bool]string{true: "on", false: "off"}[body.On])
	writeJSON(w, 200, map[string]bool{"ok": true})
}
