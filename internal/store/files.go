package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// EnvironmentPath returns the canonical path for an environment named name.
// Names are validated before they are joined to dir; callers must never build
// a path from an untrusted environment name themselves.
func EnvironmentPath(dir, name string) (string, error) {
	normalized, err := NormalizeEnvironmentName(name)
	if err != nil {
		return "", err
	}
	if err := validateDirectoryPath(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, normalized+yamlExtension)
	if err := validateYAMLPath(path); err != nil {
		return "", err
	}
	return path, nil
}

// NormalizeEnvironmentName trims whitespace and accepts an optional .yaml
// suffix while keeping the logical model name extension-free.
func NormalizeEnvironmentName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if strings.HasSuffix(strings.ToLower(name), yamlExtension) {
		name = strings.TrimSpace(name[:len(name)-len(yamlExtension)])
	}
	if err := validateName(name, "environment"); err != nil {
		return "", err
	}
	return name, nil
}

// CreateEnvironment writes a new environment file. The destination must not
// already exist, including as a symlink. If FilePath is empty, the canonical
// path derived from Name is used and written back to e on success.
func CreateEnvironment(dir string, e *model.Environment) error {
	if e == nil {
		return errors.New("create environment: nil environment")
	}
	normalized, err := NormalizeEnvironmentName(e.Name)
	if err != nil {
		return err
	}
	toSave := *e
	toSave.Name = normalized

	path := e.FilePath
	if path == "" {
		var err error
		path, err = EnvironmentPath(dir, e.Name)
		if err != nil {
			return err
		}
	} else if err := validateEnvironmentPath(dir, path); err != nil {
		return err
	}

	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("environment %q already exists: %w: %w", e.Name, ErrEnvironmentExists, ErrAlreadyExists)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect environment %q: %w", e.Name, err)
	}

	// SaveEnvironment is intentionally not used here: it permits replacing an
	// existing regular file, which is correct for updates but not for creation.
	data, err := yaml.Marshal(&toSave)
	if err != nil {
		return err
	}
	if err := environmentAtomicCreateFile(path, data, 0o600); err != nil {
		return err
	}
	e.Name = normalized
	e.FilePath = path
	return nil
}

// CreateEnvironmentWithData atomically creates a canonical environment file
// and returns a detached model containing its persisted path. The input model
// is never mutated, and an existing regular file, directory, or symlink is
// never replaced.
//
// This value-oriented form is intended for application imports: callers can
// validate and persist the complete environment first, then install the
// returned value in an in-memory cache only after the filesystem operation
// succeeds.
func CreateEnvironmentWithData(dir string, e model.Environment) (model.Environment, error) {
	created := cloneEnvironment(e)
	// Imports always use the canonical destination derived from Name; never
	// trust a FilePath carried by an external model.
	created.FilePath = ""
	if err := CreateEnvironment(dir, &created); err != nil {
		return model.Environment{}, err
	}
	return cloneEnvironment(created), nil
}

// environmentAtomicCreateFile installs a new file with a no-overwrite link,
// so a concurrent creator cannot be silently replaced.
func environmentAtomicCreateFile(path string, data []byte, mode os.FileMode) error {
	if err := validateYAMLPath(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := validateDirectoryPath(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".postkid-env-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary environment file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set temporary environment permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary environment file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary environment file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary environment file: %w", err)
	}
	if err := os.Link(tmpName, path); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("environment already exists: %w: %w", ErrEnvironmentExists, ErrAlreadyExists)
		}
		return fmt.Errorf("install environment file: %w", err)
	}
	return syncDirectory(dir)
}

// RenameEnvironment atomically changes an environment's file name and YAML
// Name field. The model is updated only after both filesystem operations
// succeed, so callers can safely keep it in an application cache.
func RenameEnvironment(e *model.Environment, newName string) error {
	if e == nil {
		return errors.New("rename environment: nil environment")
	}
	normalized, err := NormalizeEnvironmentName(newName)
	if err != nil {
		return err
	}
	if err := validateYAMLPath(e.FilePath); err != nil {
		return err
	}
	destination, err := EnvironmentPath(filepath.Dir(e.FilePath), normalized)
	if err != nil {
		return err
	}
	if sameEnvironmentPath(e.FilePath, destination) {
		updated := *e
		updated.Name = normalized
		if err := SaveEnvironment(&updated); err != nil {
			return err
		}
		*e = updated
		return nil
	}
	if err := RenameEnvironmentFile(e.FilePath, destination); err != nil {
		return err
	}
	updated := *e
	updated.Name = normalized
	updated.FilePath = destination
	if err := SaveEnvironment(&updated); err != nil {
		rollbackErr := RenameEnvironmentFile(destination, e.FilePath)
		return combineStoreErrors(err, rollbackErr)
	}
	*e = updated
	return nil
}

// DeleteEnvironment removes one environment file without mutating the model.
// Application code can move the file to a temporary path first when it needs
// a larger transaction involving config.yaml.
func DeleteEnvironment(e *model.Environment) error {
	if e == nil {
		return errors.New("delete environment: nil environment")
	}
	return RemoveEnvironmentFile(e.FilePath)
}

// RenameEnvironmentFile atomically moves an environment file to newPath.
// Both paths must be regular YAML files in the same validated directory, and
// the destination must not exist. The caller owns updating the model/cache.
func RenameEnvironmentFile(oldPath, newPath string) error {
	if err := validateYAMLPath(oldPath); err != nil {
		return err
	}
	if err := validateYAMLPath(newPath); err != nil {
		return err
	}
	oldDir := filepath.Dir(oldPath)
	newDir := filepath.Dir(newPath)
	if err := validateDirectoryPath(oldDir); err != nil {
		return err
	}
	if err := validateDirectoryPath(newDir); err != nil {
		return err
	}
	if !sameEnvironmentPath(oldDir, newDir) {
		return fmt.Errorf("%w: environment files must stay in one directory", ErrInvalidPath)
	}
	if err := ensureRegularFile(oldPath); err != nil {
		return err
	}
	if _, err := os.Lstat(newPath); err == nil {
		return fmt.Errorf("destination environment file already exists: %w", os.ErrExist)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination environment file: %w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename environment file: %w", err)
	}
	return syncDirectory(oldDir)
}

// RemoveEnvironmentFile removes one existing regular environment file. It is
// kept separate from RenameEnvironmentFile so application code can implement
// a move-to-backup transaction and restore the file when config persistence
// fails.
func RemoveEnvironmentFile(path string) error {
	if err := validateYAMLPath(path); err != nil {
		return err
	}
	if err := ensureRegularFile(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove environment file: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

// ReserveTemporaryYAMLPath returns a unique, currently unused YAML path in
// dir. The path is reserved only briefly; callers should immediately move a
// file into it with RenameEnvironmentFile. The temporary placeholder is
// removed before returning.
func ReserveTemporaryYAMLPath(dir, prefix string) (string, error) {
	if err := validateDirectoryPath(dir); err != nil {
		return "", err
	}
	if strings.TrimSpace(prefix) == "" {
		prefix = ".postkid-tmp-"
	}
	file, err := os.CreateTemp(dir, prefix+"*.yaml")
	if err != nil {
		return "", fmt.Errorf("reserve temporary environment path: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close temporary environment path: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("release temporary environment path: %w", err)
	}
	return path, nil
}

func validateEnvironmentPath(dir, path string) error {
	if err := validateDirectoryPath(dir); err != nil {
		return err
	}
	if err := validateYAMLPath(path); err != nil {
		return err
	}
	cleanDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("resolve environment directory: %w", err)
	}
	cleanPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("resolve environment path: %w", err)
	}
	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: environment path %q is outside %q", ErrInvalidPath, path, dir)
	}
	return nil
}

func ensureRegularFile(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("environment file %q does not exist: %w", path, os.ErrNotExist)
	}
	if err != nil {
		return fmt.Errorf("inspect environment file %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: refusing to operate on symlink %q", ErrInvalidPath, path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %q is not a regular file", ErrInvalidPath, path)
	}
	return nil
}

func sameEnvironmentPath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(filepath.Clean(left))
	rightAbs, rightErr := filepath.Abs(filepath.Clean(right))
	return leftErr == nil && rightErr == nil && leftAbs == rightAbs
}

func syncDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer file.Close()
	_ = file.Sync()
	return nil
}

func combineStoreErrors(first, second error) error {
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	return fmt.Errorf("%v; rollback: %v", first, second)
}
