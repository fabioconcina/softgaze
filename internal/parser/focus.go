package parser

import (
	"sort"
	"time"
)

// Focus stretches: how long the user has stayed with one session. From the
// prompts alone, a stretch is a run of prompts to the same session with no
// prompt to another session in between and no pause (idle time, see
// idleGaps) over 15 minutes. The server narrows it further with what is in
// front on screen (internal/attention) and records it, so the switch list
// can show the minutes of the work period.

const focusPause = 15 * time.Minute

// FocusStretch is the user's current stretch on one session.
type FocusStretch struct {
	Since   time.Time `json:"since"`
	Prompts int       `json:"prompts"` // prompts to the session since Since

	// Set by the server from what is on screen (zero without it): time
	// away in short absences since Since, and the start of the current one.
	AwayMs    int64      `json:"away_ms"`
	AwaySince *time.Time `json:"away_since,omitempty"`
	Watched   bool       `json:"watched"` // the screen was watched

	// PromptTimes are the stretch's prompts, for counting them from a later
	// start.
	PromptTimes []time.Time `json:"-"`
}

// FocusFor returns the current stretch when it is on sessionID and still
// going: Claude is working on it (working), or the user's last move in it
// was at most 15 minutes ago. nil otherwise, and until the prompt timeline
// has had its first full read (started when Softgaze opens), so this
// never makes a request wait for it.
func FocusFor(claudeDir, sessionID string, working bool, now time.Time) *FocusStretch {
	if !workTimelines.ready.Load() {
		return nil
	}
	prompts, ends := workTimeline(claudeDir, now)
	return focusFrom(prompts, ends, sessionID, working, now)
}

func focusFrom(prompts, ends []stamp, sessionID string, working bool, now time.Time) *FocusStretch {
	n := len(prompts)
	if n == 0 || prompts[n-1].session != sessionID {
		return nil // the user's latest prompt went to another session
	}
	gaps := idleGaps(stampTimes(prompts), stampTimes(ends))
	first := n - 1
	for first > 0 && prompts[first-1].session == sessionID && gaps[first-1] <= focusPause {
		first--
	}
	lastMove := prompts[n-1].t
	if e, ok := turnEnd(ends, sessionID, lastMove); ok {
		lastMove = e
	}
	if !working && now.Sub(lastMove) > focusPause {
		return nil // the stretch is over
	}
	f := &FocusStretch{Since: prompts[first].t, Prompts: n - first}
	for _, p := range prompts[first:] {
		f.PromptTimes = append(f.PromptTimes, p.t)
	}
	return f
}

// turnEnd returns when the session's turn started at or after t went quiet.
func turnEnd(ends []stamp, session string, t time.Time) (time.Time, bool) {
	for i := sort.Search(len(ends), func(i int) bool { return !ends[i].t.Before(t) }); i < len(ends); i++ {
		if ends[i].session == session {
			return ends[i].t, true
		}
	}
	return time.Time{}, false
}
