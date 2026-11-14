// Package pins compares versions and keeps the overrides a user picked over
// the versions the image pins. The image is the baseline: an override stays
// only until a newer image ships that entity at the override's version or
// later.
package pins

import (
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Bump kinds, from the image or current version to a newer one.
const (
	BumpMajor = "major"
	BumpMinor = "minor"
	BumpPatch = "patch"
)

// Override is a version picked over the image's pin. Image is the pin it
// was picked against, so a later image can tell whether it moved.
type Override struct {
	Version string `json:"version"`
	Image   string `json:"image"`
}

// Overrides are keyed by entity name: an npm package or a mise tool.
type Overrides map[string]Override

var versionPattern = regexp.MustCompile(`^v?[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)

// ValidVersion reports whether v may name a version on a command line.
func ValidVersion(v string) bool { return versionPattern.MatchString(v) }

// Compare orders two versions: dot-separated numbers compare numerically,
// a release sorts after its pre-releases, build metadata is ignored, and
// anything else falls back to string order. It returns -1, 0 or 1.
func Compare(a, b string) int {
	ac, ap := split(a)
	bc, bp := split(b)
	if c := compareParts(ac, bc, true); c != 0 {
		return c
	}
	switch {
	case ap == bp:
		return 0
	case ap == "":
		return 1
	case bp == "":
		return -1
	}
	return compareParts(strings.Split(ap, "."), strings.Split(bp, "."), false)
}

// Stable reports whether v is a release, not a pre-release.
func Stable(v string) bool {
	_, pre := split(v)
	return pre == ""
}

// Bump names the kind of move from from to to: major when the first number
// changes, minor for the second, patch otherwise. It is empty when to is
// not newer.
func Bump(from, to string) string {
	if Compare(to, from) <= 0 {
		return ""
	}
	fc, _ := split(from)
	tc, _ := split(to)
	for i, kind := range []string{BumpMajor, BumpMinor} {
		if part(fc, i) != part(tc, i) {
			return kind
		}
	}
	return BumpPatch
}

// Rebase applies a new image to o. An override is dropped when the image no
// longer ships its entity, or when the image's pin moved to a version at
// least as new as the override; one the image did not move is kept as is.
// Kept overrides record the new pin.
func Rebase(o Overrides, image map[string]string) (Overrides, []string) {
	kept := Overrides{}
	var dropped []string
	for _, name := range slices.Sorted(maps.Keys(o)) {
		ov, pin := o[name], image[name]
		switch {
		case pin == "" || pin == ov.Version:
			dropped = append(dropped, name)
		case pin == ov.Image:
			kept[name] = ov
		case Compare(pin, ov.Version) >= 0:
			dropped = append(dropped, name)
		default:
			kept[name] = Override{Version: ov.Version, Image: pin}
		}
	}
	return kept, dropped
}

// Desired is image with o applied.
func Desired(image map[string]string, o Overrides) map[string]string {
	out := maps.Clone(image)
	if out == nil {
		out = map[string]string{}
	}
	for name, ov := range o {
		if _, ok := out[name]; ok {
			out[name] = ov.Version
		}
	}
	return out
}

// Diff is the overrides that turn image into set: every entity both have
// at a different version.
func Diff(set, image map[string]string) Overrides {
	out := Overrides{}
	for name, v := range set {
		if pin, ok := image[name]; ok && pin != v {
			out[name] = Override{Version: v, Image: pin}
		}
	}
	return out
}

// Versions are the versions o picks, keyed like o.
func (o Overrides) Versions() map[string]string {
	out := make(map[string]string, len(o))
	for name, ov := range o {
		out[name] = ov.Version
	}
	return out
}

func split(v string) (core []string, pre string) {
	v = strings.TrimPrefix(v, "v")
	v, _, _ = strings.Cut(v, "+")
	v, pre, _ = strings.Cut(v, "-")
	return strings.Split(v, "."), pre
}

func part(parts []string, i int) string {
	if i < len(parts) {
		return parts[i]
	}
	return "0"
}

// compareParts compares identifier lists. Numbers sort numerically and
// before words; with pad, a missing part counts as 0, else a shorter list
// sorts first when it is a prefix of the longer.
func compareParts(a, b []string, pad bool) int {
	n := max(len(a), len(b))
	for i := range n {
		if !pad && (i >= len(a) || i >= len(b)) {
			return cmpInt(len(a), len(b))
		}
		x, y := part(a, i), part(b, i)
		xn, xerr := strconv.ParseUint(x, 10, 64)
		yn, yerr := strconv.ParseUint(y, 10, 64)
		switch {
		case xerr == nil && yerr == nil:
			if c := cmpInt(xn, yn); c != 0 {
				return c
			}
		case xerr == nil:
			return -1
		case yerr == nil:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	return 0
}

func cmpInt[T int | uint64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
