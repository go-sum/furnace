package cli

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/storage"
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

func newProxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Manage the Caddy reverse proxy",
	}
	cmd.AddCommand(
		newProxyInitCmd(),
		newProxyUpCmd(),
		newProxyStatusCmd(),
		newProxyDownCmd(),
		newProxyLogsCmd(),
	)
	return cmd
}

func newProxyInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate Caddyfile and compose.yml from current app config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

			db, err := storage.OpenDB(DBPath, true, logger)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer db.Close()

			appStore := storage.NewSQLiteAppStore(db, logger)
			apps, err := appStore.ListApps(ctx)
			if err != nil {
				return fmt.Errorf("list apps: %w", err)
			}

			return writeProxyFiles(apps)
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
			c.Dir = ProxyDir
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
			c.Dir = ProxyDir
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
			c := exec.Command("docker", "compose", "-f", ProxyDir+"/compose.yml", "down")
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
			args := []string{"compose", "-f", ProxyDir + "/compose.yml", "logs"}
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

func generateCaddyfile(apps []model.AppConfig) ([]byte, error) {
	sorted := make([]model.AppConfig, len(apps))
	copy(sorted, apps)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	caddyApps := make([]caddyApp, len(sorted))
	for i, a := range sorted {
		caddyApps[i] = caddyApp{Name: a.Name, Domain: a.Domain, Port: a.Port, TLS: a.TLS}
	}

	tmpl, err := template.New("caddyfile").Parse(caddyfileTmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, caddyfileData{Apps: caddyApps}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
