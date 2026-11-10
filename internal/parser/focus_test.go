package parser

import (
	"sort"
	"testing"
	"time"
)

// focusTimeline builds a timeline from prompts; each turn goes quiet a
// minute after its prompt.
func focusTimeline(prompts ...stamp) ([]stamp, []stamp) {
	var ends []stamp
	for _, p := range prompts {
		ends = append(ends, stamp{p.t.Add(time.Minute), p.session})
	}
	sort.Slice(ends, func(i, j int) bool { return ends[i].t.Before(ends[j].t) })
	return prompts, ends
}

func st(min int, session string) stamp {
	return stamp{t0.Add(time.Duration(min) * time.Minute), session}
}

func TestFocusFrom(t *testing.T) {
	// A, a detour to B, back to A.
	prompts, ends := focusTimeline(st(0, "A"), st(5, "A"), st(10, "A"), st(30, "B"), st(40, "A"), st(45, "A"))
	now := t0.Add(50 * time.Minute)

	f := focusFrom(prompts, ends, "A", false, now)
	if f == nil || !f.Since.Equal(t0.Add(40*time.Minute)) || f.Prompts != 2 || len(f.PromptTimes) != 2 {
		t.Fatalf("focus = %+v, want the stretch since the return to A at 40", f)
	}
	if f := focusFrom(prompts, ends, "B", false, now); f != nil {
		t.Errorf("following B while working on A: %+v, want none", f)
	}
	// Last move at 46: over 15 minutes later the stretch is over, unless
	// Claude is still working.
	late := t0.Add(62 * time.Minute)
	if f := focusFrom(prompts, ends, "A", false, late); f != nil {
		t.Errorf("after a 16-minute pause: %+v, want none", f)
	}
	if f := focusFrom(prompts, ends, "A", true, late); f == nil {
		t.Error("Claude still working: the stretch should go on")
	}
	// A pause over 15 minutes in the same session starts a new stretch.
	prompts, ends = focusTimeline(st(0, "A"), st(5, "A"), st(30, "A"))
	if f := focusFrom(prompts, ends, "A", true, t0.Add(31*time.Minute)); f == nil || !f.Since.Equal(t0.Add(30*time.Minute)) {
		t.Errorf("after a pause: %+v, want a stretch since 30", f)
	}
}
