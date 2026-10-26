package host

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func adminFixture(t *testing.T, dir, config string) string {
	t.Helper()
	path := filepath.Join(dir, "engine/data/config.php")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(config), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "admin.php"), []byte("<?php // original admin\n"), 0750); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAdminPathRoundTrip(t *testing.T) {
	for _, quote := range []string{"'", `"`} {
		t.Run(quote, func(t *testing.T) {
			s := isolatedStack(t)
			dir := s.HostDir("shop")
			original := "<?php\r\n// 'admin_path' => 'ignored.php',\r\n$config = array('site_offline' => '1', " + quote + "admin_path" + quote + " => " + quote + "admin.php" + quote + ", 'offline_reason' => \"text 'admin_path' => 'ignored.php'\");\r\n"
			path := adminFixture(t, dir, original)
			adminInfo, _ := os.Stat(filepath.Join(dir, "admin.php"))
			configInfo, _ := os.Stat(path)
			old := "admin.php"
			for _, value := range []string{"panel.php", "panel.php", "Control-2_test.php", "admin.php"} {
				if err := SetSetting(s, "shop", "admin_path", value); err != nil {
					t.Fatal(err)
				}
				got, err := GetSetting(s, "shop", "admin_path")
				if err != nil || got != value {
					t.Fatalf("get: %q %v", got, err)
				}
				if got := readTestFile(t, path); got != strings.Replace(original, quote+"admin.php"+quote, quote+value+quote, 1) {
					t.Fatalf("unexpected config: %s", got)
				}
				info, err := os.Stat(filepath.Join(dir, value))
				if err != nil || !os.SameFile(adminInfo, info) || info.Mode().Perm() != 0750 {
					t.Fatalf("admin metadata: %v %v", info, err)
				}
				current, _ := os.Stat(path)
				if current.Mode().Perm() != configInfo.Mode().Perm() {
					t.Fatal("config permissions changed")
				}
				if old != value {
					if _, err := os.Lstat(filepath.Join(dir, old)); !os.IsNotExist(err) {
						t.Fatal("old entry point remains")
					}
				}
				old = value
			}
			files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".admin-path-*"))
			if len(files) != 0 {
				t.Fatal("staging files remain")
			}
		})
	}
}

func TestAdminPathRejectsNames(t *testing.T) {
	for _, value := range []string{"", "panel", "panel.PHP", "../admin.php", "/admin.php", "dir/admin.php", `dir\admin.php`, "index.php/x", ".panel.php", "panel.test.php", "panel.php?x=1", "a'b.php", "панель.php", "a\x00.php", strings.Repeat("x", 252) + ".php"} {
		if ValidateSetting("admin_path", value) == nil {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestAdminPathRejectsConfiguration(t *testing.T) {
	for _, value := range []string{"", "'other' => 'admin.php'", "'admin_path' => '../escape.php'", "'admin_path' => $path", "'admin_path' => 'admin.php'.'bad'", "'admin_path' => 'admin.php', 'admin_path' => 1", "'admin_path' => 'admin.php', 'admin_path' => 'admin.php'"} {
		t.Run(value, func(t *testing.T) {
			s := isolatedStack(t)
			dir := s.HostDir("shop")
			config := "<?php $config = array(" + value + ");"
			path := adminFixture(t, dir, config)
			if _, err := GetSetting(s, "shop", "admin_path"); err == nil {
				t.Fatal("read invalid setting")
			}
			if err := SetSetting(s, "shop", "admin_path", "panel.php"); err == nil {
				t.Fatal("changed invalid configuration")
			}
			if readTestFile(t, path) != config {
				t.Fatal("config changed")
			}
			if _, err := os.Stat(filepath.Join(dir, "admin.php")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAdminPathFilesystemFailures(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink", "dangling symlink", "missing source", "symlink source", "directory source", "symlink config", "unwritable config directory", "unwritable site directory", "commit"} {
		t.Run(kind, func(t *testing.T) {
			s := isolatedStack(t)
			dir := s.HostDir("shop")
			original := "<?php $config = array('admin_path' => 'admin.php', 'site_offline' => '1');"
			path := adminFixture(t, dir, original)
			source, target := filepath.Join(dir, "admin.php"), filepath.Join(dir, "panel.php")
			var err error
			switch kind {
			case "file":
				err = os.WriteFile(target, []byte("collision"), 0644)
			case "directory":
				err = os.Mkdir(target, 0755)
			case "symlink":
				err = os.Symlink(source, target)
			case "dangling symlink":
				err = os.Symlink(filepath.Join(dir, "missing.php"), target)
			case "missing source", "symlink source", "directory source":
				if err = os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink source" {
					err = os.Symlink(path, source)
				}
				if kind == "directory source" {
					err = os.Mkdir(source, 0755)
				}
			case "unwritable config directory", "unwritable site directory":
				if os.Geteuid() == 0 {
					t.Skip("requires an unprivileged user")
				}
				protected := dir
				if kind == "unwritable config directory" {
					protected = filepath.Dir(path)
				}
				err = os.Chmod(protected, 0555)
				t.Cleanup(func() { _ = os.Chmod(protected, 0755) })
			case "symlink config":
				if err = os.Rename(path, path+".real"); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(path+".real", path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "commit" {
				failure := errors.New("injected config rename failure")
				err = setAdminPath(dir, "panel.php", func(string, string) error { return failure })
				if !errors.Is(err, failure) {
					t.Fatalf("lost error: %v", err)
				}
			} else {
				err = SetSetting(s, "shop", "admin_path", "panel.php")
			}
			if err == nil {
				t.Fatal("accepted failure")
			}
			if readTestFile(t, path) != original {
				t.Fatal("config changed")
			}
			if kind == "file" && readTestFile(t, target) != "collision" {
				t.Fatal("collision overwritten")
			}
			if kind == "commit" {
				if readTestFile(t, source) != "<?php // original admin\n" {
					t.Fatal("admin not restored")
				}
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Fatal("new entry point left after rollback")
				}
			}
			files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".admin-path-*"))
			if len(files) != 0 {
				t.Fatal("staging files remain")
			}
		})
	}
}
