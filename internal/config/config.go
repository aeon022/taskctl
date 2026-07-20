package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	DefaultList   string   `mapstructure:"default_list"`
	ExcludedLists []string `mapstructure:"excluded_lists"`
}

var Active Config

func Load() error {
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "taskctl")
	_ = os.MkdirAll(cfgDir, 0755)

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(cfgDir)

	viper.SetDefault("default_list", "")
	viper.SetDefault("excluded_lists", []string{})

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return err
		}
		// write defaults
		_ = viper.WriteConfigAs(filepath.Join(cfgDir, "config.yaml"))
	}
	return viper.Unmarshal(&Active)
}

// DBPathOverride, when non-empty, overrides DBPath()'s return value. Used by tests
// to point at a temporary database instead of the real one on disk.
var DBPathOverride string

func DBPath() string {
	if DBPathOverride != "" {
		return DBPathOverride
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "taskctl")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "taskctl.db")
}
