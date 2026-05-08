package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/spf13/cobra"

	"github.com/go-sum/furnace/internal/audit"
	"github.com/go-sum/furnace/internal/creds"
	"github.com/go-sum/furnace/internal/deploy"
	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/registry"
	"github.com/go-sum/furnace/internal/storage"
	"github.com/go-sum/furnace/internal/verify"
	"github.com/go-sum/furnace/internal/worker"
)

func newWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage the furnace deployment worker",
	}
	cmd.AddCommand(
		newWorkerRunCmd(),
		newWorkerStopCmd(),
		newWorkerStatusCmd(),
		newWorkerLogsCmd(),
	)
	return cmd
}

func newWorkerRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the furnace-worker poll loop (used by systemd)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

			db, err := storage.OpenDB(DBPath, false, logger)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer db.Close()

			appStore := storage.NewSQLiteAppStore(db, logger)

			dataDir, _, err := appStore.GetConfigValue(ctx, "data_dir")
			if err != nil {
				return fmt.Errorf("get data_dir: %w", err)
			}
			if dataDir == "" {
				dataDir = DataDir
			}

			pollIntervalStr, _, err := appStore.GetConfigValue(ctx, "poll_interval")
			if err != nil {
				return fmt.Errorf("get poll_interval: %w", err)
			}
			pollInterval, err := time.ParseDuration(pollIntervalStr)
			if err != nil || pollInterval == 0 {
				pollInterval = time.Minute
			}

			appList, err := appStore.ListApps(ctx)
			if err != nil {
				return fmt.Errorf("list apps: %w", err)
			}
			apps := make(map[string]model.AppConfig, len(appList))
			for _, a := range appList {
				apps[a.Name] = a
			}

			token, err := creds.LoadFromCredentialsDir()
			if err != nil {
				return fmt.Errorf("load registry credential: %w", err)
			}

			var keychain authn.Keychain
			var extraEnv []string
			var executor *deploy.DockerExecutor
			if token != "" {
				keychain = creds.TokenKeychain(token)
				dockerConfigDir, err := creds.CreateDockerConfigDir(token)
				if err != nil {
					return fmt.Errorf("create docker config: %w", err)
				}
				defer creds.RemoveDockerConfigDir(dockerConfigDir)
				extraEnv = []string{"DOCKER_CONFIG=" + dockerConfigDir}
				executor = deploy.NewDockerExecutorWithEnv(extraEnv)
			} else {
				executor = deploy.NewDockerExecutor()
			}

			reg := registry.NewClient(keychain)
			verifier := verify.NewCLI(extraEnv)
			composeFetcher := deploy.NewArtifactFetcher(verifier, keychain)
			releases := deploy.NewReleaseManager(logger)

			dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
			if err != nil {
				return fmt.Errorf("create docker client: %w", err)
			}
			defer dockerClient.Close()

			lock := deploy.NewFileLock(filepath.Join(dataDir, "locks"))
			health := deploy.NewDockerHealthChecker(dockerClient)
			store := storage.NewSQLiteDeploymentStore(db, logger)

			auditLogger, err := audit.NewFileLogger(filepath.Join(dataDir, "audit"))
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
				DataDir:  dataDir,
				Logger:   logger,
				Context:  ctx,
				Releases: releases,
			})
			svc.ReconcileOnStartup(ctx)

			w := worker.New(worker.Config{
				Apps:            apps,
				PollInterval:    pollInterval,
				DataDir:         dataDir,
				Registry:        reg,
				Verifier:        verifier,
				Deployer:        svc,
				ArtifactFetcher: composeFetcher,
				Releases:        releases,
				Logger:          logger,
			})

			logger.Info("furnace-worker starting",
				"apps", len(apps),
				"poll_interval", pollInterval,
			)
			return w.Run(ctx)
		},
	}
}

func newWorkerStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the furnace-worker systemd unit (requires root)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if os.Geteuid() != 0 {
				return fmt.Errorf("furnace worker stop requires root privileges (run with sudo)")
			}
			return systemctl("stop", "furnace-worker")
		},
	}
}

func newWorkerStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show furnace-worker systemd unit status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := exec.Command("systemctl", "status", "furnace-worker")
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			_ = c.Run() // systemctl status exits non-zero when stopped; ignore error
			return nil
		},
	}
}

func newWorkerLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show furnace-worker logs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			args := []string{"-u", "furnace-worker"}
			if follow {
				args = append(args, "-f")
			}
			c := exec.Command("journalctl", args...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	return cmd
}
