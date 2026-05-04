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

type dirOwner int

const (
	ownerRoot        dirOwner = iota // root:root
	ownerFurnace                     // furnace:furnace
	ownerRootFurnace                 // root:furnace — readable by furnace group, written by root
)

type dirSpec struct {
	path  string
	owner dirOwner
	mode  os.FileMode
}

var managedDirs = []dirSpec{
	{"/etc/furnace",       ownerRootFurnace, 0750},
	{"/var/lib/furnace",   ownerFurnace,     0755},
	{"/srv/apps",          ownerFurnace,     0755},
	{"/srv/furnace/proxy", ownerFurnace,     0755},
	{"/srv/furnace/certs", ownerFurnace,     0755},
}

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

	for _, d := range managedDirs {
		if err := ensureManagedDir(d, uid, gid); err != nil {
			return err
		}
	}

	if err := ensureConfigScaffold(gid); err != nil {
		return fmt.Errorf("config scaffold: %w", err)
	}

	if err := ensureDockerNetwork("caddy_net"); err != nil {
		return fmt.Errorf("docker network: %w", err)
	}

	return nil
}

func ensureManagedDir(d dirSpec, uid, gid int) error {
	created, err := ensureDir(d.path, d.mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", d.path, err)
	}
	if created {
		fmt.Printf("created  %s\n", d.path)
	} else {
		fmt.Printf("exists   %s\n", d.path)
	}

	var duid, dgid int
	switch d.owner {
	case ownerFurnace:
		duid, dgid = uid, gid
	case ownerRootFurnace:
		duid, dgid = 0, gid
	default: // ownerRoot
		duid, dgid = 0, 0
	}
	if err := os.Chown(d.path, duid, dgid); err != nil {
		return fmt.Errorf("chown %s: %w", d.path, err)
	}
	if err := os.Chmod(d.path, d.mode); err != nil {
		return fmt.Errorf("chmod %s: %w", d.path, err)
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

func ensureDir(path string, mode os.FileMode) (created bool, err error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	return true, os.MkdirAll(path, mode)
}

func ensureConfigScaffold(gid int) error {
	const configPath = "/etc/furnace/furnace.yaml"
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("exists   %s (not overwritten)\n", configPath)
		_ = os.Chown(configPath, 0, gid)
		return nil
	}
	if err := os.WriteFile(configPath, furnaceconfig.ExampleConfig, 0640); err != nil {
		return err
	}
	if err := os.Chown(configPath, 0, gid); err != nil {
		return fmt.Errorf("chown config: %w", err)
	}
	fmt.Printf("created  %s\n", configPath)
	return nil
}

func ensureDockerNetwork(name string) error {
	out, err := exec.Command("docker", "network", "create", name).CombinedOutput()
	if err == nil {
		fmt.Printf("created  docker network %s\n", name)
		return nil
	}
	if strings.Contains(string(out), "already exists") {
		fmt.Printf("exists   docker network %s\n", name)
		return nil
	}
	return fmt.Errorf("create network %s: %w\n%s", name, err, out)
}
