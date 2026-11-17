package store

import (
	"path/filepath"
	"testing"
)

func TestSessionNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSessionName("s1", "Abdul handoff"); err != nil {
		t.Fatal(err)
	}
	st.SetSessionName("s2", "first")
	st.SetSessionName("s2", "renamed")
	st.SetSessionName("s3", "gone")
	st.SetSessionName("s3", "")
	st.Close()

	// Kept across a restart.
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.SessionNames()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"s1": "Abdul handoff", "s2": "renamed"}
	if len(got) != len(want) || got["s1"] != want["s1"] || got["s2"] != want["s2"] {
		t.Errorf("names = %v, want %v", got, want)
	}
	if err := st.Prune(30); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.SessionNames(); len(got) != 2 {
		t.Errorf("a fresh name was pruned: %v", got)
	}
}
