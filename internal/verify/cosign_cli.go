package verify

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/go-sum/furnace/internal/model"
)

// CLI wraps `cosign verify` so Furnace can use the same registry verification
// path that was proven manually against GHCR.
type CLI struct {
	extraEnv []string
}

func NewCLI(extraEnv []string) *CLI {
	return &CLI{extraEnv: append([]string(nil), extraEnv...)}
}

func (c *CLI) Verify(ctx context.Context, imageRef, allowedIdentity string) error {
	if strings.TrimSpace(imageRef) == "" {
		return fmt.Errorf("%w: empty image ref", model.ErrSignatureInvalid)
	}
	if strings.TrimSpace(allowedIdentity) == "" {
		return fmt.Errorf("%w: empty allowed identity", model.ErrSignatureInvalid)
	}

	identityRegexp := "^https://github\\.com/" + regexp.QuoteMeta(allowedIdentity) + "/"
	args := []string{
		"verify",
		"--certificate-identity-regexp", identityRegexp,
		"--certificate-github-workflow-repository", allowedIdentity,
		"--certificate-oidc-issuer", githubOIDCIssuer,
		imageRef,
	}

	cmd := exec.CommandContext(ctx, "cosign", args...)
	if len(c.extraEnv) > 0 {
		cmd.Env = append(os.Environ(), c.extraEnv...)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%w: %s", model.ErrSignatureInvalid, msg)
	}
	return nil
}
