// Package config 解析 ~/.postkid 数据目录与 config.yaml。
package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultDirName 是用户主目录下的数据目录名。
const DefaultDirName = ".postkid"

// Config 描述 postkid 运行所需的配置。
type Config struct {
	Dir        string `yaml:"-"`
	CurrentEnv string `yaml:"current_env"`
	configPath string `yaml:"-"`
}

// Load 解析数据目录。dir 为空时默认 ~/.postkid，不存在则创建。
func Load(dir string) (*Config, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, DefaultDirName)
	}
	c := &Config{Dir: dir, configPath: filepath.Join(dir, "config.yaml")}
	if err := c.ensureDirs(); err != nil {
		return nil, err
	}
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

// CollectionsDir / EnvironmentsDir / HistoryDir 返回各子目录绝对路径。
func (c *Config) CollectionsDir() string  { return filepath.Join(c.Dir, "collections") }
func (c *Config) EnvironmentsDir() string { return filepath.Join(c.Dir, "environments") }
func (c *Config) HistoryDir() string      { return filepath.Join(c.Dir, "history") }

// Save 持久化 config.yaml（current_env 等）。
func (c *Config) Save() error {
	data, err := yaml.Marshal(struct {
		CurrentEnv string `yaml:"current_env"`
	}{CurrentEnv: c.CurrentEnv})
	if err != nil {
		return err
	}
	return atomicWrite(c.configPath, data, 0o644)
}

// atomicWrite keeps the previous config intact if serialization, writing, or
// rename fails. Environment mutations rely on this property when they update
// current_env as part of a file transaction.
func atomicWrite(path string, data []byte, newMode os.FileMode) error {
	dir := filepath.Dir(path)
	mode := newMode
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("config path is not a regular file: %s", path)
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect config path: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".postkid-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set temporary config permissions: %w", err)
	}
	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func (c *Config) ensureDirs() error {
	for _, sub := range []string{"", "collections", "environments", "history"} {
		if err := os.MkdirAll(filepath.Join(c.Dir, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) load() error {
	data, err := os.ReadFile(c.configPath)
	if os.IsNotExist(err) {
		return nil // 首次运行，无配置文件
	}
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, c)
}
