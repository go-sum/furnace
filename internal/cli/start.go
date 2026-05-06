package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/deploy"
	"github.com/go-sum/furnace/internal/app"
)

const (
	workerUnitDest = "/etc/systemd/system/furnace-worker.service"
	proxyDir       = "/srv/furnace/proxy"
)

func newStartCmd(configPath *string) *cobra.Command {
	var credentialStdin bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Install systemd unit, start proxy and worker (requires root)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if os.Geteuid() != 0 {
				return fmt.Errorf("furnace start requires root privileges (run with sudo)")
			}
			var credential string
			if credentialStdin {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("read credential from stdin: %w", err)
				}
				credential = strings.TrimSpace(string(data))
				if credential == "" {
					return fmt.Errorf("--credential-stdin provided but stdin was empty")
				}
			}
			return runStart(*configPath, credential)
		},
	}
	cmd.Flags().BoolVar(&credentialStdin, "credential-stdin", false, "read registry token from stdin")
	return cmd
}

func runStart(configPath, credential string) error {
	cfg, err := app.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := writeProxyFiles(cfg); err != nil {
		return fmt.Errorf("write proxy files: %w", err)
	}

	if err := dockerComposeUp(); err != nil {
		return fmt.Errorf("start proxy: %w", err)
	}

	if err := installWorker(credential); err != nil {
		return fmt.Errorf("install worker: %w", err)
	}

	fmt.Println("furnace started")
	fmt.Println("  proxy:  docker compose up -d (/srv/furnace/proxy)")
	fmt.Println("  worker: systemctl enable --now furnace-worker")
	return nil
}

func writeProxyFiles(cfg *app.Config) error {
	if err := os.MkdirAll(proxyDir, 0755); err != nil {
		return fmt.Errorf("create proxy dir: %w", err)
	}

	composePath := proxyDir + "/compose.yml"
	if err := os.WriteFile(composePath, deploy.ProxyComposeYML, 0644); err != nil {
		return fmt.Errorf("write compose.yml: %w", err)
	}
	fmt.Printf("wrote %s\n", composePath)

	caddyfile, err := generateCaddyfile(cfg)
	if err != nil {
		return fmt.Errorf("generate Caddyfile: %w", err)
	}
	caddyPath := proxyDir + "/Caddyfile"
	if err := os.WriteFile(caddyPath, caddyfile, 0644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}
	fmt.Printf("wrote %s (%d apps)\n", caddyPath, len(cfg.Apps))
	return nil
}

func dockerComposeUp() error {
	c := exec.Command("docker", "compose", "-f", proxyDir+"/compose.yml", "up", "-d")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func systemctl(args ...string) error {
	c := exec.Command("systemctl", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
