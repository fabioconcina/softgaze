package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHandleFollow(t *testing.T) {
	srv, _ := setupTestServer(t)
	// No background timeline read: it could still hold a file open when the
	// temp dir is removed (Windows).
	srv.Handlers.warmWork.Do(func() {})

	get := func(path string) (int, string) {
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w.Code, strings.TrimSpace(w.Body.String())
	}

	if code, body := get("/api/follow"); code != http.StatusOK || body != `{"session":null}` {
		t.Errorf("nothing running: %d %s, want 200 with no session", code, body)
	}
	if code, _ := get("/api/follow?session=..%5Cx"); code != http.StatusBadRequest {
		t.Errorf("bad session id: %d, want 400", code)
	}
	if code, body := get("/api/follow/pin"); code != http.StatusOK || !strings.HasPrefix(body, `{"supported":`) {
		t.Errorf("pin info: %d %s", code, body)
	}
	for _, bad := range []string{`{}`, `{"token":"x"}`, `{"token":"sg-pin-../../"}`, `not json`} {
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/follow/pin", strings.NewReader(bad)))
		if w.Code != http.StatusBadRequest {
			t.Errorf("pin %s: %d, want 400", bad, w.Code)
		}
	}
	for body, want := range map[string]int{`{"session":"..\\x"}`: http.StatusBadRequest, `{"session":"nope"}`: http.StatusNotFound} {
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/follow/focus", strings.NewReader(body)))
		if w.Code != want {
			t.Errorf("focus %s: %d, want %d", body, w.Code, want)
		}
	}
	// No Softgaze window seen yet: the extension shows its own.
	back := httptest.NewRecorder()
	srv.Mux.ServeHTTP(back, httptest.NewRequest("POST", "/api/follow/back", nil))
	if back.Code != http.StatusNotFound {
		t.Errorf("back with no Softgaze window: %d, want 404", back.Code)
	}
	if code, body := get("/api/follow/sessions"); code != http.StatusOK ||
		body != `{"sessions":[],"current":[],"since":null,"break_minutes":240,"learned":false}` {
		t.Errorf("switch list with nothing running: %d %s, want 200 with empty lists, the default break and no minutes (wellbeing off)", code, body)
	}
	srv.Handlers.Wellbeing = true
	if _, body := get("/api/follow/sessions"); !strings.HasSuffix(body, `"learned":false,"focused_minutes":0}`) || strings.Contains(body, "turns") {
		t.Errorf("switch list with wellbeing on: %s, want the minutes and no turn count", body)
	}
	srv.Handlers.Wellbeing = false

	// A closed session can still be asked for by id.
	proj := filepath.Join(srv.Handlers.claudeDir, "projects", "p")
	os.MkdirAll(proj, 0o755)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	transcript := `{"type":"user","timestamp":"` + ts + `","cwd":"/work/app","origin":{"kind":"human"},"message":{"role":"user","content":"ship it"}}` + "\n" +
		`{"type":"assistant","timestamp":"` + ts + `","message":{"id":"m","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"Shipped."}]}}` + "\n"
	os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(transcript), 0o644)

	code, body := get("/api/follow?session=s1")
	var resp struct {
		Session *struct {
			State    string `json:"state"`
			Prompt   string `json:"prompt"`
			Headline string `json:"headline"`
			Project  string `json:"project"`
		} `json:"session"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &resp) != nil || resp.Session == nil {
		t.Fatalf("GET /api/follow?session=s1 = %d %s", code, body)
	}
	if s := resp.Session; s.State != "ended" || s.Prompt != "ship it" || s.Headline != "Shipped." || s.Project != "app" {
		t.Errorf("session = %+v", *s)
	}
}

func TestSessionURL(t *testing.T) {
	for _, c := range []struct{ editor, want string }{
		{"Code.exe", "vscode://anthropic.claude-code/open?session=4028d6e8-07cb"},
		{"Code - Insiders.exe", "vscode-insiders://anthropic.claude-code/open?session=4028d6e8-07cb"},
		{"", "vscode://anthropic.claude-code/open?session=4028d6e8-07cb"},
	} {
		if got := sessionURL(c.editor, "4028d6e8-07cb"); got != c.want {
			t.Errorf("sessionURL(%q) = %q, want %q", c.editor, got, c.want)
		}
	}
}

func TestEditorEnv(t *testing.T) {
	got := editorEnv([]string{"PATH=x", "ELECTRON_RUN_AS_NODE=1", "VSCODE_PID=4", "Vscode_ipc_hook=y", "HOME=h"})
	if want := []string{"PATH=x", "HOME=h"}; !reflect.DeepEqual(got, want) {
		t.Errorf("editorEnv = %q, want %q", got, want)
	}
}
