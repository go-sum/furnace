package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	gcr "github.com/google/go-containerregistry/pkg/v1"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

const maxBlobSize = 1 << 20 // 1MB per file

// artifactVerifier is satisfied by *verify.Verifier.
type artifactVerifier interface {
	Verify(ctx context.Context, imageRef, allowedIdentity string) error
}

// ArtifactFetcher fetches and verifies OCI compose artifacts from a container registry.
type ArtifactFetcher struct {
	verifier  artifactVerifier
	keychain  authn.Keychain
	nameOpts  []name.Option // injected in tests for insecure registries
}

// NewArtifactFetcher creates a fetcher that uses v for signature verification.
func NewArtifactFetcher(v artifactVerifier) *ArtifactFetcher {
	return &ArtifactFetcher{verifier: v, keychain: authn.DefaultKeychain}
}

// FetchAndVerify resolves artifactRef to a digest, verifies its cosign signature,
// then writes all artifact blobs to destDir. Filenames come from the
// org.opencontainers.image.title annotation on each manifest layer.
func (f *ArtifactFetcher) FetchAndVerify(ctx context.Context, artifactRef, allowedIdentity, destDir string) error {
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(f.keychain),
	}

	ref, err := name.ParseReference(artifactRef, f.nameOpts...)
	if err != nil {
		return fmt.Errorf("parse artifact ref %q: %w", artifactRef, err)
	}

	desc, err := remote.Get(ref, opts...)
	if err != nil {
		return fmt.Errorf("resolve artifact %q: %w", artifactRef, err)
	}
	digestRef := ref.Context().Digest(desc.Digest.String())

	if err := f.verifier.Verify(ctx, digestRef.String(), allowedIdentity); err != nil {
		return fmt.Errorf("verify compose artifact: %w", err)
	}

	var manifest gcr.Manifest
	if err := json.Unmarshal(desc.Manifest, &manifest); err != nil {
		return fmt.Errorf("parse artifact manifest: %w", err)
	}
	if len(manifest.Layers) == 0 {
		return fmt.Errorf("artifact %q contains no files", artifactRef)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	for i, layer := range manifest.Layers {
		filename := layer.Annotations[specsv1.AnnotationTitle]
		if filename == "" {
			filename = fmt.Sprintf("compose-%d.yml", i)
		}
		if err := validateArtifactFilename(filename); err != nil {
			return fmt.Errorf("layer %d: %w", i, err)
		}

		blobRef := ref.Context().Digest(layer.Digest.String())
		content, err := fetchBlob(ctx, blobRef, opts)
		if err != nil {
			return fmt.Errorf("fetch layer %d (%s): %w", i, filename, err)
		}

		if err := os.WriteFile(filepath.Join(destDir, filename), content, 0644); err != nil {
			return fmt.Errorf("write %s: %w", filename, err)
		}
	}
	return nil
}

// ResolveArtifactRef substitutes {tag} in pattern with tag.
func ResolveArtifactRef(pattern, tag string) string {
	return strings.ReplaceAll(pattern, "{tag}", tag)
}

func fetchBlob(ctx context.Context, ref name.Digest, opts []remote.Option) ([]byte, error) {
	l, err := remote.Layer(ref, opts...)
	if err != nil {
		return nil, fmt.Errorf("get blob: %w", err)
	}
	rc, err := l.Compressed()
	if err != nil {
		return nil, fmt.Errorf("open blob: %w", err)
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxBlobSize))
}

func validateArtifactFilename(filename string) error {
	if filename == "" {
		return fmt.Errorf("empty filename")
	}
	clean := filepath.Clean(filename)
	if clean != filepath.Base(clean) || clean == "." || clean == ".." {
		return fmt.Errorf("filename must be a plain name with no path components: %q", filename)
	}
	return nil
}
