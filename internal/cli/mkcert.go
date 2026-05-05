package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/internal/app"
	"github.com/go-sum/furnace/internal/certgen"
)

const (
	caCertPath     = "/var/lib/furnace/ca/ca.pem"
	caKeyPath      = "/var/lib/furnace/ca/ca-key.pem"
	serverCertPath = "/srv/furnace/certs/local.pem"
	serverKeyPath  = "/srv/furnace/certs/local-key.pem"
	systemCADest   = "/usr/local/share/ca-certificates/furnace-ca.crt"
)

func newMkcertCmd(configPath *string) *cobra.Command {
	var install bool

	cmd := &cobra.Command{
		Use:   "mkcert [app-name...]",
		Short: "Generate TLS certificates for local/staging",
		RunE: func(cmd *cobra.Command, args []string) error {
			if install {
				return runMkcertInstall()
			}
			return runMkcertGenerate(*configPath, args)
		},
	}

	cmd.Flags().BoolVar(&install, "install", false, "generate CA and install to system trust store")
	return cmd
}

func runMkcertInstall() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("furnace mkcert --install requires root privileges (run with sudo)")
	}

	if _, err := os.Stat(caCertPath); err == nil {
		fmt.Println("exists   CA at", caCertPath)
		return nil
	}

	ca, err := certgen.GenerateCA()
	if err != nil {
		return fmt.Errorf("generate CA: %w", err)
	}

	if err := certgen.WriteCA(ca, caCertPath, caKeyPath); err != nil {
		return fmt.Errorf("write CA: %w", err)
	}
	fmt.Printf("wrote    %s\n", caCertPath)
	fmt.Printf("wrote    %s\n", caKeyPath)

	if err := copyFile(caCertPath, systemCADest, 0644); err != nil {
		return fmt.Errorf("install CA: %w", err)
	}
	fmt.Printf("installed %s\n", systemCADest)

	c := exec.Command("update-ca-certificates")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("update-ca-certificates: %w", err)
	}

	return nil
}

func runMkcertGenerate(configPath string, args []string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("furnace mkcert requires root privileges (run with sudo)")
	}

	ca, err := certgen.LoadCA(caCertPath, caKeyPath)
	if err != nil {
		return fmt.Errorf("load CA: run 'furnace mkcert --install' first")
	}

	cfg, err := app.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	var domains []string
	if len(args) == 0 {
		names := make([]string, 0, len(cfg.Apps))
		for name := range cfg.Apps {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			appCfg, _ := cfg.AppConfig(name)
			if appCfg.TLS {
				domains = append(domains, appCfg.Domain)
			}
		}
	} else {
		for _, name := range args {
			appCfg, ok := cfg.AppConfig(name)
			if !ok {
				return fmt.Errorf("app %q not found in config", name)
			}
			if appCfg.TLS {
				domains = append(domains, appCfg.Domain)
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

	if err := os.MkdirAll("/srv/furnace/certs", 0755); err != nil {
		return fmt.Errorf("create certs dir: %w", err)
	}
	if err := os.WriteFile(serverCertPath, certPEM, 0644); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(serverKeyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}

	fmt.Printf("wrote    %s\n", serverCertPath)
	fmt.Printf("wrote    %s\n", serverKeyPath)
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
