// Package bundle installs the CLI's embedded agent skills without network access.
package bundle

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
)

// Source is the upstream revision used for the bundled copies (with local fixes).
const Source = "9a24d6385200c32b9925ac27eb0af8b3ec148981"
const marker = ".dleplugin-managed.json"

//go:embed assets
var assets embed.FS

// Names returns the skills shipped and installed together.
func Names() []string { return []string{"dleplugin", "dle-admin-ui"} }

type receipt struct {
	Owner   string            `json:"owner"`
	Schema  int               `json:"schema"`
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Source  string            `json:"source"`
	Files   map[string]string `json:"files"`
}

// Target resolves the agent's skills directory. Project scope is exactly workdir.
func Target(workdir, home, agent string, global bool) (string, error) {
	var dir string
	switch agent {
	case "codex":
		dir = ".agents"
	case "claude":
		dir = ".claude"
	default:
		return "", fmt.Errorf("--agent must be codex or claude")
	}
	base := workdir
	if global {
		base = home
	}
	if base == "" {
		return "", fmt.Errorf("empty installation base directory")
	}
	return filepath.Join(base, dir, "skills"), nil
}

// safeRoot refuses symlinks in the agent directory and skills directory.
// The invocation/home directory itself may have been resolved by the OS.
func safeRoot(root string) error {
	for _, p := range []string{filepath.Dir(root), root} {
		st, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !st.IsDir() {
			return fmt.Errorf("%s: expected a real directory (no symlinks)", p)
		}
	}
	return nil
}

// snapshot never follows symlinks and includes empty directories. The receipt
// is excluded; its recorded paths are only compared, never used for filesystem IO.
func snapshot(tree fs.FS) (map[string]string, error) {
	files := map[string]string{}
	err := fs.WalkDir(tree, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." || p == marker {
			return nil
		}
		if d.IsDir() {
			files[p] = "dir"
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: expected a regular file (no symlinks)", p)
		}
		b, err := fs.ReadFile(tree, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		files[p] = hex.EncodeToString(sum[:])
		return nil
	})
	return files, err
}

// inspect reports whether an existing directory is owned and unchanged.
func inspect(dir, name string) (bool, error) {
	st, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !st.IsDir() {
		return true, fmt.Errorf("%s: expected a real skill directory (no symlinks)", dir)
	}
	mst, err := os.Lstat(filepath.Join(dir, marker))
	if err != nil || !mst.Mode().IsRegular() {
		return true, fmt.Errorf("%s: not managed by dleplugin", dir)
	}
	b, err := os.ReadFile(filepath.Join(dir, marker))
	if err != nil {
		return true, err
	}
	var r receipt
	if json.Unmarshal(b, &r) != nil || r.Owner != "dleplugin" || r.Schema != 1 || r.Name != name || r.Files["SKILL.md"] == "" {
		return true, fmt.Errorf("%s: invalid dleplugin ownership receipt", dir)
	}
	files, err := snapshot(os.DirFS(dir))
	if err != nil {
		return true, err
	}
	if !reflect.DeepEqual(files, r.Files) {
		return true, fmt.Errorf("%s: skill has user changes", dir)
	}
	return true, nil
}

func stage(dir, name, version string) error {
	tree, err := fs.Sub(assets, "assets/"+name)
	if err != nil {
		return err
	}
	files, err := snapshot(tree)
	if err != nil {
		return err
	}
	err = fs.WalkDir(tree, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := fs.ReadFile(tree, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(receipt{Owner: "dleplugin", Schema: 1, Name: name, Version: version, Source: Source, Files: files}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, marker), append(b, '\n'), 0o644)
}

// Install copies both skills, preflighting all conflicts before replacing either.
// Force permits replacing foreign/modified real directories, never symlinks.
func Install(root, version string, force bool) error {
	return change(root, version, force, false)
}

// Uninstall removes only unchanged managed skills; foreign and modified copies
// cause an error before either skill is removed. Missing copies are harmless.
func Uninstall(root string) error { return change(root, "", false, true) }

func change(root, version string, force, remove bool) (result error) {
	if err := safeRoot(root); err != nil {
		return err
	}
	exists := map[string]bool{}
	for _, name := range Names() {
		dir := filepath.Join(root, name)
		found, err := inspect(dir, name)
		exists[name] = found
		if err != nil {
			if !force {
				if remove {
					return fmt.Errorf("%w; restore or move the directory before uninstalling", err)
				}
				return fmt.Errorf("%w; install with --force to replace, or restore/move the directory", err)
			}
			st, statErr := os.Lstat(dir)
			if statErr != nil {
				return statErr
			}
			if !st.IsDir() {
				return fmt.Errorf("%s: --force requires a real directory (no symlinks)", dir)
			}
		}
	}
	if remove && !exists[Names()[0]] && !exists[Names()[1]] {
		return nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(filepath.Dir(root), ".dleplugin-skills-")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			if err := os.RemoveAll(temp); err != nil {
				result = errors.Join(result, fmt.Errorf("cleanup of %s failed; remove remaining temporary files manually: %w", temp, err))
			}
		}
	}()
	if !remove {
		for _, name := range Names() {
			if err := stage(filepath.Join(temp, "new-"+name), name, version); err != nil {
				return fmt.Errorf("stage %s: %w", name, err)
			}
		}
	}
	var backed, installed []string
	rollback := func(cause error) error {
		var restoreErr error
		for _, name := range installed {
			restoreErr = errors.Join(restoreErr, os.RemoveAll(filepath.Join(root, name)))
		}
		for _, name := range backed {
			restoreErr = errors.Join(restoreErr, os.Rename(filepath.Join(temp, "old-"+name), filepath.Join(root, name)))
		}
		if restoreErr != nil {
			cleanup = false // Retain backups if restoration failed.
			return errors.Join(cause, fmt.Errorf("rollback failed; backups retained in %s: %w", temp, restoreErr))
		}
		return cause
	}
	for _, name := range Names() {
		dst := filepath.Join(root, name)
		if exists[name] {
			if err := os.Rename(dst, filepath.Join(temp, "old-"+name)); err != nil {
				return rollback(err)
			}
			backed = append(backed, name)
		}
		if !remove {
			if err := os.Rename(filepath.Join(temp, "new-"+name), dst); err != nil {
				return rollback(err)
			}
			installed = append(installed, name)
		}
	}
	return nil
}
