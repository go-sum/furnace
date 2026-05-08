package cli

import (
	"fmt"
	"os"

	"github.com/go-sum/furnace/internal/creds"
)

// installWorker encrypts the credential (if provided), renders and writes the
// systemd unit, and enables/starts the service.
func installWorker(credential string) error {
	if credential != "" {
		if err := creds.Encrypt(credential, CredPath); err != nil {
			return fmt.Errorf("encrypt credential: %w", err)
		}
	}

	unitBytes, err := renderWorkerUnit(credential != "")
	if err != nil {
		return fmt.Errorf("render worker unit: %w", err)
	}
	if err := os.WriteFile(WorkerUnitDest, unitBytes, 0644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	fmt.Printf("wrote %s\n", WorkerUnitDest)

	if err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if err := systemctl("enable", "--now", "furnace-worker"); err != nil {
		return fmt.Errorf("enable worker: %w", err)
	}
	return nil
}
