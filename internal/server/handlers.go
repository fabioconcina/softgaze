package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/fabioconcina/softgaze/internal/attention"
	"github.com/fabioconcina/softgaze/internal/ontop"
	"github.com/fabioconcina/softgaze/internal/store"
)

type Handlers struct {
	claudeDir string
	store     *store.Store
	events    *SSEBroker // to ask a Softgaze panel in VS Code to come forward
	Version   string

	warmWork sync.Once // first read of the prompt timeline, when Softgaze opens

	// Wellbeing turns on the wellbeing extras: the minutes counter with
	// the break line, and today's minutes in the switch list.
	Wellbeing bool
	// Attention watches the window in front while Softgaze is open, for the
	// counter (nil: off, or stretches come from prompts alone).
	Attention  AttentionSource
	focusMu    sync.Mutex
	viewWinMu  sync.Mutex
	viewWin    ontop.Window         // the Softgaze window last seen in front
	viewApp    string               // and its executable, in case the handle is reused
	focusSaved map[string]time.Time // stretch -> when it was last recorded
}

// AttentionSource is what the window in front says about the user: an
// *attention.Sampler, or a stand-in in tests.
type AttentionSource interface {
	Supported() bool
	Want(cwd string, now time.Time)
	Attention(cwd string, from, now time.Time, maxAway time.Duration) attention.Span
	Hints() *attention.Hints
}

func NewHandlers(claudeDir string, st *store.Store) *Handlers {
	return &Handlers{claudeDir: claudeDir, store: st}
}

// HandleInfo reports the version.
func (h *Handlers) HandleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"version": h.Version})
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
