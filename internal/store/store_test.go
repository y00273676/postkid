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

func TestCollectionGRPCDescriptorConfigRoundTripAndClone(t *testing.T) {
	dir := t.TempDir()
	src := model.Collection{
		Name: "grpc",
		Requests: []model.Request{{
			Name: "call", Protocol: model.ProtocolGRPC, URL: "localhost:50051", Method: "demo.Service/Get",
			GRPC: &model.GRPCRequest{
				ProtoFiles:    []string{"proto/service.proto"},
				ImportPaths:   []string{"proto"},
				DescriptorSet: "", Metadata: map[string]string{"x-token": "secret"},
				TLS: &model.GRPCTLSConfig{Enabled: true, CAFile: "ca.pem"},
			},
		}},
		FilePath: filepath.Join(dir, "grpc.yaml"),
	}
	if err := SaveCollection(&src); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	loaded, err := LoadCollections(dir)
	if err != nil {
		t.Fatalf("LoadCollections: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Requests[0].GRPC == nil {
		t.Fatalf("loaded = %#v", loaded)
	}
	grpc := loaded[0].Requests[0].GRPC
	if len(grpc.ProtoFiles) != 1 || grpc.ProtoFiles[0] != "proto/service.proto" || len(grpc.ImportPaths) != 1 || grpc.ImportPaths[0] != "proto" {
		t.Fatalf("descriptor config lost in round trip: %#v", grpc)
	}
	if grpc.TLS == nil || !grpc.TLS.Enabled || grpc.TLS.CAFile != "ca.pem" {
		t.Fatalf("TLS config lost in round trip: %#v", grpc.TLS)
	}

	cloned := cloneCollection(loaded[0])
	cloned.Requests[0].GRPC.ProtoFiles[0] = "changed.proto"
	cloned.Requests[0].GRPC.ImportPaths[0] = "changed"
	cloned.Requests[0].GRPC.Metadata["x-token"] = "changed"
	cloned.Requests[0].GRPC.TLS.CAFile = "changed.pem"
	if grpc.ProtoFiles[0] != "proto/service.proto" || grpc.ImportPaths[0] != "proto" || grpc.Metadata["x-token"] != "secret" || grpc.TLS.CAFile != "ca.pem" {
		t.Fatalf("clone shares nested gRPC state: %#v", grpc)
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

func TestCreateCollectionWithDataIsAtomicAndDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	input := model.Collection{
		Name:      " imported.yaml ",
		Variables: map[string]string{"base_url": "https://example.test"},
		Requests: []model.Request{{
			Name:      " list ",
			Method:    "GET",
			URL:       "https://example.test/list",
			Headers:   map[string]string{"Accept": "application/json"},
			Params:    map[string]string{"page": "1"},
			Variables: map[string]string{"scope": "all"},
		}},
		FilePath: filepath.Join(t.TempDir(), "caller-owned.yaml"),
	}
	created, err := CreateCollectionWithData(dir, input)
	if err != nil {
		t.Fatalf("CreateCollectionWithData: %v", err)
	}
	wantPath := filepath.Join(dir, "imported.yaml")
	if created.Name != "imported" || created.FilePath != wantPath {
		t.Fatalf("created collection = %#v", created)
	}
	if created.Requests[0].Name != " list " {
		t.Fatalf("store unexpectedly normalized request name: %#v", created.Requests[0])
	}

	// The returned model and the caller's input are independent from each
	// other. (The persisted bytes are already detached by YAML marshaling.)
	created.Variables["base_url"] = "changed"
	created.Requests[0].Headers["Accept"] = "text/plain"
	input.Variables["base_url"] = "caller-changed"
	input.Requests[0].Params["page"] = "2"
	loaded, err := LoadCollections(dir)
	if err != nil {
		t.Fatalf("LoadCollections: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Variables["base_url"] != "https://example.test" ||
		loaded[0].Requests[0].Headers["Accept"] != "application/json" ||
		loaded[0].Requests[0].Params["page"] != "1" {
		t.Fatalf("persisted collection changed unexpectedly: %#v", loaded)
	}

	original, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCollectionWithData(dir, model.Collection{Name: "imported", Requests: []model.Request{}}); !errors.Is(err, ErrCollectionExists) {
		t.Fatalf("duplicate error = %v, want ErrCollectionExists", err)
	}
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("duplicate import replaced existing file:\n got %s\nwant %s", got, original)
	}
}

func TestCreateCollectionWithDataRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(target, []byte("keep: me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := CreateCollectionWithData(dir, model.Collection{Name: "linked"}); !errors.Is(err, ErrCollectionExists) {
		t.Fatalf("symlink error = %v, want ErrCollectionExists", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep: me\n" {
		t.Fatalf("symlink target changed: %q", data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".postkid-") {
			t.Errorf("temporary file left after failed create: %s", entry.Name())
		}
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
