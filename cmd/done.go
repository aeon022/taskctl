package cmd

import (
	"fmt"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/aeon022/taskctl/internal/tasks"
	"github.com/spf13/cobra"
)

var doneList string

var doneCmd = &cobra.Command{
	Use:   "done <title>",
	Short: "Mark a task as completed",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		title := args[0]

		s, err := store.New(config.DBPath(), config.Shared())
		if err == nil {
			defer s.Close()
		}

		if err := tasks.Complete(s, title, doneList); err != nil {
			return fmt.Errorf("complete: %w", err)
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
