package creds

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Encrypt encrypts a plaintext token via systemd-creds and writes
// ciphertext to /etc/furnace/registry-token.cred.
func Encrypt(token string) error {
	if err := os.MkdirAll("/etc/furnace", 0755); err != nil {
		return fmt.Errorf("create /etc/furnace: %w", err)
	}
	cmd := exec.Command("systemd-creds", "encrypt", "-", "/etc/furnace/registry-token.cred")
	cmd.Stdin = strings.NewReader(token)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemd-creds encrypt: %w: %s", err, out)
	}
	return nil
}

// CreateDockerConfigDir writes an ephemeral Docker config directory with a
// ghcr.io auth entry and returns its path. The caller should remove it after
// use.
func CreateDockerConfigDir(token string) (string, error) {
	dockerDir, err := os.MkdirTemp("", "furnace-docker-*")
	if err != nil {
		return "", fmt.Errorf("create docker config dir: %w", err)
	}
	if err := os.Chmod(dockerDir, 0700); err != nil {
		os.RemoveAll(dockerDir)
		return "", fmt.Errorf("chmod docker config dir: %w", err)
	}
	if err := writeDockerConfig(dockerDir, token); err != nil {
		os.RemoveAll(dockerDir)
		return "", err
	}
	return dockerDir, nil
}

func writeDockerConfig(dockerDir, token string) error {
	if err := os.MkdirAll(dockerDir, 0700); err != nil {
		return fmt.Errorf("create docker config dir: %w", err)
	}
	auth := base64.StdEncoding.EncodeToString([]byte("furnace:" + token))
	config := fmt.Sprintf(`{"auths":{"ghcr.io":{"auth":%q}}}`, auth)
	if err := os.WriteFile(dockerDir+"/config.json", []byte(config), 0600); err != nil {
		return fmt.Errorf("write docker config: %w", err)
	}
	return nil
}

// RemoveDockerConfigDir deletes a Docker config directory created by
// CreateDockerConfigDir. Returns nil if the directory does not exist.
func RemoveDockerConfigDir(path string) error {
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove docker config dir: %w", err)
	}
	return nil
}

// LoadFromCredentialsDir reads the decrypted credential from
// $CREDENTIALS_DIRECTORY/registry-token. Returns ("", nil) if unset/missing.
func LoadFromCredentialsDir() (string, error) {
	dir := os.Getenv("CREDENTIALS_DIRECTORY")
	if dir == "" {
		return "", nil
	}
	data, err := os.ReadFile(dir + "/registry-token")
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read registry-token credential: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
