package app

import (
	"os"
	"path/filepath"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/config"
	"go.planetmeican.com/yangguang/postkid/internal/model"
	"go.planetmeican.com/yangguang/postkid/internal/store"
)

func TestEnvironmentCRUDCreateAndRejectDuplicate(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"base_url": "https://local.example.com"}
	if err := a.CreateEnvironment("local", vars); err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	vars["base_url"] = "https://mutated.example.com"
	created, found := a.environmentIndex("local")
	if !found {
		t.Fatal("created environment missing from cache")
	}
	if got := a.Environments()[created].Variables["base_url"]; got != "https://local.example.com" {
		t.Fatalf("variables were not copied: %q", got)
	}
	if err := a.CreateEnvironment("local", nil); err == nil {
		t.Fatal("duplicate environment creation unexpectedly succeeded")
	}

	path := filepath.Join(cfg.EnvironmentsDir(), "local.yaml")
	loaded, err := store.LoadEnvironments(cfg.EnvironmentsDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("created file missing: %v", err)
	}
	var got model.Environment
	for _, candidate := range loaded {
		if candidate.Name == "local" {
			got = candidate
			break
		}
	}
	if got.Variables["base_url"] != "https://local.example.com" {
		t.Fatalf("persisted variables = %#v", got.Variables)
	}
}

func TestEnvironmentRenameCurrentUpdatesConfigAndCache(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetEnvironment("sandbox"); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(cfg.EnvironmentsDir(), "sandbox.yaml")
	newPath := filepath.Join(cfg.EnvironmentsDir(), "dev.yaml")
	if err := a.RenameEnvironment("sandbox", "dev"); err != nil {
		t.Fatalf("RenameEnvironment: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old path still exists: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new path missing: %v", err)
	}
	if a.CurrentEnvironment() == nil || a.CurrentEnvironment().Name != "dev" {
		t.Fatalf("current cache = %#v", a.CurrentEnvironment())
	}
	reloaded, err := config.Load(cfg.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CurrentEnv != "dev" {
		t.Fatalf("persisted current_env = %q", reloaded.CurrentEnv)
	}
	if err := a.RenameEnvironment("dev", "prod"); err == nil {
		t.Fatal("rename to a duplicate environment unexpectedly succeeded")
	}
}

func TestEnvironmentDeleteCurrentSelectsFallback(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetEnvironment("sandbox"); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteEnvironment("sandbox"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	if a.CurrentEnvironment() == nil || a.CurrentEnvironment().Name != "prod" {
		t.Fatalf("fallback current cache = %#v", a.CurrentEnvironment())
	}
	if got := a.cfg.CurrentEnv; got != "prod" {
		t.Fatalf("current_env cache = %q", got)
	}
	if _, err := os.Stat(filepath.Join(cfg.EnvironmentsDir(), "sandbox.yaml")); !os.IsNotExist(err) {
		t.Fatalf("deleted environment still exists: %v", err)
	}
	reloaded, err := config.Load(cfg.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CurrentEnv != "prod" {
		t.Fatalf("persisted fallback current_env = %q", reloaded.CurrentEnv)
	}
}

func TestEnvironmentEditUpdatesNameAndVariablesTogether(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	variables := map[string]string{"base_url": "https://edited.example.com", "token": "new"}
	if err := a.EditEnvironment("sandbox", "development", variables); err != nil {
		t.Fatal(err)
	}
	idx, ok := a.environmentIndex("development")
	if !ok || a.environments[idx].Variables["token"] != "new" {
		t.Fatalf("edited environment = %#v", a.environments)
	}
	loaded, err := store.LoadEnvironments(cfg.EnvironmentsDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, environment := range loaded {
		if environment.Name == "development" && environment.Variables["base_url"] == variables["base_url"] {
			return
		}
	}
	t.Fatalf("edited environment not persisted: %#v", loaded)
}

func TestEnvironmentUpdateWithoutRenameAndDeleteOtherKeepsCurrentBound(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetEnvironment("sandbox"); err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateEnvironment("sandbox", map[string]string{"base_url": "https://changed.example.com"}); err != nil {
		t.Fatal(err)
	}
	if got := a.CurrentEnvironment().Variables["base_url"]; got != "https://changed.example.com" {
		t.Fatalf("current base_url = %q", got)
	}
	if err := a.DeleteEnvironment("prod"); err != nil {
		t.Fatal(err)
	}
	if a.CurrentEnvironment() == nil || a.CurrentEnvironment().Name != "sandbox" {
		t.Fatalf("current environment after deleting another = %#v", a.CurrentEnvironment())
	}
}

func TestEnvironmentCurrentMutationsRollbackWhenConfigPersistenceFails(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetEnvironment("sandbox"); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(cfg.Dir, "config.yaml")
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(configPath, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(configPath) })

	oldPath := filepath.Join(cfg.EnvironmentsDir(), "sandbox.yaml")
	if err := a.RenameEnvironment("sandbox", "renamed"); err == nil {
		t.Fatal("rename should fail when config persistence is unavailable")
	}
	if a.cfg.CurrentEnv != "sandbox" {
		t.Fatalf("current env changed after failed rename: %q", a.cfg.CurrentEnv)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("rename touched file after config failure: %v", err)
	}

	if err := a.DeleteEnvironment("sandbox"); err == nil {
		t.Fatal("delete should fail when config persistence is unavailable")
	}
	if a.cfg.CurrentEnv != "sandbox" {
		t.Fatalf("current env changed after failed delete: %q", a.cfg.CurrentEnv)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("delete did not restore file after config failure: %v", err)
	}
}
