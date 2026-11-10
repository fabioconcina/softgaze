package parser

import (
	"bytes"
	"encoding/json"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Which sessions is the user working on now? Claude Code sessions stay open
// for days, so running is not the same as current.
//
// All the user's prompts, across sessions, form one timeline. The pauses in
// it fall into two groups: breaks during the day (coffee, lunch, meetings)
// and the end of a working day. The current work period starts after the
// last end-of-day pause, and the cut between the two groups is learned from
// the user's own history: a two-component log-normal mixture over pauses of
// 20 minutes or more (human inter-activity times follow log-normal mixtures;
// Halfaker et al., "User Session Identification Based on Strong Regularities
// in Inter-activity Time", WWW 2015), cut where the end-of-day component
// becomes the more likely one.
//
// A pause is idle time, not time between prompts: it runs from the later of
// the previous prompt and the end of the last turn the user started, so
// waiting while Claude works does not count as a pause.

const (
	workLookback     = 30 * 24 * time.Hour
	workPauseMin     = 20 * time.Minute // shorter gaps are part of working
	workDefaultBreak = 4 * time.Hour    // until there is enough history to learn from
	workMinBreak     = time.Hour
	workMaxBreak     = 12 * time.Hour
	workMinPauses    = 12
)

// WorkPeriod is the user's current stretch of work, across sessions.
type WorkPeriod struct {
	// Since is the first prompt after the last end-of-day pause; zero when
	// there is no history. When the user is away, the period is the one that
	// ended last: what they are most likely to come back to.
	Since time.Time
	// Break is the pause length that ends a working day.
	Break time.Duration
	// Learned is true when Break was fitted to the user's history rather
	// than taken from the default.
	Learned bool
}

// CurrentWorkPeriod reads the prompt timeline of the transcripts touched in
// the last 30 days (only lines added since the previous call) and returns
// the current work period.
func CurrentWorkPeriod(claudeDir string, now time.Time) WorkPeriod {
	prompts, ends := workTimeline(claudeDir, now)
	return workPeriodFrom(stampTimes(prompts), stampTimes(ends))
}

// workPeriodFrom finds the work period in a timeline; prompts and ends are
// sorted.
func workPeriodFrom(prompts, ends []time.Time) WorkPeriod {
	wp := WorkPeriod{Break: workDefaultBreak}
	if len(prompts) == 0 {
		return wp
	}
	gaps := idleGaps(prompts, ends)
	if thr, ok := fitBreak(gaps); ok {
		wp.Break, wp.Learned = thr, true
	}
	i := len(prompts) - 1
	for i > 0 && gaps[i-1] <= wp.Break {
		i--
	}
	wp.Since = prompts[i]
	return wp
}

// idleGaps returns, for each prompt after the first, how long the user had
// been idle: since the previous prompt or the end of a turn they started,
// whichever came later. gaps[i] is the pause before prompts[i+1].
func idleGaps(prompts, ends []time.Time) []time.Duration {
	if len(prompts) < 2 {
		return nil
	}
	gaps := make([]time.Duration, 0, len(prompts)-1)
	var lastEnd time.Time
	j := 0
	for i := 1; i < len(prompts); i++ {
		t := prompts[i]
		for j < len(ends) && ends[j].Before(t) {
			lastEnd = ends[j]
			j++
		}
		from := prompts[i-1]
		if lastEnd.After(from) {
			from = lastEnd
		}
		gaps = append(gaps, t.Sub(from))
	}
	return gaps
}

// fitBreak learns the pause length that separates breaks during the day from
// the end of one: EM for a two-component Gaussian mixture on the log of the
// pauses of 20 minutes or more. It reports false when there are too few
// pauses or they do not form two groups.
func fitBreak(gaps []time.Duration) (time.Duration, bool) {
	var xs []float64
	for _, g := range gaps {
		if g >= workPauseMin {
			xs = append(xs, math.Log(g.Seconds()))
		}
	}
	if len(xs) < workMinPauses {
		return 0, false
	}
	sort.Float64s(xs)
	n := float64(len(xs))
	var mean, v float64
	for _, x := range xs {
		mean += x / n
	}
	for _, x := range xs {
		v += (x - mean) * (x - mean) / n
	}
	if v == 0 {
		return 0, false
	}
	// Component 0: breaks during the day; 1: the end of a day.
	mu := [2]float64{xs[len(xs)/4], xs[len(xs)*9/10]}
	sd := [2]float64{math.Sqrt(v) / 2, math.Sqrt(v) / 2}
	w := [2]float64{0.5, 0.5}
	logp := func(c int, x float64) float64 {
		z := (x - mu[c]) / sd[c]
		return math.Log(w[c]) - math.Log(sd[c]) - z*z/2
	}
	r := make([]float64, len(xs)) // P(end of day | pause)
	for iter := 0; iter < 300; iter++ {
		for i, x := range xs {
			r[i] = 1 / (1 + math.Exp(logp(0, x)-logp(1, x)))
		}
		var s0, s1, m0, m1 float64
		for i, x := range xs {
			s0 += 1 - r[i]
			s1 += r[i]
			m0 += (1 - r[i]) * x
			m1 += r[i] * x
		}
		if s0 < 1 || s1 < 1 {
			return 0, false // collapsed into one group
		}
		mu = [2]float64{m0 / s0, m1 / s1}
		var v0, v1 float64
		for i, x := range xs {
			v0 += (1 - r[i]) * (x - mu[0]) * (x - mu[0])
			v1 += r[i] * (x - mu[1]) * (x - mu[1])
		}
		sd = [2]float64{math.Max(0.1, math.Sqrt(v0/s0)), math.Max(0.1, math.Sqrt(v1/s1))}
		w = [2]float64{s0 / n, s1 / n}
	}
	if mu[0] > mu[1] {
		mu[0], mu[1], sd[0], sd[1], w[0], w[1] = mu[1], mu[0], sd[1], sd[0], w[1], w[0]
	}
	if mu[1]-mu[0] < math.Log(3) {
		return 0, false // medians within a factor of 3: not two groups
	}
	// The cut: the first point between the medians where a pause is more
	// likely the end of a day than a break.
	const steps = 400
	for k := 1; k < steps; k++ {
		x := mu[0] + float64(k)*(mu[1]-mu[0])/steps
		if logp(1, x) > logp(0, x) {
			d := time.Duration(math.Exp(x) * float64(time.Second))
			return min(max(d, workMinBreak), workMaxBreak), true
		}
	}
	return 0, false
}

// promptTimeline is what one transcript contributes to the timeline. It is
// fed incrementally, like the follow builders.
type promptTimeline struct {
	offset  int64
	modTime time.Time
	prompts []time.Time // the user's prompts
	ends    []time.Time // when each turn the user started went quiet
	open    bool        // a turn the user started is still the latest
	last    time.Time   // the latest line of that turn
}

var (
	userTypeTag      = []byte(`"type":"user"`)
	toolUseResultTag = []byte(`"toolUseResult"`)
)

func (p *promptTimeline) consume(line []byte) {
	// Only lines that can be prompts are decoded in full; tool results (most
	// of a transcript's bytes) are not.
	if bytes.Contains(line, userTypeTag) && !bytes.Contains(line, toolUseResultTag) {
		var e followEntry
		if json.Unmarshal(line, &e) == nil && e.Type == "user" && e.Message != nil && !e.Timestamp.IsZero() &&
			!e.IsSidechain && !e.IsMeta && !e.IsCompactSummary && len(e.ToolUseResult) == 0 {
			blocks := parseContentBlocks(e.Message.Content)
			results := false
			for _, blk := range blocks {
				results = results || blk.Type == "tool_result"
			}
			if !results {
				switch promptKind(&e, promptText(blocks)) {
				case "human":
					p.closeTurn()
					p.prompts = append(p.prompts, e.Timestamp)
					p.open, p.last = true, e.Timestamp
					return
				case "auto":
					p.closeTurn() // Claude carries on by itself: not the user's turn
					return
				}
			}
		}
	}
	if !p.open {
		return
	}
	var e struct {
		Type        string    `json:"type"`
		Timestamp   time.Time `json:"timestamp"`
		IsSidechain bool      `json:"isSidechain"`
	}
	if json.Unmarshal(line, &e) == nil && (e.Type == "assistant" || e.Type == "user") && !e.IsSidechain && e.Timestamp.After(p.last) {
		p.last = e.Timestamp
	}
}

func (p *promptTimeline) closeTurn() {
	if p.open {
		p.ends = append(p.ends, p.last)
		p.open = false
	}
}

var workTimelines = struct {
	sync.Mutex
	m     map[string]*promptTimeline
	ready atomic.Bool // the first full read is done: later reads are cheap
}{m: map[string]*promptTimeline{}}

// stamp is a moment in the timeline and the session it happened in.
type stamp struct {
	t       time.Time
	session string
}

func stampTimes(s []stamp) []time.Time {
	out := make([]time.Time, len(s))
	for i := range s {
		out[i] = s[i].t
	}
	return out
}

// workTimeline brings every recent transcript's timeline up to date and
// returns the merged, sorted prompts and turn ends of the last 30 days.
func workTimeline(claudeDir string, now time.Time) (prompts, ends []stamp) {
	defer workTimelines.ready.Store(true)
	files, err := enumerateSessionFiles(claudeDir)
	if err != nil {
		return nil, nil
	}
	cutoff := now.Add(-workLookback)
	workTimelines.Lock()
	defer workTimelines.Unlock()
	keep := make(map[string]bool, len(files))
	for _, f := range files {
		if f.modTime.Before(cutoff) {
			continue
		}
		keep[f.path] = true
		p := workTimelines.m[f.path]
		if p == nil || f.size < p.offset {
			p = &promptTimeline{}
			workTimelines.m[f.path] = p
		}
		if f.size > p.offset || !f.modTime.Equal(p.modTime) {
			n, err := consumeFrom(f.path, p.offset, p)
			if err != nil {
				continue
			}
			p.offset += n
			p.modTime = f.modTime
		}
		id := strings.TrimSuffix(filepath.Base(f.path), ".jsonl")
		for _, t := range p.prompts {
			if t.After(cutoff) {
				prompts = append(prompts, stamp{t, id})
			}
		}
		for _, t := range p.ends {
			if t.After(cutoff) {
				ends = append(ends, stamp{t, id})
			}
		}
		if p.open {
			ends = append(ends, stamp{p.last, id})
		}
	}
	for path := range workTimelines.m {
		if !keep[path] {
			delete(workTimelines.m, path)
		}
	}
	sort.SliceStable(prompts, func(i, j int) bool { return prompts[i].t.Before(prompts[j].t) })
	sort.SliceStable(ends, func(i, j int) bool { return ends[i].t.Before(ends[j].t) })
	return prompts, ends
}

// breakPause is the shortest pause that counts as a real break: neither the
// user nor a turn they started was active for this long.
const breakPause = 15 * time.Minute

// AtItSince is when the user's current run of work began, across sessions:
// the first prompt after their last pause of at least 15 minutes (time
// waiting on Claude counts as work). Zero when they are on a pause now, and
// until the prompt timeline has had its first full read.
func AtItSince(claudeDir string, now time.Time) time.Time {
	if !workTimelines.ready.Load() {
		return time.Time{}
	}
	prompts, ends := workTimeline(claudeDir, now)
	return atItSince(stampTimes(prompts), stampTimes(ends), now)
}

func atItSince(prompts, ends []time.Time, now time.Time) time.Time {
	n := len(prompts)
	if n == 0 {
		return time.Time{}
	}
	last := prompts[n-1]
	if len(ends) > 0 && ends[len(ends)-1].After(last) {
		last = ends[len(ends)-1]
	}
	if now.Sub(last) >= breakPause {
		return time.Time{} // on a break now
	}
	gaps := idleGaps(prompts, ends)
	start := n - 1
	for start > 0 && gaps[start-1] < breakPause {
		start--
	}
	return prompts[start]
}
