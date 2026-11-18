package app

import (
	"testing"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/sab"
)

func TestSABWatchSpotsAStuckUnpack(t *testing.T) {
	var w sabWatch
	t0 := time.Date(2026, 11, 18, 1, 38, 0, 0, time.UTC)
	snap := func(action string) *sab.Snapshot {
		return &sab.Snapshot{Waiting: 82, Working: &sab.Working{Name: "Defiance.S01E05", Status: "Extracting", Action: action}}
	}
	if w.see(snap("Unpacking: 52/101"), t0) != nil {
		t.Fatal("not stuck yet")
	}
	// Moving along resets the clock.
	if w.see(snap("Unpacking: 53/101"), t0.Add(15*time.Minute)) != nil {
		t.Fatal("progress means not stuck")
	}
	if w.see(snap("Unpacking: 53/101"), t0.Add(30*time.Minute)) != nil {
		t.Fatal("only 15 minutes without progress")
	}
	st := w.see(snap("Unpacking: 53/101"), t0.Add(36*time.Minute))
	if st == nil || st.Waiting != 82 || st.Action != "Unpacking: 53/101" {
		t.Fatalf("should be stuck now: %+v", st)
	}
	// Told once, not every poll.
	if w.see(snap("Unpacking: 53/101"), t0.Add(40*time.Minute)) != nil {
		t.Fatal("should only alert once")
	}
	if w.current(snap("Unpacking: 53/101"), t0.Add(40*time.Minute)) == nil {
		t.Fatal("Health should still show it")
	}
	// Nothing being unpacked: all clear.
	if w.see(&sab.Snapshot{}, t0.Add(41*time.Minute)) != nil || w.current(&sab.Snapshot{}, t0.Add(41*time.Minute)) != nil {
		t.Fatal("idle SABnzbd isn't stuck")
	}
}
