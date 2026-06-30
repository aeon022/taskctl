package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aeon022/taskctl/internal/googletasks"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth <provider>",
	Short: "Authenticate with a task provider",
	Long: `Authenticate with an external task provider.

Supported providers:
  google   — Google Tasks (requires client_id + client_secret in config)

Setup for Google Tasks:
  1. Go to https://console.cloud.google.com/
  2. Create a project → Enable "Tasks API"
  3. Create OAuth2 credentials (Desktop app)
  4. Add to ~/.config/taskctl/config.yaml:
       google_tasks:
         client_id: "YOUR_CLIENT_ID"
         client_secret: "YOUR_CLIENT_SECRET"
  5. Run: taskctl auth google`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"google"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "google":
			return authGoogle()
		default:
			return fmt.Errorf("unknown provider %q", args[0])
		}
	},
}

func init() {
	rootCmd.AddCommand(authCmd)
}

func authGoogle() error {
	if !googletasks.IsConfigured() {
		return fmt.Errorf(`Google Tasks not configured.

Add to ~/.config/taskctl/config.yaml:
  google_tasks:
    client_id: "YOUR_CLIENT_ID"
    client_secret: "YOUR_CLIENT_SECRET"

See: taskctl auth --help`)
	}

	cfg := googletasks.ExportConfig()
	url := googletasks.AuthURL(cfg.ClientID, cfg.ClientSecret)

	fmt.Println("Open this URL in your browser:")
	fmt.Println()
	fmt.Println(url)
	fmt.Println()
	fmt.Print("Paste the authorization code: ")

	reader := bufio.NewReader(os.Stdin)
	code, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("no code entered")
	}

	if err := googletasks.ExchangeCode(context.Background(), cfg.ClientID, cfg.ClientSecret, code); err != nil {
		return err
	}

	fmt.Println("\nAuthenticated successfully!")
	fmt.Println("Run 'taskctl sync' to import your Google Tasks.")
	return nil
}
