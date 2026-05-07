package registry

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// filterAndSortTags returns the tags matching pattern sorted newest-first.
// Semver tags sort before non-semver tags; non-semver tags fall back to
// descending lexical order.
func filterAndSortTags(tags []string, pattern string) ([]string, error) {
	matching := make([]string, 0, len(tags))
	for _, tag := range tags {
		ok, err := path.Match(pattern, tag)
		if err != nil {
			return nil, fmt.Errorf("match tag pattern %q: %w", pattern, err)
		}
		if ok {
			matching = append(matching, tag)
		}
	}
	sortTagsDesc(matching)
	return matching, nil
}

// sortTagsDesc sorts tags newest-first.
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
	Major, Minor, Patch int
	Prerelease          string
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
	return semver{Major: major, Minor: minor, Patch: patch, Prerelease: pre}, nil
}

func compareSemver(a, b semver) int {
	if d := a.Major - b.Major; d != 0 {
		return sign(d)
	}
	if d := a.Minor - b.Minor; d != 0 {
		return sign(d)
	}
	if d := a.Patch - b.Patch; d != 0 {
		return sign(d)
	}
	switch {
	case a.Prerelease == "" && b.Prerelease != "":
		return 1
	case a.Prerelease != "" && b.Prerelease == "":
		return -1
	default:
		return comparePrerelease(a.Prerelease, b.Prerelease)
	}
}

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
			return -1
		case bErr == nil:
			return 1
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
