package verify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/sigstore/sigstore/pkg/signature/payload"

	"github.com/go-sum/furnace/internal/model"
)

// anonKeychain resolves all resources to anonymous auth.
type anonKeychain struct{}

func (anonKeychain) Resolve(_ authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

const testDigest = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// --- Payload construction & validation (pure unit tests, no network) ---

func TestSimpleSigningPayload(t *testing.T) {
	ref, err := name.NewDigest("ghcr.io/test/repo@" + testDigest)
	if err != nil {
		t.Fatalf("NewDigest: %v", err)
	}
	data, err := payload.Cosign{Image: ref}.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	want := `{"critical":{"identity":{"docker-reference":"ghcr.io/test/repo"},"image":{"docker-manifest-digest":"` + testDigest + `"},"type":"cosign container image signature"},"optional":null}`
	if string(data) != want {
		t.Fatalf("payload mismatch:\ngot  %s\nwant %s", data, want)
	}
}

func TestValidatePayload_ValidPayload(t *testing.T) {
	ref, err := name.NewDigest("ghcr.io/test/repo@" + testDigest)
	if err != nil {
		t.Fatalf("NewDigest: %v", err)
	}
	data, err := payload.Cosign{Image: ref}.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if err := validatePayload(data, ref); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePayload_WrongDigest(t *testing.T) {
	ref, err := name.NewDigest("ghcr.io/test/repo@" + testDigest)
	if err != nil {
		t.Fatalf("NewDigest: %v", err)
	}
	data, err := payload.Cosign{Image: ref}.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	otherDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	otherRef, err := name.NewDigest("ghcr.io/test/repo@" + otherDigest)
	if err != nil {
		t.Fatalf("NewDigest other: %v", err)
	}
	err = validatePayload(data, otherRef)
	if err == nil {
		t.Fatal("expected error for wrong digest")
	}
	if !strings.Contains(err.Error(), "does not match expected") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidatePayload_WrongIdentity(t *testing.T) {
	ref, err := name.NewDigest("ghcr.io/test/repo@" + testDigest)
	if err != nil {
		t.Fatalf("NewDigest: %v", err)
	}
	data, err := payload.Cosign{Image: ref}.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	otherRef, err := name.NewDigest("ghcr.io/other/repo@" + testDigest)
	if err != nil {
		t.Fatalf("NewDigest other: %v", err)
	}
	err = validatePayload(data, otherRef)
	if err == nil {
		t.Fatal("expected error for wrong identity")
	}
	if !strings.Contains(err.Error(), "does not match expected") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidatePayload_InvalidType(t *testing.T) {
	badPayload := `{"critical":{"identity":{"docker-reference":"ghcr.io/test/repo"},"image":{"docker-manifest-digest":"` + testDigest + `"},"type":"wrong type"},"optional":null}`
	ref, err := name.NewDigest("ghcr.io/test/repo@" + testDigest)
	if err != nil {
		t.Fatalf("NewDigest: %v", err)
	}
	err = validatePayload([]byte(badPayload), ref)
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
	if !strings.Contains(err.Error(), "payload content mismatch") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// --- Bundle fetching (in-memory OCI registry) ---

func newTestVerifier(t *testing.T, srv *httptest.Server) *Verifier {
	t.Helper()
	host := strings.TrimPrefix(srv.URL, "http://")
	v := NewFromTrustedMaterial(nil, anonKeychain{})
	v.nameOpts = []name.Option{name.Insecure}
	_ = host
	return v
}

func TestFetchBundle_InvalidImageRef(t *testing.T) {
	v := NewFromTrustedMaterial(nil, anonKeychain{})
	v.nameOpts = []name.Option{name.Insecure}
	_, err := v.fetchBundle(context.Background(), "not-a-digest-ref:tag")
	if err == nil {
		t.Fatal("expected error for non-digest ref")
	}
	if !strings.Contains(err.Error(), "parse digest ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchBundle_NoReferrers(t *testing.T) {
	srv := httptest.NewServer(registry.New(registry.WithReferrersSupport(true)))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	imgDigest := pushImage(t, srv.URL, "test/app")
	imageRef := host + "/test/app@" + imgDigest

	v := NewFromTrustedMaterial(nil, anonKeychain{})
	v.nameOpts = []name.Option{name.Insecure}

	_, err := v.fetchBundle(context.Background(), imageRef)
	if err == nil {
		t.Fatal("expected error when no referrers exist")
	}
	if !strings.Contains(err.Error(), "no sigstore bundle found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchBundle_WrongArtifactType(t *testing.T) {
	srv := httptest.NewServer(registry.New(registry.WithReferrersSupport(true)))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	imgDigest := pushImage(t, srv.URL, "test/app")
	pushBundleReferrer(t, srv.URL, "test/app", imgDigest, `{"hello":"world"}`, "application/vnd.wrong.type")
	imageRef := host + "/test/app@" + imgDigest

	v := NewFromTrustedMaterial(nil, anonKeychain{})
	v.nameOpts = []name.Option{name.Insecure}

	_, err := v.fetchBundle(context.Background(), imageRef)
	if err == nil {
		t.Fatal("expected error when no bundle referrer matches artifactType")
	}
	if !strings.Contains(err.Error(), "no sigstore bundle found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchBundle_ValidBundleInConfig(t *testing.T) {
	srv := httptest.NewServer(registry.New(registry.WithReferrersSupport(true)))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	imgDigest := pushImage(t, srv.URL, "test/app")
	bundleData := `{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","content":"test"}`
	pushBundleReferrer(t, srv.URL, "test/app", imgDigest, bundleData, bundleArtifactType)
	imageRef := host + "/test/app@" + imgDigest

	v := NewFromTrustedMaterial(nil, anonKeychain{})
	v.nameOpts = []name.Option{name.Insecure}

	got, err := v.fetchBundle(context.Background(), imageRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != bundleData {
		t.Fatalf("bundle mismatch:\ngot  %s\nwant %s", got, bundleData)
	}
}

func TestFetchBundle_OversizedConfig(t *testing.T) {
	srv := httptest.NewServer(registry.New(registry.WithReferrersSupport(true)))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	imgDigest := pushImage(t, srv.URL, "test/app")
	// Build JSON > 1MB: starts with '{' so the size check fires.
	large := make([]byte, (1<<20)+1)
	large[0] = '{'
	for i := 1; i < len(large)-1; i++ {
		large[i] = 'a'
	}
	large[len(large)-1] = '}'
	pushBundleReferrer(t, srv.URL, "test/app", imgDigest, string(large), bundleArtifactType)
	imageRef := host + "/test/app@" + imgDigest

	v := NewFromTrustedMaterial(nil, anonKeychain{})
	v.nameOpts = []name.Option{name.Insecure}

	_, err := v.fetchBundle(context.Background(), imageRef)
	if err == nil {
		t.Fatal("expected error for oversized config")
	}
	if !strings.Contains(err.Error(), "exceeds size limit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Verify error paths ---

func TestVerify_InvalidImageRef(t *testing.T) {
	v := NewFromTrustedMaterial(nil, anonKeychain{})
	v.nameOpts = []name.Option{name.Insecure}
	err := v.Verify(context.Background(), "not-a-digest:tag", "org/repo")
	if !errors.Is(err, model.ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid, got %v", err)
	}
	if !strings.Contains(err.Error(), "parse image ref") {
		t.Fatalf("expected 'parse image ref' in error, got: %v", err)
	}
}

func TestVerify_NoBundleFound(t *testing.T) {
	srv := httptest.NewServer(registry.New(registry.WithReferrersSupport(true)))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	imgDigest := pushImage(t, srv.URL, "test/app")
	imageRef := host + "/test/app@" + imgDigest

	v := NewFromTrustedMaterial(nil, anonKeychain{})
	v.nameOpts = []name.Option{name.Insecure}

	err := v.Verify(context.Background(), imageRef, "org/repo")
	if !errors.Is(err, model.ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid, got %v", err)
	}
	if !strings.Contains(err.Error(), "no sigstore bundle found") {
		t.Fatalf("expected bundle error, got: %v", err)
	}
}

func TestVerify_MalformedBundleJSON(t *testing.T) {
	srv := httptest.NewServer(registry.New(registry.WithReferrersSupport(true)))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	imgDigest := pushImage(t, srv.URL, "test/app")
	pushBundleReferrer(t, srv.URL, "test/app", imgDigest, `{invalid json`, bundleArtifactType)
	imageRef := host + "/test/app@" + imgDigest

	v := NewFromTrustedMaterial(nil, anonKeychain{})
	v.nameOpts = []name.Option{name.Insecure}

	err := v.Verify(context.Background(), imageRef, "org/repo")
	if !errors.Is(err, model.ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid, got %v", err)
	}
	if !strings.Contains(err.Error(), "parse bundle") {
		t.Fatalf("expected 'parse bundle' in error, got: %v", err)
	}
}

func TestVerify_NoMessageSignature(t *testing.T) {
	srv := httptest.NewServer(registry.New(registry.WithReferrersSupport(true)))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	imgDigest := pushImage(t, srv.URL, "test/app")
	// A bundle with a DSSE envelope (no messageSignature) that passes validate().
	dsseBundle := `{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","verificationMaterial":{"publicKey":{"hint":"test"}},"dsseEnvelope":{}}`
	pushBundleReferrer(t, srv.URL, "test/app", imgDigest, dsseBundle, bundleArtifactType)
	imageRef := host + "/test/app@" + imgDigest

	v := NewFromTrustedMaterial(nil, anonKeychain{})
	v.nameOpts = []name.Option{name.Insecure}

	err := v.Verify(context.Background(), imageRef, "org/repo")
	if !errors.Is(err, model.ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid, got %v", err)
	}
	if !strings.Contains(err.Error(), "no message signature") {
		t.Fatalf("expected 'no message signature' in error, got: %v", err)
	}
}

// --- Registry helpers ---

// pushImage pushes a minimal OCI image to the test registry and returns its digest.
func pushImage(t *testing.T, registryBase, repo string) string {
	t.Helper()

	configBlob := []byte(`{}`)
	configDigest := pushBlob(t, registryBase, repo, configBlob)

	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]interface{}{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    configDigest,
			"size":      len(configBlob),
		},
		"layers": []interface{}{},
	}
	return pushManifest(t, registryBase, repo, "latest", manifest)
}

// pushBundleReferrer pushes an OCI artifact manifest as a referrer of subjectDigest.
// The artifactType is set as the config.mediaType (the in-memory registry derives
// artifactType in the referrers index from config.mediaType).
func pushBundleReferrer(t *testing.T, registryBase, repo, subjectDigest, configJSON, artifactType string) {
	t.Helper()

	configBlob := []byte(configJSON)
	configBlobDigest := pushBlob(t, registryBase, repo, configBlob)

	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]interface{}{
			"mediaType": artifactType,
			"digest":    configBlobDigest,
			"size":      len(configBlob),
		},
		"layers": []interface{}{},
		"subject": map[string]interface{}{
			"mediaType": "application/vnd.oci.image.manifest.v1+json",
			"digest":    subjectDigest,
			"size":      0,
		},
	}
	pushManifest(t, registryBase, repo, "bundle-ref", manifest)
}

func pushBlob(t *testing.T, registryBase, repo string, content []byte) string {
	t.Helper()
	h := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(h[:])

	startResp, err := http.Post(registryBase+"/v2/"+repo+"/blobs/uploads/", "", nil)
	if err != nil {
		t.Fatalf("start blob upload: %v", err)
	}
	startResp.Body.Close()
	location := startResp.Header.Get("Location")
	if location == "" {
		t.Fatal("no Location header in blob upload response")
	}

	putURL := registryBase + location + "?digest=" + url.QueryEscape(digest)
	req, _ := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(content))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(content))
	putResp, err := http.DefaultClient.Do(req)
	if err != nil || putResp.StatusCode != http.StatusCreated {
		t.Fatalf("complete blob upload: status=%v err=%v", putResp.StatusCode, err)
	}
	putResp.Body.Close()
	return digest
}

func pushManifest(t *testing.T, registryBase, repo, tag string, manifest map[string]interface{}) string {
	t.Helper()
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPut, registryBase+"/v2/"+repo+"/manifests/"+tag, bytes.NewReader(manifestJSON))
	req.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("push manifest: status=%v err=%v", resp.StatusCode, err)
	}
	resp.Body.Close()

	h := sha256.Sum256(manifestJSON)
	return "sha256:" + hex.EncodeToString(h[:])
}
