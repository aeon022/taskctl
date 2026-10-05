package config

import (
	"os"
	"path/filepath"
	"testing"
)

// isolate gives each test its own HOME and a fresh settings store (the store
// is package-global, like the viper instance it replaced).
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TASKCTL_DATA_DIR", "")
	settings.Reset()
	Active = Config{}
	DBPathOverride = ""
	t.Cleanup(func() { settings.Reset(); Active = Config{}; DBPathOverride = "" })
	return home
}

func writeCfg(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "taskctl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFirstRunWritesDefaults(t *testing.T) {
	home := isolate(t)
	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if Active.DefaultList != "" {
		t.Errorf("DefaultList = %q, want empty default", Active.DefaultList)
	}
	b, err := os.ReadFile(filepath.Join(home, ".config", "taskctl", "config.yaml"))
	if err != nil || len(b) == 0 {
		t.Fatalf("first run must write the defaults file: %v %q", err, b)
	}
}

func TestLoadReadsConfigFile(t *testing.T) {
	home := isolate(t)
	writeCfg(t, home, "default_list: Work\n")
	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if Active.DefaultList != "Work" {
		t.Errorf("DefaultList = %q, want Work", Active.DefaultList)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	home := isolate(t)
	writeCfg(t, home, "default_list: Work\n")
	t.Setenv("TASKCTL_DEFAULT_LIST", "Home")
	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if Active.DefaultList != "Home" {
		t.Errorf("DefaultList = %q, want the TASKCTL_ env var to win", Active.DefaultList)
	}
}

func TestLoadRejectsBrokenYAML(t *testing.T) {
	home := isolate(t)
	writeCfg(t, home, "default_list: [unterminated\n")
	if err := Load(); err == nil {
		t.Error("a corrupt config file must surface an error, not be silently ignored")
	}
}

func TestEnvIsIgnoredBeforeLoad(t *testing.T) {
	isolate(t)
	t.Setenv("TASKCTL_DATA_DIR", "/should/not/apply")
	// tests (and anything that never calls Load) must not be redirected by the shell
	want := filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "taskctl", "taskctl.db")
	if got := DBPath(); got != want {
		t.Errorf("DBPath() = %q, want the private default %q", got, want)
	}
	if Shared() {
		t.Error("Shared() must be false without a data dir")
	}
}

func TestDBPathDefaultOverrideAndDataDir(t *testing.T) {
	home := isolate(t)
	def := filepath.Join(home, "Library", "Application Support", "taskctl", "taskctl.db")
	if got := DBPath(); got != def {
		t.Errorf("default = %q, want %q", got, def)
	}

	DBPathOverride = "/tmp/override.db"
	if DBPath() != "/tmp/override.db" || Shared() {
		t.Error("DBPathOverride must win and never count as shared")
	}
	DBPathOverride = ""

	shared := filepath.Join(home, "Dropbox", "tasks")
	writeCfg(t, home, "data_dir: "+shared+"\n")
	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if got := DBPath(); got != filepath.Join(shared, "taskctl.db") {
		t.Errorf("data_dir DBPath = %q", got)
	}
	if !Shared() {
		t.Error("a configured data_dir is a shared (possibly synced) location")
	}
	if _, err := os.Stat(shared); err != nil {
		t.Errorf("the data dir must be created: %v", err)
	}

	DBPathOverride = "/tmp/override.db"
	if Shared() || DBPath() != "/tmp/override.db" {
		t.Error("override must beat data_dir")
	}
}

func TestDataDirFromEnvAfterLoad(t *testing.T) {
	home := isolate(t)
	dir := filepath.Join(home, "iCloud", "t")
	t.Setenv("TASKCTL_DATA_DIR", dir)
	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if got := DBPath(); got != filepath.Join(dir, "taskctl.db") || !Shared() {
		t.Errorf("DBPath = %q shared=%v, want env data dir after Load", got, Shared())
	}
}

func TestAppSupportFiles(t *testing.T) {
	home := isolate(t)
	base := filepath.Join(home, "Library", "Application Support", "taskctl")
	if got := UIStatePath(); got != filepath.Join(base, "ui_state.json") {
		t.Errorf("UIStatePath = %q", got)
	}
	if got := LastSyncedPath(); got != filepath.Join(base, "last_synced") {
		t.Errorf("LastSyncedPath = %q", got)
	}
	if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
		t.Errorf("the app-support dir must be created: %v", err)
	}
}
