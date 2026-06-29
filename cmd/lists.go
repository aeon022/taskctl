package cmd

import (
	"fmt"

	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/spf13/cobra"
)

var listsCmd = &cobra.Command{
	Use:   "lists",
	Short: "List all Apple Reminders list names",
	RunE: func(cmd *cobra.Command, args []string) error {
		lists, err := reminders.ListLists()
		if err != nil {
			return err
		}
		if isJSON() {
			outputJSON(map[string]any{"tool": "taskctl", "command": "lists", "data": lists})
			return nil
		}
		for _, l := range lists {
			fmt.Println(l)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(listsCmd) }
