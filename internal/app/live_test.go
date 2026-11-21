package app

import (
	"io"
	"log"
	"testing"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/arr"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
)

func TestDownloadsBusy(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(st, log.New(io.Discard, "", 0))
	if a.downloadsBusy() {
		t.Fatal("nothing going on yet")
	}
	a.mu.Lock()
	a.downloads = map[string]map[int][]arr.QueueItem{"radarr": {7: {{SizeLeft: 1e9}}}}
	a.mu.Unlock()
	if !a.downloadsBusy() {
		t.Fatal("a running download should keep it checking quickly")
	}
	a.mu.Lock()
	a.downloads = nil
	a.mu.Unlock()
	a.wakeDownloads()
	if !a.downloadsBusy() {
		t.Fatal("just after a request it should check quickly")
	}
	a.mu.Lock()
	a.wokeAt = time.Now().Add(-5 * time.Minute)
	a.mu.Unlock()
	if a.downloadsBusy() {
		t.Fatal("it should slow down again")
	}
}
