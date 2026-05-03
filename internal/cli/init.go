package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	furnaceconfig "github.com/go-sum/furnace/config"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize the VPS for furnace (requires root)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit()
		},
	}
}

func runInit() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("furnace init requires root privileges (run with sudo)")
	}

	if err := ensureSystemUser(); err != nil {
		return fmt.Errorf("system user: %w", err)
	}

	furnaceUser, err := user.Lookup("furnace")
	if err != nil {
		return fmt.Errorf("lookup furnace user: %w", err)
	}
	uid, _ := strconv.Atoi(furnaceUser.Uid)
	gid, _ := strconv.Atoi(furnaceUser.Gid)

	dirs := []string{
		"/etc/furnace",
		"/var/lib/furnace",
		"/srv/apps",
		"/srv/furnace/proxy",
		"/opt/vps/certs",
	}
	ownedDirs := map[string]bool{
		"/var/lib/furnace": true,
		"/srv/apps":        true,
	}

	for _, dir := range dirs {
		created, err := ensureDir(dir)
		if err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if created {
			fmt.Printf("created  %s\n", dir)
		} else {
			fmt.Printf("exists   %s\n", dir)
		}
		if ownedDirs[dir] {
			if err := os.Chown(dir, uid, gid); err != nil {
				return fmt.Errorf("chown %s: %w", dir, err)
			}
		}
	}

	if err := ensureConfigScaffold(); err != nil {
		return fmt.Errorf("config scaffold: %w", err)
	}

	if err := ensureDockerNetwork("caddy_net"); err != nil {
		return fmt.Errorf("docker network: %w", err)
	}

	return nil
}

func ensureSystemUser() error {
	out, _ := exec.Command("id", "-u", "furnace").Output()
	if strings.TrimSpace(string(out)) != "" {
		fmt.Println("exists   user furnace")
		return nil
	}

	if err := exec.Command(
		"useradd", "--system", "--shell", "/usr/sbin/nologin",
		"--no-create-home", "furnace",
	).Run(); err != nil {
		return fmt.Errorf("useradd: %w", err)
	}
	if err := exec.Command("usermod", "-aG", "docker", "furnace").Run(); err != nil {
		return fmt.Errorf("usermod: %w", err)
	}
	fmt.Println("created  user furnace (docker group)")
	return nil
}

func ensureDir(path string) (created bool, err error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	return true, os.MkdirAll(path, 0755)
}

func ensureConfigScaffold() error {
	const configPath = "/etc/furnace/furnace.yaml"
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("exists   %s (not overwritten)\n", configPath)
		return nil
	}
	if err := os.WriteFile(configPath, furnaceconfig.ExampleConfig, 0640); err != nil {
		return err
	}
	fmt.Printf("created  %s\n", configPath)
	return nil
}

func ensureDockerNetwork(name string) error {
	out, err := exec.Command("docker", "network", "inspect", name).CombinedOutput()
	if err == nil {
		fmt.Printf("exists   docker network %s\n", name)
		return nil
	}
	if !strings.Contains(string(out), "No such network") {
		return fmt.Errorf("inspect network: %w", err)
	}
	if err := exec.Command("docker", "network", "create", name).Run(); err != nil {
		return fmt.Errorf("create network: %w", err)
	}
	fmt.Printf("created  docker network %s\n", name)
	return nil
}
