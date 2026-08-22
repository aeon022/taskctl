package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aeon022/missionctl-core/dateutil"
	"github.com/aeon022/taskctl/internal/ai"
	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/aeon022/taskctl/internal/tasks"
	"github.com/spf13/cobra"
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "AI review of this week's tasks, with follow-up suggestions you approve one by one",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return err
		}
		defer s.Close()

		ctx := context.Background()

		// Status "" (all) rather than week.go's "needsAction" — a review
		// needs completed tasks too, not just what's still open.
		all, err := s.ListTasks(ctx, store.ListFilter{Status: ""})
		if err != nil {
			return err
		}

		now := time.Now()
		mon, sun := dateutil.WeekRange(now)
		weekTasks := filterWeek(all, mon, sun)

		if len(weekTasks) == 0 {
			fmt.Println("No tasks this week to review.")
			return nil
		}

		suggestions, err := ai.ReviewWeek(ctx, weekTasks, now)
		if err != nil {
			return fmt.Errorf("AI review: %w", err)
		}
		if len(suggestions) == 0 {
			fmt.Println("The AI didn't find any follow-ups worth suggesting this week.")
			return nil
		}

		reader := bufio.NewReader(os.Stdin)
		var created []string
		for _, sug := range suggestions {
			fmt.Printf("\nSuggested: %s\n  Due:    %s\n  Reason: %s\n", sug.Title, sug.DueDate, sug.Reason)

			due, err := dateutil.ParseDateArg(sug.DueDate)
			if err != nil {
				fmt.Printf("  Skipping — %v\n", err)
				continue
			}

			fmt.Print("  Create this task? [y/N] ")
			line, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(line)) != "y" {
				fmt.Println("  Skipped.")
				continue
			}

			t, err := tasks.Create(s, sug.Title, "", "", "", &due)
			if err != nil {
				fmt.Printf("  Failed to create: %v\n", err)
				continue
			}
			created = append(created, t.Title)
			fmt.Println("  Created.")
		}

		fmt.Println()
		if len(created) == 0 {
			fmt.Println("No follow-up tasks created.")
		} else {
			fmt.Printf("Created %d follow-up task(s):\n", len(created))
			for _, title := range created {
				fmt.Printf("  - %s\n", title)
			}
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(reviewCmd) }

// filterWeek narrows tasks to those due within [mon, sun] — same
// date-range filter week.go and the week_tasks MCP handler use, just
// applied to all tasks (pending + completed) instead of needsAction only.
func filterWeek(all []models.Task, mon, sun time.Time) []models.Task {
	var week []models.Task
	for _, t := range all {
		if t.DueDate != nil && !t.DueDate.Before(mon) && !t.DueDate.After(sun) {
			week = append(week, t)
		}
	}
	return week
}
