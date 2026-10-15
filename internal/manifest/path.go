package manifest

import (
	"fmt"
	"path"
	"strings"
)

// ValidateFilePath accepts relative slash paths that stay inside a DLE root
// on every supported platform. Backslashes and colons are not portable names.
func ValidateFilePath(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\:\x00") || path.Clean(name) == "." {
		return fmt.Errorf("invalid relative file path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return fmt.Errorf("file path %q must not contain '..'", name)
		}
	}
	return nil
}

// NormalizePath normalizes a slash-separated path: repeated
// slashes are collapsed (a leading "//" is preserved), "." segments and the
// trailing slash are dropped, ".." is kept. An empty result is returned as "".
func NormalizePath(p string) string {
	prefix := ""
	if strings.HasPrefix(p, "/") {
		prefix = "/"
		if strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///") {
			prefix = "//"
		}
	}
	var parts []string
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." {
			continue
		}
		parts = append(parts, seg)
	}
	return prefix + strings.Join(parts, "/")
}

// JoinMount joins a normalized mount directory and a relative path.
// An empty mount yields rel unchanged.
func JoinMount(mount, rel string) string {
	if mount == "" {
		return rel
	}
	return NormalizePath(mount + "/" + rel)
}

// ValidateSlug accepts a single portable filename component.
func ValidateSlug(slug string) error {
	if err := ValidateFilePath(slug); err != nil {
		return fmt.Errorf("invalid slug: %w", err)
	}
	if strings.Contains(slug, "/") || strings.TrimSpace(slug) != slug || strings.HasSuffix(slug, ".") {
		return fmt.Errorf("slug %q must be a single filename component", slug)
	}
	return nil
}
