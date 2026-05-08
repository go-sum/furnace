package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/internal/certgen"
	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/storage"
)

func newMkcertCmd() *cobra.Command {
	var install bool

	cmd := &cobra.Command{
		Use:   "mkcert [app-name...]",
		Short: "Generate TLS certificates for local/staging",
		RunE: func(cmd *cobra.Command, args []string) error {
			if install {
				return runMkcertInstall()
			}
			return runMkcertGenerate(cmd.Context(), args)
		},
	}

	cmd.Flags().BoolVar(&install, "install", false, "generate CA and install to system trust store")
	return cmd
}

func runMkcertInstall() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("furnace mkcert --install requires root privileges (run with sudo)")
	}

	if _, err := os.Stat(CACertPath); err == nil {
		fmt.Println("exists   CA at", CACertPath)
		return nil
	}

	ca, err := certgen.GenerateCA()
	if err != nil {
		return fmt.Errorf("generate CA: %w", err)
	}

	if err := certgen.WriteCA(ca, CACertPath, CAKeyPath); err != nil {
		return fmt.Errorf("write CA: %w", err)
	}
	fmt.Printf("wrote    %s\n", CACertPath)
	fmt.Printf("wrote    %s\n", CAKeyPath)

	if err := copyFile(CACertPath, SystemCADest, 0644); err != nil {
		return fmt.Errorf("install CA: %w", err)
	}
	fmt.Printf("installed %s\n", SystemCADest)

	c := exec.Command("update-ca-certificates")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("update-ca-certificates: %w", err)
	}

	return nil
}

func runMkcertGenerate(ctx context.Context, args []string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("furnace mkcert requires root privileges (run with sudo)")
	}

	ca, err := certgen.LoadCA(CACertPath, CAKeyPath)
	if err != nil {
		return fmt.Errorf("load CA: run 'furnace mkcert --install' first")
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	db, err := storage.OpenDB(DBPath, true, logger)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	appStore := storage.NewSQLiteAppStore(db, logger)
	appList, err := appStore.ListApps(ctx)
	if err != nil {
		return fmt.Errorf("list apps: %w", err)
	}

	var domains []string
	if len(args) == 0 {
		// appList is already sorted by name (ORDER BY name in ListApps)
		for _, a := range appList {
			if a.TLS {
				domains = append(domains, a.Domain)
			}
		}
	} else {
		appMap := make(map[string]model.AppConfig, len(appList))
		for _, a := range appList {
			appMap[a.Name] = a
		}
		for _, name := range args {
			a, ok := appMap[name]
			if !ok {
				return fmt.Errorf("app %q not found in config", name)
			}
			if a.TLS {
				domains = append(domains, a.Domain)
			}
		}
	}

	if len(domains) == 0 {
		fmt.Println("no certs created")
		return nil
	}

	certPEM, keyPEM, err := certgen.GenerateServerCert(ca, domains)
	if err != nil {
		return fmt.Errorf("generate server cert: %w", err)
	}

	if err := os.MkdirAll(CertsDir, 0755); err != nil {
		return fmt.Errorf("create certs dir: %w", err)
	}
	if err := os.WriteFile(ServerCertPath, certPEM, 0644); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(ServerKeyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}

	fmt.Printf("wrote    %s\n", ServerCertPath)
	fmt.Printf("wrote    %s\n", ServerKeyPath)
	fmt.Println("domains covered:")
	for _, d := range domains {
		fmt.Printf("  %s\n", d)
	}

	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
