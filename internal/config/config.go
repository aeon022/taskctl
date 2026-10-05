package config

import (
	"errors"
	"os"
	"path/filepath"

	coreconfig "github.com/aeon022/missionctl-core/config"
)

// settings is this tool's config store (replaces the former global viper).
var settings = coreconfig.NewStore("config")

type Config struct {
	DefaultList string `yaml:"default_list"`
}

var Active Config

func Load() error {
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "taskctl")
	_ = os.MkdirAll(cfgDir, 0755)

	settings.SetEnvPrefix("TASKCTL")
	settings.AddPath(cfgDir)

	settings.SetDefault("default_list", "")

	if err := settings.Read(); err != nil {
		if !errors.Is(err, coreconfig.ErrNotFound) {
			return err
		}
		// write defaults
		_ = settings.Write(filepath.Join(cfgDir, "config.yaml"))
	}
	return settings.Unmarshal(&Active)
}

// DBPathOverride, when non-empty, overrides DBPath()'s return value. Used by tests
// to point at a temporary database instead of the real one on disk.
var DBPathOverride string

// DBPath returns the database file path. DBPathOverride (test-only) wins
// if set; otherwise data_dir (config key, also settable via
// TASKCTL_DATA_DIR) points it at a user-chosen directory — e.g. inside
// iCloud Drive or Dropbox — resolved via coreconfig.ResolveDir; with
// neither set, the private default (~/Library/Application Support/taskctl)
// is unchanged from before this existed.
func DBPath() string {
	if DBPathOverride != "" {
		return DBPathOverride
	}
	if dir := settings.GetString("data_dir"); dir != "" {
		resolved, _ := coreconfig.ResolveDir("taskctl", dir)
		return filepath.Join(resolved, "taskctl.db")
	}
	return appSupportFile("taskctl.db")
}

// Shared reports whether DBPath currently resolves to a user-configured
// directory (data_dir) rather than the tool's private default.
func Shared() bool {
	return DBPathOverride == "" && settings.GetString("data_dir") != ""
}

// UIStatePath is where the TUI persists small preferences (last active
// filter mode) — see missionctl-core/uistate.
func UIStatePath() string {
	return appSupportFile("ui_state.json")
}

// LastSyncedPath is the marker file (see missionctl-core/lastsync) tracking
// when a sync last completed, for the TUI's "synced Xh ago" indicator.
func LastSyncedPath() string {
	return appSupportFile("last_synced")
}

// appSupportFile returns the path to name inside taskctl's private
// ~/Library/Application Support/taskctl directory, creating it if needed.
func appSupportFile(name string) string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", "taskctl")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, name)
}
