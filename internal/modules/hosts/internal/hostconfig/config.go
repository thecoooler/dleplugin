// Package hostconfig holds the hosts settings in ~/.config/dlehost/config.yml.
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
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the hosts settings, stored in ~/.config/dlehost/config.yml
// (the path can be overridden with DLEHOST_CONFIG).
type Config struct {
	BaseDomain     string   `yaml:"base_domain"`
	ExternalOrigin string   `yaml:"external_origin"`
	ProjectDir     string   `yaml:"project_dir"`
	DistsDir       string   `yaml:"dists_dir"`
	DefaultDLE     string   `yaml:"default_dle"`
	DefaultPHP     string   `yaml:"default_php"`
	PHPVersions    []string `yaml:"php_versions"`
	// Protect enables the password login form for hosts in the default mode.
	Protect bool `yaml:"protect,omitempty"`
	// ProtectSessionTTL is the login form session lifetime; empty or never means no expiry.
	ProtectSessionTTL string `yaml:"protect_session_ttl,omitempty"`
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
		Help: "directory of the Docker stack, sites and databases (empty: ~/.local/share/dlehost; DLEHOST_DIR takes precedence)",
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
		Help: "PHP for add: auto (by DLE version) or a preference used when compatible with DLE",
		Get:  func(c *Config) string { return c.DefaultPHP },
		Set: func(c *Config, v string) error {
			if v != "" && v != "auto" && !slices.Contains(c.PHPVersions, v) {
				return fmt.Errorf("PHP version %s is not in php_versions (%s)", v, strings.Join(c.PHPVersions, ", "))
			}
			c.DefaultPHP = v
			return nil
		},
	},
	"protect": {
		Help: "password login form for hosts in the default mode: on or off (default off)",
		Get: func(c *Config) string {
			if c.Protect {
				return "on"
			}
			return "off"
		},
		Set: func(c *Config, v string) error {
			switch v {
			case "on":
				c.Protect = true
			case "off", "":
				c.Protect = false
			default:
				return fmt.Errorf("protect: use on or off")
			}
			return nil
		},
	},
	"protect_session_ttl": {
		Help: "login form session lifetime: never (default, no server-side expiry) or 30d, 12h, 90m",
		Get: func(c *Config) string {
			if c.SessionTTL() == 0 {
				return "never"
			}
			return c.ProtectSessionTTL
		},
		Set: func(c *Config, v string) error {
			ttl, err := ParseSessionTTL(v)
			if err != nil {
				return err
			}
			c.ProtectSessionTTL = v
			if ttl == 0 {
				c.ProtectSessionTTL = ""
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
		DefaultPHP:  "auto",
		PHPVersions: []string{"8.5", "7.4"},
	}
}

// Path returns the config file path.
func Path() (string, error) {
	if p := os.Getenv("DLEHOST_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "dlehost", "config.yml"), nil
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

// ParseSessionTTL parses the login form session lifetime: "", 0, never or
// unlimited mean no server-side expiry; otherwise Nd (days) or a Go duration
// (12h, 90m) from one minute to 100 years.
func ParseSessionTTL(v string) (time.Duration, error) {
	switch v {
	case "", "0", "never", "unlimited":
		return 0, nil
	}
	var ttl time.Duration
	var err error
	if days, ok := strings.CutSuffix(v, "d"); ok {
		var n int
		if n, err = strconv.Atoi(days); err == nil {
			ttl = time.Duration(n) * 24 * time.Hour
			if n > 36500 {
				ttl = -1
			}
		}
	} else {
		ttl, err = time.ParseDuration(v)
	}
	if err != nil || ttl < time.Minute || ttl > 36500*24*time.Hour {
		return 0, fmt.Errorf("protect_session_ttl %q: expected never or a lifetime from 1m to 36500d, e.g. 30d, 12h, 90m", v)
	}
	return ttl, nil
}

// SessionTTL is the login form session lifetime; 0 means no server-side
// expiry. The value is checked by Validate.
func (c *Config) SessionTTL() time.Duration {
	ttl, _ := ParseSessionTTL(c.ProtectSessionTTL)
	return ttl
}

// Validate checks the values, including after manual YAML edits.
func (c *Config) Validate() error {
	if err := validateDomain(c.BaseDomain); err != nil {
		return err
	}
	if _, err := ParseSessionTTL(c.ProtectSessionTTL); err != nil {
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
