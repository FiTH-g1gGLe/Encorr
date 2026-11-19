package app

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/netfind"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

// UpdateAPIURL is where Encorr asks GitHub for the newest release.
var UpdateAPIURL = "https://api.github.com/repos/FiTH-g1gGLe/Encorr/releases/latest"

// releaseHosts are the only places update files are downloaded from.
var releaseHosts = map[string]bool{"github.com": true, "objects.githubusercontent.com": true, "release-assets.githubusercontent.com": true}

// exePath finds the running program (a variable so tests can point it at a
// scratch file).
var exePath = os.Executable

const dumbVersion = "0.1.1" // a build from source without a version: never updates

// UpdateInfo is what the Settings page shows.
type UpdateInfo struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest,omitempty"`
	Available bool      `json:"available"`
	Notes     string    `json:"notes,omitempty"`
	URL       string    `json:"url,omitempty"` // release page
	Published time.Time `json:"published,omitempty"`
	Checked   time.Time `json:"checked,omitempty"`
	// How: "button" (Encorr can update itself), "docker" (pull the new
	// image), or "manual" (download it yourself).
	How      string `json:"how"`
	Error    string `json:"error,omitempty"`
	Updating bool   `json:"updating,omitempty"`
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

type updater struct {
	mu       sync.Mutex
	rel      *ghRelease
	checked  time.Time
	err      string
	updating bool
}

// assetName is the download for this computer.
func assetName() string {
	if runtime.GOOS == "windows" {
		return "Encorr-Windows.zip"
	}
	return "Encorr-Linux-" + runtime.GOARCH + ".tar.gz"
}

func updateHow() string {
	switch {
	case netfind.InDocker():
		return "docker"
	case runtime.GOOS == "windows" || runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"):
		return "button"
	}
	return "manual"
}

// newerVersion reports whether version a is newer than b ("0.1.54" > "0.1.53").
func newerVersion(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// checkUpdate asks GitHub for the newest release.
func (a *App) checkUpdate(ctx context.Context) error {
	if Version == dumbVersion {
		return nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, UpdateAPIURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Encorr/"+Version)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	var rel ghRelease
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			err = fmt.Errorf("GitHub answered %d", resp.StatusCode)
		} else {
			err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&rel)
		}
	}
	a.upd.mu.Lock()
	defer a.upd.mu.Unlock()
	a.upd.checked = time.Now()
	if err != nil {
		a.upd.err = "Couldn't check for updates: " + err.Error()
		return err
	}
	a.upd.rel, a.upd.err = &rel, ""
	return nil
}

func (a *App) jobUpdateCheck(ctx context.Context) (string, error) {
	if Version == dumbVersion {
		return "Development build: no update checks", nil
	}
	if err := a.checkUpdate(ctx); err != nil {
		return "", err
	}
	info := a.updateInfo()
	if info.Available {
		return "Version " + info.Latest + " is available", nil
	}
	return "Up to date", nil
}

func (a *App) updateInfo() UpdateInfo {
	a.upd.mu.Lock()
	defer a.upd.mu.Unlock()
	out := UpdateInfo{Current: Version, How: updateHow(), Checked: a.upd.checked, Error: a.upd.err, Updating: a.upd.updating}
	if r := a.upd.rel; r != nil {
		out.Latest, out.URL, out.Published = strings.TrimPrefix(r.TagName, "v"), r.HTMLURL, r.PublishedAt
		out.Notes = r.Body
		if len(out.Notes) > 1500 {
			out.Notes = out.Notes[:1500]
		}
		out.Available = Version != dumbVersion && newerVersion(out.Latest, Version)
	}
	return out
}

func (a *App) hUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	a.upd.mu.Lock()
	stale := time.Since(a.upd.checked) > 30*time.Minute
	a.upd.mu.Unlock()
	if r.URL.Query().Get("check") == "1" || stale && Version != dumbVersion {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		_ = a.checkUpdate(ctx)
		cancel()
	}
	writeJSON(w, 200, a.updateInfo())
}

// hInstallUpdate downloads the new version, checks it against GitHub's
// published checksum, swaps it in and restarts Encorr.
func (a *App) hInstallUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	if updateHow() != "button" {
		fail(w, userErr("Encorr can't update itself here. In Docker, pull the new image (see the guide)."))
		return
	}
	if a.Restart == nil {
		fail(w, userErr("This copy of Encorr can't restart itself. Download the new version from GitHub instead."))
		return
	}
	a.upd.mu.Lock()
	if a.upd.updating {
		a.upd.mu.Unlock()
		fail(w, userErr("An update is already under way."))
		return
	}
	rel := a.upd.rel
	if rel == nil || !newerVersion(rel.TagName, Version) {
		a.upd.mu.Unlock()
		fail(w, userErr("You're already on the newest version."))
		return
	}
	a.upd.updating = true
	a.upd.mu.Unlock()
	done := func() { a.upd.mu.Lock(); a.upd.updating = false; a.upd.mu.Unlock() }

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	newExe, err := a.downloadUpdate(ctx, rel)
	if err != nil {
		done()
		a.logf("update to %s failed: %v", rel.TagName, err)
		fail(w, err)
		return
	}
	if err := swapExe(newExe); err != nil {
		_ = os.Remove(newExe)
		done()
		a.logf("update to %s failed: %v", rel.TagName, err)
		fail(w, userErr("Downloaded the update but couldn't put it in place: "+err.Error()))
		return
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	a.logf("updated from %s to %s by %s; restarting", Version, latest, u.Username)
	a.activity("action", "Updating Encorr", fmt.Sprintf("%s → %s (%s)", Version, latest, u.Username))
	writeJSON(w, 200, map[string]any{"ok": true, "version": latest})
	go func() {
		time.Sleep(1500 * time.Millisecond) // let the answer reach the browser
		a.Restart()
	}()
}

// downloadUpdate fetches this computer's file from the release, checks its
// SHA-256 against the release's SHA256SUMS.txt and unpacks the program next
// to the running one. It returns the new program's path.
func (a *App) downloadUpdate(ctx context.Context, rel *ghRelease) (string, error) {
	var fileURL, sumsURL string
	want := assetName()
	for _, as := range rel.Assets {
		switch as.Name {
		case want:
			fileURL = as.URL
		case "SHA256SUMS.txt":
			sumsURL = as.URL
		}
	}
	if fileURL == "" {
		return "", userErr("The new release has no " + want + " yet. Try again in a few minutes.")
	}
	if sumsURL == "" {
		return "", userErr("The new release has no checksum file, so Encorr won't install it automatically. Download it from GitHub instead.")
	}
	sums, err := fetchRelease(ctx, sumsURL, 1<<20)
	if err != nil {
		return "", err
	}
	wantSum := ""
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == want {
			wantSum = strings.ToLower(f[0])
		}
	}
	if wantSum == "" {
		return "", userErr("The checksum file doesn't list " + want + ".")
	}
	archive, err := fetchRelease(ctx, fileURL, 200<<20)
	if err != nil {
		return "", err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != wantSum {
		return "", userErr("The download didn't match GitHub's checksum, so it wasn't installed. Try again later.")
	}
	bin, err := extractProgram(archive, want)
	if err != nil {
		return "", err
	}
	exe, err := exePath()
	if err != nil {
		return "", err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	tmp := exe + ".update"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return "", userErr("Couldn't save the update next to Encorr: " + err.Error())
	}
	return tmp, nil
}

func fetchRelease(ctx context.Context, raw string, max int64) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !releaseHosts[u.Hostname()] {
		return nil, userErr("Refusing to download an update from " + raw)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	req.Header.Set("User-Agent", "Encorr/"+Version)
	c := &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || r.URL.Scheme != "https" || !releaseHosts[r.URL.Hostname()] {
			return errors.New("unexpected redirect to " + r.URL.Host)
		}
		return nil
	}}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't download the update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("couldn't download the update: GitHub answered %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("couldn't download the update: %w", err)
	}
	if int64(len(b)) > max {
		return nil, errors.New("the update is bigger than expected")
	}
	return b, nil
}

// extractProgram pulls encorr.exe (Windows zip) or encorr (Linux tarball)
// out of a release download.
func extractProgram(archive []byte, name string) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if strings.EqualFold(path.Base(f.Name), "encorr.exe") {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, 300<<20))
			}
		}
		return nil, errors.New("encorr.exe isn't in the download")
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("encorr isn't in the download")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == "encorr" {
			return io.ReadAll(io.LimitReader(tr, 300<<20))
		}
	}
}

// swapExe puts the new program in place of the running one. A running
// program can be renamed (even on Windows) but not overwritten, so the old
// one becomes encorr.exe.old and is tidied up on the next update.
func swapExe(newPath string) error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(old, exe) // put it back
		return err
	}
	return nil
}

// SetExePath tells Encorr where its program file is, found at start-up
// (after an update the running file has been renamed).
func SetExePath(p string) {
	if p != "" {
		exePath = func() (string, error) { return p, nil }
	}
}
