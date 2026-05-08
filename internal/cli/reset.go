package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func newResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Remove all furnace state from the VPS (requires root)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReset()
		},
	}
}

func runReset() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("furnace reset requires root privileges (run with sudo)")
	}

	fmt.Print("This will remove all furnace data, services, and directories. Type 'yes' to confirm: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	if strings.TrimSpace(scanner.Text()) != "yes" {
		return fmt.Errorf("reset cancelled")
	}

	// Best-effort teardown: each step is independent and may already be absent.
	// Errors are non-fatal — the goal is to remove as much state as possible.
	_ = exec.Command("systemctl", "stop", "furnace-worker").Run()
	fmt.Println("stopped  furnace-worker")

	_ = exec.Command("systemctl", "disable", "furnace-worker").Run()
	fmt.Println("disabled furnace-worker")

	if err := os.Remove(WorkerUnitDest); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("warn: remove service file: %v\n", err)
	}
	fmt.Printf("removed  %s\n", WorkerUnitDest)

	_ = systemctl("daemon-reload")
	fmt.Println("reloaded systemd daemon")

	if err := os.Remove(CredPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("warn: remove registry credential: %v\n", err)
	}
	fmt.Printf("removed  %s\n", CredPath)

	if err := os.RemoveAll(DataDir + "/.docker"); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("warn: remove docker config: %v\n", err)
	}
	fmt.Printf("removed  %s/.docker\n", DataDir)

	_ = exec.Command("docker", "compose", "-f", ProxyDir+"/compose.yml", "down").Run()
	fmt.Println("stopped  proxy")

	_ = exec.Command("docker", "network", "rm", "caddy_net").Run()
	fmt.Println("removed  docker network caddy_net")

	_ = os.Remove(SystemCADest)
	_ = exec.Command("update-ca-certificates").Run()
	fmt.Println("removed  system CA")

	for _, dir := range []string{CredDir, DataDir, AppsDir, InfraDir} {
		_ = os.RemoveAll(dir)
		fmt.Printf("removed  %s\n", dir)
	}

	_ = exec.Command("userdel", "furnace").Run()
	fmt.Println("removed  user furnace")

	fmt.Println("furnace reset complete")
	return nil
}
