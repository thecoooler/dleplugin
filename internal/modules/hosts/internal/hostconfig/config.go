// Package config — настройки CLI в ~/.config/dlehost/config.yml.
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

// Config — настройки CLI, хранятся в ~/.config/dlehost/config.yml
// (путь можно переопределить переменной DLEHOST_CONFIG).
type Config struct {
	BaseDomain     string   `yaml:"base_domain"`
	ExternalOrigin string   `yaml:"external_origin"`
	ProjectDir     string   `yaml:"project_dir"`
	DistsDir       string   `yaml:"dists_dir"`
	DefaultDLE     string   `yaml:"default_dle"`
	DefaultPHP     string   `yaml:"default_php"`
	PHPVersions    []string `yaml:"php_versions"`
	// Protect — форма входа по отдельному паролю для хостов в режиме default.
	Protect bool `yaml:"protect,omitempty"`
	// ProtectSessionTTL — срок сессии формы входа; пусто или never — без истечения.
	ProtectSessionTTL string `yaml:"protect_session_ttl,omitempty"`
}

// Key — ключ конфига, доступный через dlehost config.
type Key struct {
	Help string
	Get  func(*Config) string
	Set  func(*Config, string) error
}

// Keys — все ключи конфига по имени.
var Keys = map[string]Key{
	"base_domain": {
		Help: "базовый DNS-домен новых хостов (пусто — localhost)",
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
		Help: "внешний origin базового домена, например https://dle.example.com (без внутреннего HTTP_PORT)",
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
		Help: "каталог docker-стека, сайтов и баз (пусто — ~/.local/share/dlehost; DLEHOST_DIR приоритетнее)",
		Get:  func(c *Config) string { return c.ProjectDir },
		Set: func(c *Config, v string) error {
			p, err := absPath(v)
			c.ProjectDir = p
			return err
		},
	},
	"dists_dir": {
		Help: "каталог с чистыми дистрибутивами DLE (ZIP-архивы с install.php)",
		Get:  func(c *Config) string { return c.DistsDir },
		Set: func(c *Config, v string) error {
			p, err := absPath(v)
			c.DistsDir = p
			return err
		},
	},
	"default_dle": {
		Help: "версия DLE по умолчанию для add",
		Get:  func(c *Config) string { return c.DefaultDLE },
		Set:  func(c *Config, v string) error { c.DefaultDLE = v; return nil },
	},
	"default_php": {
		Help: "PHP для add: auto (по версии DLE) или предпочтение, если оно совместимо с DLE",
		Get:  func(c *Config) string { return c.DefaultPHP },
		Set: func(c *Config, v string) error {
			if v != "" && v != "auto" && !slices.Contains(c.PHPVersions, v) {
				return fmt.Errorf("версия PHP %s не входит в php_versions (%s)", v, strings.Join(c.PHPVersions, ", "))
			}
			c.DefaultPHP = v
			return nil
		},
	},
	"protect": {
		Help: "форма входа по отдельному паролю для хостов в режиме default: on или off (по умолчанию off)",
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
				return fmt.Errorf("protect: допустимы on или off")
			}
			return nil
		},
	},
	"protect_session_ttl": {
		Help: "срок сессии формы входа: never (по умолчанию, без серверного истечения) или 30d, 12h, 90m",
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
		Help: "доступные версии PHP через запятую (должны быть сервисы phpXY в docker-compose)",
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

// KeyNames — имена ключей по алфавиту.
func KeyNames() []string {
	names := make([]string, 0, len(Keys))
	for k := range Keys {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Lookup находит ключ по имени.
func Lookup(name string) (Key, error) {
	key, ok := Keys[name]
	if !ok {
		return key, fmt.Errorf("неизвестный ключ %q, доступны: %s", name, strings.Join(KeyNames(), ", "))
	}
	return key, nil
}

func defaultConfig() *Config {
	return &Config{
		DefaultPHP:  "auto",
		PHPVersions: []string{"8.5", "7.4"},
	}
}

// Path — путь к файлу конфига.
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

// Load читает конфиг; если файла нет — значения по умолчанию.
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

// Save записывает конфиг.
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
		return fmt.Errorf("base_domain слишком длинный (максимум 210 символов)")
	}
	for _, label := range strings.Split(domain, ".") {
		if !domainLabel.MatchString(label) {
			return fmt.Errorf("base_domain: нужен DNS-домен в нижнем регистре без схемы, порта и wildcard")
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
		return fmt.Errorf("external_origin: нужен http(s)://базовый-домен[:порт] без пути, авторизации, query и fragment")
	}
	if err := validateDomain(u.Hostname()); err != nil {
		return fmt.Errorf("external_origin: %w", err)
	}
	if strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("external_origin: пустой порт")
	}
	if u.Port() != "" {
		p, err := strconv.Atoi(u.Port())
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("external_origin: некорректный порт")
		}
	}
	return nil
}

// ParseSessionTTL разбирает срок сессии формы входа: "", 0, never или
// unlimited — без серверного истечения; иначе Nd (дни) или длительность Go
// (12h, 90m) не меньше минуты и не больше 100 лет.
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
		return 0, fmt.Errorf("protect_session_ttl %q: нужно never или срок от 1m до 36500d, например 30d, 12h, 90m", v)
	}
	return ttl, nil
}

// SessionTTL — срок сессии формы входа; 0 — без серверного истечения.
// Значение проверено в Validate.
func (c *Config) SessionTTL() time.Duration {
	ttl, _ := ParseSessionTTL(c.ProtectSessionTTL)
	return ttl
}

// Validate проверяет значения также при ручном редактировании YAML.
func (c *Config) Validate() error {
	if err := validateDomain(c.BaseDomain); err != nil {
		return err
	}
	if _, err := ParseSessionTTL(c.ProtectSessionTTL); err != nil {
		return err
	}
	return validateOrigin(c.ExternalOrigin)
}

// ValidateRouting вызывается до любых операций со стеком. Раздельная проверка
// ключей позволяет последовательно менять пару через config set.
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
			return fmt.Errorf("external_origin должен использовать base_domain (%s)", domain)
		}
	}
	return nil
}
