package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabioconcina/softgaze/internal/store"
)

func setupTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	os.MkdirAll(filepath.Join(claudeDir, "projects"), 0755)
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(claudeDir, st, os.DirFS(dir))
	// Tests send requests to 127.0.0.1, the only kind of host allowed.
	inner := srv.Mux
	srv.Mux = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "example.com" {
			r.Host = "127.0.0.1:3133"
		}
		inner.ServeHTTP(w, r)
	})
	return srv, st
}

func TestRejectsDNSRebindingHosts(t *testing.T) {
	srv, _ := setupTestServer(t)
	for _, host := range []string{"attacker.example:3133", "attacker.example.:3133", "localhost.attacker.example", ""} {
		for _, path := range []string{"/api/follow", "/follow.html", "/api/events"} {
			req := httptest.NewRequest("GET", "http://127.0.0.1:3133"+path, nil)
			req.Host = host
			w := httptest.NewRecorder()
			srv.Mux.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Errorf("host %q %s: status = %d, want 403", host, path, w.Code)
			}
		}
	}
}

func TestMutationRejectsCrossOriginBrowserRequest(t *testing.T) {
	srv, _ := setupTestServer(t)
	req := httptest.NewRequest("POST", "http://127.0.0.1:3133/api/follow/back", strings.NewReader(""))
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST: status = %d, want 403", w.Code)
	}
}

func TestRootGoesToFocus(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:3133/", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/follow.html" {
		t.Errorf("/: %d %q, want a redirect to /follow.html", w.Code, w.Header().Get("Location"))
	}
}

// Forwarded headers are not trusted: there is no proxy in front of this
// server, so they cannot make another site's page same-origin.
func TestMutationOrigin(t *testing.T) {
	srv, _ := setupTestServer(t)
	tests := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"same origin", map[string]string{"Origin": "http://127.0.0.1:3133"}, http.StatusNotFound},
		{"no origin (a CLI client)", nil, http.StatusNotFound},
		{"https origin", map[string]string{"Origin": "https://127.0.0.1:3133"}, http.StatusForbidden},
		{"another port", map[string]string{"Origin": "http://127.0.0.1:8080"}, http.StatusForbidden},
		{"cross-site without origin", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"forwarded to look like the other site", map[string]string{
			"Origin": "https://evil.example", "X-Forwarded-Host": "evil.example", "X-Forwarded-Proto": "https"}, http.StatusForbidden},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("POST", "http://127.0.0.1:3133/api/follow/back", strings.NewReader(""))
		for k, v := range tt.headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, req)
		// 404: let through to the handler, which has no Softgaze window to go back to.
		if w.Code != tt.want {
			t.Errorf("%s: status = %d, want %d", tt.name, w.Code, tt.want)
		}
	}
}
