package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFollowName(t *testing.T) {
	srv, _ := setupTestServer(t)
	srv.Handlers.warmWork.Do(func() {})
	proj := filepath.Join(srv.Handlers.claudeDir, "projects", "p")
	os.MkdirAll(proj, 0o755)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(`{"type":"user","timestamp":"`+ts+`","cwd":"/work/app","origin":{"kind":"human"},"message":{"role":"user","content":"ship it"}}`+"\n"), 0o644)

	post := func(body string) int {
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/follow/name", strings.NewReader(body)))
		return w.Code
	}
	name := func() string {
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/follow?session=s1", nil))
		var resp struct {
			Session struct {
				Name    string `json:"name"`
				Project string `json:"project"`
			} `json:"session"`
		}
		json.Unmarshal(w.Body.Bytes(), &resp)
		return resp.Session.Name
	}
	for _, bad := range []string{`{}`, `not json`, `{"session":"..\x","name":"x"}`} {
		if code := post(bad); code != http.StatusBadRequest {
			t.Errorf("name %s: %d, want 400", bad, code)
		}
	}
	if code := post(`{"session":"s1","name":" Release\nnotes "}`); code != http.StatusOK {
		t.Fatalf("naming: %d", code)
	}
	if got := name(); got != "Release notes" {
		t.Errorf("name = %q, want %q", got, "Release notes")
	}
	post(`{"session":"s1","name":"` + strings.Repeat("x", 100) + `"}`)
	if got := name(); len(got) != nameMax {
		t.Errorf("a long name kept %d characters, want %d", len(got), nameMax)
	}
	post(`{"session":"s1","name":""}`)
	if got := name(); got != "" {
		t.Errorf("name after clearing = %q, want none", got)
	}
}
