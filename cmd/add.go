package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/nlpdate"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	addList  string
	addDue   string
	addNotes string
)

var addCmd = &cobra.Command{
	Use:     "add <title>",
	Short:   "Create a new task in Apple Reminders",
	Example: `  taskctl add "Call dentist" --due 2026-07-05 --list Privat`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		t := &models.Task{
			ID:        "taskctl-" + uuid.New().String(),
			Title:     args[0],
			List:      addList,
			Notes:     addNotes,
			Status:    "needsAction",
			Source:    "taskctl",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if addDue != "" {
			d, err := nlpdate.Parse(addDue)
			if err != nil {
				return err
			}
			t.DueDate = d
		}

		ctx := context.Background()
		s, err := store.New(config.DBPath())
		if err == nil {
			defer s.Close()
			_ = s.ClearPendingDelete(ctx, t.Title, t.List)
			_ = s.UpsertTask(ctx, t)
		}

		if err := reminders.CreateTask(t); err != nil {
			return fmt.Errorf("create: %w", err)
		}

		if isJSON() {
			outputJSON(map[string]any{"tool": "taskctl", "command": "add", "status": "created", "task": t})
			return nil
		}
		due := ""
		if t.DueDate != nil {
			due = "  due " + t.DueDate.Format("Mon, Jan 02 2006")
		}
		fmt.Printf("Created: %s%s\n", t.Title, due)
		return nil
	},
}

func init() {
	addCmd.Flags().StringVar(&addList, "list", "", "Reminder list (default: system default)")
	addCmd.Flags().StringVar(&addDue, "due", "", "Due date (YYYY-MM-DD)")
	addCmd.Flags().StringVar(&addNotes, "notes", "", "Notes")
	rootCmd.AddCommand(addCmd)
}
