package logger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogFileWatchdog(t *testing.T) {
	tmpDir := t.TempDir()

	// Shorten the watch interval for testing.
	origInterval := fileWatchInterval
	fileWatchInterval = 100 * time.Millisecond
	t.Cleanup(func() {
		fileWatchInterval = origInterval
		ShutdownFileHandler()
	})

	if err := InitWithFile(tmpDir); err != nil {
		t.Fatalf("InitWithFile failed: %v", err)
	}

	// Find the log file that was created.
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created")
	}
	logPath := filepath.Join(tmpDir, entries[0].Name())

	// Verify the file exists.
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log file should exist: %v", err)
	}

	// Delete the file.
	if err := os.Remove(logPath); err != nil {
		t.Fatalf("remove log file failed: %v", err)
	}

	// Verify it's gone.
	if _, err := os.Stat(logPath); err == nil {
		t.Fatal("log file should be deleted")
	}

	// Wait for the watchdog to detect and reopen.
	time.Sleep(300 * time.Millisecond)

	// Verify the file was recreated.
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log file watchdog should have recreated the file: %v", err)
	}

	// Verify we can still write after reopen.
	Info("test message after watchdog reopen")

	logFileMu.Lock()
	f := logFile
	logFileMu.Unlock()
	if f == nil {
		t.Fatal("logFile should not be nil after watchdog reopen")
	}

	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("stat reopened logFile: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("reopened log file should have content")
	}
}
