package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCacheRootPlatforms(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, tc := range []struct{ os, xdg, local, want string }{
		{"darwin", "", "", filepath.Join(home, "Library", "Caches")},
		{"linux", "", "", filepath.Join(home, ".cache")},
		{"windows", "", "", filepath.Join(home, "AppData", "Local")},
		{"windows", "", home, home},
		{"linux", home, "", home},
		{"darwin", home, "", home},
		{"windows", home, filepath.Join(home, "local"), home},
		{"linux", "relative", "", filepath.Join(home, ".cache")},
	} {
		t.Run(tc.os+tc.xdg+tc.local, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", tc.xdg)
			t.Setenv("LOCALAPPDATA", tc.local)
			got, err := cacheRoot(tc.os)
			if err != nil || got != tc.want {
				t.Fatalf("%q %v; want %q", got, err, tc.want)
			}
		})
	}
	t.Setenv("XDG_CACHE_HOME", "")
	got, err := ComposerCacheDir()
	root, rootErr := cacheRoot(runtime.GOOS)
	if err != nil || rootErr != nil || got != filepath.Join(root, "dleplugin", "composer") {
		t.Fatalf("%q %v", got, err)
	}
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", "")
		if _, err := cacheRoot("linux"); err == nil {
			t.Fatal("missing HOME accepted")
		}
		t.Setenv("LOCALAPPDATA", home)
		if got, err := cacheRoot("windows"); err != nil || got != home {
			t.Fatalf("LOCALAPPDATA without HOME: %q %v", got, err)
		}
	}
}

func TestCacheDirNamesAndErrors(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	for _, name := range []string{"composer", "test", "static"} {
		got, err := CacheDir(name)
		if want := filepath.Join(root, "dleplugin", name); err != nil || got != want {
			t.Errorf("CacheDir(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	t.Setenv("XDG_CACHE_HOME", "relative")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("LOCALAPPDATA", "")
	if got, err := CacheDir("test"); err == nil || got != "" {
		t.Fatalf("missing cache root: %q, %v", got, err)
	}
}

func TestAncestorRootAndSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "marker"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if got, ok := FindAncestorWith(link, "marker"); !ok || got != link {
		t.Fatalf("%q %v", got, ok)
	}
	if got, ok := FindAncestorWith(string(filepath.Separator), "dleplugin-nonexistent-marker"); ok || got != "" {
		t.Fatalf("%q %v", got, ok)
	}
	if got, ok := FindAncestorWith(filepath.Join(link, "..", "link"), "marker"); !ok || got != link {
		t.Fatalf("%q %v", got, ok)
	}
}
