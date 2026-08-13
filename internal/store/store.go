// Package store 负责 collection 与 environment 的 YAML 持久化。
package store

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// CollectionsDir / EnvironmentsDir 是数据目录下的子目录名。
const (
	CollectionsDir  = "collections"
	EnvironmentsDir = "environments"

	yamlExtension = ".yaml"
)

// ErrInvalidPath 表示存储路径不是一个安全的 postkid 数据文件路径。
//
// FilePath 来自 YAML 加载结果，也可能由调用方构造。保存前必须再次校验，
// 不能因为字段名看起来像内部元数据就把它当成可信路径。
var ErrInvalidPath = errors.New("invalid storage path")

// ErrInvalidName 表示 collection/environment 名称不能作为安全的逻辑名称。
var ErrInvalidName = errors.New("invalid storage name")

// LoadCollections 读取 dir 下所有 *.yaml collection 文件。
func LoadCollections(dir string) ([]model.Collection, error) {
	paths, err := yamlFiles(dir)
	if err != nil {
		return nil, err
	}

	out := make([]model.Collection, 0, len(paths))
	for _, path := range paths {
		c, err := loadYAML[model.Collection](path)
		if err != nil {
			return nil, fmt.Errorf("load collection %q: %w", path, err)
		}
		if err := validateName(c.Name, "collection"); err != nil {
			return nil, fmt.Errorf("load collection %q: %w", path, err)
		}
		c.FilePath = path
		out = append(out, c)
	}
	return out, nil
}

// LoadEnvironments 读取 dir 下所有 *.yaml environment 文件。
func LoadEnvironments(dir string) ([]model.Environment, error) {
	paths, err := yamlFiles(dir)
	if err != nil {
		return nil, err
	}

	out := make([]model.Environment, 0, len(paths))
	for _, path := range paths {
		e, err := loadYAML[model.Environment](path)
		if err != nil {
			return nil, fmt.Errorf("load environment %q: %w", path, err)
		}
		if err := validateName(e.Name, "environment"); err != nil {
			return nil, fmt.Errorf("load environment %q: %w", path, err)
		}
		e.FilePath = path
		out = append(out, e)
	}
	return out, nil
}

// SaveCollection 把 collection 原子写回其 FilePath（临时文件 + rename）。
func SaveCollection(c *model.Collection) error {
	if c == nil {
		return errors.New("save collection: nil collection")
	}
	if err := validateName(c.Name, "collection"); err != nil {
		return err
	}
	return saveYAML(c.FilePath, c)
}

// SaveEnvironment 把 environment 原子写回其 FilePath。
func SaveEnvironment(e *model.Environment) error {
	if e == nil {
		return errors.New("save environment: nil environment")
	}
	if err := validateName(e.Name, "environment"); err != nil {
		return err
	}
	return saveYAML(e.FilePath, e)
}

// yamlFiles 枚举目录中的 YAML 普通文件。
//
// filepath.Glob 会悄悄跟随符号链接，而且目录名中的 glob 元字符会改变
// 匹配语义。ReadDir 只返回直接子项，并显式拒绝符号链接和特殊文件，避免
// 一个看似普通的 collection 文件把读取/写入导向数据目录之外。
func yamlFiles(dir string) ([]string, error) {
	if err := validateDirectoryPath(dir); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read store directory %q: %w", dir, err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != yamlExtension {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: symlink %q", ErrInvalidPath, filepath.Join(dir, entry.Name()))
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect store file %q: %w", filepath.Join(dir, entry.Name()), err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: %q is not a regular file", ErrInvalidPath, filepath.Join(dir, entry.Name()))
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	return paths, nil
}

func validateDirectoryPath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: empty directory", ErrInvalidPath)
	}
	if !utf8.ValidString(path) || strings.IndexByte(path, 0) >= 0 {
		return fmt.Errorf("%w: invalid directory %q", ErrInvalidPath, path)
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect store directory %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: symlink directory %q", ErrInvalidPath, path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %q is not a directory", ErrInvalidPath, path)
	}
	return nil
}

func validateName(name, kind string) error {
	if name == "" || strings.TrimSpace(name) == "" || name == "." || name == ".." {
		return fmt.Errorf("%w: %s name %q", ErrInvalidName, kind, name)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: %s name is not valid UTF-8", ErrInvalidName, kind)
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r == 0 || unicode.IsControl(r) {
			return fmt.Errorf("%w: %s name %q contains a path separator or control character", ErrInvalidName, kind, name)
		}
	}
	return nil
}

func validateYAMLPath(path string) error {
	if err := validateFilePath(path); err != nil {
		return err
	}
	if filepath.Ext(filepath.Base(path)) != yamlExtension {
		return fmt.Errorf("%w: expected a .yaml file, got %q", ErrInvalidPath, path)
	}
	return nil
}

func validateFilePath(path string) error {
	if path == "" || !utf8.ValidString(path) || strings.IndexByte(path, 0) >= 0 {
		return fmt.Errorf("%w: invalid file path %q", ErrInvalidPath, path)
	}
	// Reject an explicit parent component instead of cleaning it away. Cleaning
	// first would make a caller-provided traversal indistinguishable from a
	// deliberately chosen path.
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return fmt.Errorf("%w: parent traversal in %q", ErrInvalidPath, path)
		}
	}
	base := filepath.Base(path)
	if base == "" || base == "." || base == ".." {
		return fmt.Errorf("%w: invalid file name in %q", ErrInvalidPath, path)
	}
	return nil
}

func loadYAML[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, err
	}
	var v T
	if err := yaml.Unmarshal(data, &v); err != nil {
		return zero, err
	}
	return v, nil
}

// saveYAML 用「写临时文件 + rename」实现原子写，避免中途崩溃损坏原文件。
func saveYAML(path string, v any) error {
	if err := validateYAMLPath(path); err != nil {
		return err
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0o600)
}

// atomicWriteFile 将 data 写入同目录临时文件后替换目标文件。
// 新建文件使用 0600（collection/environment 可能包含 token）；更新既有
// 文件时保留其权限，避免一次保存意外改变用户的 Git 工作区权限。
func atomicWriteFile(path string, data []byte, newMode os.FileMode) error {
	if err := validateFilePath(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := validateDirectoryPath(dir); err != nil {
		return err
	}

	mode := newMode
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: refusing to replace symlink %q", ErrInvalidPath, path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: %q is not a regular file", ErrInvalidPath, path)
		}
		mode = info.Mode().Perm()
	case !os.IsNotExist(err):
		return fmt.Errorf("inspect storage file %q: %w", path, err)
	}

	tmp, err := os.CreateTemp(dir, ".postkid-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary storage file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set temporary storage permissions: %w", err)
	}
	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary storage file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary storage file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary storage file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace storage file: %w", err)
	}
	// Directory fsync is best effort: the rename is already atomic, while some
	// supported filesystems/platforms do not permit syncing a directory.
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}
