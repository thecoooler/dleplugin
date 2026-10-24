package bundle

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Downgrade describes a managed skill installed from a newer CLI release.
type Downgrade struct{ Name, From, To string }

// Downgrades reports release downgrades for unchanged managed copies. Unknown
// development versions are not ordered. Install remains responsible for conflicts.
func Downgrades(root, version string) []Downgrade {
	if safeRoot(root) != nil {
		return nil
	}
	var result []Downgrade
	for _, name := range Names() {
		dir := filepath.Join(root, name)
		if found, err := inspect(dir, name); !found || err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, marker))
		if err != nil {
			continue
		}
		var r receipt
		if json.Unmarshal(b, &r) == nil && newerRelease(r.Version, version) {
			result = append(result, Downgrade{Name: name, From: r.Version, To: version})
		}
	}
	return result
}

// Match complete release tags, including prerelease/build identifiers.
// Development labels such as dev deliberately remain unordered.
var releaseVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func numeric(s string) bool { return s != "" && strings.Trim(s, "0123456789") == "" }

func parseRelease(s string) []string {
	parts := releaseVersion.FindStringSubmatch(s)
	if parts == nil {
		return nil
	}
	for _, id := range strings.Split(parts[4], ".") {
		if numeric(id) && len(id) > 1 && id[0] == '0' {
			return nil
		}
	}
	return parts
}

func compareNumber(a, b string) int {
	if n := cmp.Compare(len(a), len(b)); n != 0 {
		return n
	}
	return strings.Compare(a, b)
}

func newerRelease(a, b string) bool {
	av, bv := parseRelease(a), parseRelease(b)
	if av == nil || bv == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		if n := compareNumber(av[i], bv[i]); n != 0 {
			return n > 0
		}
	}
	if av[4] == "" || bv[4] == "" {
		return av[4] == "" && bv[4] != ""
	}
	ap, bp := strings.Split(av[4], "."), strings.Split(bv[4], ".")
	for i := 0; i < min(len(ap), len(bp)); i++ {
		an, bn := numeric(ap[i]), numeric(bp[i])
		n := strings.Compare(ap[i], bp[i])
		if an && bn {
			n = compareNumber(ap[i], bp[i])
		} else if an != bn {
			if an {
				n = -1
			} else {
				n = 1
			}
		}
		if n != 0 {
			return n > 0
		}
	}
	return len(ap) > len(bp)
}
