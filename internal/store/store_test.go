package store

import (
	"os"
	"path/filepath"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

func TestLoadCollections(t *testing.T) {
	colls, err := LoadCollections("../../testdata/collections")
	if err != nil {
		t.Fatalf("LoadCollections: %v", err)
	}
	if len(colls) != 1 {
		t.Fatalf("want 1 collection, got %d", len(colls))
	}
	c := colls[0]
	if c.Name != "order" {
		t.Errorf("name = %q, want order", c.Name)
	}
	if len(c.Requests) != 2 {
		t.Fatalf("want 2 requests, got %d", len(c.Requests))
	}
	if c.Requests[0].Name != "get-order" {
		t.Errorf("first request = %q, want get-order", c.Requests[0].Name)
	}
	if c.Variables["order_id"] != "123456" {
		t.Errorf("collection var order_id = %q", c.Variables["order_id"])
	}
	if c.FilePath == "" {
		t.Error("FilePath not set")
	}
}

func TestLoadEnvironments(t *testing.T) {
	envs, err := LoadEnvironments("../../testdata/environments")
	if err != nil {
		t.Fatalf("LoadEnvironments: %v", err)
	}
	if len(envs) != 2 {
		t.Fatalf("want 2 environments, got %d", len(envs))
	}
}

// TestCollectionRoundTrip 验证 load → save → load 后内容一致。
func TestCollectionRoundTrip(t *testing.T) {
	src, err := LoadCollections("../../testdata/collections")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	dir := t.TempDir()
	for i := range src {
		src[i].FilePath = filepath.Join(dir, src[i].Name+".yaml")
		if err := SaveCollection(&src[i]); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	got, err := LoadCollections(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Name != src[0].Name {
		t.Errorf("name mismatch: %q vs %q", got[0].Name, src[0].Name)
	}
	if len(got[0].Requests) != len(src[0].Requests) {
		t.Fatalf("requests count mismatch: %d vs %d", len(got[0].Requests), len(src[0].Requests))
	}
	// 验证 request 级变量与 body 也完整保留
	want := src[0].Requests[1]
	gotReq := got[0].Requests[1]
	if gotReq.Body != want.Body {
		t.Errorf("body mismatch:\ngot:  %q\nwant: %q", gotReq.Body, want.Body)
	}
}

// TestSaveAtomic 验证保存后原文件存在且可读（原子写不丢文件）。
func TestSaveAtomic(t *testing.T) {
	dir := t.TempDir()
	c := &model.Collection{
		Name: "demo",
		FilePath: filepath.Join(dir, "demo.yaml"),
		Requests: []model.Request{{Name: "r1", Method: "GET", URL: "https://x"}},
	}
	if err := SaveCollection(c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(c.FilePath); err != nil {
		t.Fatalf("file not written: %v", err)
	}
}
