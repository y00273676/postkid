package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"go.planetmeican.com/yangguang/postkid/internal/model"
	"go.planetmeican.com/yangguang/postkid/internal/store"
)

// CreateEnvironment adds a new environment and persists it as one YAML file.
// The in-memory cache is changed only after the file has been written.
func (a *App) CreateEnvironment(name string, variables map[string]string) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("application is required")
	}
	name, err := store.NormalizeEnvironmentName(name)
	if err != nil {
		return err
	}
	if _, ok := a.environmentIndex(name); ok {
		return fmt.Errorf("environment %q already exists", name)
	}

	e := model.Environment{
		Name:      name,
		Variables: cloneVariables(variables),
	}
	if err := store.CreateEnvironment(a.cfg.EnvironmentsDir(), &e); err != nil {
		return err
	}
	a.environments = append(a.environments, e)
	a.rebindCurrentEnvironment()
	return nil
}

// AddEnvironment is an alias for CreateEnvironment for callers that use the
// same terminology as collection mutation APIs.
func (a *App) AddEnvironment(name string, variables map[string]string) error {
	return a.CreateEnvironment(name, variables)
}

// ImportEnvironment validates, atomically persists, and caches a complete
// environment. The caller's model and variables map are never mutated or
// shared with the returned value or the application cache.
func (a *App) ImportEnvironment(environment model.Environment) (*model.Environment, error) {
	if a == nil || a.cfg == nil {
		return nil, fmt.Errorf("app and config are required")
	}
	normalizedName, err := store.NormalizeEnvironmentName(environment.Name)
	if err != nil {
		return nil, err
	}
	variables, err := normalizeImportedEnvironmentVariables(environment.Variables)
	if err != nil {
		return nil, err
	}
	if _, exists := a.environmentIndex(normalizedName); exists {
		return nil, environmentAlreadyExistsError(normalizedName)
	}

	normalized := model.Environment{Name: normalizedName, Variables: variables}
	persisted, err := store.CreateEnvironmentWithData(a.cfg.EnvironmentsDir(), normalized)
	if err != nil {
		return nil, err
	}
	// Keep the cache and returned pointer detached from each other. An import
	// caller commonly edits the returned variables immediately afterwards.
	cached := cloneEnvironment(persisted)
	a.environments = append(a.environments, cached)
	a.rebindCurrentEnvironment()
	result := cloneEnvironment(cached)
	return &result, nil
}

// ImportEnvironmentAndSelect imports an environment and makes it current as
// one transaction. If current_env cannot be persisted, the newly-created YAML
// file and cache entry are removed and the previous current environment is
// restored in memory.
func (a *App) ImportEnvironmentAndSelect(environment model.Environment) (*model.Environment, error) {
	if a == nil || a.cfg == nil {
		return nil, fmt.Errorf("app and config are required")
	}
	previousCurrent := a.cfg.CurrentEnv
	imported, err := a.ImportEnvironment(environment)
	if err != nil {
		return nil, err
	}

	a.cfg.CurrentEnv = imported.Name
	if err := a.cfg.Save(); err != nil {
		a.cfg.CurrentEnv = previousCurrent
		rollbackErr := store.DeleteEnvironment(imported)
		if rollbackErr == nil {
			a.removeCachedEnvironment(imported.FilePath)
		}
		return nil, combineEnvironmentErrors(fmt.Errorf("save current environment: %w", err), rollbackErr)
	}
	a.rebindCurrentEnvironment()
	return imported, nil
}

// UpdateEnvironment replaces an environment's variables and persists the
// change. The cache is updated only after persistence succeeds.
func (a *App) UpdateEnvironment(name string, variables map[string]string) error {
	return a.EditEnvironment(name, name, variables)
}

// RenameEnvironment changes an environment's logical name and file name. A
// current environment also updates config.yaml transactionally: if any file
// operation fails, both config and the environment file are restored.
func (a *App) RenameEnvironment(name, newName string) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("application is required")
	}
	normalized, err := store.NormalizeEnvironmentName(name)
	if err != nil {
		return err
	}
	idx, ok := a.environmentIndex(normalized)
	if !ok {
		return fmt.Errorf("environment %q not found", normalized)
	}
	return a.EditEnvironment(normalized, newName, a.environments[idx].Variables)
}

// EditEnvironment persists a name and variable update as one transaction.
// The cache changes only after both the YAML and current_env are durable.
func (a *App) EditEnvironment(name, newName string, variables map[string]string) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("application is required")
	}
	name, err := store.NormalizeEnvironmentName(name)
	if err != nil {
		return err
	}
	newName, err = store.NormalizeEnvironmentName(newName)
	if err != nil {
		return err
	}
	idx, ok := a.environmentIndex(name)
	if !ok {
		return fmt.Errorf("environment %q not found", name)
	}
	if existing, exists := a.environmentIndex(newName); exists && existing != idx {
		return fmt.Errorf("environment %q already exists", newName)
	}

	old := a.environments[idx]
	updated := old
	updated.Variables = cloneVariables(variables)

	wasCurrent := a.isCurrentEnvironment(old)
	previousCurrent := a.cfg.CurrentEnv
	if newName == old.Name {
		if err := store.SaveEnvironment(&updated); err != nil {
			return err
		}
	} else {
		if err := store.RenameEnvironment(&updated, newName); err != nil {
			return err
		}
		if wasCurrent {
			a.cfg.CurrentEnv = newName
			if err := a.cfg.Save(); err != nil {
				a.cfg.CurrentEnv = previousCurrent
				rollback := old
				rollback.FilePath = updated.FilePath
				rollbackErr := store.RenameEnvironment(&rollback, old.Name)
				return combineEnvironmentErrors(fmt.Errorf("save current environment: %w", err), rollbackErr)
			}
		}
	}

	a.environments[idx] = updated
	a.rebindCurrentEnvironment()
	return nil
}

// RenameEnvironmentByName is an explicit alias for integrations that prefer
// a name-oriented method name.
func (a *App) RenameEnvironmentByName(name, newName string) error {
	return a.RenameEnvironment(name, newName)
}

// DeleteEnvironment removes one environment. If it is selected, the first
// remaining environment becomes selected; deleting the last one clears the
// current selection. The file is moved to a temporary path until config.yaml
// and the final delete both succeed, allowing rollback on persistence errors.
func (a *App) DeleteEnvironment(name string) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("application is required")
	}
	name, err := store.NormalizeEnvironmentName(name)
	if err != nil {
		return err
	}
	idx, ok := a.environmentIndex(name)
	if !ok {
		return fmt.Errorf("environment %q not found", name)
	}

	old := a.environments[idx]
	wasCurrent := a.isCurrentEnvironment(old)
	previousCurrent := a.cfg.CurrentEnv
	fallback := ""
	if wasCurrent {
		for i := range a.environments {
			if i != idx {
				fallback = a.environments[i].Name
				break
			}
		}
	}

	backup, err := store.ReserveTemporaryYAMLPath(filepath.Dir(old.FilePath), ".postkid-delete-")
	if err != nil {
		return err
	}
	if err := store.RenameEnvironmentFile(old.FilePath, backup); err != nil {
		return err
	}

	if wasCurrent {
		a.cfg.CurrentEnv = fallback
		if err := a.cfg.Save(); err != nil {
			restoreErr := store.RenameEnvironmentFile(backup, old.FilePath)
			a.cfg.CurrentEnv = previousCurrent
			return combineEnvironmentErrors(fmt.Errorf("save current environment: %w", err), restoreErr)
		}
	}

	if err := store.RemoveEnvironmentFile(backup); err != nil {
		var rollbackErr error
		if wasCurrent {
			rollbackErr = a.restoreCurrentEnvironment(previousCurrent)
		}
		fileErr := store.RenameEnvironmentFile(backup, old.FilePath)
		return combineEnvironmentErrors(err, rollbackErr, fileErr)
	}

	// Only now make the cache change. Removing from a slice moves elements, so
	// always rebind curEnv even when a different environment was deleted.
	a.environments = append(a.environments[:idx], a.environments[idx+1:]...)
	a.rebindCurrentEnvironment()
	return nil
}

// DeleteEnvironmentByName is an explicit alias for integrations that prefer
// a name-oriented method name.
func (a *App) DeleteEnvironmentByName(name string) error {
	return a.DeleteEnvironment(name)
}

func (a *App) environmentIndex(name string) (int, bool) {
	for i := range a.environments {
		if a.environments[i].Name == name {
			return i, true
		}
	}
	return 0, false
}

func (a *App) rebindCurrentEnvironment() {
	a.curEnv = nil
	if a == nil || a.cfg == nil || a.cfg.CurrentEnv == "" {
		return
	}
	if idx, ok := a.environmentIndex(a.cfg.CurrentEnv); ok {
		a.curEnv = &a.environments[idx]
	}
}

func (a *App) isCurrentEnvironment(e model.Environment) bool {
	return a.cfg.CurrentEnv == e.Name || (a.curEnv != nil && a.curEnv.FilePath == e.FilePath)
}

func (a *App) restoreCurrentEnvironment(name string) error {
	a.cfg.CurrentEnv = name
	if err := a.cfg.Save(); err != nil {
		return fmt.Errorf("restore current environment: %w", err)
	}
	return nil
}

func cloneVariables(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneEnvironment(in model.Environment) model.Environment {
	out := in
	out.Variables = cloneVariables(in.Variables)
	return out
}

func normalizeImportedEnvironmentVariables(in map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for rawKey, value := range in {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			return nil, fmt.Errorf("environment variable key cannot be empty")
		}
		if !validEnvironmentVariableName(key) {
			return nil, fmt.Errorf("environment variable name %q is not supported; use letters, digits, or underscore", key)
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate environment variable %q", key)
		}
		out[key] = value
	}
	return out, nil
}

func validEnvironmentVariableName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func environmentAlreadyExistsError(name string) error {
	return fmt.Errorf("environment %q already exists: %w: %w", name, store.ErrEnvironmentExists, store.ErrAlreadyExists)
}

func (a *App) removeCachedEnvironment(path string) {
	for i := range a.environments {
		if a.environments[i].FilePath == path {
			a.environments = append(a.environments[:i], a.environments[i+1:]...)
			a.rebindCurrentEnvironment()
			return
		}
	}
}

func combineEnvironmentErrors(values ...error) error {
	var nonNil []error
	for _, err := range values {
		if err != nil {
			nonNil = append(nonNil, err)
		}
	}
	if len(nonNil) == 0 {
		return nil
	}
	if len(nonNil) == 1 {
		return nonNil[0]
	}
	return fmt.Errorf("%v; rollback: %v", nonNil[0], nonNil[1:])
}
