package updater

import (
	"strconv"
	"strings"
)

type semanticVersion struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func compareVersionNumbers(a, b string) int {
	left := parseVersion(a)
	right := parseVersion(b)

	switch {
	case left.major != right.major:
		return compareInts(left.major, right.major)
	case left.minor != right.minor:
		return compareInts(left.minor, right.minor)
	default:
		return compareInts(left.patch, right.patch)
	}
}

func compareVersions(a, b string) int {
	left := parseVersion(a)
	right := parseVersion(b)

	if result := compareVersionNumbers(a, b); result != 0 {
		return result
	}

	leftRank := prereleaseRank(left.prerelease)
	rightRank := prereleaseRank(right.prerelease)
	if leftRank != rightRank {
		return compareInts(leftRank, rightRank)
	}
	if left.prerelease == right.prerelease {
		return 0
	}
	if left.prerelease > right.prerelease {
		return 1
	}
	if left.prerelease < right.prerelease {
		return -1
	}
	return 0
}

func prereleaseRank(value string) int {
	switch value {
	case "lite":
		// Lite is this product's enhanced channel and supersedes the matching stable build.
		return 2
	case "":
		return 1
	default:
		return 0
	}
}

func parseVersion(raw string) semanticVersion {
	clean := strings.TrimSpace(strings.TrimPrefix(raw, "v"))
	if clean == "" {
		return semanticVersion{}
	}

	var prerelease string
	if idx := strings.Index(clean, "-"); idx >= 0 {
		prerelease = clean[idx+1:]
		clean = clean[:idx]
	}

	parts := strings.Split(clean, ".")
	result := semanticVersion{prerelease: prerelease}
	if len(parts) > 0 {
		result.major = atoi(parts[0])
	}
	if len(parts) > 1 {
		result.minor = atoi(parts[1])
	}
	if len(parts) > 2 {
		result.patch = atoi(parts[2])
	}
	return result
}

func atoi(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}

func compareInts(a, b int) int {
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	default:
		return 0
	}
}
