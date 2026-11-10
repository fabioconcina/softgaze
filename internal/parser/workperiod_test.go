package parser

import (
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pauses draws n log-normal durations around median (spread is the x/÷
// factor of one standard deviation), from a fixed seed.
func pauses(rng *rand.Rand, n int, median time.Duration, spread float64) []time.Duration {
	out := make([]time.Duration, n)
	for i := range out {
		out[i] = time.Duration(float64(median) * math.Exp(rng.NormFloat64()*math.Log(spread)))
	}
	return out
}

func TestFitBreak(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	// Working gaps (ignored), breaks during the day, nights.
	gaps := append(pauses(rng, 400, 3*time.Minute, 2.5), pauses(rng, 60, 45*time.Minute, 1.7)...)
	gaps = append(gaps, pauses(rng, 20, 16*time.Hour, 1.4)...)
	thr, ok := fitBreak(gaps)
	if !ok || thr < 2*time.Hour || thr > 8*time.Hour {
		t.Errorf("fitBreak = %v %v, want a cut between the day's breaks and the nights", thr, ok)
	}

	if _, ok := fitBreak(pauses(rng, 5, time.Hour, 2)); ok {
		t.Error("too few pauses should not be fitted")
	}
	if thr, ok := fitBreak(pauses(rng, 80, 40*time.Minute, 1.3)); ok {
		t.Errorf("one group of pauses fitted a cut at %v", thr)
	}
}

func TestIdleGaps(t *testing.T) {
	m := func(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }
	// The turn started at 0 ends at 8: the pause before 10 is 2 minutes, not
	// 10. The turn end at 25 belongs to the prompt at 20 (still running when
	// the next prompt comes, at 22).
	got := idleGaps([]time.Time{m(0), m(10), m(20), m(22)}, []time.Time{m(8), m(25)})
	want := []time.Duration{2 * time.Minute, 10 * time.Minute, 2 * time.Minute}
	if len(got) != len(want) {
		t.Fatalf("idleGaps = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("gap %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// workdays builds a timeline of n working days: prompts every 4 minutes from
// 09:00 to 12:00 and 13:00 to 18:00 (a 1-hour lunch), each turn going quiet
// a minute after its prompt. The last day stops at 11:00.
func workdays(n int) (prompts, ends []time.Time) {
	day0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for d := 0; d < n; d++ {
		start := day0.AddDate(0, 0, d)
		for _, span := range [][2]int{{0, 180}, {240, 540}} {
			for m := span[0]; m < span[1]; m += 4 {
				if d == n-1 && m >= 120 {
					return prompts, ends
				}
				p := start.Add(time.Duration(m) * time.Minute)
				prompts = append(prompts, p)
				ends = append(ends, p.Add(time.Minute))
			}
		}
	}
	return prompts, ends
}

func TestWorkPeriodFrom(t *testing.T) {
	prompts, ends := workdays(15)
	wp := workPeriodFrom(prompts, ends)
	today := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	if !wp.Learned || wp.Break < 2*time.Hour || wp.Break > 12*time.Hour {
		t.Errorf("break = %v learned=%v, want one learned between lunch and the night", wp.Break, wp.Learned)
	}
	if !wp.Since.Equal(today) {
		t.Errorf("since = %v, want this morning's first prompt %v (lunch does not end the day)", wp.Since, today)
	}

	// Too little history: the default break, still split at the night.
	prompts, ends = workdays(2)
	wp = workPeriodFrom(prompts, ends)
	if wp.Learned || wp.Break != workDefaultBreak || !wp.Since.Equal(time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("short history: %+v", wp)
	}

	if wp := workPeriodFrom(nil, nil); !wp.Since.IsZero() || wp.Learned {
		t.Errorf("no history: %+v", wp)
	}
}

func TestPromptTimeline(t *testing.T) {
	var p promptTimeline
	for _, l := range []string{
		human(0, "add a health endpoint"),
		use(1, "b1", "Bash", map[string]interface{}{"command": "go test"}),
		result(2, "b1"),
		say(3, "m1", "Done.", "end_turn"),
		// A compaction summary is not a prompt, and its old timestamp does
		// not move the turn back.
		`{"type":"user","timestamp":"` + at(-50) + `","isCompactSummary":true,"origin":{"kind":"human"},"message":{"role":"user","content":"summary"}}`,
		autoPrompt(10, "<task-notification>build finished</task-notification>"),
		say(11, "m2", "The build passed.", "end_turn"),
		human(20, "ship it"),
		say(21, "m3", "Shipping.", "tool_use"),
	} {
		p.consume([]byte(l))
	}
	if len(p.prompts) != 2 || !p.prompts[0].Equal(t0) || !p.prompts[1].Equal(t0.Add(20*time.Minute)) {
		t.Errorf("prompts = %v, want the two typed ones", p.prompts)
	}
	// The first turn went quiet at 3; the automatic turn at 10 is not the
	// user's; the last turn is still open at 21.
	if len(p.ends) != 1 || !p.ends[0].Equal(t0.Add(3*time.Minute)) || !p.open || !p.last.Equal(t0.Add(21*time.Minute)) {
		t.Errorf("ends = %v open=%v last=%v", p.ends, p.open, p.last)
	}
}

func TestWorkTimelineIncremental(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "projects", "p")
	os.MkdirAll(proj, 0o755)
	// Recent times, so the 30-day window keeps them.
	now := time.Now().UTC().Truncate(time.Second)
	stamp := func(l string, d time.Duration) string {
		return strings.Replace(l, at(0), now.Add(d).Format(time.RFC3339Nano), 1)
	}
	a := filepath.Join(proj, "a.jsonl")
	os.WriteFile(a, []byte(stamp(human(0, "first"), -2*time.Hour)+"\n"), 0o644)
	os.WriteFile(filepath.Join(proj, "b.jsonl"), []byte(stamp(human(0, "second"), -time.Hour)+"\n"), 0o644)

	prompts, _ := workTimeline(dir, now)
	if len(prompts) != 2 || !prompts[0].t.Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("prompts = %v, want both files merged in order", prompts)
	}
	fh, _ := os.OpenFile(a, os.O_APPEND|os.O_WRONLY, 0o644)
	fh.WriteString(stamp(human(0, "third"), -time.Minute) + "\n" + `{"type":"user"`) // the last line is still being written
	fh.Close()
	prompts, _ = workTimeline(dir, now)
	if len(prompts) != 3 || !prompts[2].t.Equal(now.Add(-time.Minute)) {
		t.Errorf("after append: %v", prompts)
	}
	if wp := CurrentWorkPeriod(dir, now); !wp.Since.Equal(now.Add(-2 * time.Hour)) {
		t.Errorf("since = %v, want the first prompt (no pause over the default break)", wp.Since)
	}
}

func TestAtItSince(t *testing.T) {
	base := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }
	// 8:00 and 8:10, a 50-minute break, then prompts every 10 min from 9:10
	// with one turn that ran 25 minutes (waiting counts as work).
	prompts := []time.Time{at(0), at(10), at(70), at(80), at(115), at(125)}
	ends := []time.Time{at(2), at(12), at(72), at(105), at(117), at(127)}
	if got := atItSince(prompts, ends, at(130)); !got.Equal(at(70)) {
		t.Errorf("at it since %v, want 9:10 (after the 50-minute break)", got)
	}
	if got := atItSince(prompts, ends, at(150)); !got.IsZero() {
		t.Errorf("23 min after the last turn: %v, want zero (on a break)", got)
	}
	if got := atItSince(nil, nil, at(0)); !got.IsZero() {
		t.Errorf("no prompts: %v", got)
	}
}
