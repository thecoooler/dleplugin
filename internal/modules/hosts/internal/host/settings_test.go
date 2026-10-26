package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSiteOfflineSetting(t *testing.T) {
	for _, setting := range []string{`'site_offline' => '0'`, `"site_offline" => "0"`, `'site_offline' => 0`} {
		t.Run(setting, func(t *testing.T) {
			s := isolatedStack(t)
			path := filepath.Join(s.HostDir("shop"), "engine/data/config.php")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			original := "<?php\r\n$config = array(" + setting + ", 'offline_reason' => 'Maintenance');\r\n"
			if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{"off", "on", "on", "off"} {
				if err := SetSetting(s, "shop", "site_offline", value); err != nil {
					t.Fatal(err)
				}
				got, err := GetSetting(s, "shop", "site_offline")
				if err != nil || got != value {
					t.Fatalf("read %s: %q, %v", value, got, err)
				}
				want := original
				if value == "on" {
					want = strings.Replace(original, "0", "1", 1)
				}
				if data := readTestFile(t, path); data != want {
					t.Fatalf("unexpected config change: %q", data)
				}
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o640 {
				t.Fatalf("config permissions changed: %v %v", info, err)
			}
		})
	}
}

func TestSiteOfflineRejectsInvalidConfiguration(t *testing.T) {
	for _, config := range []string{"<?php", `<?php $config = ['site_offline' => 'yes'];`, `<?php $config = ['site_offline' => 10];`, `<?php $config = ['site_offline' => '0', 'site_offline' => '1'];`} {
		t.Run(config, func(t *testing.T) {
			s := isolatedStack(t)
			path := filepath.Join(s.HostDir("shop"), "engine/data/config.php")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := GetSetting(s, "shop", "site_offline"); err == nil {
				t.Fatal("read invalid config")
			}
			if err := SetSetting(s, "shop", "site_offline", "on"); err == nil {
				t.Fatal("updated invalid config")
			}
			if got := readTestFile(t, path); got != config {
				t.Fatal("invalid config modified")
			}
		})
	}
}

func TestHostSettingsRejectInvalidArguments(t *testing.T) {
	s := isolatedStack(t)
	for _, tc := range []struct{ name, key, value string }{
		{"missing", "site_offline", "on"},
		{"../escape", "site_offline", "on"},
		{"shop", "unknown", "on"},
		{"shop", "site_offline", "yes"},
		{"shop", "site_offline", ""},
	} {
		if err := SetSetting(s, tc.name, tc.key, tc.value); err == nil {
			t.Errorf("accepted %+v", tc)
		}
	}
}

func TestAddSiteOffline(t *testing.T) {
	for _, value := range []string{"", "on", "off"} {
		t.Run("mode="+value, func(t *testing.T) {
			s := isolatedStack(t)
			dists := t.TempDir()
			writeTestDist(t, dists, "21.0")
			_, err := Add(s, AddOptions{Name: "shop", DLE: "21.0", PHPVersions: []string{"8.5"},
				DistsDir: dists, Admin: Credentials{"admin", "test-password"}, SiteOffline: value}, func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			want := value
			if want == "" {
				want = "on"
			}
			got, err := GetSetting(s, "shop", "site_offline")
			if err != nil || got != want {
				t.Fatalf("created site: got %q, want %q, error %v", got, want, err)
			}
		})
	}
}
