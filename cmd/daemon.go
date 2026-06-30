package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/spf13/cobra"
)

var daemonInterval int

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run background sync loop with macOS notifications",
	Long: `Syncs Apple Reminders every N minutes and sends a macOS notification
for tasks that are due today or overdue.

To run at login, use: taskctl daemon --install
To stop the daemon:   taskctl daemon --stop`,
	RunE: runDaemon,
}

var daemonInstall bool
var daemonStop bool
var daemonStatus bool

func init() {
	daemonCmd.Flags().IntVar(&daemonInterval, "interval", 5, "Sync interval in minutes")
	daemonCmd.Flags().BoolVar(&daemonInstall, "install", false, "Install as a LaunchAgent (runs at login)")
	daemonCmd.Flags().BoolVar(&daemonStop, "stop", false, "Stop a running daemon")
	daemonCmd.Flags().BoolVar(&daemonStatus, "status", false, "Show daemon status")
	rootCmd.AddCommand(daemonCmd)
}

func pidPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "taskctl", "taskctl.pid")
}

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.taskctl.daemon.plist")
}

func runDaemon(_ *cobra.Command, _ []string) error {
	if daemonStop {
		return stopDaemon()
	}
	if daemonStatus {
		return showDaemonStatus()
	}
	if daemonInstall {
		return installLaunchAgent()
	}
	return startDaemonLoop()
}

func startDaemonLoop() error {
	// write PID
	if err := os.MkdirAll(filepath.Dir(pidPath()), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(pidPath(), fmt.Appendf(nil, "%d", os.Getpid()), 0644); err != nil {
		return err
	}
	defer os.Remove(pidPath())

	fmt.Printf("taskctl daemon started (PID %d, interval %dm)\n", os.Getpid(), daemonInterval)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	ticker := time.NewTicker(time.Duration(daemonInterval) * time.Minute)
	defer ticker.Stop()

	// sync immediately on start
	syncOnce(ctx)

	for {
		select {
		case <-ticker.C:
			syncOnce(ctx)
		case <-sig:
			fmt.Println("taskctl daemon stopped")
			return nil
		}
	}
}

func syncOnce(ctx context.Context) {
	tasks, err := reminders.FetchTasks("")
	if err != nil {
		return
	}

	s, err := store.New(config.DBPath())
	if err != nil {
		return
	}
	defer s.Close()

	_ = s.DeleteBySource(ctx, "apple")
	s.OverrideWithPendingStatus(ctx, tasks)
	for i := range tasks {
		if s.IsPendingDelete(ctx, tasks[i].Title, tasks[i].List) {
			continue
		}
		_ = s.UpsertTask(ctx, &tasks[i])
	}
	_ = s.RemoveShadowedLocal(ctx)
	_ = s.PrunePendingDeletes(ctx)
	_ = s.PrunePendingStatus(ctx)

	// update list cache
	if entries, err := reminders.ListListsWithAccounts(); err == nil && len(entries) > 0 {
		_ = s.StoreListEntries(ctx, entries, "apple")
	}

	reminders.NotifyDueTasks(tasks)
}

func stopDaemon() error {
	data, err := os.ReadFile(pidPath())
	if err != nil {
		return fmt.Errorf("no daemon running (no PID file)")
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return fmt.Errorf("invalid PID file")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found", pid)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("could not stop PID %d: %w", pid, err)
	}
	fmt.Printf("Sent SIGTERM to daemon (PID %d)\n", pid)
	return nil
}

func showDaemonStatus() error {
	data, err := os.ReadFile(pidPath())
	if err != nil {
		fmt.Println("Daemon: not running")
		return nil
	}
	var pid int
	fmt.Sscanf(string(data), "%d", &pid)
	proc, err := os.FindProcess(pid)
	if err != nil || proc.Signal(syscall.Signal(0)) != nil {
		fmt.Println("Daemon: not running (stale PID file)")
		os.Remove(pidPath())
		return nil
	}
	fmt.Printf("Daemon: running (PID %d)\n", pid)
	return nil
}

func installLaunchAgent() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.taskctl.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>daemon</string>
        <string>--interval</string>
        <string>%d</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s/taskctl-daemon.log</string>
    <key>StandardErrorPath</key>
    <string>%s/taskctl-daemon.log</string>
</dict>
</plist>
`, self, daemonInterval, logsDir(), logsDir())

	logD := logsDir()
	if err := os.MkdirAll(logD, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(launchAgentPath()), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(launchAgentPath(), []byte(plist), 0644); err != nil {
		return err
	}
	fmt.Printf("LaunchAgent installed: %s\n", launchAgentPath())
	fmt.Println("To activate: launchctl load", launchAgentPath())
	return nil
}

func logsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "taskctl")
}
