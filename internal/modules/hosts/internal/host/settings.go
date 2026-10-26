package host

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

// ValidateSetting checks the supported host key and its value before any changes.
// Each key owns its update operation; future keys may also rename site files.
func ValidateSetting(key, value string) error {
	if err := ValidateSettingKey(key); err != nil {
		return err
	}
	if value != "on" && value != "off" {
		return fmt.Errorf("%s: use on or off", key)
	}
	return nil
}

// ValidateSettingKey checks whether a host setting is supported.
func ValidateSettingKey(key string) error {
	if key != "site_offline" {
		return fmt.Errorf("unknown host setting %q, available: site_offline", key)
	}
	return nil
}

// GetSetting reads the current value from DLE, including changes made in its admin UI.
func GetSetting(s *stack.Stack, name, key string) (string, error) {
	if err := ValidateSettingKey(key); err != nil {
		return "", err
	}
	dir, err := Path(s, name, false)
	if err != nil {
		return "", err
	}
	data, match, err := readSiteOffline(dir)
	if err != nil {
		return "", err
	}
	for i := 4; i < len(match); i += 2 {
		if match[i] >= 0 && data[match[i]] == '1' {
			return "on", nil
		}
	}
	return "off", nil
}

// SetSetting updates a supported DLE setting without restarting the Docker stack.
func SetSetting(s *stack.Stack, name, key, value string) error {
	if err := ValidateSetting(key, value); err != nil {
		return err
	}
	dir, err := Path(s, name, false)
	if err != nil {
		return err
	}
	return setSiteOffline(dir, value)
}

var siteOfflineRE = regexp.MustCompile(`(['"]site_offline['"]\s*=>\s*)(?:'([01])'|"([01])"|([01])\b)`)

func readSiteOffline(dir string) ([]byte, []int, error) {
	path := filepath.Join(dir, "engine/data/config.php")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read DLE configuration: %w", err)
	}
	matches := siteOfflineRE.FindAllSubmatchIndex(data, -1)
	if len(matches) != 1 {
		return nil, nil, fmt.Errorf("%s: expected exactly one site_offline setting with value 0 or 1", path)
	}
	return data, matches[0], nil
}

func setSiteOffline(dir, value string) error {
	data, match, err := readSiteOffline(dir)
	if err != nil {
		return err
	}
	digit := byte('0')
	if value == "on" {
		digit = '1'
	}
	for i := 4; i < len(match); i += 2 {
		if match[i] >= 0 {
			data[match[i]] = digit
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "engine/data/config.php"), data, 0o644); err != nil {
		return fmt.Errorf("write DLE configuration: %w", err)
	}
	return nil
}
