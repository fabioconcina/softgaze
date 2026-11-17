package store

import (
	"fmt"
	"time"
)

// The names the user gives sessions in the window, shown in place of the
// project. Only the name and the session id are kept.

// SetSessionName names a session; an empty name drops it.
func (s *Store) SetSessionName(sessionID, name string) error {
	var err error
	if name == "" {
		_, err = s.db.Exec(`DELETE FROM session_names WHERE session_id = ?`, sessionID)
	} else {
		_, err = s.db.Exec(`INSERT INTO session_names (session_id, name, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(session_id) DO UPDATE SET name = excluded.name, updated_at = excluded.updated_at`,
			sessionID, name, time.Now().UTC().Format(time.RFC3339))
	}
	if err != nil {
		return fmt.Errorf("saving session name: %w", err)
	}
	return nil
}

// SessionNames returns every named session: session id -> name.
func (s *Store) SessionNames() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT session_id, name FROM session_names`)
	if err != nil {
		return nil, fmt.Errorf("reading session names: %w", err)
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("reading session names: %w", err)
		}
		names[id] = name
	}
	return names, rows.Err()
}
