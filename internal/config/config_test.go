package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	off := false
	tests := []struct {
		name      string
		file      string // config.json; "" for none
		want      Config
		wellbeing bool
	}{
		{"no file", "", Config{Port: DefaultPort}, true},
		{"bad json", "{not json", Config{Port: DefaultPort}, true},
		{"port only", `{"port": 4000}`, Config{Port: 4000}, true},
		{"wellbeing off", `{"wellbeing": false}`, Config{Port: DefaultPort, Wellbeing: &off}, false},
		{"everything", `{"port": 4001, "claude_dir": "/c", "db_path": "/d/x.db"}`, Config{Port: 4001, ClaudeDir: "/c", DBPath: "/d/x.db"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, home := t.TempDir(), t.TempDir()
			if tt.file != "" {
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(tt.file), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want := tt.want
			if want.ClaudeDir == "" {
				want.ClaudeDir = filepath.Join(home, ".claude")
			}
			if want.DBPath == "" {
				want.DBPath = filepath.Join(dir, "focus.db")
			}
			got := load(dir, home)
			if got.Port != want.Port || got.ClaudeDir != want.ClaudeDir || got.DBPath != want.DBPath {
				t.Errorf("load = %+v, want %+v", got, want)
			}
			if got.WellbeingOn() != tt.wellbeing {
				t.Errorf("WellbeingOn = %v, want %v", got.WellbeingOn(), tt.wellbeing)
			}
		})
	}
}
