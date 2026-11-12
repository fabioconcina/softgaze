package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFollowAside(t *testing.T) {
	srv, _ := setupTestServer(t)
	srv.Handlers.warmWork.Do(func() {}) // no background read left behind
	claudeDir := srv.Handlers.claudeDir
	proj := filepath.Join(claudeDir, "projects", "p")
	os.MkdirAll(proj, 0o755)
	os.MkdirAll(filepath.Join(claudeDir, "sessions"), 0o755)
	now := time.Now()
	prompt := func(id string, minsAgo int) {
		line := `{"type":"user","timestamp":"` + now.Add(-time.Duration(minsAgo)*time.Minute).UTC().Format(time.RFC3339Nano) +
			`","cwd":"/repo","origin":{"kind":"human"},"message":{"role":"user","content":"task ` + id + `"}}` + "\n"
		fh, err := os.OpenFile(filepath.Join(proj, id+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		fh.WriteString(line)
		fh.Close()
	}
	// Two live processes stand in for two running sessions; b was prompted last.
	for id, pid := range map[string]int{"a": os.Getpid(), "b": os.Getppid()} {
		info, _ := json.Marshal(map[string]any{"pid": pid, "sessionId": id, "cwd": "/repo", "status": "busy"})
		os.WriteFile(filepath.Join(claudeDir, "sessions", strconv.Itoa(pid)+".json"), info, 0o644)
	}
	prompt("a", 10)
	prompt("b", 5)

	post := func(body string) int {
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/follow/aside", strings.NewReader(body)))
		return w.Code
	}
	followed := func() string {
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/follow", nil))
		var resp struct {
			Session *struct {
				ID string `json:"session_id"`
			} `json:"session"`
		}
		if json.Unmarshal(w.Body.Bytes(), &resp) != nil || resp.Session == nil {
			return ""
		}
		return resp.Session.ID
	}
	aside := func() map[string]bool {
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/follow/sessions", nil))
		var resp struct {
			Sessions []struct {
				ID    string `json:"session_id"`
				Aside bool   `json:"aside"`
			} `json:"sessions"`
		}
		json.Unmarshal(w.Body.Bytes(), &resp)
		out := map[string]bool{}
		for _, s := range resp.Sessions {
			out[s.ID] = s.Aside
		}
		return out
	}

	for body, want := range map[string]int{`{}`: 400, `not json`: 400, `{"session":"..\\x","aside":true}`: 400, `{"session":"nope","aside":true}`: 404} {
		if code := post(body); code != want {
			t.Errorf("aside %s: %d, want %d", body, code, want)
		}
	}
	if got := followed(); got != "b" {
		t.Fatalf("followed = %q, want b (prompted last)", got)
	}

	// Set aside: out of the list, and never the window's own pick.
	if code := post(`{"session":"b","aside":true}`); code != http.StatusOK {
		t.Fatalf("setting b aside: %d", code)
	}
	if got := aside(); !got["b"] || got["a"] {
		t.Errorf("aside = %v, want b only", got)
	}
	if got := followed(); got != "a" {
		t.Errorf("followed with b aside = %q, want a", got)
	}

	// A new prompt brings it back by itself.
	prompt("b", 0)
	if got := aside(); got["b"] {
		t.Error("b is still aside after a new prompt")
	}
	if got := followed(); got != "b" {
		t.Errorf("followed after b is prompted again = %q, want b", got)
	}
	if _, ok := srv.Handlers.aside["b"]; ok {
		t.Error("b's mark should be gone once it is back")
	}

	// Brought back by hand.
	post(`{"session":"a","aside":true}`)
	if !aside()["a"] {
		t.Fatal("a is not aside")
	}
	post(`{"session":"a","aside":false}`)
	if aside()["a"] {
		t.Error("a is still aside after being brought back")
	}
}
