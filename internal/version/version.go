// Package version parses and compares dotted numeric release versions such
// as "8.3.14".
package version

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// maxParts bounds how many components a version may have.
const maxParts = 4

// Version is a release version made of dot separated numbers. The zero
// Version has no components and is a prefix of every version.
type Version struct {
	parts []int
}

// Parse parses a version such as "8", "8.3" or "8.3.14". Components must be
// plain decimal numbers without signs or leading zeros.
func Parse(s string) (Version, error) {
	if s == "" {
		return Version{}, errors.New("empty version")
	}

	fields := strings.Split(s, ".")
	if len(fields) > maxParts {
		return Version{}, fmt.Errorf("version %q has more than %d parts", s, maxParts)
	}

	parts := make([]int, len(fields))
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 || f != strconv.Itoa(n) {
			return Version{}, fmt.Errorf("invalid version %q", s)
		}
		parts[i] = n
	}

	return Version{parts: parts}, nil
}

// String returns the version in dotted form.
func (v Version) String() string {
	s := make([]string, len(v.parts))
	for i, p := range v.parts {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, ".")
}

// HasPrefix reports whether v starts with every component of prefix, so
// 8.3.14 has the prefixes 8, 8.3 and 8.3.14 but not 8.31.
func (v Version) HasPrefix(prefix Version) bool {
	n := len(prefix.parts)
	return n <= len(v.parts) && slices.Equal(v.parts[:n], prefix.parts)
}

// Compare returns -1, 0 or +1 when a is older than, equal to or newer than
// b. Components are compared as numbers, so 8.10 is newer than 8.9, and a
// version is older than any longer version it is a prefix of.
func Compare(a, b Version) int {
	return slices.Compare(a.parts, b.parts)
}
