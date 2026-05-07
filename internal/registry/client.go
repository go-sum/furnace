package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
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

// LatestTag returns the tag and manifest digest of the highest-version tag in
// imageRepo that matches the glob pattern (e.g. "v*"). Tags are sorted by
// semver descending; non-semver tags fall back to lexicographic descending.
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
	latest := matching[0]

	digestCtx, digestCancel := context.WithTimeout(ctx, 30*time.Second)
	defer digestCancel()
	d, err := crane.Digest(imageRepo+":"+latest, c.craneOpts(digestCtx)...)
	if err != nil {
		return "", "", fmt.Errorf("digest for %s:%s: %w", imageRepo, latest, err)
	}
	return latest, d, nil
}
