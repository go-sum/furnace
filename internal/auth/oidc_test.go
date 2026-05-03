package auth

import (
	"errors"
	"testing"

	"github.com/go-sum/furnace/internal/model"
)

func TestValidateClaims_Success(t *testing.T) {
	claims := &GitHubClaims{
		Repository:  "org/repo",
		Ref:         "refs/tags/v1.2.3",
		Workflow:    "Release",
		WorkflowRef: "org/repo/.github/workflows/release.yml@refs/tags/v1.2.3",
	}
	app := model.AppConfig{
		Repo:       "org/repo",
		AllowedRef: "refs/tags/v*",
		Workflow:   ".github/workflows/release.yml",
	}

	err := ValidateClaims(claims, app)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateClaims_RepoMismatch(t *testing.T) {
	claims := &GitHubClaims{
		Repository:  "evil/repo",
		Ref:         "refs/tags/v1.0.0",
		WorkflowRef: "evil/repo/.github/workflows/release.yml@refs/tags/v1.0.0",
	}
	app := model.AppConfig{
		Repo:       "org/repo",
		AllowedRef: "refs/tags/v*",
		Workflow:   ".github/workflows/release.yml",
	}

	err := ValidateClaims(claims, app)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, model.ErrRepoMismatch) {
		t.Fatalf("expected ErrRepoMismatch, got: %v", err)
	}
}

func TestValidateClaims_RefNotAllowed(t *testing.T) {
	claims := &GitHubClaims{
		Repository:  "org/repo",
		Ref:         "refs/heads/main",
		WorkflowRef: "org/repo/.github/workflows/release.yml@refs/heads/main",
	}
	app := model.AppConfig{
		Repo:       "org/repo",
		AllowedRef: "refs/tags/v*",
		Workflow:   ".github/workflows/release.yml",
	}

	err := ValidateClaims(claims, app)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, model.ErrRefNotAllowed) {
		t.Fatalf("expected ErrRefNotAllowed, got: %v", err)
	}
}

func TestValidateClaims_WorkflowMismatch(t *testing.T) {
	claims := &GitHubClaims{
		Repository:  "org/repo",
		Ref:         "refs/tags/v1.0.0",
		WorkflowRef: "org/repo/.github/workflows/ci.yml@refs/tags/v1.0.0",
	}
	app := model.AppConfig{
		Repo:       "org/repo",
		AllowedRef: "refs/tags/v*",
		Workflow:   ".github/workflows/release.yml",
	}

	err := ValidateClaims(claims, app)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, model.ErrWorkflowMismatch) {
		t.Fatalf("expected ErrWorkflowMismatch, got: %v", err)
	}
}

func TestValidateClaims_ReusableWorkflowMatch(t *testing.T) {
	claims := &GitHubClaims{
		Repository:     "org/repo",
		Ref:            "refs/tags/v1.0.0",
		WorkflowRef:    "org/repo/.github/workflows/deploy.yml@refs/tags/v1.0.0",
		JobWorkflowRef: "org/repo/.github/workflows/release.yml@refs/heads/main",
	}
	app := model.AppConfig{
		Repo:       "org/repo",
		AllowedRef: "refs/tags/v*",
		Workflow:   ".github/workflows/release.yml",
	}

	err := ValidateClaims(claims, app)
	if err != nil {
		t.Fatalf("expected reusable workflow match to pass, got: %v", err)
	}
}

func TestValidateClaims_MissingWorkflowIdentity(t *testing.T) {
	claims := &GitHubClaims{
		Repository: "org/repo",
		Ref:        "refs/tags/v1.0.0",
		Workflow:   "Release",
	}
	app := model.AppConfig{
		Repo:       "org/repo",
		AllowedRef: "refs/tags/v*",
		Workflow:   ".github/workflows/release.yml",
	}

	err := ValidateClaims(claims, app)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, model.ErrWorkflowClaimMissing) {
		t.Fatalf("expected ErrWorkflowClaimMissing, got: %v", err)
	}
}

func TestValidateClaims_InvalidWorkflowIdentity(t *testing.T) {
	claims := &GitHubClaims{
		Repository:  "org/repo",
		Ref:         "refs/tags/v1.0.0",
		WorkflowRef: "not-a-valid-workflow-ref",
	}
	app := model.AppConfig{
		Repo:       "org/repo",
		AllowedRef: "refs/tags/v*",
		Workflow:   ".github/workflows/release.yml",
	}

	err := ValidateClaims(claims, app)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, model.ErrWorkflowClaimInvalid) {
		t.Fatalf("expected ErrWorkflowClaimInvalid, got: %v", err)
	}
}

func TestMatchRef_Patterns(t *testing.T) {
	tests := []struct {
		ref     string
		pattern string
		want    bool
	}{
		{"refs/tags/v1.0.0", "refs/tags/v*", true},
		{"refs/tags/v2.3.4-beta", "refs/tags/v*", true},
		{"refs/heads/main", "refs/tags/v*", false},
		{"refs/heads/main", "refs/heads/main", true},
		{"refs/heads/feature/x", "refs/heads/*", false},
	}

	for _, tt := range tests {
		t.Run(tt.ref+"_"+tt.pattern, func(t *testing.T) {
			got := matchRef(tt.ref, tt.pattern)
			if got != tt.want {
				t.Fatalf("matchRef(%q, %q) = %v, want %v", tt.ref, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestParseWorkflowIdentity(t *testing.T) {
	got, err := parseWorkflowIdentity("org/repo/.github/workflows/release.yml@refs/tags/v1.0.0")
	if err != nil {
		t.Fatalf("parseWorkflowIdentity returned error: %v", err)
	}

	want := workflowIdentity{
		Repository: "org/repo",
		Path:       ".github/workflows/release.yml",
		Ref:        "refs/tags/v1.0.0",
	}
	if got != want {
		t.Fatalf("parseWorkflowIdentity mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestValidateRefPattern_Valid(t *testing.T) {
	patterns := []string{
		"refs/tags/v*",
		"refs/heads/main",
		"refs/heads/*",
	}
	for _, p := range patterns {
		if err := ValidateRefPattern(p); err != nil {
			t.Fatalf("ValidateRefPattern(%q) = %v, want nil", p, err)
		}
	}
}

func TestValidateRefPattern_Invalid(t *testing.T) {
	err := ValidateRefPattern("refs/[invalid")
	if err == nil {
		t.Fatal("expected error for malformed pattern")
	}
}

func TestExtractBearer(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"valid", "Bearer abc123", "abc123"},
		{"lowercase", "bearer xyz", "xyz"},
		{"empty", "", ""},
		{"no space", "Bearertoken", ""},
		{"wrong scheme", "Basic abc123", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBearerFromHeader(tt.header)
			if got != tt.want {
				t.Fatalf("extractBearer(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}
