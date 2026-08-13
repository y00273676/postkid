package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

func TestSaveCollectionRejectsTraversalAndUnsafeName(t *testing.T) {
	dir := t.TempDir()
	collection := &model.Collection{
		Name:     "demo",
		FilePath: dir + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "outside.yaml",
	}
	if err := SaveCollection(collection); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("traversal error = %v, want ErrInvalidPath", err)
	}
	collection.FilePath = filepath.Join(dir, "demo.yaml")
	collection.Name = "../outside"
	if err := SaveCollection(collection); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("unsafe name error = %v, want ErrInvalidName", err)
	}
}

func TestSaveCollectionPreservesExistingModeAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.yaml")
	if err := os.WriteFile(path, []byte("old: value\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	c := &model.Collection{
		Name:     "demo",
		FilePath: path,
		Requests: []model.Request{{Name: "r", Method: "GET", URL: "https://example.test"}},
	}
	if err := SaveCollection(c); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o640); got != want {
		t.Fatalf("mode = %o, want %o", got, want)
	}
	if leftovers, err := filepath.Glob(filepath.Join(dir, ".postkid-*.tmp")); err != nil {
		t.Fatal(err)
	} else if len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestLoadCollectionsRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "outside.yaml")
	if err := os.WriteFile(target, []byte("name: outside\nrequests: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := LoadCollections(dir); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("LoadCollections error = %v, want ErrInvalidPath", err)
	}
}
