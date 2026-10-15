// Package paths holds filesystem location helpers shared by core and modules.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// FindAncestorWith walks from dir upward until dir/marker exists.
// It returns the directory containing marker and true, or "" and false.
func FindAncestorWith(dir, marker string) (string, bool) {
	current := filepath.Clean(dir)
	for {
		if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

// CacheDir returns the dleplugin cache directory name: an absolute
// $XDG_CACHE_HOME, then the platform cache root, plus dleplugin/name.
func CacheDir(name string) (string, error) {
	root, err := cacheRoot(runtime.GOOS)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "dleplugin", name), nil
}

// ComposerCacheDir returns the Composer cache shared by dleplugin tools.
func ComposerCacheDir() (string, error) { return CacheDir("composer") }

func cacheRoot(goos string) (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" && filepath.IsAbs(xdg) {
		return xdg, nil
	}
	if goos == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return local, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Caches"), nil
	case "windows":
		return filepath.Join(home, "AppData", "Local"), nil
	default:
		return filepath.Join(home, ".cache"), nil
	}
}

// Resolve resolves p relative to wd, cleaning absolute paths.
func Resolve(wd, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(wd, p)
}
