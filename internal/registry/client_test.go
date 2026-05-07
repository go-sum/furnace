package registry

import (
	"errors"
	"testing"

	gcr "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestSortTagsDesc_Semver(t *testing.T) {
	tags := []string{"v1.0.0", "v10.0.0", "v2.3.0", "v2.10.0", "v0.9.9"}
	sortTagsDesc(tags)
	want := []string{"v10.0.0", "v2.10.0", "v2.3.0", "v1.0.0", "v0.9.9"}
	for i, got := range tags {
		if got != want[i] {
			t.Fatalf("sortTagsDesc[%d]: got %q, want %q", i, got, want[i])
		}
	}
}

func TestSortTagsDesc_PreRelease(t *testing.T) {
	tags := []string{"v1.0.0-rc1", "v1.0.0", "v0.9.0"}
	sortTagsDesc(tags)
	// Release sorts higher than pre-release; v0.9.0 last.
	want := []string{"v1.0.0", "v1.0.0-rc1", "v0.9.0"}
	for i, got := range tags {
		if got != want[i] {
			t.Fatalf("sortTagsDesc[%d]: got %q, want %q", i, got, want[i])
		}
	}
}

func TestSortTagsDesc_MultiplePreReleases(t *testing.T) {
	tags := []string{"v1.0.0-beta1", "v1.0.0", "v1.0.0-rc1", "v1.0.0-rc2"}
	sortTagsDesc(tags)
	want := []string{"v1.0.0", "v1.0.0-rc2", "v1.0.0-rc1", "v1.0.0-beta1"}
	for i, got := range tags {
		if got != want[i] {
			t.Fatalf("sortTagsDesc[%d]: got %q, want %q", i, got, want[i])
		}
	}
}

func TestSortTagsDesc_NonSemver(t *testing.T) {
	tags := []string{"main", "latest", "abc"}
	sortTagsDesc(tags)
	// Falls back to lexicographic descending: main > latest > abc
	want := []string{"main", "latest", "abc"}
	for i, got := range tags {
		if got != want[i] {
			t.Fatalf("sortTagsDesc[%d]: got %q, want %q", i, got, want[i])
		}
	}
}

func TestParseSemver(t *testing.T) {
	cases := []struct {
		input string
		want  semver
		ok    bool
	}{
		{"v1.2.3", semver{Major: 1, Minor: 2, Patch: 3}, true},
		{"v10.0.0", semver{Major: 10, Minor: 0, Patch: 0}, true},
		{"v1.2.3-rc1", semver{Major: 1, Minor: 2, Patch: 3, Prerelease: "rc1"}, true},
		{"1.2.3", semver{Major: 1, Minor: 2, Patch: 3}, true},
		{"latest", semver{}, false},
		{"v1.2", semver{}, false},
	}
	for _, tc := range cases {
		got, err := parseSemver(tc.input)
		if tc.ok && err != nil {
			t.Fatalf("parseSemver(%q): unexpected error: %v", tc.input, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("parseSemver(%q): expected error, got %+v", tc.input, got)
		}
		if tc.ok && got != tc.want {
			t.Fatalf("parseSemver(%q): got %+v, want %+v", tc.input, got, tc.want)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b semver
		want int
	}{
		{semver{Major: 2, Minor: 0, Patch: 0}, semver{Major: 1, Minor: 0, Patch: 0}, 1},
		{semver{Major: 1, Minor: 0, Patch: 0}, semver{Major: 2, Minor: 0, Patch: 0}, -1},
		{semver{Major: 1, Minor: 2, Patch: 3}, semver{Major: 1, Minor: 2, Patch: 3}, 0},
		{semver{Major: 1, Minor: 10, Patch: 0}, semver{Major: 1, Minor: 9, Patch: 0}, 1},
		{semver{Major: 1, Minor: 0, Patch: 5}, semver{Major: 1, Minor: 0, Patch: 3}, 1},
		// Release > pre-release at same version numbers.
		{semver{Major: 1, Minor: 0, Patch: 0}, semver{Major: 1, Minor: 0, Patch: 0, Prerelease: "rc1"}, 1},
		{semver{Major: 1, Minor: 0, Patch: 0, Prerelease: "rc1"}, semver{Major: 1, Minor: 0, Patch: 0}, -1},
		// Lexicographic among pre-releases.
		{semver{Major: 1, Minor: 0, Patch: 0, Prerelease: "rc2"}, semver{Major: 1, Minor: 0, Patch: 0, Prerelease: "rc1"}, 1},
	}
	for _, tc := range cases {
		got := compareSemver(tc.a, tc.b)
		if got != tc.want {
			t.Fatalf("compareSemver(%+v, %+v): got %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestIsDeployableImageDescriptor(t *testing.T) {
	cases := []struct {
		name string
		desc gcr.Descriptor
		want bool
	}{
		{
			name: "oci image index",
			desc: gcr.Descriptor{MediaType: types.OCIImageIndex},
			want: true,
		},
		{
			name: "docker manifest list",
			desc: gcr.Descriptor{MediaType: types.DockerManifestList},
			want: true,
		},
		{
			name: "oci image manifest",
			desc: gcr.Descriptor{MediaType: types.OCIManifestSchema1},
			want: true,
		},
		{
			name: "artifact manifest rejected",
			desc: gcr.Descriptor{
				MediaType:    types.OCIManifestSchema1,
				ArtifactType: "application/vnd.furnace.compose",
			},
			want: false,
		},
		{
			name: "unknown media type rejected",
			desc: gcr.Descriptor{MediaType: "application/octet-stream"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDeployableImageDescriptor(tc.desc); got != tc.want {
				t.Fatalf("isDeployableImageDescriptor(%+v) = %v, want %v", tc.desc, got, tc.want)
			}
		})
	}
}

func TestSelectLatestDeployableTag(t *testing.T) {
	tags := []string{"v0.1.99-test1-compose", "v0.1.99-test1", "v0.1.98"}
	descByTag := map[string]gcr.Descriptor{
		"v0.1.99-test1-compose": {
			MediaType:    types.OCIManifestSchema1,
			ArtifactType: "application/vnd.furnace.compose",
			Digest:       mustDigestHash(t, "sha256:fb58c2f5ef713189b089cd0a4b8cc77130006f99da6eaac6f393dec7c4c3b11c"),
		},
		"v0.1.99-test1": {
			MediaType: types.OCIImageIndex,
			Digest:    mustDigestHash(t, "sha256:bd2437990a7019548c0908d81b7a34a605836a788bbb72040b0bd4c45c067137"),
		},
	}

	tag, digest, err := selectLatestDeployableTag(tags, func(tag string) (gcr.Descriptor, error) {
		desc, ok := descByTag[tag]
		if !ok {
			return gcr.Descriptor{}, errors.New("unexpected tag lookup")
		}
		return desc, nil
	})
	if err != nil {
		t.Fatalf("selectLatestDeployableTag: %v", err)
	}
	if tag != "v0.1.99-test1" {
		t.Fatalf("tag = %q, want %q", tag, "v0.1.99-test1")
	}
	wantDigest := "sha256:bd2437990a7019548c0908d81b7a34a605836a788bbb72040b0bd4c45c067137"
	if digest != wantDigest {
		t.Fatalf("digest = %q, want %q", digest, wantDigest)
	}
}

func TestSelectLatestDeployableTag_NoDeployableTags(t *testing.T) {
	tags := []string{"v0.1.99-test1-compose"}
	_, _, err := selectLatestDeployableTag(tags, func(tag string) (gcr.Descriptor, error) {
		return gcr.Descriptor{
			MediaType:    types.OCIManifestSchema1,
			ArtifactType: "application/vnd.furnace.compose",
			Digest:       mustDigestHash(t, "sha256:fb58c2f5ef713189b089cd0a4b8cc77130006f99da6eaac6f393dec7c4c3b11c"),
		}, nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	want := `no deployable image tags found`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func selectLatestDeployableTag(tags []string, resolve func(tag string) (gcr.Descriptor, error)) (string, string, error) {
	for _, candidate := range tags {
		desc, err := resolve(candidate)
		if err != nil {
			return "", "", err
		}
		if !isDeployableImageDescriptor(desc) {
			continue
		}
		return candidate, desc.Digest.String(), nil
	}
	return "", "", errors.New("no deployable image tags found")
}

func mustDigestHash(t *testing.T, value string) gcr.Hash {
	t.Helper()
	h, err := gcr.NewHash(value)
	if err != nil {
		t.Fatalf("NewHash(%q): %v", value, err)
	}
	return h
}
