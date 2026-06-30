package cmd

import (
	"context"
	"fmt"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/spf13/cobra"
)

var doneList string

var doneCmd = &cobra.Command{
	Use:   "done <title>",
	Short: "Mark a task as completed",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		title := args[0]
		t := &models.Task{Title: title, List: doneList}

		// write SQLite first, then call AppleScript
		ctx := context.Background()
		s, err := store.New(config.DBPath())
		if err == nil {
			defer s.Close()
			tasks, _ := s.ListTasks(ctx, store.ListFilter{List: doneList, Status: "needsAction"})
			for i := range tasks {
				if tasks[i].Title == title {
					tasks[i].Status = "completed"
					_ = s.UpsertTask(ctx, &tasks[i])
					break
				}
			}
			_ = s.AddPendingStatus(ctx, title, doneList, "completed")
		}

		if err := reminders.CompleteTask(t); err != nil {
			return fmt.Errorf("complete: %w", err)
		}

		if s != nil {
			_ = s.ClearPendingStatus(ctx, title, doneList)
		}

		if isJSON() {
			outputJSON(map[string]any{"tool": "taskctl", "command": "done", "title": title})
			return nil
		}
		fmt.Printf("Done: %s\n", title)
		return nil
	},
}

func init() {
	doneCmd.Flags().StringVar(&doneList, "list", "", "Reminder list to search in")
	rootCmd.AddCommand(doneCmd)
}
