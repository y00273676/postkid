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

// ErrCollectionExists 表示目标 collection 文件已经存在。
var ErrCollectionExists = errors.New("collection already exists")

// ErrCollectionNotFound 表示要操作的 collection 文件不存在。
var ErrCollectionNotFound = errors.New("collection not found")

// ErrAlreadyExists / ErrNotFound 是面向调用方的通用别名，保留更具体的
// collection 错误值供 errors.Is 使用。
var (
	ErrAlreadyExists = ErrCollectionExists
	ErrNotFound      = ErrCollectionNotFound
)

// LoadCollections 读取 dir 下所有 *.yaml collection 文件。
func LoadCollections(dir string) ([]model.Collection, error) {
	paths, err := yamlFiles(dir)
	if err != nil {
		return nil, err
	}

	out := make([]model.Collection, 0, len(paths))
	seenNames := make(map[string]string, len(paths))
	for _, path := range paths {
		c, err := loadYAML[model.Collection](path)
		if err != nil {
			return nil, fmt.Errorf("load collection %q: %w", path, err)
		}
		normalizedName, err := NormalizeCollectionName(c.Name)
		if err != nil {
			return nil, fmt.Errorf("load collection %q: %w", path, err)
		}
		if previous, ok := seenNames[normalizedName]; ok {
			return nil, fmt.Errorf("load collection %q: duplicate name %q also used by %q", path, normalizedName, previous)
		}
		c.Name = normalizedName
		seenNames[normalizedName] = path
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
	seenNames := make(map[string]string, len(paths))
	for _, path := range paths {
		e, err := loadYAML[model.Environment](path)
		if err != nil {
			return nil, fmt.Errorf("load environment %q: %w", path, err)
		}
		normalizedName, err := NormalizeEnvironmentName(e.Name)
		if err != nil {
			return nil, fmt.Errorf("load environment %q: %w", path, err)
		}
		if previous, ok := seenNames[normalizedName]; ok {
			return nil, fmt.Errorf("load environment %q: duplicate name %q also used by %q", path, normalizedName, previous)
		}
		e.Name = normalizedName
		seenNames[normalizedName] = path
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
	normalizedName, err := NormalizeEnvironmentName(e.Name)
	if err != nil {
		return err
	}
	updated := *e
	updated.Name = normalizedName
	return saveYAML(e.FilePath, &updated)
}

// NormalizeCollectionName 校验并规范化 collection 的逻辑名称。
//
// collection 名称不是任意文件路径：它只能是一个普通文件名，并且不能
// 通过名称把写入导向 collections 目录之外。调用方可以传入可选的 .yaml
// 后缀，但模型中的 Name 始终只保存逻辑名称，文件名始终使用一个 .yaml
// 后缀。这也避免了 foo 与 foo.yaml 产生两个语义相同的 collection。
func NormalizeCollectionName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if strings.HasSuffix(strings.ToLower(name), yamlExtension) {
		name = strings.TrimSpace(name[:len(name)-len(yamlExtension)])
	}
	if err := validateName(name, "collection"); err != nil {
		return "", err
	}
	return name, nil
}

// CollectionPath 返回 name 对应的 collection 文件路径，并执行与保存相同
// 的目录和名称安全校验。
func CollectionPath(dir, name string) (string, error) {
	if err := validateDirectoryPath(dir); err != nil {
		return "", err
	}
	normalized, err := NormalizeCollectionName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, normalized+yamlExtension), nil
}

// CreateCollection 在 dir 中创建一个空 collection。
//
// 创建使用同目录临时文件 + 硬链接，目标路径不会被覆盖；即使另一个
// 进程同时创建同名文件，也只能有一个调用成功。返回的 Collection 已经
// 带有可直接用于 SaveCollection 的 FilePath。
func CreateCollection(dir, name string) (model.Collection, error) {
	path, err := CollectionPath(dir, name)
	if err != nil {
		return model.Collection{}, err
	}
	normalized, _ := NormalizeCollectionName(name) // CollectionPath 已完成校验
	c := model.Collection{
		Name:      normalized,
		Requests:  []model.Request{},
		FilePath:  path,
		Variables: nil,
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return model.Collection{}, fmt.Errorf("marshal collection: %w", err)
	}
	if err := atomicCreateFile(path, data, 0o600); err != nil {
		return model.Collection{}, err
	}
	return c, nil
}

// RenameCollection 原子地重命名 collection 文件，并同步 YAML 中的 Name。
//
// c 只有在整个操作成功后才会被修改。旧文件先移动到同目录备份，再将
// 已序列化的新内容放到目标路径；写入失败会尝试恢复旧路径，避免请求和
// 变量数据因为重命名而丢失。
func RenameCollection(c *model.Collection, newName string) error {
	if c == nil {
		return errors.New("rename collection: nil collection")
	}
	normalized, err := NormalizeCollectionName(newName)
	if err != nil {
		return err
	}
	if err := validateYAMLPath(c.FilePath); err != nil {
		return err
	}
	info, err := os.Lstat(c.FilePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("%w: %q", ErrCollectionNotFound, c.FilePath)
	}
	if err != nil {
		return fmt.Errorf("inspect collection %q: %w", c.FilePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: collection %q is not a regular file", ErrInvalidPath, c.FilePath)
	}

	dir := filepath.Dir(c.FilePath)
	destination, err := CollectionPath(dir, normalized)
	if err != nil {
		return err
	}
	if sameCollectionPath(c.FilePath, destination) {
		// The filename already has the desired spelling. We may still need to
		// repair the YAML Name field, so use the same atomic save path without
		// changing the caller's value until persistence succeeds.
		updated := *c
		updated.Name = normalized
		updated.FilePath = c.FilePath
		if err := SaveCollection(&updated); err != nil {
			return err
		}
		c.Name = updated.Name
		c.FilePath = updated.FilePath
		return nil
	}

	if destinationInfo, err := os.Lstat(destination); err == nil {
		// A symlink is an occupied path too; never replace it.
		_ = destinationInfo
		return fmt.Errorf("%w: %q", ErrCollectionExists, destination)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect collection destination %q: %w", destination, err)
	}

	updated := *c
	updated.Name = normalized
	updated.FilePath = destination
	data, err := yaml.Marshal(updated)
	if err != nil {
		return fmt.Errorf("marshal collection: %w", err)
	}
	tmpName, err := collectionWriteTempFile(dir, data, info.Mode().Perm(), ".postkid-rename-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmpName)

	backupName, err := collectionReserveTempName(dir, ".postkid-rename-*.bak")
	if err != nil {
		return err
	}
	defer os.Remove(backupName)
	if err := os.Rename(c.FilePath, backupName); err != nil {
		return fmt.Errorf("move collection to backup: %w", err)
	}
	if err := os.Rename(tmpName, destination); err != nil {
		restoreErr := os.Rename(backupName, c.FilePath)
		if restoreErr != nil {
			return fmt.Errorf("rename collection: %w (restore old file: %v)", err, restoreErr)
		}
		return fmt.Errorf("rename collection: %w", err)
	}
	c.Name = updated.Name
	c.FilePath = updated.FilePath
	// The rename is already committed. Backup cleanup is best effort: reporting
	// an error here would leave callers' cache stale even though the destination
	// file is live.
	_ = os.Remove(backupName)
	return nil
}

// DeleteCollection removes the collection file represented by c. It refuses
// symlinks and does not mutate c, allowing callers to update their cache only
// after the filesystem operation succeeds.
func DeleteCollection(c *model.Collection) error {
	if c == nil {
		return errors.New("delete collection: nil collection")
	}
	if err := validateYAMLPath(c.FilePath); err != nil {
		return err
	}
	info, err := os.Lstat(c.FilePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("%w: %q", ErrCollectionNotFound, c.FilePath)
	}
	if err != nil {
		return fmt.Errorf("inspect collection %q: %w", c.FilePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: collection %q is not a regular file", ErrInvalidPath, c.FilePath)
	}
	if err := os.Remove(c.FilePath); err != nil {
		return fmt.Errorf("delete collection %q: %w", c.FilePath, err)
	}
	return nil
}

// samePath compares paths after cleaning them without following symlinks.
func sameCollectionPath(a, b string) bool {
	if filepath.IsAbs(a) != filepath.IsAbs(b) {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
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

// atomicCreateFile writes a new file without replacing an existing path. A
// hard link is used for the final installation so the no-overwrite guarantee
// also holds when another process creates the same path between our checks.
func atomicCreateFile(path string, data []byte, mode os.FileMode) error {
	if err := validateYAMLPath(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := validateDirectoryPath(dir); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		_ = info
		return fmt.Errorf("%w: %q", ErrCollectionExists, path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect storage file %q: %w", path, err)
	}
	tmpName, err := collectionWriteTempFile(dir, data, mode, ".postkid-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmpName)
	if err := os.Link(tmpName, path); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%w: %q", ErrCollectionExists, path)
		}
		return fmt.Errorf("install storage file: %w", err)
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}

// writeTempFile creates and fully syncs a temporary file in dir. The caller
// owns the returned path and is responsible for removing it after any rename.
func collectionWriteTempFile(dir string, data []byte, mode os.FileMode, pattern string) (string, error) {
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create temporary storage file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("set temporary storage permissions: %w", err)
	}
	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write temporary storage file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("sync temporary storage file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temporary storage file: %w", err)
	}
	cleanup = false
	return tmpName, nil
}

// reserveTempName reserves a unique pathname without leaving a file at it.
// It is used as a rename backup target, where os.Rename must be guaranteed not
// to collide with an unrelated file.
func collectionReserveTempName(dir, pattern string) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("reserve temporary storage path: %w", err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("close temporary storage path: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return "", fmt.Errorf("clear temporary storage path: %w", err)
	}
	return name, nil
}
