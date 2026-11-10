package store

import (
	"testing"
	"time"
)

func TestFocusStretches(t *testing.T) {
	s := openTestStore(t)
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	start := now.Add(-time.Hour)

	// Saved again as it grows: the largest value stays.
	for _, mins := range []int{5, 20, 12} {
		if err := s.SaveFocusStretch("a", start, time.Duration(mins)*time.Minute, 3); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.FocusSince(start); err != nil || got != 20*time.Minute {
		t.Errorf("FocusSince = %v %v, want 20m", got, err)
	}
}

func TestFocusSince(t *testing.T) {
	s := openTestStore(t)
	morning := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	s.SaveFocusStretch("a", morning.Add(30*time.Minute), 25*time.Minute, 3)
	s.SaveFocusStretch("a", morning.Add(30*time.Minute), 40*time.Minute, 4) // grew: counted once
	s.SaveFocusStretch("b", morning.Add(2*time.Hour), 15*time.Minute, 1)
	s.SaveFocusStretch("c", morning.Add(-10*time.Hour), 90*time.Minute, 5) // yesterday
	if got, err := s.FocusSince(morning); err != nil || got != 55*time.Minute {
		t.Errorf("FocusSince = %v %v, want 55m", got, err)
	}
	if got, _ := s.FocusSince(morning.Add(24 * time.Hour)); got != 0 {
		t.Errorf("nothing since tomorrow: %v", got)
	}
}
