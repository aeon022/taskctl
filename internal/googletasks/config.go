package googletasks

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type GoogleConfig struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
}

// ExportConfig returns the Google Tasks config (for use in cmd package).
func ExportConfig() GoogleConfig { return loadGoogleConfig() }

func loadGoogleConfig() GoogleConfig {
	// support env vars as override
	clientID := os.Getenv("GOOGLE_TASKS_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_TASKS_CLIENT_SECRET")
	if clientID != "" && clientSecret != "" {
		return GoogleConfig{ClientID: clientID, ClientSecret: clientSecret}
	}

	home, _ := os.UserHomeDir()
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(filepath.Join(home, ".config", "taskctl"))
	_ = viper.ReadInConfig()

	var cfg struct {
		GoogleTasks GoogleConfig `mapstructure:"google_tasks"`
	}
	_ = viper.Unmarshal(&cfg)
	return cfg.GoogleTasks
}
