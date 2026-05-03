package cli

import "github.com/spf13/cobra"

func NewRootCommand() *cobra.Command {
	var configPath string

	root := &cobra.Command{
		Use:          "furnace",
		Short:        "Secure VPS deployment agent",
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
