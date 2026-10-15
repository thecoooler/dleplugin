// Package convert turns a DLE plugin XML file into dleplugin.yml text.
package convert

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/thecoooler/dleplugin/internal/dlexml"
	"github.com/thecoooler/dleplugin/internal/manifest"
)

var mysqlTags = map[string]bool{
	"mysqlinstall": true, "mysqlupgrade": true, "mysqlenable": true,
	"mysqldisable": true, "mysqldelete": true,
}

// Convert renders the plugin XML at xmlPath as dleplugin.yml text. If srcDir
// is not empty, files whose first action is "create" are written under it
// relative to the mount, with aliases for files outside it, instead of being
// listed in the YAML. The mount comes from namespace when it
// is not empty, otherwise from the plugin files (see inferMount).
func Convert(xmlPath, srcDir, namespace string) (string, error) {
	if namespace != "" {
		if err := manifest.ValidateNamespace(namespace); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(xmlPath); err != nil {
		return "", fmt.Errorf("XML file does not exist: %s", xmlPath)
	}
	root, err := dlexml.ParseFile(xmlPath)
	if err != nil {
		return "", err
	}
	return FromElement(root, strings.TrimSuffix(filepath.Base(xmlPath), ".xml"), srcDir, namespace)
}

// FromElement renders parsed plugin XML as dleplugin.yml text. The slug is
// supplied by the caller; source extraction and namespace handling match Convert.
func FromElement(root *dlexml.Element, slug, srcDir, namespace string) (string, error) {
	if namespace != "" {
		if err := manifest.ValidateNamespace(namespace); err != nil {
			return "", err
		}
	}
	ops := dlexml.LoadFileOperations(root)
	for _, op := range ops {
		if err := manifest.ValidateFilePath(op.File); err != nil {
			return "", err
		}
	}
	name, err := textOf(root, "name")
	if err != nil {
		return "", err
	}
	description, err := textOf(root, "description")
	if err != nil {
		return "", err
	}

	var order []string
	grouped := map[string][]dlexml.FileOperation{}
	for _, op := range ops {
		if _, ok := grouped[op.File]; !ok {
			order = append(order, op.File)
		}
		grouped[op.File] = append(grouped[op.File], op)
	}

	mount := manifest.NamespaceMount(namespace)
	if namespace == "" {
		mount = inferMount(slug, order)
	}
	var sources []sourceFile
	var aliases string
	if srcDir != "" {
		sources, aliases, err = extractSources(order, grouped, mount)
		if err != nil {
			return "", err
		}
	}

	filesYML, err := renderFiles(order, grouped, srcDir)
	if err != nil {
		return "", err
	}
	mysqlYML := renderMySQL(root)

	out := "\nslug: " + manifest.YAMLScalar(slug) + "\nname: " + manifest.YAMLScalar(name) + "\ndescription: " + manifest.YAMLScalar(description) +
		"\nmount: " + manifest.YAMLScalar(mount) + renderDependencies(root) + aliases + "\nfiles:\n" + filesYML + "\nmysql:\n" + mysqlYML + "\n    "
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(out), &document); err != nil {
		return "", fmt.Errorf("validate converted YAML: %w", err)
	}
	if srcDir != "" {
		if err := writeSources(srcDir, sources); err != nil {
			return "", err
		}
	}
	return strings.TrimFunc(out, isSpace), nil
}

// inferMount returns the engine/<Vendor>/<slug> directory holding plugin
// files, or engine/<slug> when there is none.
func inferMount(slug string, files []string) string {
	for _, f := range files {
		parts := strings.Split(f, "/")
		if len(parts) > 3 && parts[0] == "engine" && parts[2] == slug {
			return strings.Join(parts[:3], "/")
		}
	}
	return "engine/" + slug
}

// renderDependencies converts the comma-separated <needplugin> into a
// "dependencies" list ("" when there are none).
func renderDependencies(root *dlexml.Element) string {
	e := root.Find("needplugin")
	if e == nil {
		return ""
	}
	var b strings.Builder
	for _, name := range strings.Split(e.Text, ",") {
		if name = strings.TrimSpace(name); name != "" {
			b.WriteString("\n  - " + manifest.YAMLScalar(name))
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\ndependencies:" + b.String()
}

// textOf returns the text of the first child tag, "None" when it has no text.
func textOf(root *dlexml.Element, tag string) (string, error) {
	e := root.Find(tag)
	if e == nil {
		return "", fmt.Errorf("XML element <%s> is missing", tag)
	}
	if !e.HasText {
		return "None", nil
	}
	return e.Text, nil
}

func renderFiles(order []string, grouped map[string][]dlexml.FileOperation, srcDir string) (string, error) {
	var parts []string
	for _, name := range order {
		items := grouped[name]
		if items[0].Action == "create" && srcDir != "" {
			continue
		}
		parts = append(parts, manifest.YAMLScalar(name)+":")
		for _, it := range items {
			block := "- " + manifest.YAMLScalar(it.Action) + ":\n    search: " + codeScalar(it.Search, 6, false) +
				"    replace: " + codeScalar(it.Replace, 6, false)
			parts = append(parts, indent(block, 2))
		}
	}
	if len(parts) == 0 {
		return "  []", nil
	}
	return indent(strings.Join(parts, "\n"), 2), nil
}

type sourceFile struct {
	target string
	path   string
	code   string
}

// extractSources resolves src-relative paths before writing anything. Paths
// outside the mount keep their DLE path and override the mount through aliases.
func extractSources(order []string, grouped map[string][]dlexml.FileOperation, mount string) ([]sourceFile, string, error) {
	var sources []sourceFile
	var aliases strings.Builder
	used := make(map[string]string)
	for _, name := range order {
		if grouped[name][0].Action != "create" {
			continue
		}
		rel := strings.TrimPrefix(name, mount+"/")
		if previous, ok := used[rel]; ok {
			return nil, "", fmt.Errorf("source path collision: %q and %q both extract to src/%s", previous, name, rel)
		}
		used[rel] = name
		if rel == name {
			if aliases.Len() == 0 {
				aliases.WriteString("\nalias:")
			}
			aliases.WriteString("\n  " + manifest.YAMLScalar(rel) + ": " + manifest.YAMLScalar(name))
		}
		sources = append(sources, sourceFile{target: name, path: rel, code: grouped[name][0].Replace})
	}
	return sources, aliases.String(), nil
}

// writeSources builds a fresh tree before publishing it. Existing directories
// (including symlinks) are never used as extraction roots.
func writeSources(srcDir string, sources []sourceFile) error {
	if len(sources) == 0 {
		return nil
	}
	if _, err := os.Lstat(srcDir); err == nil {
		return fmt.Errorf("source directory already exists: %s", srcDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check source directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(srcDir), 0o755); err != nil {
		return fmt.Errorf("create source parent: %w", err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(srcDir), ".convert-*")
	if err != nil {
		return fmt.Errorf("stage sources: %w", err)
	}
	defer os.RemoveAll(stage)
	root, err := os.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, source := range sources {
		name := source.target
		local := filepath.FromSlash(source.path)
		if err := root.MkdirAll(filepath.Dir(local), 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", name, err)
		}
		f, err := root.OpenFile(local, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
		_, writeErr := f.WriteString(source.code)
		closeErr := f.Close()
		if writeErr != nil {
			return fmt.Errorf("write %s: %w", name, writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", name, closeErr)
		}
	}
	if err := os.Chmod(stage, 0o755); err != nil {
		return err
	}
	if err := root.Close(); err != nil {
		return err
	}
	if err := os.Rename(stage, srcDir); err != nil {
		return fmt.Errorf("publish sources: %w", err)
	}
	return nil
}

func renderMySQL(root *dlexml.Element) string {
	var parts []string
	for _, e := range root.Children {
		if !mysqlTags[e.Tag] || e.Text == "" {
			continue
		}
		parts = append(parts, strings.TrimPrefix(e.Tag, "mysql")+": "+codeScalar(e.Text, 2, true))
	}
	return indent(strings.Join(parts, "\n"), 2)
}

// codeScalar keeps readable block scalars where YAML preserves the code.
// Quoted scalars protect whitespace-only lines, trailing whitespace (including
// the final hook before output trimming), and characters YAML normalizes.
func codeScalar(text string, n int, trimEnd bool) string {
	quoted := false
	for _, r := range text {
		if r != '\n' && r != '\t' && !unicode.IsPrint(r) {
			quoted = true
		}
	}
	trimmed := strings.Trim(text, "\r\n")
	if trimEnd && strings.TrimRightFunc(trimmed, isSpace) != trimmed {
		quoted = true
	}
	for _, line := range strings.Split(text, "\n") {
		if line != "" && strings.TrimFunc(line, isSpace) == "" {
			quoted = true
		}
	}
	if quoted {
		return manifest.YAMLScalar(text) + "\n"
	}
	return literalIndicator(text) + "\n" + indent(text, n) + "\n"
}

// literalIndicator prevents YAML from treating leading code whitespace as
// indentation. Both patch and hook bodies are two spaces deeper than their keys.
func literalIndicator(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			return "|2"
		}
		break
	}
	return "|"
}

// isSpace reports Unicode whitespace including the ASCII separators
// 0x1c-0x1f.
func isSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// splitLinesKeepEnds splits s after every line break, keeping the breaks.
func splitLinesKeepEnds(s string) []string {
	var lines []string
	start := 0
	pos := 0
	for pos < len(s) {
		end := -1
		for j, r := range s[pos:] {
			switch r {
			case '\n', '\v', '\f', '\x1c', '\x1d', '\x1e', '\u0085', ' ', ' ':
				end = pos + j + len(string(r))
			case '\r':
				end = pos + j + 1
				if end < len(s) && s[end] == '\n' {
					end++
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			end = len(s)
		}
		lines = append(lines, s[start:end])
		start = end
		pos = end
	}
	return lines
}

// indent prefixes every non-blank line of text with n spaces.
func indent(text string, n int) string {
	prefix := strings.Repeat(" ", n)
	var b strings.Builder
	for _, l := range splitLinesKeepEnds(text) {
		if strings.TrimFunc(l, isSpace) != "" {
			b.WriteString(prefix)
		}
		b.WriteString(l)
	}
	return b.String()
}
