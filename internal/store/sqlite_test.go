package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestPruneDropsOldStretches(t *testing.T) {
	st := openTestStore(t)
	now := time.Now()
	if err := st.SaveFocusStretch("old", now.AddDate(0, 0, -40), 30*time.Minute, 3); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveFocusStretch("new", now.Add(-time.Hour), 20*time.Minute, 2); err != nil {
		t.Fatal(err)
	}
	if err := st.Prune(30); err != nil {
		t.Fatal(err)
	}
	if d, err := st.FocusSince(now.AddDate(0, 0, -60)); err != nil || d != 20*time.Minute {
		t.Errorf("after prune: %v %v, want only the new stretch (20m)", d, err)
	}
}
