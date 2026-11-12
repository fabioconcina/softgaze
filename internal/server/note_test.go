package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fabioconcina/softgaze/internal/parser"
)

func TestCleanNote(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"  reply to Anna  ", "reply to Anna"},
		{"check the\n401 path\tfirst", "check the 401 path first"},
		{"a\x00b\x1bc", "a b c"},
		{"   ", ""},
		{strings.Repeat("é", noteMax+10), strings.Repeat("é", noteMax)},
	} {
		if got := cleanNote(c.in); got != c.want {
			t.Errorf("cleanNote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFollowNote(t *testing.T) {
	srv, _ := setupTestServer(t)
	post := func(body string) int {
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/follow/note", strings.NewReader(body)))
		return w.Code
	}
	for _, bad := range []string{`{}`, `not json`, `{"session":"..\\x","text":"x"}`, `{"text":"x"}`} {
		if code := post(bad); code != http.StatusBadRequest {
			t.Errorf("note %s: %d, want 400", bad, code)
		}
	}

	turn := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	f := &parser.Follow{SessionID: "s1", TurnStart: &turn}
	if code := post(`{"session":"s1","text":" reply\nto Anna "}`); code != http.StatusOK {
		t.Fatalf("saving a note: %d", code)
	}
	if got := srv.Handlers.noteFor(f); got != "reply to Anna" {
		t.Errorf("note = %q, want %q", got, "reply to Anna")
	}
	// It stays with the session at the next prompt, until it is done.
	next := turn.Add(time.Hour)
	if got := srv.Handlers.noteFor(&parser.Follow{SessionID: "s1", TurnStart: &next}); got != "reply to Anna" {
		t.Errorf("note at the next turn = %q, want it kept", got)
	}
	if got := srv.Handlers.noteFor(&parser.Follow{SessionID: "s2", TurnStart: &turn}); got != "" {
		t.Errorf("another session's note = %q, want none", got)
	}
	// Done: an empty text drops it.
	post(`{"session":"s1","text":""}`)
	if got := srv.Handlers.noteFor(f); got != "" {
		t.Errorf("note after done = %q, want none", got)
	}
}

func TestFollowNoteKeepsAtMostNotesKept(t *testing.T) {
	srv, _ := setupTestServer(t)
	for i := 0; i <= notesKept; i++ {
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/follow/note", strings.NewReader(`{"session":"s`+strconv.Itoa(i)+`","text":"n"}`)))
		if w.Code != http.StatusOK {
			t.Fatalf("note %d: %d", i, w.Code)
		}
	}
	if n := len(srv.Handlers.notes); n != notesKept {
		t.Errorf("%d notes kept, want %d", n, notesKept)
	}
	if _, ok := srv.Handlers.notes["s0"]; ok {
		t.Error("the note written longest ago should have gone")
	}
}
