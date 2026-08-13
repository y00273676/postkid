package app

import (
	"fmt"
	"path/filepath"

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
