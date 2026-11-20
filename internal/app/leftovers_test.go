package app

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/sab"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

func TestLeftovers(t *testing.T) {
	base := t.TempDir()
	tv := filepath.Join(base, "tv")
	old := time.Now().Add(-48 * time.Hour)
	mk := func(name string, files map[string]int64, age time.Time) string {
		dir := filepath.Join(tv, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f, size := range files {
			fp := filepath.Join(dir, f)
			fh, err := os.Create(fp)
			if err != nil {
				t.Fatal(err)
			}
			_ = fh.Truncate(size) // sparse: big without using disk
			fh.Close()
		}
		_ = os.Chtimes(dir, age, age)
		return dir
	}
	big := int64(200 << 20)
	unpack := mk("_UNPACK_Show.S01E01", map[string]int64{"a.rar": 1 << 20}, old)
	failed := mk("_FAILED_Show.S01E02", nil, old)
	novideo := mk("Show.S01E03", map[string]int64{"show.nfo": 2000, "sample.mkv": 80 << 20}, old)
	inlib := mk("Show.S01E04", map[string]int64{"Show.S01E04.mkv": big}, old)
	upgrade := mk("Show.S01E05", map[string]int64{"Show.S01E05.mkv": big}, old)
	unknown := mk("Mystery.S01E01", map[string]int64{"Mystery.S01E01.mkv": big}, old)
	busy := mk("Show.S01E06", map[string]int64{"Show.S01E06.mkv": big}, old)
	fresh := mk("_UNPACK_Show.S01E07", nil, time.Now())

	sonarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/queue":
			_ = json.NewEncoder(w).Encode(map[string]any{"records": []any{map[string]any{"outputPath": filepath.Join(busy, "Show.S01E06.mkv")}}, "totalRecords": 1})
		case "/api/v3/manualimport":
			folder := r.URL.Query().Get("folder")
			name := filepath.Base(folder)
			item := map[string]any{"path": filepath.Join(folder, name+".mkv")}
			switch name {
			case "Show.S01E04": // already in the library
				item["series"] = map[string]any{"id": 1}
				item["episodes"] = []any{map[string]any{"hasFile": true}}
				item["rejections"] = []any{map[string]any{"reason": "Not an upgrade for existing episode file(s)"}}
			case "Show.S01E05": // a better copy waiting to be imported
				item["series"] = map[string]any{"id": 1}
				item["episodes"] = []any{map[string]any{"hasFile": true}}
			case "Mystery.S01E01":
				item["rejections"] = []any{map[string]any{"reason": "Unknown Series"}}
			}
			_ = json.NewEncoder(w).Encode([]any{item})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sonarr.Close()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Update(func(d *store.Data) error {
		d.Settings.Sonarr = store.ArrSettings{Enabled: true, URL: sonarr.URL, APIKey: "k"}
		return nil
	})
	a := New(st, log.New(io.Discard, "", 0))
	cfg := &sab.Config{Misc: map[string]any{"complete_dir": base}, Categories: []map[string]any{{"name": "tv", "dir": "tv"}, {"name": "*", "dir": ""}}}
	ctx := context.Background()
	got := a.findLeftovers(ctx, cfg)
	found := map[string]string{}
	for _, l := range got {
		found[filepath.Base(l.Path)] = l.Reason
	}
	want := map[string]string{filepath.Base(unpack): "half-unpacked leftover", filepath.Base(failed): "failed download",
		filepath.Base(novideo): "no video in it", filepath.Base(inlib): "already in your library"}
	for name, reason := range want {
		if found[name] != reason {
			t.Fatalf("%s: got %q, want %q (all: %v)", name, found[name], reason, found)
		}
	}
	for _, keep := range []string{upgrade, unknown, busy, fresh, tv} {
		if _, ok := found[filepath.Base(keep)]; ok {
			t.Fatalf("%s must be left alone (all: %v)", filepath.Base(keep), found)
		}
	}
	// Something outside the complete folder is never touched, even if asked.
	outside := t.TempDir()
	got = append(got, leftover{Path: outside, Root: base})
	n, _, err := a.deleteLeftovers(ctx, got)
	if err != nil || n != 4 {
		t.Fatalf("deleted %d, err %v", n, err)
	}
	for _, gone := range []string{unpack, failed, novideo, inlib} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s should be gone", gone)
		}
	}
	for _, kept := range []string{upgrade, unknown, busy, fresh, outside} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("%s should still be there", kept)
		}
	}
}
