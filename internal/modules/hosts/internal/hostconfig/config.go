// Package hostconfig holds the hosts settings in ~/.config/dleplugin/hosts.yml.
package hostconfig

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the hosts settings, stored in ~/.config/dleplugin/hosts.yml
// (the path can be overridden with DLEPLUGIN_HOSTS_CONFIG).
type Config struct {
	BaseDomain         string   `yaml:"base_domain"`
	ExternalOrigin     string   `yaml:"external_origin"`
	ProjectDir         string   `yaml:"project_dir"`
	DistsDir           string   `yaml:"dists_dir"`
	DefaultDLE         string   `yaml:"default_dle"`
	DefaultAdminPath   string   `yaml:"default_admin_path"`
	DefaultSiteOffline bool     `yaml:"default_site_offline"`
	DefaultPHP         string   `yaml:"default_php"`
	PHPVersions        []string `yaml:"php_versions"`
}

// Key is a config key available through dleplugin hosts config.
type Key struct {
	Help string
	Get  func(*Config) string
	Set  func(*Config, string) error
}

// Keys holds all config keys by name.
var Keys = map[string]Key{
	"base_domain": {
		Help: "base DNS domain of new hosts (empty: localhost)",
		Get:  func(c *Config) string { return c.BaseDomain },
		Set: func(c *Config, v string) error {
			if err := validateDomain(v); err != nil {
				return err
			}
			c.BaseDomain = v
			return nil
		},
	},
	"external_origin": {
		Help: "external origin of the base domain, e.g. https://dle.example.com (without the internal HTTP_PORT)",
		Get:  func(c *Config) string { return c.ExternalOrigin },
		Set: func(c *Config, v string) error {
			if err := validateOrigin(v); err != nil {
				return err
			}
			c.ExternalOrigin = v
			return nil
		},
	},
	"project_dir": {
		Help: "directory of the Docker stack, sites and databases (empty: ~/.local/share/dleplugin/hosts; DLEPLUGIN_HOSTS_DIR takes precedence)",
		Get:  func(c *Config) string { return c.ProjectDir },
		Set: func(c *Config, v string) error {
			p, err := absPath(v)
			c.ProjectDir = p
			return err
		},
	},
	"dists_dir": {
		Help: "directory with clean DLE distributions (ZIP archives with install.php)",
		Get:  func(c *Config) string { return c.DistsDir },
		Set: func(c *Config, v string) error {
			p, err := absPath(v)
			c.DistsDir = p
			return err
		},
	},
	"default_dle": {
		Help: "default DLE version for add",
		Get:  func(c *Config) string { return c.DefaultDLE },
		Set:  func(c *Config, v string) error { c.DefaultDLE = v; return nil },
	},
	"default_php": {
		Help: "PHP for add: auto (by DLE version) or an explicit default version",
		Get:  func(c *Config) string { return c.DefaultPHP },
		Set: func(c *Config, v string) error {
			if v != "" && v != "auto" && !slices.Contains(c.PHPVersions, v) {
				return fmt.Errorf("PHP version %s is not in php_versions (%s)", v, strings.Join(c.PHPVersions, ", "))
			}
			c.DefaultPHP = v
			return nil
		},
	},
	"default_admin_path": {
		Help: "administrator PHP filename for new hosts (default: admin.php)",
		Get:  func(c *Config) string { return c.AdminPath() },
		Set: func(c *Config, v string) error {
			if v == "" {
				v = "admin.php"
			}
			if err := ValidateAdminPath(v); err != nil {
				return fmt.Errorf("default_admin_path: %w", err)
			}
			c.DefaultAdminPath = v
			return nil
		},
	},
	"default_site_offline": {
		Help: "DLE maintenance mode for new hosts: on (default, site closed) or off",
		Get: func(c *Config) string {
			if c.DefaultSiteOffline {
				return "on"
			}
			return "off"
		},
		Set: func(c *Config, v string) error {
			switch v {
			case "on", "":
				c.DefaultSiteOffline = true
			case "off":
				c.DefaultSiteOffline = false
			default:
				return fmt.Errorf("default_site_offline: use on or off")
			}
			return nil
		},
	},
	"php_versions": {
		Help: "available PHP versions, comma-separated (phpXY services must exist in docker-compose)",
		Get:  func(c *Config) string { return strings.Join(c.PHPVersions, ",") },
		Set: func(c *Config, v string) error {
			var list []string
			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					list = append(list, s)
				}
			}
			c.PHPVersions = list
			return nil
		},
	},
}

// KeyNames returns the key names in alphabetical order.
func KeyNames() []string {
	names := make([]string, 0, len(Keys))
	for k := range Keys {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Lookup finds a key by name.
func Lookup(name string) (Key, error) {
	key, ok := Keys[name]
	if !ok {
		return key, fmt.Errorf("unknown key %q, available: %s", name, strings.Join(KeyNames(), ", "))
	}
	return key, nil
}

func defaultConfig() *Config {
	return &Config{
		DefaultSiteOffline: true,
		DefaultAdminPath:   "admin.php",
		DefaultPHP:         "auto",
		PHPVersions:        []string{"8.5", "7.4"},
	}
}

// Path returns the config file path.
func Path() (string, error) {
	if p := os.Getenv("DLEPLUGIN_HOSTS_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "dleplugin", "hosts.yml"), nil
}

// Load reads the config; a missing file yields the defaults.
func Load() (*Config, error) {
	cfg := defaultConfig()
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config.
func Save(cfg *Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func absPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[2:])
	}
	return filepath.Abs(p)
}

// AdminPath returns the administrator filename for new hosts, including the
// default for configs written before default_admin_path was introduced.
func (c *Config) AdminPath() string {
	if c.DefaultAdminPath == "" {
		return "admin.php"
	}
	return c.DefaultAdminPath
}

var adminPathRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*\.php$`)

// ValidateAdminPath checks the filename shared by host settings and defaults.
func ValidateAdminPath(value string) error {
	if len(value) > 255 || !adminPathRE.MatchString(value) {
		return fmt.Errorf("admin_path: expected a PHP filename, e.g. panel.php (ASCII letters, digits, _ and -; no directories; at most 255 bytes)")
	}
	return nil
}

var domainLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func validateDomain(domain string) error {
	if domain == "" {
		return nil
	}
	if len(domain) > 210 {
		return fmt.Errorf("base_domain is too long (210 characters at most)")
	}
	for _, label := range strings.Split(domain, ".") {
		if !domainLabel.MatchString(label) {
			return fmt.Errorf("base_domain: expected a lower-case DNS domain without scheme, port or wildcard")
		}
	}
	return nil
}

func validateOrigin(origin string) error {
	if origin == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("external_origin: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(origin, "?#") {
		return fmt.Errorf("external_origin: expected http(s)://base-domain[:port] without path, credentials, query or fragment")
	}
	if err := validateDomain(u.Hostname()); err != nil {
		return fmt.Errorf("external_origin: %w", err)
	}
	if strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("external_origin: empty port")
	}
	if u.Port() != "" {
		p, err := strconv.Atoi(u.Port())
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("external_origin: invalid port")
		}
	}
	return nil
}

// Validate checks the values, including after manual YAML edits.
func (c *Config) Validate() error {
	if err := ValidateAdminPath(c.AdminPath()); err != nil {
		return fmt.Errorf("default_admin_path: %w", err)
	}
	if err := validateDomain(c.BaseDomain); err != nil {
		return err
	}
	return validateOrigin(c.ExternalOrigin)
}

// ValidateRouting is called before any stack operation. Checking the keys
// separately lets config set change the pair one key at a time.
func (c *Config) ValidateRouting() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.ExternalOrigin != "" {
		u, _ := url.Parse(c.ExternalOrigin)
		domain := c.BaseDomain
		if domain == "" {
			domain = "localhost"
		}
		if u.Hostname() != domain {
			return fmt.Errorf("external_origin must use base_domain (%s)", domain)
		}
	}
	return nil
}
