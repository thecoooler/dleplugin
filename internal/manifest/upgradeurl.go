package manifest

import (
	"fmt"
	"net/url"
)

// ValidateUpgradeURL accepts the empty string (no URL) or an absolute https
// URL with a host and no user info that DLE requests to upgrade the plugin.
func ValidateUpgradeURL(s string) error {
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	switch {
	case err != nil:
		return fmt.Errorf("invalid upgrade URL %q: %w", s, err)
	case u.Scheme != "https":
		return fmt.Errorf("invalid upgrade URL %q: scheme must be https", s)
	case u.Hostname() == "":
		return fmt.Errorf("invalid upgrade URL %q: host is empty", s)
	case u.User != nil:
		return fmt.Errorf("invalid upgrade URL %q: user info is not allowed", s)
	}
	return nil
}
