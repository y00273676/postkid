package store

import (
	"bufio"
	"bytes"
	"container/heap"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

const (
	historyFile = "history.jsonl"
	maxHistory  = 500

	// A history record contains request/response bodies, so allow the same
	// order of magnitude as the HTTP body limit while still bounding one bad
	// JSONL line. Oversized lines are ignored by LoadHistory.
	maxHistoryLineSize = 10 * 1024 * 1024
)

var (
	historyMu = new(sync.Mutex)

	// ErrHistoryEntryTooLarge is returned before writing an entry that could
	// never be read back within maxHistoryLineSize.
	ErrHistoryEntryTooLarge = errors.New("history entry too large")
)

// SaveHistory 将一条历史记录追加到 history.jsonl。
// 超过 maxHistory 条时以原子替换方式截断最旧的记录。
func SaveHistory(dir string, entry model.HistoryEntry) error {
	path, err := historyPath(dir)
	if err != nil {
		return err
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode history entry: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxHistoryLineSize {
		return fmt.Errorf("%w: %d bytes (limit %d)", ErrHistoryEntryTooLarge, len(data), maxHistoryLineSize)
	}

	historyMu.Lock()
	defer historyMu.Unlock()

	f, err := openHistoryAppend(path)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("append history: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync history: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close history: %w", err)
	}

	entries, overLimit, err := readHistoryFile(path)
	if err != nil {
		return err
	}
	if !overLimit {
		return nil
	}
	return rewriteHistory(path, entries)
}

// LoadHistory 读取 history.jsonl 中最近 N 条记录（最多 maxHistory）。
// 损坏、空白或超过 maxHistoryLineSize 的单行会被跳过，其他记录仍可用。
func LoadHistory(dir string) ([]model.HistoryEntry, error) {
	path, err := historyPath(dir)
	if err != nil {
		return nil, err
	}

	historyMu.Lock()
	defer historyMu.Unlock()
	entries, _, err := readHistoryFile(path)
	return entries, err
}

func historyPath(dir string) (string, error) {
	if err := validateDirectoryPath(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, historyFile)
	if err := validateFilePath(path); err != nil {
		return "", err
	}
	return path, nil
}

func openHistoryAppend(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: refusing to follow history symlink %q", ErrInvalidPath, path)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: history path %q is not a regular file", ErrInvalidPath, path)
		}
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("inspect history %q: %w", path, err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open history %q: %w", path, err)
	}
	return f, nil
}

// historyItem keeps the source sequence so ties have deterministic ordering:
// a later line is treated as the newer record when timestamps are equal.
type historyItem struct {
	entry model.HistoryEntry
	seq   uint64
}

type historyHeap []historyItem

func (h historyHeap) Len() int { return len(h) }

func (h historyHeap) Less(i, j int) bool {
	left, right := h[i], h[j]
	if left.entry.Timestamp.Equal(right.entry.Timestamp) {
		return left.seq < right.seq
	}
	return left.entry.Timestamp.Before(right.entry.Timestamp)
}

func (h historyHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *historyHeap) Push(x any) { *h = append(*h, x.(historyItem)) }

func (h *historyHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = historyItem{}
	*h = old[:n-1]
	return item
}

// readHistoryFile parses one line at a time and retains only the newest
// maxHistory records. A heap keeps memory bounded even if an old history file
// grew far beyond the normal compacted size.
func readHistoryFile(path string) ([]model.HistoryEntry, bool, error) {
	info, statErr := os.Lstat(path)
	if os.IsNotExist(statErr) {
		return nil, false, nil
	}
	if statErr != nil {
		return nil, false, fmt.Errorf("inspect history %q: %w", path, statErr)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("%w: refusing to read history symlink %q", ErrInvalidPath, path)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%w: history path %q is not a regular file", ErrInvalidPath, path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("open history %q: %w", path, err)
	}
	defer f.Close()

	h := make(historyHeap, 0, maxHistory)
	var seq uint64
	err = eachHistoryLine(f, func(line []byte) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || bytes.Equal(line, []byte("null")) {
			return
		}
		var entry model.HistoryEntry
		if json.Unmarshal(line, &entry) != nil {
			return
		}
		item := historyItem{entry: entry, seq: seq}
		seq++
		heap.Push(&h, item)
		if h.Len() > maxHistory {
			heap.Pop(&h)
		}
	})
	if err != nil {
		return historyItemsToEntries(h), false, fmt.Errorf("read history %q: %w", path, err)
	}

	// seq counts valid records. Once it exceeds maxHistory, the heap has
	// discarded at least one record, which tells SaveHistory to compact.
	overLimit := seq > maxHistory
	entries := historyItemsToEntries(h)
	return entries, overLimit, nil
}

func historyItemsToEntries(h historyHeap) []model.HistoryEntry {
	items := append(historyHeap(nil), h...)
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.entry.Timestamp.Equal(right.entry.Timestamp) {
			return left.seq > right.seq
		}
		return left.entry.Timestamp.After(right.entry.Timestamp)
	})
	entries := make([]model.HistoryEntry, len(items))
	for i := range items {
		entries[i] = items[i].entry
	}
	return entries
}

// eachHistoryLine avoids bufio.Scanner's fixed token error. It discards an
// oversized line and continues at the next newline, so one corrupt response
// body cannot make all later history unavailable.
func eachHistoryLine(r io.Reader, fn func([]byte)) error {
	reader := bufio.NewReaderSize(r, 64*1024)
	line := make([]byte, 0, 4096)
	oversized := false
	for {
		fragment, prefix, err := reader.ReadLine()
		if err == io.EOF {
			if len(fragment) > 0 || len(line) > 0 {
				if !oversized {
					line = append(line, fragment...)
					fn(line)
				}
			}
			return nil
		}
		if err != nil {
			return err
		}
		if !oversized {
			if len(line)+len(fragment) > maxHistoryLineSize {
				oversized = true
				line = line[:0]
			} else {
				line = append(line, fragment...)
			}
		}
		if prefix {
			continue
		}
		if !oversized {
			fn(line)
		}
		line = line[:0]
		oversized = false
	}
}

func rewriteHistory(path string, entries []model.HistoryEntry) error {
	if len(entries) > maxHistory {
		entries = entries[:maxHistory]
	}
	var data bytes.Buffer
	enc := json.NewEncoder(&data)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			return fmt.Errorf("encode compacted history: %w", err)
		}
	}
	return atomicWriteFile(path, data.Bytes(), 0o600)
}
