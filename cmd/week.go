package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/spf13/cobra"
)

var weekCmd = &cobra.Command{
	Use:   "week",
	Short: "Show tasks due this week (Mon–Sun)",
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

		mon, sun := weekRange()
		curList := ""

		if isJSON() {
			var out []any
			for _, t := range tasks {
				if t.DueDate == nil || t.DueDate.Before(mon) || t.DueDate.After(sun) {
					continue
				}
				out = append(out, t)
			}
			outputJSON(map[string]any{"tool": "taskctl", "command": "week",
				"from": mon.Format("2006-01-02"), "to": sun.Format("2006-01-02"),
				"count": len(out), "data": out})
			return nil
		}

		fmt.Printf("Week %s – %s\n\n", mon.Format("Mon Jan 02"), sun.Format("Mon Jan 02"))
		found := false
		for _, t := range tasks {
			if t.DueDate == nil || t.DueDate.Before(mon) || t.DueDate.After(sun) {
				continue
			}
			found = true
			if t.List != curList {
				if curList != "" {
					fmt.Println()
				}
				curList = t.List
				fmt.Println(t.List)
				fmt.Println(repeat("─", len(t.List)+2))
			}
			due := ""
			if t.DueDate != nil {
				due = "  " + t.DueDate.Format("Mon Jan 02")
			}
			fmt.Printf("  ○  %s%s\n", t.Title, due)
		}
		if !found {
			fmt.Println("No tasks due this week.")
		}
		return nil
	},
}

func weekRange() (time.Time, time.Time) {
	now := time.Now()
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	mon := now.AddDate(0, 0, -(wd - 1))
	mon = time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, time.Local)
	sun := mon.AddDate(0, 0, 6)
	sun = time.Date(sun.Year(), sun.Month(), sun.Day(), 23, 59, 59, 0, time.Local)
	return mon, sun
}

func init() { rootCmd.AddCommand(weekCmd) }
