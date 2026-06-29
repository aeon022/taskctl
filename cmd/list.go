package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	listList     string
	listAll      bool
	listToday    bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks from local cache",
	Example: `  taskctl list
  taskctl list --list Work
  taskctl list --today
  taskctl list --all
  taskctl list --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.New(config.DBPath())
		if err != nil {
			return err
		}
		defer s.Close()

		status := "needsAction"
		if listAll {
			status = ""
		}
		tasks, err := s.ListTasks(context.Background(), store.ListFilter{
			List:   listList,
			Status: status,
		})
		if err != nil {
			return err
		}

		if listToday {
			today := time.Now()
			filtered := tasks[:0]
			for _, t := range tasks {
				if t.DueDate != nil && !t.DueDate.After(endOfDay(today)) {
					filtered = append(filtered, t)
				}
			}
			tasks = filtered
		}

		if isJSON() {
			outputJSON(map[string]any{
				"tool":    "taskctl",
				"command": "list",
				"count":   len(tasks),
				"data":    tasks,
			})
			return nil
		}

		if len(tasks) == 0 {
			fmt.Println("No tasks found. Run: taskctl sync")
			return nil
		}

		curList := ""
		for _, t := range tasks {
			if t.List != curList {
				if curList != "" {
					fmt.Println()
				}
				curList = t.List
				fmt.Printf("%s\n%s\n", curList, repeat("─", len(curList)+2))
			}
			mark := "○"
			if t.Done() {
				mark = "✓"
			}
			due := ""
			if t.DueDate != nil {
				due = "  due " + t.DueDate.Format("Mon Jan 02")
			}
			fmt.Printf("  %s  %s%s\n", mark, t.Title, due)
		}
		return nil
	},
}

func init() {
	listCmd.Flags().StringVar(&listList, "list", "", "Filter by list name")
	listCmd.Flags().BoolVar(&listAll, "all", false, "Include completed tasks")
	listCmd.Flags().BoolVar(&listToday, "today", false, "Only tasks due today or overdue")
	rootCmd.AddCommand(listCmd)
}

func endOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, 0, t.Location())
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
