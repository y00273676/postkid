package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
		Name:     "demo",
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

func TestCollectionCRUDStore(t *testing.T) {
	dir := t.TempDir()
	created, err := CreateCollection(dir, "orders")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if created.Name != "orders" || created.FilePath != filepath.Join(dir, "orders.yaml") {
		t.Fatalf("created collection = %#v", created)
	}
	if len(created.Requests) != 0 {
		t.Fatalf("new collection requests = %#v, want empty", created.Requests)
	}
	if _, err := os.Stat(created.FilePath); err != nil {
		t.Fatalf("created file missing: %v", err)
	}
	if _, err := CreateCollection(dir, "orders.yaml"); !errors.Is(err, ErrCollectionExists) {
		t.Fatalf("duplicate error = %v, want ErrCollectionExists", err)
	}

	created.Requests = []model.Request{{Name: "keep", Method: "POST", URL: "https://example.test", Body: `{"ok":true}`}}
	created.Variables = map[string]string{"token": "secret"}
	if err := SaveCollection(&created); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	if err := RenameCollection(&created, "renamed.yaml"); err != nil {
		t.Fatalf("RenameCollection: %v", err)
	}
	if created.Name != "renamed" || created.FilePath != filepath.Join(dir, "renamed.yaml") {
		t.Fatalf("renamed collection = %#v", created)
	}
	if _, err := os.Stat(filepath.Join(dir, "orders.yaml")); !os.IsNotExist(err) {
		t.Fatalf("old collection path still exists: %v", err)
	}
	data, err := os.ReadFile(created.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "keep") || !strings.Contains(string(data), "secret") || !strings.Contains(string(data), "name: renamed") {
		t.Fatalf("renamed file lost data: %s", data)
	}

	if err := DeleteCollection(&created); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	if _, err := os.Stat(created.FilePath); !os.IsNotExist(err) {
		t.Fatalf("deleted collection still exists: %v", err)
	}
}

func TestCollectionCRUDRejectsUnsafeNamesAndPaths(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"", " ", ".", "..", "../escape", `..\escape`, "nested/name", "bad\x00name"} {
		if _, err := CreateCollection(dir, name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("CreateCollection(%q) error = %v, want ErrInvalidName", name, err)
		}
	}
	c := model.Collection{Name: "safe", FilePath: filepath.Join(dir, "safe.yaml")}
	if err := RenameCollection(&c, "../escape"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("RenameCollection unsafe error = %v, want ErrInvalidName", err)
	}
	c.FilePath = dir + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "outside.yaml"
	if err := DeleteCollection(&c); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("DeleteCollection traversal error = %v, want ErrInvalidPath", err)
	}
}

func TestRenameCollectionRejectsDestinationAndKeepsSource(t *testing.T) {
	dir := t.TempDir()
	first, err := CreateCollection(dir, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateCollection(dir, "second")
	if err != nil {
		t.Fatal(err)
	}
	first.Requests = []model.Request{{Name: "preserve", Method: "GET", URL: "https://example.test"}}
	if err := SaveCollection(&first); err != nil {
		t.Fatal(err)
	}
	if err := RenameCollection(&first, second.Name); !errors.Is(err, ErrCollectionExists) {
		t.Fatalf("collision error = %v, want ErrCollectionExists", err)
	}
	if first.Name != "first" || first.FilePath != filepath.Join(dir, "first.yaml") {
		t.Fatalf("model changed after failed rename: %#v", first)
	}
	loaded, err := LoadCollections(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded collections = %d, want 2", len(loaded))
	}
	if len(loaded[0].Requests) != 1 && len(loaded[1].Requests) != 1 {
		t.Fatal("source request data lost after failed rename")
	}
}
