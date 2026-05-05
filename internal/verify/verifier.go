package verify

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	sgverify "github.com/sigstore/sigstore-go/pkg/verify"

	"github.com/go-sum/furnace/internal/model"
)

const (
	bundleArtifactType = "application/vnd.dev.sigstore.bundle.v0.3+json"
	githubOIDCIssuer   = "https://token.actions.githubusercontent.com"
)

// Verifier checks Sigstore keyless signatures on OCI images.
// It fetches the Sigstore bundle from OCI referrers (as written by cosign 2.x)
// and verifies it against the public Sigstore trust root via TUF.
type Verifier struct {
	trustedRoot root.TrustedMaterial
	keychain    authn.Keychain
}

// New creates a Verifier by fetching the Sigstore public trust root from TUF.
// cacheDir is the directory where TUF metadata is cached (e.g. /var/lib/furnace/sigstore-tuf).
// If keychain is nil, authn.DefaultKeychain is used.
// This makes a network call to the Sigstore TUF repository on first use.
func New(cacheDir string, keychain authn.Keychain) (*Verifier, error) {
	if keychain == nil {
		keychain = authn.DefaultKeychain
	}
	opts := tuf.DefaultOptions()
	opts.CachePath = cacheDir
	tr, err := root.FetchTrustedRootWithOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("fetch sigstore trust root: %w", err)
	}
	return &Verifier{
		trustedRoot: tr,
		keychain:    keychain,
	}, nil
}

// Verify checks that the image at imageRef has a valid Sigstore keyless signature
// from a GitHub Actions workflow belonging to allowedIdentity (format: "org/repo").
//
// imageRef must be a digest-addressed reference: ghcr.io/org/repo@sha256:abc…
func (v *Verifier) Verify(ctx context.Context, imageRef, allowedIdentity string) error {
	bundleJSON, err := v.fetchBundle(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("%w: fetch bundle for %s: %v", model.ErrSignatureInvalid, imageRef, err)
	}

	b := new(bundle.Bundle)
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("%w: parse bundle: %v", model.ErrSignatureInvalid, err)
	}

	sv, err := sgverify.NewVerifier(v.trustedRoot,
		sgverify.WithTransparencyLog(1),
		sgverify.WithObserverTimestamps(1),
	)
	if err != nil {
		return fmt.Errorf("create signed entity verifier: %w", err)
	}

	digestBytes, err := digestHexBytes(imageRef)
	if err != nil {
		return fmt.Errorf("parse image digest from %s: %w", imageRef, err)
	}

	// Match any workflow URL in the given org/repo.
	sanRegexp := `^https://github\.com/` + regexp.QuoteMeta(allowedIdentity) + `/`
	identity, err := sgverify.NewShortCertificateIdentity(githubOIDCIssuer, "", "", sanRegexp)
	if err != nil {
		return fmt.Errorf("create certificate identity: %w", err)
	}

	policy := sgverify.NewPolicy(
		sgverify.WithArtifactDigest("sha256", digestBytes),
		sgverify.WithCertificateIdentity(identity),
	)

	if _, err := sv.Verify(b, policy); err != nil {
		return fmt.Errorf("%w: %v", model.ErrSignatureInvalid, err)
	}
	return nil
}

// fetchBundle fetches the Sigstore bundle JSON from OCI referrers of the image.
// cosign 2.x stores the bundle as the config blob of an OCI artifact whose
// artifactType is application/vnd.dev.sigstore.bundle.v0.3+json.
func (v *Verifier) fetchBundle(ctx context.Context, imageRef string) ([]byte, error) {
	digestRef, err := name.NewDigest(imageRef)
	if err != nil {
		return nil, fmt.Errorf("parse digest ref %s: %w", imageRef, err)
	}

	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(v.keychain),
	}

	idx, err := remote.Referrers(digestRef, opts...)
	if err != nil {
		return nil, fmt.Errorf("get referrers for %s: %w", imageRef, err)
	}

	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("parse referrers index: %w", err)
	}

	for _, desc := range manifest.Manifests {
		if desc.ArtifactType != bundleArtifactType {
			continue
		}
		bundleRef := digestRef.Context().Digest(desc.Digest.String())
		bundleImg, err := remote.Image(bundleRef, opts...)
		if err != nil {
			return nil, fmt.Errorf("fetch bundle artifact: %w", err)
		}
		// cosign stores the bundle JSON as the OCI image config blob.
		data, err := bundleImg.RawConfigFile()
		if err != nil {
			return nil, fmt.Errorf("read bundle config: %w", err)
		}
		if len(data) > 1<<20 {
			return nil, fmt.Errorf("bundle config exceeds size limit")
		}
		if len(data) > 0 && data[0] == '{' {
			return data, nil
		}
		// Fallback: check the first layer (some older cosign versions use layers).
		layers, err := bundleImg.Layers()
		if err != nil || len(layers) == 0 {
			continue
		}
		rc, err := layers[0].Compressed()
		if err != nil {
			continue
		}
		data, err = io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err == nil && len(data) > 0 {
			return data, nil
		}
	}
	return nil, fmt.Errorf("no sigstore bundle found in referrers of %s", imageRef)
}

// digestHexBytes parses the hex-encoded digest bytes from a reference like
// "ghcr.io/org/repo@sha256:abc123..." or "sha256:abc123...".
func digestHexBytes(imageRef string) ([]byte, error) {
	// Find sha256:<hex> anywhere in the ref.
	idx := strings.Index(imageRef, "sha256:")
	if idx < 0 {
		return nil, fmt.Errorf("no sha256 digest in %q", imageRef)
	}
	hexStr := imageRef[idx+len("sha256:"):]
	// Trim anything after the hex (e.g. trailing slash or query string).
	if end := strings.IndexAny(hexStr, " \t\n@"); end >= 0 {
		hexStr = hexStr[:end]
	}
	return hex.DecodeString(hexStr)
}
