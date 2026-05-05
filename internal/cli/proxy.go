package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/deploy"
	"github.com/go-sum/furnace/internal/app"
)

const caddyfileTmpl = `{
	auto_https off
	admin off
}
{{range .Apps}}
{{.Domain}} {
	{{if .TLS}}tls /certs/local.pem /certs/local-key.pem
	{{end}}reverse_proxy {{.Name}}-web-1:{{.Port}}
}
{{end}}`

func newProxyCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Manage the Caddy reverse proxy",
	}
	cmd.AddCommand(
		newProxyInitCmd(configPath),
		newProxyUpCmd(),
		newProxyStatusCmd(),
		newProxyDownCmd(),
		newProxyLogsCmd(),
	)
	return cmd
}

func newProxyInitCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate Caddyfile and compose.yml from current app config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := app.LoadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
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
		},
	}
}

func newProxyUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Start the Caddy reverse proxy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := exec.Command("docker", "compose", "up", "-d")
			c.Dir = "/srv/furnace/proxy"
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}

func newProxyStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Caddy reverse proxy container status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := exec.Command("docker", "compose", "ps")
			c.Dir = "/srv/furnace/proxy"
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}

type caddyApp struct {
	Name   string
	Domain string
	Port   int
	TLS    bool
}

type caddyfileData struct {
	Apps []caddyApp
}

func newProxyDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the Caddy reverse proxy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := exec.Command("docker", "compose", "-f", "/srv/furnace/proxy/compose.yml", "down")
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}

func newProxyLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show Caddy reverse proxy logs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			args := []string{"compose", "-f", "/srv/furnace/proxy/compose.yml", "logs"}
			if follow {
				args = append(args, "-f")
			}
			c := exec.Command("docker", args...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	return cmd
}

func generateCaddyfile(cfg *app.Config) ([]byte, error) {
	names := make([]string, 0, len(cfg.Apps))
	for name := range cfg.Apps {
		names = append(names, name)
	}
	sort.Strings(names)

	apps := make([]caddyApp, 0, len(names))
	for _, name := range names {
		appCfg, _ := cfg.AppConfig(name)
		apps = append(apps, caddyApp{
			Name:   name,
			Domain: appCfg.Domain,
			Port:   appCfg.Port,
			TLS:    appCfg.TLS,
		})
	}

	tmpl, err := template.New("caddyfile").Parse(caddyfileTmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, caddyfileData{Apps: apps}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
