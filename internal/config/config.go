// Package config 解析 ~/.tpost 数据目录与 config.yaml。
package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultDirName 是用户主目录下的数据目录名。
const DefaultDirName = ".tpost"

// Config 描述 tpost 运行所需的配置。
type Config struct {
	Dir         string `yaml:"-"`
	CurrentEnv  string `yaml:"current_env"`
	configPath  string `yaml:"-"`
}

// Load 解析数据目录。dir 为空时默认 ~/.tpost，不存在则创建。
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
func (c *Config) HistoryDir() string     { return filepath.Join(c.Dir, "history") }

// Save 持久化 config.yaml（current_env 等）。
func (c *Config) Save() error {
	data, err := yaml.Marshal(struct {
		CurrentEnv string `yaml:"current_env"`
	}{CurrentEnv: c.CurrentEnv})
	if err != nil {
		return err
	}
	return os.WriteFile(c.configPath, data, 0o644)
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
