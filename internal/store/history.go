package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

const (
	historyFile    = "history.jsonl"
	maxHistory     = 500
)

// SaveHistory 将一条历史记录追加到 history.jsonl。
// 超过 maxHistory 条时截断最旧的记录。
func SaveHistory(dir string, entry model.HistoryEntry) error {
	path := filepath.Join(dir, historyFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if err := enc.Encode(entry); err != nil {
		return err
	}
	return nil
}

// LoadHistory 读取 history.jsonl 中最近 N 条记录（最多 maxHistory）。
func LoadHistory(dir string) ([]model.HistoryEntry, error) {
	path := filepath.Join(dir, historyFile)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []model.HistoryEntry
	scanner := bufio.NewScanner(f)
	// 先全读，避免大文件时一次性读入太大
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		var entry model.HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // 跳过损坏的记录
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return entries, err
	}

	// 按时间降序排列，取最近 maxHistory 条
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})
	if len(entries) > maxHistory {
		entries = entries[:maxHistory]
	}
	return entries, nil
}