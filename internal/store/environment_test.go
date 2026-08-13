package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

func TestEnvironmentFileCRUD(t *testing.T) {
	dir := t.TempDir()
	e := &model.Environment{Name: "dev", Variables: map[string]string{"base_url": "https://dev.example.com"}}
	if err := CreateEnvironment(dir, e); err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if e.FilePath != filepath.Join(dir, "dev.yaml") {
		t.Fatalf("FilePath = %q", e.FilePath)
	}
	duplicate := &model.Environment{Name: "dev"}
	if err := CreateEnvironment(dir, duplicate); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate error = %v, want ErrAlreadyExists", err)
	}

	if err := RenameEnvironment(e, "sandbox"); err != nil {
		t.Fatalf("RenameEnvironment: %v", err)
	}
	if e.Name != "sandbox" || e.FilePath != filepath.Join(dir, "sandbox.yaml") {
		t.Fatalf("renamed environment = %+v", *e)
	}
	loaded, err := LoadEnvironments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Name != "sandbox" || loaded[0].Variables["base_url"] != "https://dev.example.com" {
		t.Fatalf("loaded renamed environment = %+v", loaded)
	}

	if err := DeleteEnvironment(e); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	if _, err := os.Stat(e.FilePath); !os.IsNotExist(err) {
		t.Fatalf("deleted path stat = %v", err)
	}
	if e.Name != "sandbox" {
		t.Fatalf("DeleteEnvironment mutated model: %+v", *e)
	}
}

func TestEnvironmentPathRejectsUnsafeNamesAndOutsideFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"", "..", "../escape", "a/b", "a\\b", "\n"} {
		if _, err := EnvironmentPath(dir, name); err == nil {
			t.Errorf("EnvironmentPath(%q) unexpectedly succeeded", name)
		}
	}
	e := &model.Environment{Name: "dev", FilePath: filepath.Join(dir, "..", "outside.yaml")}
	if err := CreateEnvironment(dir, e); err == nil {
		t.Fatal("CreateEnvironment accepted a path outside the environment directory")
	}
}

func TestLoadEnvironmentsRejectsDuplicateLogicalNames(t *testing.T) {
	dir := t.TempDir()
	content := []byte("name: same\nvariables: {}\n")
	if err := os.WriteFile(filepath.Join(dir, "one.yaml"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "two.yaml"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEnvironments(dir); err == nil {
		t.Fatal("LoadEnvironments accepted duplicate logical names")
	}
}

func TestLoadCollectionsRejectsDuplicateLogicalNames(t *testing.T) {
	dir := t.TempDir()
	content := []byte("name: same\nrequests: []\n")
	for _, filename := range []string{"one.yaml", "two.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, filename), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadCollections(dir); err == nil {
		t.Fatal("LoadCollections accepted duplicate logical names")
	}
}
