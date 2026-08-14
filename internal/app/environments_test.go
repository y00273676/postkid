package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestImportEnvironmentNormalizesPersistsAndDetaches(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := len(a.Environments())
	source := model.Environment{
		Name:      " imported.yaml ",
		Variables: map[string]string{"base_url": "https://example.test", "token": "secret"},
	}
	created, err := a.ImportEnvironment(source)
	if err != nil {
		t.Fatalf("ImportEnvironment: %v", err)
	}
	if created.Name != "imported" || created.FilePath != filepath.Join(cfg.EnvironmentsDir(), "imported.yaml") {
		t.Fatalf("created environment = %#v", created)
	}
	if len(a.Environments()) != before+1 {
		t.Fatalf("cache count = %d, want %d", len(a.Environments()), before+1)
	}

	source.Variables["base_url"] = "https://source-mutated.test"
	created.Variables["token"] = "returned-mutated"
	idx, ok := a.environmentIndex("imported")
	if !ok || a.Environments()[idx].Variables["base_url"] != "https://example.test" ||
		a.Environments()[idx].Variables["token"] != "secret" {
		t.Fatalf("cache shares imported state: %#v", a.Environments()[idx])
	}
	loaded, err := store.LoadEnvironments(cfg.EnvironmentsDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, environment := range loaded {
		if environment.Name == "imported" {
			if environment.Variables["base_url"] != "https://example.test" || environment.Variables["token"] != "secret" {
				t.Fatalf("persisted environment changed unexpectedly: %#v", environment)
			}
			goto persisted
		}
	}
	t.Fatal("imported environment was not persisted")

persisted:
	_, err = a.ImportEnvironment(model.Environment{Name: "prod", Variables: map[string]string{"token": "other"}})
	if !errors.Is(err, store.ErrEnvironmentExists) || !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("duplicate import error = %v, want environment exists sentinel", err)
	}
	if len(a.Environments()) != before+1 {
		t.Fatalf("cache changed after duplicate import: %d", len(a.Environments()))
	}
}

func TestImportEnvironmentRejectsInvalidValuesWithoutCreatingFile(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := len(a.Environments())
	for _, test := range []struct {
		name string
		vars map[string]string
		want string
	}{
		{name: "bad/name", vars: map[string]string{}, want: "invalid storage name"},
		{name: "bad-key", vars: map[string]string{"bad-key": "value"}, want: "environment variable name"},
		{name: "empty-key", vars: map[string]string{" ": "value"}, want: "cannot be empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := a.ImportEnvironment(model.Environment{Name: test.name, Variables: test.vars})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("ImportEnvironment error = %v, want substring %q", err, test.want)
			}
			if len(a.Environments()) != before {
				t.Fatalf("cache changed after invalid import: %d", len(a.Environments()))
			}
			path := filepath.Join(cfg.EnvironmentsDir(), test.name+".yaml")
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("invalid import left %q: %v", path, statErr)
			}
		})
	}
}

func TestImportEnvironmentAndSelectRollsBackOnConfigFailure(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	previousCurrent := cfg.CurrentEnv
	before := len(a.Environments())
	configPath := filepath.Join(cfg.Dir, "config.yaml")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(configPath, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(configPath) })

	_, err = a.ImportEnvironmentAndSelect(model.Environment{
		Name:      "rollback-import",
		Variables: map[string]string{"token": "secret"},
	})
	if err == nil {
		t.Fatal("ImportEnvironmentAndSelect unexpectedly succeeded")
	}
	if cfg.CurrentEnv != previousCurrent {
		t.Fatalf("current environment changed after rollback: %q", cfg.CurrentEnv)
	}
	if len(a.Environments()) != before {
		t.Fatalf("cache changed after rollback: %d vs %d", len(a.Environments()), before)
	}
	if _, statErr := os.Stat(filepath.Join(cfg.EnvironmentsDir(), "rollback-import.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("rollback left imported file: %v", statErr)
	}
}

func TestImportEnvironmentAndSelectPersistsCurrentEnvironment(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	created, err := a.ImportEnvironmentAndSelect(model.Environment{
		Name:      "selected-import",
		Variables: map[string]string{"base_url": "https://selected.example.test"},
	})
	if err != nil {
		t.Fatalf("ImportEnvironmentAndSelect: %v", err)
	}
	if a.CurrentEnvironment() == nil || a.CurrentEnvironment().Name != created.Name {
		t.Fatalf("current environment = %#v, want %q", a.CurrentEnvironment(), created.Name)
	}
	reloaded, err := config.Load(cfg.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CurrentEnv != "selected-import" {
		t.Fatalf("persisted current_env = %q", reloaded.CurrentEnv)
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
