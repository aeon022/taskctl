package cmd

import (
	"context"
	"fmt"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/reminders"
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

		reminders.NotifyDueTasks(tasks)
		fmt.Println("Reminder check complete.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(remindCmd)
}
