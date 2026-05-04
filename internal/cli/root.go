package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X github.com/go-sum/furnace/internal/cli.Version=vX.Y.Z".
var Version = "dev"

func NewRootCommand() *cobra.Command {
	var configPath string

	root := &cobra.Command{
		Use:          "furnace",
		Short:        "Secure VPS deployment agent",
		Long:         fmt.Sprintf("Secure VPS deployment agent %s", Version),
		Version:      Version,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&configPath, "config", "/etc/furnace/furnace.yaml",
		"path to configuration file")

	root.AddCommand(
		newServeCmd(&configPath),
		newInitCmd(),
		newSystemdCmd(&configPath),
		newProxyCmd(&configPath),
		newUpdateCmd(),
	)

	return root
}
