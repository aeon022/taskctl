package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writePID(t *testing.T, body string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(pidPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func capture(t *testing.T, f func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := f()
	w.Close()
	os.Stdout = old
	b := make([]byte, 4096)
	n, _ := r.Read(b)
	return string(b[:n]), err
}

func TestPathsLiveUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for got, want := range map[string]string{
		pidPath():         filepath.Join(home, ".local", "share", "taskctl", "taskctl.pid"),
		launchAgentPath(): filepath.Join(home, "Library", "LaunchAgents", "com.taskctl.daemon.plist"),
		logsDir():         filepath.Join(home, "Library", "Logs", "taskctl"),
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestStopDaemonWithoutPIDFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := stopDaemon(); err == nil || !strings.Contains(err.Error(), "no daemon running") {
		t.Errorf("err = %v", err)
	}
}

func TestStopDaemonRejectsNonPositivePIDs(t *testing.T) {
	// kill(0)/kill(-n) address whole process groups — never signal those.
	for _, body := range []string{"0", "-5", "abc", ""} {
		writePID(t, body)
		if err := stopDaemon(); err == nil || !strings.Contains(err.Error(), "invalid PID file") {
			t.Errorf("PID file %q: err = %v, want invalid PID file", body, err)
		}
	}
}

func TestStopDaemonDeadProcess(t *testing.T) {
	writePID(t, "999999999\n")
	if err := stopDaemon(); err == nil || !strings.Contains(err.Error(), "could not stop") {
		t.Errorf("err = %v, want could not stop", err)
	}
}

func TestDaemonStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if out, _ := capture(t, showDaemonStatus); !strings.Contains(out, "not running") {
		t.Errorf("no pid file: %q", out)
	}

	writePID(t, "999999999")
	out, err := capture(t, showDaemonStatus)
	if err != nil || !strings.Contains(out, "stale PID file") {
		t.Errorf("dead pid: %q %v", out, err)
	}
	if _, statErr := os.Stat(pidPath()); !os.IsNotExist(statErr) {
		t.Error("a stale PID file must be removed")
	}

	writePID(t, "garbage")
	out, _ = capture(t, showDaemonStatus)
	if !strings.Contains(out, "invalid PID file") {
		t.Errorf("garbage pid: %q", out)
	}
	if _, statErr := os.Stat(pidPath()); !os.IsNotExist(statErr) {
		t.Error("an invalid PID file must be removed")
	}

	writePID(t, strconv.Itoa(os.Getpid()))
	if out, _ = capture(t, showDaemonStatus); !strings.Contains(out, "running (PID") {
		t.Errorf("live pid: %q", out)
	}
}

func TestInstallLaunchAgentWritesPlist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	daemonInterval = 7
	if _, err := capture(t, installLaunchAgent); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(launchAgentPath())
	if err != nil {
		t.Fatal(err)
	}
	plist := string(b)
	for _, want := range []string{"<string>com.taskctl.daemon</string>", "<string>7</string>", "<string>daemon</string>", logsDir() + "/taskctl-daemon.log", "<key>KeepAlive</key>"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q", want)
		}
	}
	if _, err := os.Stat(logsDir()); err != nil {
		t.Errorf("logs dir must exist so launchd can open the log: %v", err)
	}
}
