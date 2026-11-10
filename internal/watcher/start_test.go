package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The watcher sees transcripts written in a known project, in a project
// created while it runs, and PID files coming and going.
func TestStartDispatches(t *testing.T) {
	claudeDir := t.TempDir()
	projects := filepath.Join(claudeDir, "projects")
	sessions := filepath.Join(claudeDir, "sessions")
	for _, d := range []string{filepath.Join(projects, "p1"), sessions} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	w, err := New(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	sessionCh, processCh := make(chan string, 64), make(chan string, 64)
	w.OnSessionChange(func(p string) { sessionCh <- p })
	w.OnProcessChange(func(p string) { processCh <- p })
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- w.Start(ctx) }()

	expect := func(ch chan string, want, what string) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case got := <-ch:
				if got == want {
					return
				}
			case <-deadline:
				t.Fatalf("no %s event for %s", what, want)
			}
		}
	}
	write := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	s1 := filepath.Join(projects, "p1", "s1.jsonl")
	write(s1)
	expect(sessionCh, s1, "transcript")

	// A project folder that appears later: watched from then on, and a
	// transcript written before the watch was in place is not missed.
	if err := os.Mkdir(filepath.Join(projects, "p2"), 0755); err != nil {
		t.Fatal(err)
	}
	s2 := filepath.Join(projects, "p2", "s2.jsonl")
	write(s2)
	expect(sessionCh, s2, "transcript in a new project")

	pid := filepath.Join(sessions, "4242.json")
	write(pid)
	expect(processCh, pid, "PID file written")
	if err := os.Remove(pid); err != nil {
		t.Fatal(err)
	}
	expect(processCh, pid, "PID file removed")

	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after the context ended")
	}
}
