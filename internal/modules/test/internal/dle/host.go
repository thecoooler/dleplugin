package dle

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/thecoooler/dleplugin/internal/manifest"
)

// Project is a plugin project that can be connected to a DLE installation.
type Project interface {
	// Root is the absolute plugin directory.
	Root() string
	// Info is the expanded manifest.
	Info() *manifest.Info
	// Files lists the source files and their DLE mount paths.
	Files() ([]manifest.SourceFile, error)
}

// Host is a local DLE installation.
type Host struct {
	// Path is the DLE root directory.
	Path string
	// Registry records the plugin in the DLE database. A nil Registry means
	// only the filesystem is touched.
	Registry Registry
}

// Connect links the plugin files, templates, .env and conftest.php into the
// DLE installation, registers the plugin and clears the DLE cache.
func (h *Host) Connect(ctx context.Context, p Project) error {
	files, err := p.Files()
	if err != nil {
		return fmt.Errorf("list plugin files: %w", err)
	}
	for _, f := range files {
		if err := manifest.ValidateFilePath(f.Mount); err != nil {
			return err
		}
	}
	root, err := os.OpenRoot(h.Path)
	if err != nil {
		return fmt.Errorf("open DLE root: %w", err)
	}
	defer root.Close()
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := symlinkIfMissing(root, f.Src, filepath.FromSlash(f.Mount)); err != nil {
			return err
		}
	}

	if err := h.linkTemplates(ctx, root, p.Root()); err != nil {
		return err
	}

	for _, name := range []string{".env", "conftest.php"} {
		src := filepath.Join(p.Root(), name)
		if exists(src) {
			if err := symlinkIfMissing(root, src, name); err != nil {
				return err
			}
		}
	}

	if h.Registry != nil {
		if err := h.Registry.Add(ctx, p.Info()); err != nil {
			return fmt.Errorf("register plugin: %w", err)
		}
	}
	return h.ClearCache()
}

// ClearCache removes the DLE plugin cache (plugins.php and plugins/).
func (h *Host) ClearCache() error {
	root, err := os.OpenRoot(h.Path)
	if err != nil {
		return err
	}
	defer root.Close()
	base := filepath.Join("engine", "cache", "system")
	if err := root.Remove(filepath.Join(base, "plugins.php")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clear cache: %w", err)
	}
	if err := root.RemoveAll(filepath.Join(base, "plugins")); err != nil {
		return fmt.Errorf("clear cache: %w", err)
	}
	return nil
}

func (h *Host) linkTemplates(ctx context.Context, destination *os.Root, root string) error {
	tpl := filepath.Join(root, "templates")
	if fi, err := os.Stat(tpl); err != nil || !fi.IsDir() {
		return nil
	}
	dst := filepath.Join("templates", "Default")
	return filepath.WalkDir(tpl, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if path == tpl {
			return nil
		}
		rel, err := filepath.Rel(tpl, path)
		if err != nil {
			return err
		}
		name := filepath.Join(dst, rel)
		if err := symlinkIfMissing(destination, path, name); err != nil {
			return err
		}
		if d.IsDir() {
			if st, err := destination.Lstat(name); err != nil {
				return err
			} else if st.Mode()&fs.ModeSymlink != 0 {
				return filepath.SkipDir
			}
		}
		return nil
	})
}

// symlinkIfMissing creates a link inside root, preserving existing entries.
func symlinkIfMissing(root *os.Root, src, dest string) error {
	if err := root.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", dest, err)
	}
	if _, err := root.Lstat(dest); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := root.Symlink(src, dest); err != nil {
		return fmt.Errorf("symlink %s: %w", dest, err)
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
