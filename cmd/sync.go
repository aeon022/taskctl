package cmd

import (
	"context"
	"fmt"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/googletasks"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/spf13/cobra"
)

var syncList string

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync tasks from Apple Reminders (and Google Tasks if configured)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		// ── Apple Reminders ───────────────────────────────────────────────
		appleTasks, err := reminders.FetchTasks(syncList)
		if err != nil {
			return fmt.Errorf("fetch apple: %w", err)
		}

		s, err := store.New(config.DBPath())
		if err != nil {
			return err
		}
		defer s.Close()

		_ = s.DeleteBySource(ctx, "apple")
		s.OverrideWithPendingStatus(ctx, appleTasks)
		for i := range appleTasks {
			if s.IsPendingDelete(ctx, appleTasks[i].Title, appleTasks[i].List) {
				continue
			}
			if err := s.UpsertTask(ctx, &appleTasks[i]); err != nil {
				return fmt.Errorf("upsert: %w", err)
			}
		}

		// update Apple list cache
		if entries, err := reminders.ListListsWithAccounts(); err == nil && len(entries) > 0 {
			_ = s.StoreListEntries(ctx, entries, "apple")
		}

		total := len(appleTasks)

		// ── Google Tasks (optional) ───────────────────────────────────────
		var googleCount int
		if syncList == "" && googletasks.IsConfigured() && googletasks.IsAuthenticated() {
			googleTasks, err := googletasks.FetchTasks(ctx)
			if err != nil {
				fmt.Printf("Warning: Google Tasks sync failed: %v\n", err)
			} else {
				_ = s.DeleteBySource(ctx, "google")
				s.OverrideWithPendingStatus(ctx, googleTasks)
				for i := range googleTasks {
					if s.IsPendingDelete(ctx, googleTasks[i].Title, googleTasks[i].List) {
						continue
					}
					_ = s.UpsertTask(ctx, &googleTasks[i])
				}
				// update Google list cache
				if lists, err := googletasks.ListTaskLists(ctx); err == nil && len(lists) > 0 {
					_ = s.StoreListEntries(ctx, lists, "google")
				}
				googleCount = len(googleTasks)
				total += googleCount
			}
		}

		_ = s.RemoveShadowedLocal(ctx)
		_ = s.PrunePendingDeletes(ctx)
		_ = s.PrunePendingStatus(ctx)

		reminders.NotifyDueTasks(appleTasks)

		if isJSON() {
			out := map[string]any{
				"tool":    "taskctl",
				"command": "sync",
				"synced":  total,
				"apple":   len(appleTasks),
			}
			if googleCount > 0 {
				out["google"] = googleCount
			}
			outputJSON(out)
			return nil
		}
		if googleCount > 0 {
			fmt.Printf("Synced %d tasks (%d Apple, %d Google)\n", total, len(appleTasks), googleCount)
		} else {
			fmt.Printf("Synced %d tasks\n", len(appleTasks))
		}
		return nil
	},
}

func init() {
	syncCmd.Flags().StringVar(&syncList, "list", "", "Sync only this list (Apple only)")
	rootCmd.AddCommand(syncCmd)
}
