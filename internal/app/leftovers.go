package app

// Leftover downloads: folders in SABnzbd's "complete" folder that Radarr or
// Sonarr never cleaned up. Only ones that are safe to delete are offered:
// half-unpacked or failed scraps, folders with no video in them, and copies
// of things the library already has. Anything Encorr can't be sure about
// (a show Sonarr doesn't know, a better copy that was never imported) is
// left alone.

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/sab"
)

type leftover struct {
	Path   string
	Root   string // the complete folder it sits in
	Bytes  int64
	Reason string
}

var videoExt = map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".m4v": true, ".ts": true, ".wmv": true, ".mov": true}

// leftoverMinAge keeps Encorr away from anything that might still be
// unpacking or waiting to import.
var leftoverMinAge = 6 * time.Hour

// sabCompleteRoots are the folders finished downloads land in: the complete
// folder and each category's folder. Only absolute paths that exist on this
// computer count (in Docker they usually don't, and are skipped).
func sabCompleteRoots(cfg *sab.Config) map[string]string {
	roots := map[string]string{} // path -> "tv" / "movies" / ""
	base := canon(cfg.Misc["complete_dir"])
	if base == "" || !filepath.IsAbs(base) {
		return roots
	}
	if st, err := os.Stat(base); err != nil || !st.IsDir() {
		return roots
	}
	roots[filepath.Clean(base)] = ""
	for _, c := range cfg.Categories {
		dir, name := canon(c["dir"]), canon(c["name"])
		if dir == "" || name == "*" {
			continue
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(base, dir)
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			roots[filepath.Clean(dir)] = strings.ToLower(name)
		}
	}
	return roots
}

func folderVideo(path string) (int64, int64) {
	var video, total int64
	depth0 := strings.Count(path, string(os.PathSeparator))
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.Count(p, string(os.PathSeparator))-depth0 > 4 {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
			if videoExt[strings.ToLower(filepath.Ext(p))] && info.Size() > 50<<20 && !strings.Contains(strings.ToLower(filepath.Base(p)), "sample") {
				video += info.Size()
			}
		}
		return nil
	})
	return video, total
}

// findLeftovers lists folders that are safe to delete.
func (a *App) findLeftovers(ctx context.Context, cfg *sab.Config) []leftover {
	roots := sabCompleteRoots(cfg)
	if len(roots) == 0 {
		return nil
	}
	protected := a.busyDownloadPaths(ctx)
	type cand struct {
		path, root, cat string
	}
	var cands []cand
	for root, cat := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, en := range entries {
			if !en.IsDir() {
				continue
			}
			p := filepath.Join(root, en.Name())
			if _, isRoot := roots[filepath.Clean(p)]; isRoot || protected[strings.ToLower(filepath.Clean(p))] {
				continue // a category folder, or something still being handled
			}
			if info, err := en.Info(); err != nil || time.Since(info.ModTime()) < leftoverMinAge {
				continue
			}
			cands = append(cands, cand{p, root, cat})
		}
	}
	var mu sync.Mutex
	var out []leftover
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, c := range cands {
		if i >= 150 || ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(c cand) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			video, total := folderVideo(c.path)
			name := filepath.Base(c.path)
			reason := ""
			switch {
			case strings.HasPrefix(name, "_UNPACK_"):
				reason = "half-unpacked leftover"
			case strings.HasPrefix(name, "_FAILED_"):
				reason = "failed download"
			case video == 0:
				reason = "no video in it"
			default:
				if a.inLibraryAlready(ctx, c.path, c.cat) {
					reason = "already in your library"
				}
			}
			if reason == "" {
				return
			}
			mu.Lock()
			out = append(out, leftover{Path: c.path, Root: c.root, Bytes: total, Reason: reason})
			mu.Unlock()
		}(c)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}

// busyDownloadPaths are folders Radarr/Sonarr or SABnzbd are still working
// with, lower-cased.
func (a *App) busyDownloadPaths(ctx context.Context) map[string]bool {
	busy := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		busy[strings.ToLower(p)] = true
		if videoExt[strings.ToLower(filepath.Ext(p))] {
			busy[strings.ToLower(filepath.Dir(p))] = true
		}
	}
	for _, c := range []*arr.Client{a.Radarr(), a.Sonarr()} {
		if c == nil {
			continue
		}
		if q, err := c.Queue(ctx); err == nil {
			for _, it := range q {
				add(it.OutputPath)
			}
		}
	}
	return busy
}

// inLibraryAlready asks Radarr/Sonarr about a folder: true only when every
// video in it belongs to something they know AND the library already has a
// file for it AND it isn't a better copy waiting to be imported.
func (a *App) inLibraryAlready(ctx context.Context, path, cat string) bool {
	var clients []*arr.Client
	switch {
	case strings.Contains(cat, "tv") || strings.Contains(cat, "sonarr") || strings.Contains(cat, "show"):
		clients = []*arr.Client{a.Sonarr()}
	case strings.Contains(cat, "movie") || strings.Contains(cat, "radarr") || strings.Contains(cat, "film"):
		clients = []*arr.Client{a.Radarr()}
	default:
		clients = []*arr.Client{a.Sonarr(), a.Radarr()}
	}
	for _, c := range clients {
		if c == nil {
			continue
		}
		var items []struct {
			Path   string `json:"path"`
			Series *struct {
				ID int `json:"id"`
			} `json:"series"`
			Movie *struct {
				ID      int  `json:"id"`
				HasFile bool `json:"hasFile"`
			} `json:"movie"`
			Episodes []struct {
				HasFile bool `json:"hasFile"`
			} `json:"episodes"`
			Rejections []struct {
				Reason string `json:"reason"`
			} `json:"rejections"`
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := c.GetJSONQuery(cctx, "/manualimport", map[string][]string{"folder": {path}, "filterExistingFiles": {"false"}}, &items)
		cancel()
		if err != nil {
			continue
		}
		videos := 0
		ok := true
		for _, it := range items {
			if !videoExt[strings.ToLower(filepath.Ext(it.Path))] {
				continue
			}
			videos++
			if len(it.Rejections) == 0 {
				return false // importable: a better copy, or something missing
			}
			switch {
			case it.Movie != nil && it.Movie.ID > 0:
				ok = ok && it.Movie.HasFile
			case it.Series != nil && it.Series.ID > 0 && len(it.Episodes) > 0:
				for _, e := range it.Episodes {
					ok = ok && e.HasFile
				}
			default:
				ok = false // unknown: leave it
			}
		}
		if videos > 0 {
			return ok
		}
	}
	return false
}

// deleteLeftovers removes what findLeftovers found, checking each again.
func (a *App) deleteLeftovers(ctx context.Context, list []leftover) (int, int64, error) {
	protected := a.busyDownloadPaths(ctx)
	n := 0
	var freed int64
	var firstErr error
	for _, l := range list {
		p := filepath.Clean(l.Path)
		rel, err := filepath.Rel(l.Root, p)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") || strings.ContainsAny(rel, `/\`) {
			continue // only folders directly inside a complete folder
		}
		if protected[strings.ToLower(p)] {
			continue
		}
		st, err := os.Lstat(p)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
		freed += l.Bytes
	}
	return n, freed, firstErr
}
