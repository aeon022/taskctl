package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/spf13/cobra"
)

var remindCmd = &cobra.Command{
	Use:   "remind",
	Short: "Send a macOS notification for tasks due today or overdue",
	Long: `Check which open tasks are due today or overdue and send a
macOS notification. Same pattern habctl's own "remind" uses — ideal as
a launchd job, e.g. once each morning.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return err
		}
		defer s.Close()

		tasks, err := s.ListTasks(context.Background(), store.ListFilter{Status: "needsAction"})
		if err != nil {
			return err
		}

		eod := endOfDay(time.Now())
		sod := startOfDay(time.Now())
		var due, overdue []string
		for _, t := range tasks {
			if t.DueDate == nil || t.DueDate.After(eod) {
				continue
			}
			if t.DueDate.Before(sod) {
				overdue = append(overdue, t.Title)
			} else {
				due = append(due, t.Title)
			}
		}

		if len(due) == 0 && len(overdue) == 0 {
			fmt.Println("No tasks due today or overdue — nothing to remind.")
			return nil
		}

		title := fmt.Sprintf("%d task(s) due today", len(due)+len(overdue))
		if len(overdue) > 0 {
			title = fmt.Sprintf("%d due today, %d overdue", len(due), len(overdue))
		}
		body := strings.Join(append(overdue, due...), ", ")

		script := fmt.Sprintf(`display notification %q with title %q`, body, title)
		out, err := exec.Command("osascript", "-e", script).CombinedOutput()
		if err != nil {
			fmt.Printf("Reminder: %s — %s\n", title, body)
			if len(out) > 0 {
				fmt.Printf("osascript: %s\n", strings.TrimSpace(string(out)))
			}
			return nil
		}

		fmt.Printf("Notified: %s\n", body)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(remindCmd)
}
