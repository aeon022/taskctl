package config

import (
	"os"
	"path/filepath"

	coreconfig "github.com/aeon022/missionctl-core/config"
	"github.com/spf13/viper"
)

type Config struct {
	DefaultList   string   `mapstructure:"default_list"`
	ExcludedLists []string `mapstructure:"excluded_lists"`
	DataDir       string   `mapstructure:"data_dir"`
}

var Active Config

func Load() error {
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "taskctl")
	_ = os.MkdirAll(cfgDir, 0755)

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(cfgDir)
	viper.SetEnvPrefix("TASKCTL")
	viper.AutomaticEnv()

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

// DBPath returns the database file path. DBPathOverride (test-only) wins
// if set; otherwise data_dir (viper key, also settable via
// TASKCTL_DATA_DIR) points it at a user-chosen directory — e.g. inside
// iCloud Drive or Dropbox — resolved via coreconfig.ResolveDir; with
// neither set, the private default (~/Library/Application Support/taskctl)
// is unchanged from before this existed.
func DBPath() string {
	if DBPathOverride != "" {
		return DBPathOverride
	}
	if dir := viper.GetString("data_dir"); dir != "" {
		resolved, _ := coreconfig.ResolveDir("taskctl", dir)
		return filepath.Join(resolved, "taskctl.db")
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "taskctl")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "taskctl.db")
}

// Shared reports whether DBPath currently resolves to a user-configured
// directory (data_dir) rather than the tool's private default.
func Shared() bool {
	return DBPathOverride == "" && viper.GetString("data_dir") != ""
}

// UIStatePath is where the TUI persists small preferences (last active
// filter mode) — see missionctl-core/uistate.
func UIStatePath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "taskctl")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "ui_state.json")
}

// LastSyncedPath is the marker file (see missionctl-core/lastsync) tracking
// when a sync last completed, for the TUI's "synced Xh ago" indicator.
func LastSyncedPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "taskctl")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "last_synced")
}
