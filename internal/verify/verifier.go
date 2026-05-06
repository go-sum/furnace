package verify

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	sgverify "github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature/payload"

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
	nameOpts    []name.Option
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

// NewFromTrustedMaterial creates a Verifier from a pre-fetched TrustedMaterial.
// Intended for tests that bypass TUF. If keychain is nil, authn.DefaultKeychain is used.
func NewFromTrustedMaterial(tm root.TrustedMaterial, keychain authn.Keychain) *Verifier {
	if keychain == nil {
		keychain = authn.DefaultKeychain
	}
	return &Verifier{trustedRoot: tm, keychain: keychain}
}

// Verify checks that the image at imageRef has a valid Sigstore keyless signature
// from a GitHub Actions workflow belonging to allowedIdentity (format: "org/repo").
//
// imageRef must be a digest-addressed reference: ghcr.io/org/repo@sha256:abc…
func (v *Verifier) Verify(ctx context.Context, imageRef, allowedIdentity string) error {
	bundleJSON, err := v.fetchBundle(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("%w: %v", model.ErrSignatureInvalid, err)
	}

	b := new(bundle.Bundle)
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("%w: parse bundle: %v", model.ErrSignatureInvalid, err)
	}
	return v.verifyEntity(imageRef, allowedIdentity, b)
}

func (v *Verifier) verifyEntity(imageRef, allowedIdentity string, entity sgverify.SignedEntity) error {
	digestRef, err := name.NewDigest(imageRef, v.nameOpts...)
	if err != nil {
		return fmt.Errorf("%w: parse image ref: %v", model.ErrSignatureInvalid, err)
	}

	sigContent, err := entity.SignatureContent()
	if err != nil {
		return fmt.Errorf("%w: read signature content: %v", model.ErrSignatureInvalid, err)
	}
	if sigContent.MessageSignatureContent() == nil {
		return fmt.Errorf("%w: bundle has no message signature", model.ErrSignatureInvalid)
	}

	expectedPayload, err := payload.Cosign{Image: digestRef}.MarshalJSON()
	if err != nil {
		return fmt.Errorf("%w: build expected payload: %v", model.ErrSignatureInvalid, err)
	}

	sv, err := sgverify.NewVerifier(v.trustedRoot,
		sgverify.WithTransparencyLog(1),
		sgverify.WithObserverTimestamps(1),
	)
	if err != nil {
		return fmt.Errorf("create signed entity verifier: %w", err)
	}

	sanRegexp := `^https://github\.com/` + regexp.QuoteMeta(allowedIdentity) + `/`
	identity, err := sgverify.NewShortCertificateIdentity(githubOIDCIssuer, "", "", sanRegexp)
	if err != nil {
		return fmt.Errorf("create certificate identity: %w", err)
	}

	policy := sgverify.NewPolicy(
		sgverify.WithArtifact(bytes.NewReader(expectedPayload)),
		sgverify.WithCertificateIdentity(identity),
	)

	if _, err := sv.Verify(entity, policy); err != nil {
		return fmt.Errorf("%w: %v", model.ErrSignatureInvalid, err)
	}
	return nil
}

// fetchBundle fetches the Sigstore bundle JSON from OCI referrers of the image.
// cosign 2.x stores the bundle as the config blob of an OCI artifact whose
// artifactType is application/vnd.dev.sigstore.bundle.v0.3+json.
func (v *Verifier) fetchBundle(ctx context.Context, imageRef string) ([]byte, error) {
	digestRef, err := name.NewDigest(imageRef, v.nameOpts...)
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
		// Check the manifest descriptor size before fetching the blob to prevent
		// a malicious registry from exhausting worker memory with an oversized config.
		bundleMF, err := bundleImg.Manifest()
		if err != nil {
			return nil, fmt.Errorf("read bundle manifest: %w", err)
		}
		if bundleMF.Config.Size > 1<<20 {
			return nil, fmt.Errorf("bundle config exceeds size limit")
		}
		data, err := bundleImg.RawConfigFile()
		if err != nil {
			return nil, fmt.Errorf("read bundle config: %w", err)
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
