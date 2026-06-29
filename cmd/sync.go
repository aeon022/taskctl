package cmd

import (
	"context"
	"fmt"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/spf13/cobra"
)

var syncList string

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync tasks from Apple Reminders into local cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		tasks, err := reminders.FetchTasks(syncList)
		if err != nil {
			return fmt.Errorf("fetch: %w", err)
		}

		s, err := store.New(config.DBPath())
		if err != nil {
			return err
		}
		defer s.Close()
		ctx := context.Background()

		// replace apple-sourced tasks for the synced scope
		_ = s.DeleteBySource(ctx, "apple")

		for i := range tasks {
			if err := s.UpsertTask(ctx, &tasks[i]); err != nil {
				return fmt.Errorf("upsert: %w", err)
			}
		}

		if isJSON() {
			outputJSON(map[string]any{
				"tool":    "taskctl",
				"command": "sync",
				"synced":  len(tasks),
			})
			return nil
		}
		fmt.Printf("Synced %d tasks\n", len(tasks))
		return nil
	},
}

func init() {
	syncCmd.Flags().StringVar(&syncList, "list", "", "Sync only this list")
	rootCmd.AddCommand(syncCmd)
}
