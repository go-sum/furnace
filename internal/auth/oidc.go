package auth

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-sum/furnace/internal/model"
)

type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (*GitHubClaims, error)
}

type OIDCVerifier struct {
	verifier *oidc.IDTokenVerifier
}

func NewOIDCVerifier(ctx context.Context, issuer, audience string) (*OIDCVerifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("create OIDC provider: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: audience,
	})

	return &OIDCVerifier{verifier: verifier}, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, rawToken string) (*GitHubClaims, error) {
	idToken, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", model.ErrTokenInvalid, err)
	}

	var claims GitHubClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: parse claims: %v", model.ErrTokenInvalid, err)
	}

	return &claims, nil
}

func ValidateClaims(claims *GitHubClaims, app model.AppConfig) error {
	if claims.Repository != app.Repo {
		return fmt.Errorf("%w: got %q, want %q", model.ErrRepoMismatch, claims.Repository, app.Repo)
	}

	if !matchRef(claims.Ref, app.AllowedRef) {
		return fmt.Errorf("%w: ref %q does not match %q", model.ErrRefNotAllowed, claims.Ref, app.AllowedRef)
	}

	if err := validateWorkflowIdentity(claims, app); err != nil {
		return err
	}

	return nil
}

func matchRef(ref, pattern string) bool {
	matched, err := path.Match(pattern, ref)
	if err != nil {
		return false
	}
	return matched
}

func ValidateRefPattern(pattern string) error {
	_, err := path.Match(pattern, "test")
	if err != nil {
		return fmt.Errorf("invalid ref pattern %q: %w", pattern, err)
	}
	return nil
}

type workflowIdentity struct {
	Repository string
	Path       string
	Ref        string
}

func validateWorkflowIdentity(claims *GitHubClaims, app model.AppConfig) error {
	for _, raw := range []string{claims.WorkflowRef, claims.JobWorkflowRef} {
		if raw == "" {
			continue
		}

		identity, err := parseWorkflowIdentity(raw)
		if err != nil {
			return fmt.Errorf("%w: %v", model.ErrWorkflowClaimInvalid, err)
		}

		if identity.Repository == app.Repo && identity.Path == app.Workflow {
			return nil
		}
	}

	if claims.WorkflowRef == "" && claims.JobWorkflowRef == "" {
		return fmt.Errorf("%w: workflow_ref and job_workflow_ref are empty", model.ErrWorkflowClaimMissing)
	}

	return fmt.Errorf(
		"%w: expected repository %q workflow %q, got workflow_ref=%q job_workflow_ref=%q",
		model.ErrWorkflowMismatch,
		app.Repo,
		app.Workflow,
		claims.WorkflowRef,
		claims.JobWorkflowRef,
	)
}

func parseWorkflowIdentity(raw string) (workflowIdentity, error) {
	parts := strings.Split(raw, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return workflowIdentity{}, fmt.Errorf("malformed workflow identity %q", raw)
	}

	repoAndPath := strings.SplitN(parts[0], "/", 3)
	if len(repoAndPath) != 3 {
		return workflowIdentity{}, fmt.Errorf("workflow identity %q must include owner, repo, and path", raw)
	}

	repository := repoAndPath[0] + "/" + repoAndPath[1]
	workflowPath := repoAndPath[2]
	if !strings.HasPrefix(workflowPath, ".github/workflows/") {
		return workflowIdentity{}, fmt.Errorf("workflow identity %q must point at .github/workflows", raw)
	}

	return workflowIdentity{
		Repository: repository,
		Path:       workflowPath,
		Ref:        parts[1],
	}, nil
}
