package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/fabioconcina/softgaze/internal/parser"
)

const nameMax = 60 // characters kept of a session's name

// cleanName is a name as kept: one line, trimmed, at most nameMax
// characters.
func cleanName(s string) string {
	s = cleanNote(s)
	if r := []rune(s); len(r) > nameMax {
		s = strings.TrimSpace(string(r[:nameMax]))
	}
	return s
}

// nameSessions sets the name the user gave each session (if any).
func (h *Handlers) nameSessions(fs ...*parser.Follow) {
	if h.store == nil {
		return
	}
	names, err := h.store.SessionNames()
	if err != nil {
		log.Printf("[name] %v", err)
		return
	}
	for _, f := range fs {
		if f != nil {
			f.Name = names[f.SessionID]
		}
	}
}

// HandleFollowName names a session, shown in place of its project. Body:
// {"session": "<id>", "name": "..."}; an empty name drops it. Every open
// window is told to refresh.
func (h *Handlers) HandleFollowName(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string `json:"session"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || !sessionIDRe.MatchString(body.Session) {
		writeJSONError(w, "invalid name", http.StatusBadRequest)
		return
	}
	if h.store == nil {
		writeJSONError(w, "no database", http.StatusServiceUnavailable)
		return
	}
	name := cleanName(body.Name)
	if err := h.store.SetSessionName(body.Session, name); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if h.events != nil {
		h.events.SendJSON("session_activity", map[string]string{"reason": "name"})
	}
	writeJSON(w, map[string]string{"name": name})
}
