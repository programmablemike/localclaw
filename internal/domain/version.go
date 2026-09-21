// Package domain holds LocalClaw's entities and pure rules. It imports
// nothing from this module and performs no I/O.
package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a semantic version. Raw keeps the string it was parsed from,
// including any pre-release or build suffix, for display.
type Version struct {
	Major, Minor, Patch int
	Raw                 string
}

// ParseVersion parses "major[.minor[.patch]]" and ignores anything after the
// first "-" or "+", so "1.13.1-g684cdfb" and "2.0.0+build.7" both parse.
// Missing components are zero.
func ParseVersion(s string) (Version, error) {
	raw := strings.TrimSpace(s)
	core := raw
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	if core == "" {
		return Version{}, fmt.Errorf("parse version %q: empty", raw)
	}
	parts := strings.Split(core, ".")
	if len(parts) > 3 {
		return Version{}, fmt.Errorf("parse version %q: more than three components", raw)
	}
	v := Version{Raw: raw}
	dst := []*int{&v.Major, &v.Minor, &v.Patch}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || strings.HasPrefix(p, "+") {
			return Version{}, fmt.Errorf("parse version %q: %q is not a non-negative integer", raw, p)
		}
		*dst[i] = n
	}
	return v, nil
}

// AtLeast reports whether v is greater than or equal to min, comparing
// major, minor and patch in that order. Suffixes are ignored.
func (v Version) AtLeast(min Version) bool {
	if v.Major != min.Major {
		return v.Major > min.Major
	}
	if v.Minor != min.Minor {
		return v.Minor > min.Minor
	}
	return v.Patch >= min.Patch
}

// String renders the numeric core as "major.minor.patch".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}
