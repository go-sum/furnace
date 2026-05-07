package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	gcr "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Client queries an OCI registry for image tags.
type Client struct {
	keychain authn.Keychain
}

// NewClient returns a Client that authenticates via the provided keychain.
// If keychain is nil, authn.DefaultKeychain is used.
func NewClient(keychain authn.Keychain) *Client {
	if keychain == nil {
		keychain = authn.DefaultKeychain
	}
	return &Client{keychain: keychain}
}

func (c *Client) craneOpts(ctx context.Context) []crane.Option {
	return []crane.Option{
		crane.WithContext(ctx),
		crane.WithAuthFromKeychain(c.keychain),
	}
}

func (c *Client) remoteOpts(ctx context.Context) []remote.Option {
	return []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(c.keychain),
	}
}

// LatestTag returns the tag and manifest digest of the highest-version
// deployable image tag in imageRepo that matches the glob pattern (e.g. "v*").
// Tags are sorted by semver descending; non-semver tags fall back to
// lexicographic descending. OCI artifacts in the same repository, such as
// compose bundles, are ignored.
func (c *Client) LatestTag(ctx context.Context, imageRepo, pattern string) (tag, digest string, err error) {
	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tags, err := crane.ListTags(imageRepo, c.craneOpts(listCtx)...)
	if err != nil {
		return "", "", fmt.Errorf("list tags for %s: %w", imageRepo, err)
	}
	matching, err := filterAndSortTags(tags, pattern)
	if err != nil {
		return "", "", err
	}
	if len(matching) == 0 {
		return "", "", fmt.Errorf("no tags matching %q in %s", pattern, imageRepo)
	}

	resolveCtx, resolveCancel := context.WithTimeout(ctx, 30*time.Second)
	defer resolveCancel()

	for _, candidate := range matching {
		ref, err := name.ParseReference(imageRepo + ":" + candidate)
		if err != nil {
			return "", "", fmt.Errorf("parse image ref %s:%s: %w", imageRepo, candidate, err)
		}
		desc, err := remote.Get(ref, c.remoteOpts(resolveCtx)...)
		if err != nil {
			return "", "", fmt.Errorf("resolve descriptor for %s:%s: %w", imageRepo, candidate, err)
		}
		if !isDeployableImageDescriptor(desc.Descriptor) {
			continue
		}
		return candidate, desc.Digest.String(), nil
	}

	return "", "", fmt.Errorf("no deployable image tags matching %q in %s", pattern, imageRepo)
}

func isDeployableImageDescriptor(desc gcr.Descriptor) bool {
	if desc.ArtifactType != "" {
		return false
	}

	switch desc.MediaType {
	case types.OCIImageIndex, types.DockerManifestList, types.OCIManifestSchema1, types.DockerManifestSchema2:
		return true
	default:
		return false
	}
}
