package cmd

import (
	"os"

	"github.com/aeon022/missionctl-core/doctor"
	"github.com/aeon022/taskctl/internal/config"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check config, database, and Reminders.app health",
	Run: func(cmd *cobra.Command, args []string) {
		checks := []doctor.Check{
			doctor.CheckSQLite("Database", config.DBPath(), "tasks"),
			doctor.CheckDataDir("Data directory", config.DBPath(), config.Shared()),
			doctor.CheckAppleApp("Reminders.app", "Reminders"),
		}
		if !doctor.PrintReport(checks) {
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
