package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

func testHistoryEntry(n int) model.HistoryEntry {
	return model.HistoryEntry{
		Timestamp: time.Unix(int64(n), 0).UTC(),
		Request: model.HistoryRequest{
			Method: "GET",
			URL:    "https://example.test/" + string(rune('a'+n%26)),
		},
	}
}

func TestLoadHistorySkipsCorruptAndOversizedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, historyFile)
	oldData, err := json.Marshal(testHistoryEntry(1))
	if err != nil {
		t.Fatal(err)
	}
	newData, err := json.Marshal(testHistoryEntry(2))
	if err != nil {
		t.Fatal(err)
	}
	var file bytes.Buffer
	file.Write(oldData)
	file.WriteByte('\n')
	file.WriteString("not-json\n")
	file.WriteString("null\n")
	file.Write(bytes.Repeat([]byte{'x'}, maxHistoryLineSize+1))
	file.WriteByte('\n')
	file.Write(newData) // also verify a final line without a newline
	if err := os.WriteFile(path, file.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadHistory(dir)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Timestamp.Unix() != 2 || got[1].Timestamp.Unix() != 1 {
		t.Fatalf("got timestamps [%d %d], want [2 1]", got[0].Timestamp.Unix(), got[1].Timestamp.Unix())
	}
}

func TestSaveHistoryCompactsToMaxHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, historyFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for i := 0; i < maxHistory; i++ {
		if err := enc.Encode(testHistoryEntry(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if err := SaveHistory(dir, testHistoryEntry(maxHistory)); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}
	got, err := LoadHistory(dir)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(got) != maxHistory {
		t.Fatalf("got %d entries, want %d", len(got), maxHistory)
	}
	if got[0].Timestamp.Unix() != maxHistory || got[len(got)-1].Timestamp.Unix() != 1 {
		t.Fatalf("got range %d..%d, want %d..1", got[0].Timestamp.Unix(), got[len(got)-1].Timestamp.Unix(), maxHistory)
	}

	lines := 0
	scanner := bufio.NewScanner(mustOpen(t, path))
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if lines != maxHistory {
		t.Fatalf("history file has %d lines, want %d", lines, maxHistory)
	}
}

func TestSaveHistoryRejectsOversizedEntry(t *testing.T) {
	dir := t.TempDir()
	entry := testHistoryEntry(1)
	entry.Response.Body = strings.Repeat("x", maxHistoryLineSize)
	if err := SaveHistory(dir, entry); !errors.Is(err, ErrHistoryEntryTooLarge) {
		t.Fatalf("SaveHistory error = %v, want ErrHistoryEntryTooLarge", err)
	}
	if _, err := os.Stat(filepath.Join(dir, historyFile)); !os.IsNotExist(err) {
		t.Fatalf("history file should not be created, stat error = %v", err)
	}
}

func TestHistoryRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "outside.jsonl")
	if err := os.WriteFile(target, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, historyFile)
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := LoadHistory(dir); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("LoadHistory error = %v, want ErrInvalidPath", err)
	}
	if err := SaveHistory(dir, testHistoryEntry(1)); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("SaveHistory error = %v, want ErrInvalidPath", err)
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
