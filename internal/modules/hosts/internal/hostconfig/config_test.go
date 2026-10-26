package hostconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultSiteOffline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	t.Setenv("DLEHOST_CONFIG", path)
	for _, existing := range []bool{false, true} {
		if existing {
			if err := os.WriteFile(path, []byte("default_dle: '21.0'\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cfg, err := Load()
		if err != nil || !cfg.DefaultSiteOffline {
			t.Fatalf("default: %+v %v", cfg, err)
		}
	}
	key := Keys["default_site_offline"]
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"off", "on", ""} {
		if err := key.Set(cfg, value); err != nil {
			t.Fatal(err)
		}
		if err := Save(cfg); err != nil {
			t.Fatal(err)
		}
		cfg, err = Load()
		want := value
		if want == "" {
			want = "on"
		}
		if err != nil || key.Get(cfg) != want {
			t.Fatalf("round trip %q: %+v %v", value, cfg, err)
		}
	}
	if err := key.Set(cfg, "yes"); err == nil {
		t.Fatal("accepted invalid default")
	}
}

func TestDefaultPHPAuto(t *testing.T) {
	t.Setenv("DLEHOST_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	cfg, err := Load()
	if err != nil || cfg.DefaultPHP != "auto" {
		t.Fatalf("default PHP: %v %v", cfg, err)
	}
	key := Keys["default_php"]
	for _, value := range []string{"7.4", "auto"} {
		if err := key.Set(cfg, value); err != nil {
			t.Fatal(err)
		}
		if err := Save(cfg); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load()
		if err != nil || loaded.DefaultPHP != value {
			t.Fatalf("load %s: %v %v", value, loaded, err)
		}
	}
	if err := key.Set(cfg, "9.9"); err == nil {
		t.Fatal("accepted unavailable configured PHP")
	}
}

func TestRoutingValidation(t *testing.T) {
	for _, tc := range []struct {
		domain, origin string
		valid          bool
	}{
		{"", "", true}, {"localhost", "", true},
		{"dle.icooler.opik.net", "https://dle.icooler.opik.net", true},
		{"example.com", "http://example.com:8088", true},
		{"*.example.com", "", false}, {"example.com:8090", "", false},
		{"Example.com", "", false}, {"example.com.", "", false},
		{"bad_name.test", "", false}, {"-bad.test", "", false},
		{strings.Repeat("a", 64) + ".test", "", false},
		{"example.com", "https://other.test", false},
		{"example.com", "https://example.com/", false},
		{"example.com", "https://example.com/path", false},
		{"example.com", "https://user:pass@example.com", false},
		{"example.com", "https://example.com?", false},
		{"example.com", "https://example.com#", false},
		{"example.com", "ftp://example.com", false},
		{"example.com", "https://example.com:0", false},
		{"example.com", "https://example.com:65536", false},
		{"example.com", "https://example.com:", false},
		{"example.com", "https://example.com:abc", false},
		{"example.com\nUse PHP 74", "", false},
	} {
		t.Run(tc.domain+tc.origin, func(t *testing.T) {
			cfg := &Config{BaseDomain: tc.domain, ExternalOrigin: tc.origin}
			if err := cfg.ValidateRouting(); (err == nil) != tc.valid {
				t.Fatalf("ValidateRouting() = %v, valid=%v", err, tc.valid)
			}
		})
	}
}

func TestRoutingConfigRoundTrip(t *testing.T) {
	t.Setenv("DLEHOST_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseDomain != "" || cfg.ExternalOrigin != "" {
		t.Fatal("changed local defaults")
	}
	for key, value := range map[string]string{"base_domain": "dle.icooler.opik.net", "external_origin": "https://dle.icooler.opik.net"} {
		if err := Keys[key].Set(cfg, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateRouting(); err != nil {
		t.Fatal(err)
	}
	if cfg.BaseDomain != "dle.icooler.opik.net" || cfg.ExternalOrigin != "https://dle.icooler.opik.net" {
		t.Fatalf("round trip: %+v", cfg)
	}
	path, _ := Path()
	if err := os.WriteFile(path, []byte("base_domain: 'bad domain'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("accepted invalid YAML domain")
	}
}

func TestProtectKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	t.Setenv("DLEHOST_CONFIG", path)
	// A config without the protect key keeps the form off.
	if err := os.WriteFile(path, []byte("base_domain: dle.icooler.opik.net\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	key, err := Lookup("protect")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Protect || key.Get(cfg) != "off" {
		t.Fatal("default must be off")
	}
	for _, bad := range []string{"yes", "true", "ON", "default"} {
		if key.Set(cfg, bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if err := key.Set(cfg, "on"); err != nil || key.Get(cfg) != "on" {
		t.Fatal("set on")
	}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg, err = Load(); err != nil || !cfg.Protect || cfg.BaseDomain != "dle.icooler.opik.net" {
		t.Fatalf("reload: %+v %v", cfg, err)
	}
	if err := key.Set(cfg, ""); err != nil || cfg.Protect {
		t.Fatal("unset must turn off")
	}
}

func TestProtectSessionTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	t.Setenv("DLEHOST_CONFIG", path)
	// An old config without the key has no server-side expiry.
	if err := os.WriteFile(path, []byte("protect: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	key, _ := Lookup("protect_session_ttl")
	if cfg.SessionTTL() != 0 || key.Get(cfg) != "never" {
		t.Fatal("default must be never")
	}
	for v, want := range map[string]time.Duration{
		"": 0, "0": 0, "never": 0, "unlimited": 0,
		"30d": 30 * 24 * time.Hour, "12h": 12 * time.Hour, "90m": 90 * time.Minute, "1m": time.Minute, "36500d": 36500 * 24 * time.Hour,
	} {
		got, err := ParseSessionTTL(v)
		if err != nil || got != want {
			t.Errorf("%q: %v %v", v, got, err)
		}
	}
	for _, bad := range []string{"30s", "-1h", "0d", "-5d", "36501d", "1y", "forever", "7 d", "1.5d"} {
		if _, err := ParseSessionTTL(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if err := key.Set(cfg, "30d"); err != nil || cfg.SessionTTL() != 30*24*time.Hour || key.Get(cfg) != "30d" {
		t.Fatal("set 30d")
	}
	if err := key.Set(cfg, "never"); err != nil || cfg.ProtectSessionTTL != "" || key.Get(cfg) != "never" {
		t.Fatal("set never")
	}
	if err := os.WriteFile(path, []byte("protect_session_ttl: 5x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("invalid TTL in YAML accepted")
	}
}

func TestDefaultAdminPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	t.Setenv("DLEHOST_CONFIG", path)
	key := Keys["default_admin_path"]
	for _, content := range []string{"", "default_dle: '21.0'\n", "default_admin_path: ''\n"} {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if err != nil || key.Get(cfg) != "admin.php" {
			t.Fatalf("old config: %+v %v", cfg, err)
		}
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"control.php", "admin.php", ""} {
		if err := key.Set(cfg, value); err != nil {
			t.Fatal(err)
		}
		if err := Save(cfg); err != nil {
			t.Fatal(err)
		}
		cfg, err = Load()
		want := value
		if want == "" {
			want = "admin.php"
		}
		if err != nil || key.Get(cfg) != want {
			t.Fatalf("round trip %q: %+v %v", value, cfg, err)
		}
	}
	for _, bad := range []string{"../admin.php", "control", "/panel.php", "panel.PHP"} {
		if key.Set(cfg, bad) == nil {
			t.Fatalf("accepted %q", bad)
		}
		if key.Get(cfg) != "admin.php" {
			t.Fatal("invalid setting changed config")
		}
		if err := os.WriteFile(path, []byte("default_admin_path: '"+bad+"'\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(); err == nil {
			t.Fatalf("accepted invalid YAML default %q", bad)
		}
	}
}
