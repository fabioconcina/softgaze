package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fabioconcina/softgaze/internal/attention"
	"github.com/fabioconcina/softgaze/internal/parser"
	"github.com/fabioconcina/softgaze/internal/store"
)

// fakeAttention stands in for the sampler: what was in front is given.
type fakeAttention struct {
	supported bool
	span      attention.Span
	wanted    []string
}

func (a *fakeAttention) Supported() bool              { return a.supported }
func (a *fakeAttention) Want(cwd string, _ time.Time) { a.wanted = append(a.wanted, cwd) }
func (a *fakeAttention) Hints() *attention.Hints      { return nil }
func (a *fakeAttention) Attention(string, time.Time, time.Time, time.Duration) attention.Span {
	return a.span
}

// focusDir writes a transcript for session s1: a prompt 10 minutes ago that
// Claude answered a minute later, then another 5 minutes ago it is still
// working on. One stretch, since 10 minutes ago, with two prompts.
func focusDir(t *testing.T, now time.Time) string {
	t.Helper()
	claudeDir := t.TempDir()
	dir := filepath.Join(claudeDir, "projects", "-repo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	ago := func(m int) string { return now.Add(-time.Duration(m) * time.Minute).Format(time.RFC3339Nano) }
	lines := []map[string]interface{}{
		{"type": "user", "timestamp": ago(10), "cwd": "/repo", "origin": map[string]string{"kind": "human"},
			"message": map[string]interface{}{"role": "user", "content": "fix the flaky test"}},
		{"type": "assistant", "timestamp": ago(9), "message": map[string]interface{}{"id": "m1", "role": "assistant",
			"stop_reason": "end_turn", "content": []map[string]string{{"type": "text", "text": "Fixed."}}}},
		{"type": "user", "timestamp": ago(5), "cwd": "/repo", "origin": map[string]string{"kind": "human"},
			"message": map[string]interface{}{"role": "user", "content": "now run the whole suite"}},
	}
	var b strings.Builder
	for _, l := range lines {
		data, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	parser.CurrentWorkPeriod(claudeDir, now) // the timeline's first full read
	return claudeDir
}

func TestFocusFor(t *testing.T) {
	now := time.Now()
	claudeDir := focusDir(t, now)
	ago := func(m int) time.Time { return now.Add(-time.Duration(m) * time.Minute) }
	tests := []struct {
		name        string
		att         AttentionSource
		wantNil     bool
		since       time.Time
		prompts     int
		awayMs      int64
		awaySince   time.Time
		savedMinute int // minutes recorded for the stretch
	}{
		{"prompts only (no sampler)", nil, false, ago(10), 2, 0, time.Time{}, 10},
		{"platform cannot tell", &fakeAttention{}, false, ago(10), 2, 0, time.Time{}, 10},
		{"with it all along", &fakeAttention{supported: true, span: attention.Span{Watched: true}}, false, ago(10), 2, 0, time.Time{}, 10},
		{"back after a long absence", &fakeAttention{supported: true, span: attention.Span{Watched: true, Resumed: ago(7)}},
			false, ago(7), 1, 0, time.Time{}, 7},
		{"away for a minute now, two before", &fakeAttention{supported: true,
			span: attention.Span{Watched: true, Away: 2 * time.Minute, AwaySince: ago(1)}},
			false, ago(10), 2, (2 * time.Minute).Milliseconds(), ago(1), 7},
		{"gone too long: the stretch ended when the user left", &fakeAttention{supported: true,
			span: attention.Span{Watched: true, Away: time.Minute, AwaySince: ago(5)}},
			true, time.Time{}, 0, 0, time.Time{}, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "focus.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			h := NewHandlers(claudeDir, st)
			h.Attention = tt.att
			fo := h.focusFor(&parser.Follow{SessionID: "s1", CWD: "/repo", State: parser.FollowWorking, Running: true}, now)
			if tt.wantNil {
				if fo != nil {
					t.Fatalf("stretch = %+v, want none", fo)
				}
			} else {
				if fo == nil {
					t.Fatal("no stretch")
				}
				if !fo.Since.Equal(tt.since) || fo.Prompts != tt.prompts || fo.AwayMs != tt.awayMs {
					t.Errorf("stretch since %v with %d prompts, away %d ms; want since %v with %d, away %d ms",
						fo.Since, fo.Prompts, fo.AwayMs, tt.since, tt.prompts, tt.awayMs)
				}
				if got := fo.AwaySince; (got == nil) != tt.awaySince.IsZero() || (got != nil && !got.Equal(tt.awaySince)) {
					t.Errorf("away since %v, want %v", got, tt.awaySince)
				}
			}
			if fa, ok := tt.att.(*fakeAttention); ok && fa.supported && (len(fa.wanted) != 1 || fa.wanted[0] != "/repo") {
				t.Errorf("sampler asked to watch %v, want [/repo]", fa.wanted)
			}
			got, err := st.FocusSince(now.Add(-time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if want := time.Duration(tt.savedMinute) * time.Minute; got != want {
				t.Errorf("recorded %v, want %v", got, want)
			}
		})
	}
}

// A stretch is written at most every 20 seconds; another stretch is not
// held back by it.
func TestSaveFocusThrottles(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "focus.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h := NewHandlers(t.TempDir(), st)
	since := time.Now().Add(-10 * time.Minute)
	h.saveFocus("s1", &parser.FocusStretch{Since: since}, 5*time.Minute)
	h.saveFocus("s1", &parser.FocusStretch{Since: since}, 6*time.Minute) // too soon: skipped
	h.saveFocus("s1", &parser.FocusStretch{Since: since.Add(time.Minute)}, 2*time.Minute)
	h.saveFocus("s1", &parser.FocusStretch{Since: since}, 0) // nothing to record
	got, err := st.FocusSince(since.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if want := 7 * time.Minute; got != want {
		t.Errorf("recorded %v, want %v (5 min, the 6 held back, plus the other stretch's 2)", got, want)
	}
}
