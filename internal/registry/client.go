package registry

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
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

	var matching []string
	for _, t := range tags {
		if ok, _ := path.Match(pattern, t); ok {
			matching = append(matching, t)
		}
	}
	if len(matching) == 0 {
		return "", "", fmt.Errorf("no tags matching %q in %s", pattern, imageRepo)
	}
	sortTagsDesc(matching)
	latest := matching[0]

	digestCtx, digestCancel := context.WithTimeout(ctx, 30*time.Second)
	defer digestCancel()
	d, err := crane.Digest(imageRepo+":"+latest, c.craneOpts(digestCtx)...)
	if err != nil {
		return "", "", fmt.Errorf("digest for %s:%s: %w", imageRepo, latest, err)
	}
	return latest, d, nil
}

// sortTagsDesc sorts tags in descending semver order (highest first).
// Tags that do not parse as vMAJOR.MINOR.PATCH[-prerelease] fall back to lexicographic descending.
func sortTagsDesc(tags []string) {
	sort.Slice(tags, func(i, j int) bool {
		a, b := tags[i], tags[j]
		av, aerr := parseSemver(a)
		bv, berr := parseSemver(b)
		if aerr == nil && berr == nil {
			return compareSemver(av, bv) > 0
		}
		return a > b
	})
}

type semver struct {
	major, minor, patch int
	prerelease          string
}

func parseSemver(s string) (semver, error) {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("not semver")
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, err
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, err
	}
	patchStr, pre, _ := strings.Cut(parts[2], "-")
	patch, err := strconv.Atoi(patchStr)
	if err != nil {
		return semver{}, err
	}
	return semver{major, minor, patch, pre}, nil
}

func compareSemver(a, b semver) int {
	if d := a.major - b.major; d != 0 {
		return sign(d)
	}
	if d := a.minor - b.minor; d != 0 {
		return sign(d)
	}
	if d := a.patch - b.patch; d != 0 {
		return sign(d)
	}
	// Release (empty prerelease) sorts higher than any prerelease.
	switch {
	case a.prerelease == "" && b.prerelease != "":
		return 1
	case a.prerelease != "" && b.prerelease == "":
		return -1
	default:
		return comparePrerelease(a.prerelease, b.prerelease)
	}
}

// comparePrerelease compares two semver prerelease strings per spec §11.4:
// numeric identifiers compare as integers; alphanumeric compare lexically;
// numeric always sorts lower than alphanumeric; larger identifier set wins ties.
func comparePrerelease(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	n := min(len(aParts), len(bParts))
	for i := range n {
		aNum, aErr := strconv.Atoi(aParts[i])
		bNum, bErr := strconv.Atoi(bParts[i])
		switch {
		case aErr == nil && bErr == nil:
			if d := aNum - bNum; d != 0 {
				return sign(d)
			}
		case aErr == nil:
			return -1 // numeric < alphanumeric
		case bErr == nil:
			return 1 // alphanumeric > numeric
		default:
			if c := strings.Compare(aParts[i], bParts[i]); c != 0 {
				return c
			}
		}
	}
	return sign(len(aParts) - len(bParts))
}

func sign(n int) int {
	if n > 0 {
		return 1
	}
	if n < 0 {
		return -1
	}
	return 0
}
