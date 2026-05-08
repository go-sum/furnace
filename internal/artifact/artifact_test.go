package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
)

// anonymousKeychain is a Keychain that always resolves to authn.Anonymous.
type anonymousKeychain struct{}

func (anonymousKeychain) Resolve(_ authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

// fakeArtifactVerifier is an artifactVerifier for tests.
type fakeArtifactVerifier struct{ err error }

func (f *fakeArtifactVerifier) Verify(_ context.Context, _, _ string) error { return f.err }

func TestValidateArtifactFilename(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid compose", "docker-compose.yml", false},
		{"valid data", "docker-compose.data.yml", false},
		{"traversal", "../etc/passwd", true},
		{"absolute", "/etc/passwd", true},
		{"subdir", "subdir/file.yml", true},
		{"empty", "", true},
		{"dot", ".", true},
		{"dotdot", "..", true},
		{"furnace prefix staging", ".furnace-staging", true},
		{"furnace prefix files", ".furnace-files", true},
		{"furnace prefix artifact", ".furnace-artifact", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateArtifactFilename(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateArtifactFilename(%q) error=%v wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestResolveArtifactRef(t *testing.T) {
	got := ResolveArtifactRef("ghcr.io/go-sum/furnace:{tag}-compose", "v1.2.3")
	want := "ghcr.io/go-sum/furnace:v1.2.3-compose"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestArtifactFetcher_VerifierRejectsSignature(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	pushTestArtifact(t, srv.URL, "test/app", "v1.0.0-compose", map[string][]byte{
		"docker-compose.yml": []byte("services:\n  web:\n    image: test\n"),
	})

	fetcher := NewArtifactFetcher(&fakeArtifactVerifier{err: fmt.Errorf("signature invalid")}, anonymousKeychain{})
	fetcher.nameOpts = []name.Option{name.Insecure}

	dir := t.TempDir()
	_, err := fetcher.FetchAndVerify(context.Background(), host+"/test/app:v1.0.0-compose", "org/repo", dir)
	if err == nil {
		t.Fatal("expected error from verifier")
	}
	if !strings.Contains(err.Error(), "verify compose artifact") {
		t.Fatalf("unexpected error: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected no files written, got %d", len(entries))
	}
}

func TestArtifactFetcher_WritesFilesToDestDir(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	files := map[string][]byte{
		"docker-compose.yml": []byte("services:\n  web:\n    image: ${APP_IMAGE}\n"),
	}
	pushTestArtifact(t, srv.URL, "test/app", "v1.0.0-compose", files)

	fetcher := NewArtifactFetcher(&fakeArtifactVerifier{}, anonymousKeychain{})
	fetcher.nameOpts = []name.Option{name.Insecure}

	dir := t.TempDir()
	if _, err := fetcher.FetchAndVerify(context.Background(), host+"/test/app:v1.0.0-compose", "org/repo", dir); err != nil {
		t.Fatalf("FetchAndVerify: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("expected docker-compose.yml to be written: %v", err)
	}
	if string(got) != string(files["docker-compose.yml"]) {
		t.Fatalf("content mismatch: got %q want %q", got, files["docker-compose.yml"])
	}
}

func TestArtifactFetcher_EmptyArtifact(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	pushTestArtifact(t, srv.URL, "test/empty", "v1.0.0-compose", map[string][]byte{})

	fetcher := NewArtifactFetcher(&fakeArtifactVerifier{}, anonymousKeychain{})
	fetcher.nameOpts = []name.Option{name.Insecure}

	_, err := fetcher.FetchAndVerify(context.Background(), host+"/test/empty:v1.0.0-compose", "org/repo", t.TempDir())
	if err == nil {
		t.Fatal("expected error for empty artifact")
	}
}

func TestValidateArtifactFilename_RejectsReservedName(t *testing.T) {
	err := validateArtifactFilename(artifactTrackingFile)
	if err == nil {
		t.Fatalf("expected error for reserved filename %q", artifactTrackingFile)
	}
}

func TestArtifactFetcher_FirstRunWritesTrackingFile(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	pushTestArtifact(t, srv.URL, "test/track", "v1.0.0-compose", map[string][]byte{
		"docker-compose.yml": []byte("services:\n  web:\n    image: test\n"),
	})

	fetcher := NewArtifactFetcher(&fakeArtifactVerifier{}, anonymousKeychain{})
	fetcher.nameOpts = []name.Option{name.Insecure}

	dir := t.TempDir()
	if _, err := fetcher.FetchAndVerify(context.Background(), host+"/test/track:v1.0.0-compose", "org/repo", dir); err != nil {
		t.Fatalf("FetchAndVerify: %v", err)
	}

	trackData, err := os.ReadFile(filepath.Join(dir, artifactTrackingFile))
	if err != nil {
		t.Fatalf("tracking file not created: %v", err)
	}
	if string(trackData) != "docker-compose.yml" {
		t.Fatalf("tracking file: got %q want %q", string(trackData), "docker-compose.yml")
	}
}

func TestArtifactFetcher_ReturnsDigest(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	pushTestArtifact(t, srv.URL, "test/digest", "v1.0.0-compose", map[string][]byte{
		"docker-compose.yml": []byte("services:\n  web:\n    image: test\n"),
	})

	fetcher := NewArtifactFetcher(&fakeArtifactVerifier{}, anonymousKeychain{})
	fetcher.nameOpts = []name.Option{name.Insecure}

	dir := t.TempDir()
	digest, err := fetcher.FetchAndVerify(context.Background(), host+"/test/digest:v1.0.0-compose", "org/repo", dir)
	if err != nil {
		t.Fatalf("FetchAndVerify: %v", err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("expected sha256: digest, got %q", digest)
	}
}

func TestArtifactFetcher_PartialFetchLeavesNoCommittedFiles(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	pushTestArtifactWithMissingBlob(t, srv.URL, "test/noclean", "v1.0.0-compose",
		map[string][]byte{
			"docker-compose.yml": []byte("services:\n  web:\n    image: test\n"),
		},
		[]string{"missing-layer.yml"},
	)

	fetcher := NewArtifactFetcher(&fakeArtifactVerifier{}, anonymousKeychain{})
	fetcher.nameOpts = []name.Option{name.Insecure}

	dir := t.TempDir()
	_, err := fetcher.FetchAndVerify(context.Background(), host+"/test/noclean:v1.0.0-compose", "org/repo", dir)
	if err == nil {
		t.Fatal("expected error for missing blob")
	}
	// No committed compose files should exist.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yaml") {
			t.Fatalf("unexpected committed file after failure: %s", e.Name())
		}
	}
}

func TestArtifactFetcher_ExistingFileNotCorruptedOnWriteFailure(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	originalContent := []byte("original content")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), originalContent, 0644); err != nil {
		t.Fatalf("pre-place file: %v", err)
	}

	// Artifact with docker-compose.yml (fetched OK) + overlay.yml blob missing.
	pushTestArtifactWithMissingBlob(t, srv.URL, "test/corrupt", "v1.0.0-compose",
		map[string][]byte{
			"docker-compose.yml": []byte("new content"),
		},
		[]string{"overlay.yml"},
	)

	fetcher := NewArtifactFetcher(&fakeArtifactVerifier{}, anonymousKeychain{})
	fetcher.nameOpts = []name.Option{name.Insecure}

	if _, err := fetcher.FetchAndVerify(context.Background(), host+"/test/corrupt:v1.0.0-compose", "org/repo", dir); err == nil {
		t.Fatal("expected error for missing blob")
	}

	got, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read existing file: %v", err)
	}
	if string(got) != string(originalContent) {
		t.Fatalf("existing file corrupted: got %q want %q", got, originalContent)
	}
}

func TestArtifactFetcher_OperatorFilesUntouched(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	dir := t.TempDir()
	deployEnvContent := []byte("APP_IMAGE=old\n")
	if err := os.WriteFile(filepath.Join(dir, ".deploy.env"), deployEnvContent, 0644); err != nil {
		t.Fatalf("write .deploy.env: %v", err)
	}

	pushTestArtifact(t, srv.URL, "test/opfiles", "v1.0.0-compose", map[string][]byte{
		"docker-compose.yml": []byte("services:\n  web:\n    image: test\n"),
	})

	fetcher := NewArtifactFetcher(&fakeArtifactVerifier{}, anonymousKeychain{})
	fetcher.nameOpts = []name.Option{name.Insecure}

	if _, err := fetcher.FetchAndVerify(context.Background(), host+"/test/opfiles:v1.0.0-compose", "org/repo", dir); err != nil {
		t.Fatalf("FetchAndVerify: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, ".deploy.env"))
	if err != nil {
		t.Fatalf(".deploy.env was removed: %v", err)
	}
	if string(got) != string(deployEnvContent) {
		t.Fatalf(".deploy.env content changed: got %q want %q", got, deployEnvContent)
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err != nil {
		t.Fatalf("expected docker-compose.yml to exist: %v", err)
	}
}

func TestFetchBlob_OversizedRejected(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	// Build content that exceeds maxBlobSize by 1 byte.
	oversized := make([]byte, maxBlobSize+1)
	for i := range oversized {
		oversized[i] = 'x'
	}
	pushTestArtifact(t, srv.URL, "test/oversized", "v1.0.0-compose", map[string][]byte{
		"docker-compose.yml": oversized,
	})

	fetcher := NewArtifactFetcher(&fakeArtifactVerifier{}, anonymousKeychain{})
	fetcher.nameOpts = []name.Option{name.Insecure}

	dir := t.TempDir()
	_, err := fetcher.FetchAndVerify(context.Background(), host+"/test/oversized:v1.0.0-compose", "org/repo", dir)
	if err == nil {
		t.Fatal("expected error for oversized blob")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected 'exceeds maximum size' in error, got: %v", err)
	}
}

func TestArtifactFetcher_RejectsFurnacePrefixFilename(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	pushTestArtifact(t, srv.URL, "test/reserved", "v1.0.0-compose", map[string][]byte{
		".furnace-custom": []byte("data"),
	})

	fetcher := NewArtifactFetcher(&fakeArtifactVerifier{}, anonymousKeychain{})
	fetcher.nameOpts = []name.Option{name.Insecure}

	dir := t.TempDir()
	_, err := fetcher.FetchAndVerify(context.Background(), host+"/test/reserved:v1.0.0-compose", "org/repo", dir)
	if err == nil {
		t.Fatal("expected error for reserved filename")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected error to mention 'reserved', got: %v", err)
	}
}

// pushTestArtifact pushes an OCI artifact to a local test registry via the
// OCI Distribution Spec HTTP API. Filenames are stored as org.opencontainers.image.title
// annotations on each layer descriptor.
func pushTestArtifact(t *testing.T, registryBase, repo, tag string, files map[string][]byte) {
	t.Helper()

	type descriptor struct {
		MediaType   string            `json:"mediaType"`
		Digest      string            `json:"digest"`
		Size        int64             `json:"size"`
		Annotations map[string]string `json:"annotations,omitempty"`
	}

	pushBlob := func(content []byte) string {
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

	// Push empty config blob (required by OCI manifest spec).
	emptyConfig := []byte("{}")
	configDigest := pushBlob(emptyConfig)

	// Push each file blob.
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	layers := make([]descriptor, 0, len(files))
	for _, filename := range names {
		content := files[filename]
		digest := pushBlob(content)
		layers = append(layers, descriptor{
			MediaType: "application/yaml",
			Digest:    digest,
			Size:      int64(len(content)),
			Annotations: map[string]string{
				"org.opencontainers.image.title": filename,
			},
		})
	}

	manifest := struct {
		SchemaVersion int          `json:"schemaVersion"`
		MediaType     string       `json:"mediaType"`
		ArtifactType  string       `json:"artifactType"`
		Config        descriptor   `json:"config"`
		Layers        []descriptor `json:"layers"`
	}{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		ArtifactType:  "application/vnd.furnace.compose",
		Config: descriptor{
			MediaType: "application/vnd.oci.empty.v1+json",
			Digest:    configDigest,
			Size:      int64(len(emptyConfig)),
		},
		Layers: layers,
	}

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
}

// pushTestArtifactWithMissingBlob pushes an OCI artifact where the blobs named
// in missingNames are referenced by the manifest but never uploaded to the
// registry. This causes fetchBlob to fail for those layers, allowing tests to
// verify that no partial writes occur.
func pushTestArtifactWithMissingBlob(t *testing.T, registryBase, repo, tag string, files map[string][]byte, missingNames []string) {
	t.Helper()

	type descriptor struct {
		MediaType   string            `json:"mediaType"`
		Digest      string            `json:"digest"`
		Size        int64             `json:"size"`
		Annotations map[string]string `json:"annotations,omitempty"`
	}

	pushBlob := func(content []byte) string {
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

	emptyConfig := []byte("{}")
	configDigest := pushBlob(emptyConfig)

	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	layers := make([]descriptor, 0, len(files)+len(missingNames))
	for _, filename := range names {
		content := files[filename]
		digest := pushBlob(content)
		layers = append(layers, descriptor{
			MediaType: "application/yaml",
			Digest:    digest,
			Size:      int64(len(content)),
			Annotations: map[string]string{
				"org.opencontainers.image.title": filename,
			},
		})
	}

	// Compute digest for missing blobs without uploading them.
	for _, filename := range missingNames {
		content := []byte("blob-not-uploaded:" + filename)
		h := sha256.Sum256(content)
		digest := "sha256:" + hex.EncodeToString(h[:])
		layers = append(layers, descriptor{
			MediaType: "application/yaml",
			Digest:    digest,
			Size:      int64(len(content)),
			Annotations: map[string]string{
				"org.opencontainers.image.title": filename,
			},
		})
	}

	manifest := struct {
		SchemaVersion int          `json:"schemaVersion"`
		MediaType     string       `json:"mediaType"`
		ArtifactType  string       `json:"artifactType"`
		Config        descriptor   `json:"config"`
		Layers        []descriptor `json:"layers"`
	}{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		ArtifactType:  "application/vnd.furnace.compose",
		Config: descriptor{
			MediaType: "application/vnd.oci.empty.v1+json",
			Digest:    configDigest,
			Size:      int64(len(emptyConfig)),
		},
		Layers: layers,
	}

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
}
