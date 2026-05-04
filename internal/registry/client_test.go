package registry

import (
	"testing"
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
		{"v1.2.3", semver{1, 2, 3, ""}, true},
		{"v10.0.0", semver{10, 0, 0, ""}, true},
		{"v1.2.3-rc1", semver{1, 2, 3, "rc1"}, true}, // pre-release captured
		{"1.2.3", semver{1, 2, 3, ""}, true},          // no leading v
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
		{semver{2, 0, 0, ""}, semver{1, 0, 0, ""}, 1},
		{semver{1, 0, 0, ""}, semver{2, 0, 0, ""}, -1},
		{semver{1, 2, 3, ""}, semver{1, 2, 3, ""}, 0},
		{semver{1, 10, 0, ""}, semver{1, 9, 0, ""}, 1},
		{semver{1, 0, 5, ""}, semver{1, 0, 3, ""}, 1},
		// Release > pre-release at same version numbers.
		{semver{1, 0, 0, ""}, semver{1, 0, 0, "rc1"}, 1},
		{semver{1, 0, 0, "rc1"}, semver{1, 0, 0, ""}, -1},
		// Lexicographic among pre-releases.
		{semver{1, 0, 0, "rc2"}, semver{1, 0, 0, "rc1"}, 1},
	}
	for _, tc := range cases {
		got := compareSemver(tc.a, tc.b)
		if got != tc.want {
			t.Fatalf("compareSemver(%+v, %+v): got %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
