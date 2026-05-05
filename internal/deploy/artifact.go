package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	gcr "github.com/google/go-containerregistry/pkg/v1"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

const maxBlobSize = 1 << 20 // 1MB per file
const artifactTrackingFile = ".furnace-files"

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
// If keychain is nil, authn.DefaultKeychain is used.
func NewArtifactFetcher(v artifactVerifier, keychain authn.Keychain) *ArtifactFetcher {
	if keychain == nil {
		keychain = authn.DefaultKeychain
	}
	return &ArtifactFetcher{verifier: v, keychain: keychain}
}

// FetchAndVerify resolves artifactRef to a digest, verifies its cosign signature,
// then writes all artifact blobs to destDir. Filenames come from the
// org.opencontainers.image.title annotation on each manifest layer.
// Returns the OCI manifest digest string.
func (f *ArtifactFetcher) FetchAndVerify(ctx context.Context, artifactRef, allowedIdentity, destDir string) (string, error) {
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(f.keychain),
	}

	ref, err := name.ParseReference(artifactRef, f.nameOpts...)
	if err != nil {
		return "", fmt.Errorf("parse artifact ref %q: %w", artifactRef, err)
	}

	desc, err := remote.Get(ref, opts...)
	if err != nil {
		return "", fmt.Errorf("resolve artifact %q: %w", artifactRef, err)
	}
	digestRef := ref.Context().Digest(desc.Digest.String())

	if err := f.verifier.Verify(ctx, digestRef.String(), allowedIdentity); err != nil {
		return "", fmt.Errorf("verify compose artifact: %w", err)
	}

	var manifest gcr.Manifest
	if err := json.Unmarshal(desc.Manifest, &manifest); err != nil {
		return "", fmt.Errorf("parse artifact manifest: %w", err)
	}
	if len(manifest.Layers) == 0 {
		return "", fmt.Errorf("artifact %q contains no files", artifactRef)
	}

	// Phase 1: fetch all blobs into memory before touching the filesystem.
	type fetchedFile struct {
		name    string
		content []byte
	}
	fetched := make([]fetchedFile, 0, len(manifest.Layers))
	for i, layer := range manifest.Layers {
		filename := layer.Annotations[specsv1.AnnotationTitle]
		if filename == "" {
			filename = fmt.Sprintf("compose-%d.yml", i)
		}
		if err := validateArtifactFilename(filename); err != nil {
			return "", fmt.Errorf("layer %d: %w", i, err)
		}
		blobRef := ref.Context().Digest(layer.Digest.String())
		content, err := fetchBlob(ctx, blobRef, opts)
		if err != nil {
			return "", fmt.Errorf("fetch layer %d (%s): %w", i, filename, err)
		}
		fetched = append(fetched, fetchedFile{name: filename, content: content})
	}

	// Phase 2: write files atomically to destDir.
	for _, ff := range fetched {
		tmp, err := os.CreateTemp(destDir, ".furnace-tmp-*")
		if err != nil {
			return "", fmt.Errorf("create temp for %s: %w", ff.name, err)
		}
		if _, err := tmp.Write(ff.content); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return "", fmt.Errorf("write temp for %s: %w", ff.name, err)
		}
		if err := tmp.Chmod(0644); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return "", fmt.Errorf("chmod temp for %s: %w", ff.name, err)
		}
		if err := tmp.Close(); err != nil {
			os.Remove(tmp.Name())
			return "", fmt.Errorf("close temp for %s: %w", ff.name, err)
		}
		if err := os.Rename(tmp.Name(), filepath.Join(destDir, ff.name)); err != nil {
			return "", fmt.Errorf("place %s: %w", ff.name, err)
		}
	}

	// Write tracking file.
	sortedNames := make([]string, 0, len(fetched))
	for _, ff := range fetched {
		sortedNames = append(sortedNames, ff.name)
	}
	sort.Strings(sortedNames)
	trackContent := []byte(strings.Join(sortedNames, "\n"))

	trackTmp, err := os.CreateTemp(destDir, ".furnace-track-*")
	if err != nil {
		return "", fmt.Errorf("create tracking temp: %w", err)
	}
	if _, err := trackTmp.Write(trackContent); err != nil {
		trackTmp.Close()
		os.Remove(trackTmp.Name())
		return "", fmt.Errorf("write tracking temp: %w", err)
	}
	if err := trackTmp.Close(); err != nil {
		os.Remove(trackTmp.Name())
		return "", fmt.Errorf("close tracking temp: %w", err)
	}
	if err := os.Rename(trackTmp.Name(), filepath.Join(destDir, artifactTrackingFile)); err != nil {
		return "", fmt.Errorf("rename tracking file: %w", err)
	}

	return desc.Digest.String(), nil
}

// ResolveDigest resolves artifactRef to its manifest digest without downloading
// blobs or verifying signatures — a single HEAD request to the registry.
func (f *ArtifactFetcher) ResolveDigest(ctx context.Context, artifactRef string) (string, error) {
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(f.keychain),
	}
	ref, err := name.ParseReference(artifactRef, f.nameOpts...)
	if err != nil {
		return "", fmt.Errorf("parse artifact ref %q: %w", artifactRef, err)
	}
	desc, err := remote.Head(ref, opts...)
	if err != nil {
		return "", fmt.Errorf("resolve artifact %q: %w", artifactRef, err)
	}
	return desc.Digest.String(), nil
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
	data, err := io.ReadAll(io.LimitReader(rc, maxBlobSize+1))
	if err != nil {
		return nil, fmt.Errorf("read blob: %w", err)
	}
	if int64(len(data)) > maxBlobSize {
		return nil, fmt.Errorf("blob exceeds maximum size (%d bytes)", maxBlobSize)
	}
	return data, nil
}

func validateArtifactFilename(filename string) error {
	if filename == "" {
		return fmt.Errorf("empty filename")
	}
	clean := filepath.Clean(filename)
	if clean != filepath.Base(clean) || clean == "." || clean == ".." {
		return fmt.Errorf("filename must be a plain name with no path components: %q", filename)
	}
	if strings.HasPrefix(clean, ".furnace-") {
		return fmt.Errorf("filename %q is reserved: .furnace- prefix is not allowed", filename)
	}
	return nil
}
