// Package plugin implements a dleplugin project: locating its root, loading
// the manifest with extra configs, generating extras and listing sources.
package plugin

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/thecoooler/dleplugin/internal/manifest"
	"github.com/thecoooler/dleplugin/internal/paths"
)

// Marker is the manifest file name that identifies a plugin root.
const Marker = "dleplugin.yml"

// DLEFunctionFiles are the DLE files that receive the autoloader.
var DLEFunctionFiles = [2]string{
	"engine/inc/include/functions.inc.php",
	"engine/modules/functions.php",
}

const (
	sessionSearch = "function dle_session( $sid = false ) {"
)

// Plugin is a loaded plugin project.
type Plugin struct {
	root string
	base *manifest.Info
}

// FindRoot returns the nearest ancestor of dir (inclusive) containing
// dleplugin.yml. The manifest is not parsed.
func FindRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	root, ok := paths.FindAncestorWith(abs, Marker)
	if !ok {
		return "", errors.New("Configuration file dleplugin.yml was not found")
	}
	return root, nil
}

// FromDir finds the plugin root from dir and loads it.
func FromDir(dir string) (*Plugin, error) {
	root, err := FindRoot(dir)
	if err != nil {
		return nil, err
	}
	return Load(root)
}

// Load reads root/dleplugin.yml and merges the configured extra configs.
func Load(root string) (*Plugin, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", root, err)
	}
	data, err := readText(filepath.Join(abs, Marker),
		"dleplugin.yml was not found: %w",
		"No read permission for dleplugin.yml: %w")
	if err != nil {
		return nil, err
	}
	info, err := manifest.Parse(data)
	if err != nil {
		return nil, err
	}
	for _, entry := range info.Extras.Configs {
		extra, err := readText(filepath.Join(abs, entry+".yml"),
			"Config file "+entry+" was not found: %w",
			"No read permission for config file "+entry+": %w")
		if err != nil {
			return nil, err
		}
		files, err := manifest.ParseFiles(extra)
		if err != nil {
			return nil, err
		}
		for _, name := range files.Keys() {
			patch, _ := files.Get(name)
			info.AppendActions(name, patch.Actions...)
		}
	}
	return &Plugin{root: abs, base: info}, nil
}

// readText reads a file applying universal-newline translation.
func readText(path, missingFmt, permFmt string) ([]byte, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf(missingFmt, err)
	case errors.Is(err, fs.ErrPermission):
		return nil, fmt.Errorf(permFmt, err)
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	return []byte(strings.ReplaceAll(s, "\r", "\n")), nil
}

// Root returns the absolute plugin root directory.
func (p *Plugin) Root() string { return p.root }

// Slug returns the plugin slug.
func (p *Plugin) Slug() string { return p.base.Slug }

// Name returns the plugin name.
func (p *Plugin) Name() string { return p.base.Name }

// Description returns the plugin description.
func (p *Plugin) Description() string { return p.base.Description }

// Mount returns the normalized mount directory ("" when unset).
func (p *Plugin) Mount() string { return p.base.Mount }

// Namespace returns the PHP namespace of the plugin classes ("" when unknown).
func (p *Plugin) Namespace() string { return p.base.Namespace }

// Info returns a copy of the manifest with generated extras applied
// (admin section, autoloader). Calls are independent.
func (p *Plugin) Info() *manifest.Info {
	info := p.base.Clone()
	applyAdminSection(info)
	applyAutoloader(info)
	return info
}

func joinSQL(existing, stmt string) string {
	if existing == "" {
		return stmt
	}
	return existing + "\n\n" + stmt
}

func applyAdminSection(info *manifest.Info) {
	if !info.Extras.AdminSection {
		return
	}
	lower := strings.ToLower(info.Slug)
	insert := "INSERT IGNORE INTO `{prefix}_admin_sections` (`name`, `title`, `descr`, `icon`, `allow_groups`)" +
		" VALUES (" + sqlString(lower) + ", " + sqlString(info.Name) + ", " + sqlString(info.Description) + ", '', 'all')"
	del := "DELETE FROM `{prefix}_admin_sections` WHERE `name` = " + sqlString(lower) + ";"
	info.MySQL.Enable = joinSQL(info.MySQL.Enable, insert)
	info.MySQL.Disable = joinSQL(info.MySQL.Disable, del)
	info.MySQL.Delete = joinSQL(info.MySQL.Delete, del)
}

// sqlString preserves ordinary fixture bytes. Hex literals encode quotes,
// backslashes and controls independently of NO_BACKSLASH_ESCAPES mode.
func sqlString(s string) string {
	if strings.ContainsAny(s, "'\\\x00\n\r\x1a") || strings.Contains(s, "{prefix}") || strings.Contains(s, "{userprefix}") || strings.Contains(s, "{engine}") {
		return "CONVERT(X'" + hex.EncodeToString([]byte(s)) + "' USING utf8mb4)"
	}
	return "'" + s + "'"
}

func applyAutoloader(info *manifest.Info) {
	if !info.Extras.Autoloader[0] && !info.Extras.Autoloader[1] {
		return
	}
	replace := `
spl_autoload_register(function ($name) {
    if (strpos($name, '` + strings.ReplaceAll(info.Namespace, `\`, `\\`) + `\\') !== 0) return;
    $file = str_replace("\\", "/", $name);
    @require_once \DLEPlugins::Check(ENGINE_DIR . '/' . $file . '.php');
});
            `
	action := newAction("before", sessionSearch, replace)
	for i, file := range DLEFunctionFiles {
		if info.Extras.Autoloader[i] {
			info.AppendActions(file, action)
		}
	}
}

// newAction builds an action with the same \r\n trimming as parsed ones.
func newAction(name, search, replace string) manifest.Action {
	return manifest.Action{Name: name, Search: strings.Trim(search, "\r\n"), Replace: strings.Trim(replace, "\r\n")}
}

// Files lists every regular file under src/ with its DLE mount path, sorted
// by slash-separated path relative to src. A missing src directory yields no
// files.
func (p *Plugin) Files() ([]manifest.SourceFile, error) {
	src := filepath.Join(p.root, "src")
	var rels []string
	if err := walkFiles(src, "", &rels); err != nil {
		return nil, err
	}
	sort.Strings(rels)
	files := make([]manifest.SourceFile, 0, len(rels))
	for _, rel := range rels {
		mount, ok := p.base.Alias.Get(rel)
		if !ok {
			mount = manifest.JoinMount(p.base.Mount, rel)
		}
		files = append(files, manifest.SourceFile{
			Src:   filepath.Join(src, filepath.FromSlash(rel)),
			Mount: mount,
		})
	}
	return files, nil
}

func walkFiles(dir, rel string, out *[]string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && rel == "" {
			return nil
		}
		return fmt.Errorf("read directory %s: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		full := filepath.Join(dir, name)
		typ := e.Type()
		if typ&fs.ModeSymlink != 0 {
			st, err := os.Stat(full)
			if err != nil || !st.Mode().IsRegular() {
				continue
			}
			typ = 0
		}
		switch {
		case typ.IsDir():
			if err := walkFiles(full, childRel, out); err != nil {
				return err
			}
		case typ.IsRegular() && name != ".DS_Store":
			*out = append(*out, childRel)
		}
	}
	return nil
}

// NormalizeFiles converts file arguments to slash paths relative to root.
// Relative arguments are resolved against invocationDir; paths outside root
// are returned unchanged and the root itself becomes ".".
func NormalizeFiles(files []string, root, invocationDir string) []string {
	inv := resolve(invocationDir)
	rootAbs := resolve(root)
	out := make([]string, 0, len(files))
	for _, f := range files {
		p := f
		if !filepath.IsAbs(p) {
			p = filepath.Join(inv, p)
		}
		rel, err := filepath.Rel(rootAbs, resolve(p))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			out = append(out, f)
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

// resolve makes p absolute and resolves symlinks of the longest existing prefix.
func resolve(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	rest := ""
	cur := abs
	for {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
