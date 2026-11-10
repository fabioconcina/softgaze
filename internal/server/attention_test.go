package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fabioconcina/softgaze/internal/attention"
)

func TestAttentionReports(t *testing.T) {
	srv, _ := setupTestServer(t)
	post := func(path, body string) int {
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return w.Code
	}
	// An absolute folder on this platform ("/repo/a" is not one on Windows).
	folder, _ := json.Marshal(filepath.Join(t.TempDir(), "a"))
	for _, on := range []bool{false, true} {
		if on {
			srv.Handlers.Attention = attention.NewSampler(nil)
		}
		for body, want := range map[string]int{
			`{"window":"w1","focused":true,"folders":[` + string(folder) + `],"panel_active":false}`: http.StatusNoContent,
			`{"window":"w1","closed":true}`:               http.StatusNoContent,
			`{"window":"../x","focused":true}`:            http.StatusBadRequest,
			`{"window":"w1","folders":["relative/path"]}`: http.StatusBadRequest,
			`not json`: http.StatusBadRequest,
		} {
			if got := post("/api/attention/editor", body); got != want {
				t.Errorf("wellbeing %v: editor %s = %d, want %d", on, body, got, want)
			}
		}
		for body, want := range map[string]int{
			`{"page":"p1","focused":true}`: http.StatusNoContent,
			`{"page":"","focused":true}`:   http.StatusBadRequest,
		} {
			if got := post("/api/attention/page", body); got != want {
				t.Errorf("wellbeing %v: page %s = %d, want %d", on, body, got, want)
			}
		}
	}
	// A web page from another site cannot report.
	req := httptest.NewRequest("POST", "/api/attention/page", strings.NewReader(`{"page":"p1","focused":true}`))
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-origin report = %d, want 403", w.Code)
	}
}

// Ctrl+Alt+J where softgaze cannot raise windows: a Softgaze panel in VS
// Code is asked to come forward, when one is connected.
func TestFollowBackAsksTheVSCodePanel(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Broker.Run(ctx)
	hs := httptest.NewServer(srv.Mux)
	defer hs.Close()

	back := func() int {
		resp, err := http.Post(hs.URL+"/api/follow/back", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	// A browser page does not count: it cannot raise its window.
	browser, err := http.Get(hs.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Body.Close()
	if got := back(); got != http.StatusNotFound {
		t.Errorf("back with only a browser page: %d, want 404", got)
	}

	sctx, scancel := context.WithCancel(ctx)
	req, _ := http.NewRequestWithContext(sctx, "GET", hs.URL+"/api/events?embed=vscode", nil)
	panel, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; srv.Broker.Embedded() == 0 && i < 100; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if got := back(); got != http.StatusOK {
		t.Fatalf("back with a VS Code panel: %d, want 200", got)
	}
	got := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(panel.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "event: focus_back") {
				got <- sc.Text()
				return
			}
		}
	}()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the panel was not asked to come forward")
	}

	scancel()
	panel.Body.Close()
	for i := 0; srv.Broker.Embedded() != 0 && i < 100; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if got := back(); got != http.StatusNotFound {
		t.Errorf("back after the panel closed: %d, want 404", got)
	}
}
