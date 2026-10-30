package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindAncestorWith(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "src", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dleplugin.yml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := FindAncestorWith(nested, "dleplugin.yml")
	if !ok || got != root {
		t.Fatalf("FindAncestorWith = %q, %v; want %q", got, ok, root)
	}
	if _, ok := FindAncestorWith(t.TempDir(), "missing.marker"); ok {
		t.Fatal("expected marker to be missing")
	}
}

func TestComposerCacheDirUsesXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg")
	got, err := ComposerCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/xdg", "dleplugin", "composer"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCacheRootUsesXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg")
	got, err := CacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/xdg", "dleplugin"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCacheRootDarwin(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/home/test")
	got, err := cacheRoot("darwin")
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/test/Library/Caches"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
