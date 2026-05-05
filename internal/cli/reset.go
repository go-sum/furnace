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

	_ = exec.Command("systemctl", "stop", "furnace-worker").Run()
	fmt.Println("stopped  furnace-worker")

	_ = exec.Command("systemctl", "disable", "furnace-worker").Run()
	fmt.Println("disabled furnace-worker")

	if err := os.Remove("/etc/systemd/system/furnace-worker.service"); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("warn: remove service file: %v\n", err)
	}
	fmt.Println("removed  /etc/systemd/system/furnace-worker.service")

	_ = systemctl("daemon-reload")
	fmt.Println("reloaded systemd daemon")

	if err := os.Remove("/etc/furnace/registry-token.cred"); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("warn: remove registry credential: %v\n", err)
	}
	fmt.Println("removed  /etc/furnace/registry-token.cred")

	if err := os.RemoveAll("/var/lib/furnace/.docker"); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("warn: remove docker config: %v\n", err)
	}
	fmt.Println("removed  /var/lib/furnace/.docker")

	_ = exec.Command("docker", "compose", "-f", "/srv/furnace/proxy/compose.yml", "down").Run()
	fmt.Println("stopped  proxy")

	_ = exec.Command("docker", "network", "rm", "caddy_net").Run()
	fmt.Println("removed  docker network caddy_net")

	_ = os.Remove("/usr/local/share/ca-certificates/furnace-ca.crt")
	_ = exec.Command("update-ca-certificates").Run()
	fmt.Println("removed  system CA")

	for _, dir := range []string{"/etc/furnace", "/var/lib/furnace", "/srv/apps", "/srv/furnace"} {
		_ = os.RemoveAll(dir)
		fmt.Printf("removed  %s\n", dir)
	}

	_ = exec.Command("userdel", "furnace").Run()
	fmt.Println("removed  user furnace")

	fmt.Println("furnace reset complete")
	return nil
}
