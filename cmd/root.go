package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/tui"
	"github.com/spf13/cobra"
)

var flagJSON bool
var flagOpenTask string

var rootCmd = &cobra.Command{
	Use:   "taskctl",
	Short: "Manage Apple Reminders from your terminal",
	Long:  "taskctl — local-first task manager. Syncs with Apple Reminders via EventKit.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.Run(flagOpenTask)
	},
}

func Execute() {
	if err := config.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
	}
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output as JSON")
	rootCmd.Flags().StringVar(&flagOpenTask, "task", "", "Open directly on this task's detail view (by id) — for jumping in from another tool's linked entry")
}

func isJSON() bool { return flagJSON }

func outputJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
