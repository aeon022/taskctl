package cmd

import (
	"github.com/aeon022/taskctl/internal/mcpserver"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:    "mcp",
	Short:  "Start MCP server for Claude Code integration",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return mcpserver.Serve()
	},
}

func init() { rootCmd.AddCommand(mcpCmd) }
