package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/internal/app"
	"github.com/go-sum/furnace/internal/audit"
	"github.com/go-sum/furnace/internal/deploy"
	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/registry"
	"github.com/go-sum/furnace/internal/storage"
	"github.com/go-sum/furnace/internal/verify"
	"github.com/go-sum/furnace/internal/worker"
)

func newWorkerCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Run the furnace-worker poll loop",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

			cfg, err := app.LoadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			apps := make(map[string]model.AppConfig, len(cfg.Apps))
			for name := range cfg.Apps {
				appCfg, _ := cfg.AppConfig(name)
				apps[name] = appCfg
			}

			reg := registry.NewClient()

			verifier, err := verify.New(filepath.Join(cfg.DataDir, "sigstore-tuf"))
			if err != nil {
				return fmt.Errorf("init sigstore verifier: %w", err)
			}

			composeFetcher := deploy.NewArtifactFetcher(verifier)

			executor := deploy.NewDockerExecutor()
			lock := deploy.NewFileLock(filepath.Join(cfg.DataDir, "locks"))
			health := deploy.NewHTTPHealthChecker()
			store := storage.NewFileDeploymentStore(filepath.Join(cfg.DataDir, "deployments"), logger)

			auditLogger, err := audit.NewFileLogger(filepath.Join(cfg.DataDir, "audit"))
			if err != nil {
				return fmt.Errorf("create audit logger: %w", err)
			}

			svc := deploy.NewService(deploy.ServiceConfig{
				Apps:     apps,
				Executor: executor,
				Lock:     lock,
				Health:   health,
				Store:    store,
				Audit:    auditLogger,
				DataDir:  cfg.DataDir,
				Logger:   logger,
				Context:  ctx,
			})
			svc.ReconcileOnStartup(ctx)

			w := worker.New(worker.Config{
				Apps:           apps,
				PollInterval:   cfg.PollInterval,
				DataDir:        cfg.DataDir,
				Registry:       reg,
				Verifier:       verifier,
				Deployer:       svc,
				ComposeFetcher: composeFetcher,
				Logger:         logger,
			})

			logger.Info("furnace-worker starting",
				"apps", len(apps),
				"poll_interval", cfg.PollInterval,
			)
			return w.Run(ctx)
		},
	}
}
