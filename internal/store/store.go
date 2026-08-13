// Package store 负责 collection 与 environment 的 YAML 持久化。
package store

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// CollectionsDir / EnvironmentsDir 是数据目录下的子目录名。
const (
	CollectionsDir  = "collections"
	EnvironmentsDir = "environments"
)

// LoadCollections 读取 dir 下所有 *.yaml collection 文件。
func LoadCollections(dir string) ([]model.Collection, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	var out []model.Collection
	for _, path := range entries {
		c, err := loadYAML[model.Collection](path)
		if err != nil {
			return nil, err
		}
		c.FilePath = path
		out = append(out, c)
	}
	return out, nil
}

// LoadEnvironments 读取 dir 下所有 *.yaml environment 文件。
func LoadEnvironments(dir string) ([]model.Environment, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	var out []model.Environment
	for _, path := range entries {
		e, err := loadYAML[model.Environment](path)
		if err != nil {
			return nil, err
		}
		e.FilePath = path
		out = append(out, e)
	}
	return out, nil
}

// SaveCollection 把 collection 原子写回其 FilePath（临时文件 + rename）。
func SaveCollection(c *model.Collection) error {
	return saveYAML(c.FilePath, c)
}

// SaveEnvironment 把 environment 原子写回其 FilePath。
func SaveEnvironment(e *model.Environment) error {
	return saveYAML(e.FilePath, e)
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
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tpost-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后 tmp 已不存在，Remove 无害
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
