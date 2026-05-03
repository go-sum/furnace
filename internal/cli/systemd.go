package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/deploy"
	"github.com/go-sum/furnace/internal/app"
)

func newSystemdCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "systemd",
		Short: "Manage the furnace systemd service",
	}
	cmd.AddCommand(
		newSystemdStartCmd(),
		newSystemdHealthCmd(configPath),
		newSystemdStatusCmd(),
	)
	return cmd
}

func newSystemdStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Install and start the furnace systemd service (requires root)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if os.Geteuid() != 0 {
				return fmt.Errorf("furnace systemd start requires root privileges (run with sudo)")
			}
			const unitPath = "/etc/systemd/system/furnace.service"
			if err := os.WriteFile(unitPath, deploy.SystemdUnit, 0644); err != nil {
				return fmt.Errorf("write unit file: %w", err)
			}
			fmt.Printf("wrote %s\n", unitPath)
			if err := runCmd("systemctl", "daemon-reload"); err != nil {
				return fmt.Errorf("daemon-reload: %w", err)
			}
			if err := runCmd("systemctl", "enable", "--now", "furnace"); err != nil {
				return fmt.Errorf("enable furnace: %w", err)
			}
			fmt.Println("furnace service enabled and started")
			return nil
		},
	}
}

func newSystemdHealthCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check furnace HTTP health endpoint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr := "127.0.0.1:8080"
			if cfg, err := app.LoadConfig(*configPath); err == nil {
				addr = cfg.Listen
			}
			resp, err := http.Get("http://" + addr + "/v1/health")
			if err != nil {
				return fmt.Errorf("health check failed: %w", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("%s\n", body)
			return nil
		},
	}
}

func newSystemdStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show furnace systemd service status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := exec.Command("systemctl", "status", "furnace")
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			_ = c.Run()
			return nil
		},
	}
}

func runCmd(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
