package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Focus stretches as the Softgaze window measured them: how long the user stayed
// with a session (no titles, no content), so the switch list can show the
// minutes of the work period.

func focusKey(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// SaveFocusStretch records a stretch so far. It is called again as the
// stretch grows and keeps the largest values.
func (s *Store) SaveFocusStretch(sessionID string, start time.Time, focused time.Duration, prompts int) error {
	_, err := s.db.Exec(`INSERT INTO focus_stretches (session_id, started_at, focused_seconds, prompts)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(session_id, started_at) DO UPDATE SET
			focused_seconds = MAX(focused_seconds, excluded.focused_seconds),
			prompts = MAX(prompts, excluded.prompts)`,
		sessionID, focusKey(start), int64(focused.Seconds()), prompts)
	if err != nil {
		return fmt.Errorf("saving focus stretch: %w", err)
	}
	return nil
}

// FocusSince is the total of the stretches started at or after since.
func (s *Store) FocusSince(since time.Time) (time.Duration, error) {
	var total sql.NullInt64
	err := s.db.QueryRow(`SELECT SUM(focused_seconds) FROM focus_stretches WHERE started_at >= ?`,
		focusKey(since)).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("summing focus stretches: %w", err)
	}
	return time.Duration(total.Int64) * time.Second, nil
}
