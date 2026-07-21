package updater

import "testing"

func TestCompareVersionNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "fourth component is newer", left: "0.0.40.1", right: "0.0.40", want: 1},
		{name: "fourth component is older", left: "0.0.40", right: "0.0.40.1", want: -1},
		{name: "missing components are zero", left: "0.0.40.0", right: "v0.0.40", want: 0},
		{name: "later major version wins", left: "1.0.0", right: "0.0.40.1", want: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := compareVersionNumbers(test.left, test.right); got != test.want {
				t.Fatalf("compareVersionNumbers(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "four-component lite release supersedes prior lite release", left: "0.0.40.1-lite", right: "0.0.40-lite", want: 1},
		{name: "lite supersedes matching stable version", left: "0.0.40.1-lite", right: "0.0.40.1", want: 1},
		{name: "stable supersedes ordinary prerelease", left: "0.0.40.1", right: "0.0.40.1-beta", want: 1},
		{name: "matching versions are equal", left: "v0.0.40.1-lite", right: "0.0.40.1-lite", want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := compareVersions(test.left, test.right); got != test.want {
				t.Fatalf("compareVersions(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
			}
		})
	}
}
