package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/spf13/cobra"
)

var todayCmd = &cobra.Command{
	Use:   "today",
	Short: "Show tasks due today and overdue",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.New(config.DBPath())
		if err != nil {
			return err
		}
		defer s.Close()

		tasks, err := s.ListTasks(context.Background(), store.ListFilter{Status: "needsAction"})
		if err != nil {
			return err
		}

		eod := endOfDay(time.Now())
		var due, overdue []string
		curList := ""
		lines := []string{}

		for _, t := range tasks {
			if t.DueDate == nil || t.DueDate.After(eod) {
				continue
			}
			prefix := "○"
			if t.DueDate.Before(startOfDay(time.Now())) {
				prefix = "!"
				overdue = append(overdue, t.Title)
			} else {
				due = append(due, t.Title)
			}
			if t.List != curList {
				if curList != "" {
					lines = append(lines, "")
				}
				curList = t.List
				lines = append(lines, t.List)
				lines = append(lines, repeat("─", len(t.List)+2))
			}
			dueStr := ""
			if t.DueDate.Before(startOfDay(time.Now())) {
				dueStr = "  [overdue " + t.DueDate.Format("Jan 02") + "]"
			}
			lines = append(lines, fmt.Sprintf("  %s  %s%s", prefix, t.Title, dueStr))
		}

		if isJSON() {
			outputJSON(map[string]any{
				"tool": "taskctl", "command": "today",
				"overdue": len(overdue), "due_today": len(due),
				"data": tasks,
			})
			return nil
		}

		if len(lines) == 0 {
			fmt.Println("All clear for today.")
			return nil
		}
		for _, l := range lines {
			fmt.Println(l)
		}
		return nil
	},
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func init() { rootCmd.AddCommand(todayCmd) }
