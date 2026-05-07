package cli

import (
	"fmt"
	"os"
)

const furnaceConfigPath = "/etc/furnace/furnace.yaml"

// ensureWebReadableConfig makes the Furnace config readable by the nonroot
// furnace-web container user. Credentials are stored separately, so the config
// itself must be world-readable for the bind mount to work reliably.
func ensureWebReadableConfig(path string) error {
	if err := os.Chmod(path, 0644); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}
