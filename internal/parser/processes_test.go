package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestReadPIDFiles_OwnPID(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	pid := os.Getpid()
	info := PIDInfo{
		PID:       pid,
		SessionID: "test-session-123",
		CWD:       "/tmp",
		StartedAt: time.Now().UnixMilli(),
		Kind:      "interactive",
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(sessionsDir, strconv.Itoa(pid)+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	results := readPIDFiles(tmpDir)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].PID != pid {
		t.Errorf("expected PID %d, got %d", pid, results[0].PID)
	}
	if results[0].SessionID != "test-session-123" {
		t.Errorf("unexpected session ID: %s", results[0].SessionID)
	}
}

func TestReadPIDFiles_DeadPID(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	os.MkdirAll(sessionsDir, 0o755)

	deadPID := 99999999
	info := PIDInfo{PID: deadPID, SessionID: "dead-session"}
	data, _ := json.Marshal(info)
	os.WriteFile(filepath.Join(sessionsDir, strconv.Itoa(deadPID)+".json"), data, 0o644)

	results := readPIDFiles(tmpDir)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for dead PID, got %d", len(results))
	}
}

func TestReadPIDFiles_MismatchedFilename(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	os.MkdirAll(sessionsDir, 0o755)

	info := PIDInfo{PID: os.Getpid(), SessionID: "mismatched"}
	data, _ := json.Marshal(info)
	os.WriteFile(filepath.Join(sessionsDir, "12345.json"), data, 0o644)

	results := readPIDFiles(tmpDir)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for mismatched PID, got %d", len(results))
	}
}

func TestIsProcessRunning(t *testing.T) {
	if !isProcessRunning(os.Getpid()) {
		t.Error("expected own process to be running")
	}
	if isProcessRunning(99999999) {
		t.Error("expected bogus PID to not be running")
	}
}
