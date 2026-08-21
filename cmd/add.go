package cmd

import (
	"fmt"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/nlpdate"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/aeon022/taskctl/internal/tasks"
	"github.com/spf13/cobra"
)

var (
	addList  string
	addDue   string
	addNotes string
	addURL   string
)

var addCmd = &cobra.Command{
	Use:     "add <title>",
	Short:   "Create a new task in Apple Reminders",
	Example: `  taskctl add "Call dentist" --due 2026-07-05 --list Privat`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var due *time.Time
		if addDue != "" {
			d, err := nlpdate.Parse(addDue)
			if err != nil {
				return err
			}
			due = d
		}

		s, err := store.New(config.DBPath(), config.Shared())
		if err == nil {
			defer s.Close()
		}

		t, err := tasks.Create(s, args[0], addList, addNotes, addURL, due)
		if err != nil {
			return fmt.Errorf("create: %w", err)
		}

		if isJSON() {
			outputJSON(map[string]any{"tool": "taskctl", "command": "add", "status": "created", "task": t})
			return nil
		}
		dueOut := ""
		if t.DueDate != nil {
			dueOut = "  due " + t.DueDate.Format("Mon, Jan 02 2006")
		}
		fmt.Printf("Created: %s%s\n", t.Title, dueOut)
		return nil
	},
}

func init() {
	addCmd.Flags().StringVar(&addList, "list", "", "Reminder list (default: config default_list, else system default)")
	addCmd.Flags().StringVar(&addDue, "due", "", "Due date (YYYY-MM-DD)")
	addCmd.Flags().StringVar(&addNotes, "notes", "", "Notes")
	addCmd.Flags().StringVar(&addURL, "url", "", "URL")
	rootCmd.AddCommand(addCmd)
}
