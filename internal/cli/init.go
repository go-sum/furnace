package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/internal/storage"
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
	{CredDir, ownerRootFurnace, 0750},
	{DataDir, ownerFurnace, 0755},
	{AppsDir, ownerFurnace, 0755},
	{ProxyDir, ownerFurnace, 0755},
	{CertsDir, ownerFurnace, 0755},
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize the VPS for furnace (requires root)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd.Context())
		},
	}
}

func runInit(ctx context.Context) error {
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

	if err := ensureDockerGroup(); err != nil {
		return fmt.Errorf("docker group: %w", err)
	}

	if err := ensureDockerNetwork("caddy_net"); err != nil {
		return fmt.Errorf("docker network: %w", err)
	}

	if err := ensureSQLiteFiles(DBPath, uid, gid); err != nil {
		return fmt.Errorf("sqlite files: %w", err)
	}

	if err := ensureDBSeed(ctx, DBPath, slog.Default()); err != nil {
		return fmt.Errorf("db seed: %w", err)
	}

	return nil
}

func ensureDBSeed(ctx context.Context, dbPath string, logger *slog.Logger) error {
	db, err := storage.OpenDB(dbPath, false, logger)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()
	return storage.SeedIfEmpty(ctx, db, DataDir, AppsDir)
}

// ensureSQLiteFiles pre-creates the three SQLite WAL-mode files so that
// Docker bind-mounts them as files rather than directories when the web
// container starts for the first time.
func ensureSQLiteFiles(dbPath string, uid, gid int) error {
	paths := []string{dbPath, dbPath + "-wal", dbPath + "-shm"}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			fmt.Printf("exists   %s\n", p)
			continue
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return fmt.Errorf("create %s: %w", p, err)
		}
		f.Close()
		if err := os.Chown(p, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", p, err)
		}
		fmt.Printf("created  %s\n", p)
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
	fmt.Println("created  user furnace")
	return nil
}

func ensureDockerGroup() error {
	if err := exec.Command("usermod", "-aG", "docker", "furnace").Run(); err != nil {
		return fmt.Errorf("usermod: %w", err)
	}
	fmt.Println("added    furnace to docker group")
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
	return fmt.Errorf("create docker network %s: %w\n%s", name, err, out)
}

func ensureDir(path string, mode os.FileMode) (created bool, err error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	return true, os.MkdirAll(path, mode)
}
