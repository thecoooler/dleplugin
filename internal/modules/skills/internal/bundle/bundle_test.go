package bundle

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"testing/fstest"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateOlderBundle(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".agents", "skills")
	if err := Install(root, "v1", false); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "dleplugin")
	// Simulate a previous release: changed and removed upstream references.
	write(t, filepath.Join(dir, "SKILL.md"), []byte("older release"))
	write(t, filepath.Join(dir, "obsolete.md"), []byte("old reference"))
	files, err := snapshot(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(receipt{Owner: "dleplugin", Schema: 1, Name: "dleplugin", Version: "v0", Files: files})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, marker), b)
	if err := Install(root, "v2", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "obsolete.md")); !os.IsNotExist(err) {
		t.Fatal("obsolete file retained", err)
	}
	for _, name := range Names() {
		source, err := fs.Sub(assets, "assets/"+name)
		if err != nil {
			t.Fatal(err)
		}
		want, err := snapshot(source)
		if err != nil {
			t.Fatal(err)
		}
		got, err := snapshot(os.DirFS(filepath.Join(root, name)))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: installed tree differs from embedded bundle", name)
		}
	}
}

func TestConflicts(t *testing.T) {
	for _, kind := range []string{"foreign", "edited", "deleted", "extra", "empty-dir", "invalid-receipt", "missing-receipt"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), ".claude", "skills")
			dir := filepath.Join(root, "dle-admin-ui")
			if err := Install(root, "v1", false); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "foreign":
				if err := os.RemoveAll(dir); err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(dir, "SKILL.md"), []byte("my independently installed skill"))
			case "missing-receipt":
				if err := os.Remove(filepath.Join(dir, marker)); err != nil {
					t.Fatal(err)
				}
			case "invalid-receipt":
				write(t, filepath.Join(dir, marker), []byte(`{"owner":"other"}`))
			case "edited":
				write(t, filepath.Join(dir, "SKILL.md"), []byte("edit"))
			case "deleted":
				if err := os.Remove(filepath.Join(dir, "SKILL.md")); err != nil {
					t.Fatal(err)
				}
			case "extra":
				write(t, filepath.Join(dir, "notes.txt"), []byte("my notes"))
			case "empty-dir":
				if err := os.Mkdir(filepath.Join(dir, "mine"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, op := range []func() error{func() error { return Install(root, "v2", false) }, func() error { return Uninstall(root) }} {
				if err := op(); err == nil {
					t.Fatal("conflict accepted")
				}
				if _, err := os.Stat(filepath.Join(root, "dleplugin", "SKILL.md")); err != nil {
					t.Fatal("other skill changed", err)
				}
			}
			if err := Install(root, "v2", true); err != nil {
				t.Fatal(err)
			}
			if err := Uninstall(root); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSymlinks(t *testing.T) {
	for _, component := range []string{".agents", ".agents/skills", ".agents/skills/dleplugin", ".agents/skills/dleplugin/SKILL.md", ".agents/skills/dleplugin/.dleplugin-managed.json"} {
		t.Run(component, func(t *testing.T) {
			base, outside := t.TempDir(), t.TempDir()
			write(t, filepath.Join(outside, "keep"), []byte("keep"))
			root := filepath.Join(base, ".agents", "skills")
			if strings.Contains(component, "dleplugin/") {
				if err := Install(root, "v1", false); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(base, component)); err != nil {
					t.Fatal(err)
				}
			} else if err := os.MkdirAll(filepath.Dir(filepath.Join(base, component)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(base, component)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := Install(root, "v2", false); err == nil {
				t.Fatal("followed symlink")
			}
			if err := Uninstall(root); err == nil {
				t.Fatal("uninstall followed symlink")
			}
			if !strings.Contains(component, "dleplugin/") {
				if err := Install(root, "v2", true); err == nil {
					t.Fatal("force followed directory symlink")
				}
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 1 {
				t.Fatal("symlink target modified", err)
			}
		})
	}
}

// Markdown parsing covers inline, titled and reference-style links, and ignores
// code examples. Heading IDs come from the same parser rather than a second regex.
func checkReferences(tree fs.FS) error {
	type document struct {
		links []string
		ids   map[string]bool
	}
	docs := map[string]document{}
	md := goldmark.New(goldmark.WithParserOptions(parser.WithAutoHeadingID()))
	err := fs.WalkDir(tree, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		b, err := fs.ReadFile(tree, p)
		if err != nil {
			return err
		}
		doc := document{ids: map[string]bool{}}
		node := md.Parser().Parse(text.NewReader(b))
		err = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			switch n := n.(type) {
			case *ast.Link:
				doc.links = append(doc.links, string(n.Destination))
			case *ast.Heading:
				if id, ok := n.AttributeString("id"); ok {
					doc.ids[string(id.([]byte))] = true
				}
			}
			return ast.WalkContinue, nil
		})
		docs[p] = doc
		return err
	})
	if err != nil {
		return err
	}
	for p, doc := range docs {
		for _, link := range doc.links {
			u, err := url.Parse(link)
			if err != nil {
				return fmt.Errorf("%s: invalid reference %q: %w", p, link, err)
			}
			if u.IsAbs() || u.Host != "" {
				continue
			}
			resolved := p
			if u.Path != "" {
				resolved = path.Clean(path.Join(path.Dir(p), u.Path))
			}
			if strings.HasPrefix(u.Path, "/") || !strings.HasPrefix(resolved, "assets/") {
				return fmt.Errorf("%s: link escapes bundle: %s", p, link)
			}
			if _, err := fs.Stat(tree, resolved); err != nil {
				return fmt.Errorf("%s: broken reference %s: %w", p, link, err)
			}
			if u.Fragment != "" && !docs[resolved].ids[u.Fragment] {
				return fmt.Errorf("%s: missing anchor in %s", p, link)
			}
		}
	}
	return nil
}

func TestBundledReferences(t *testing.T) {
	if err := checkReferences(assets); err != nil {
		t.Fatal(err)
	}
}

func TestReferenceValidation(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		valid        bool
	}{
		{"inline title", "[link](other.md#target \"Title\")", true},
		{"reference style", "[link][ref]\n\n[ref]: other.md#target \"Title\"", true},
		{"local anchor", "# Local\n\n[link](#local)", true},
		{"missing anchor", "[link](other.md#missing)", false},
		{"reference missing anchor", "[link][ref]\n\n[ref]: other.md#missing", false},
		{"missing file", "[link](missing.md)", false},
		{"escape", "[link](../../outside.md)", false},
		{"code example", "`[link](missing.md)`", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tree := fstest.MapFS{
				"assets/skill/SKILL.md": &fstest.MapFile{Data: []byte(tt.source)},
				"assets/skill/other.md": &fstest.MapFile{Data: []byte("# Target\n")},
			}
			if err := checkReferences(tree); (err == nil) != tt.valid {
				t.Fatalf("valid = %v, error = %v", tt.valid, err)
			}
		})
	}
}

func TestUninstallWithMissingCompanion(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".agents", "skills")
	if err := Install(root, "v1", false); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "dle-admin-ui")); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dleplugin")); !os.IsNotExist(err) {
		t.Fatal("remaining skill not removed", err)
	}
}

func TestCleanupFailureIsReported(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix directory permissions and a non-root user")
	}
	for _, action := range []string{"install", "uninstall", "force"} {
		t.Run(action, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), ".claude", "skills")
			if err := Install(root, "v1", false); err != nil {
				t.Fatal(err)
			}
			locked := filepath.Join(root, "dle-admin-ui", "references")
			if action == "force" {
				if err := os.RemoveAll(filepath.Join(root, "dle-admin-ui")); err != nil {
					t.Fatal(err)
				}
				locked = filepath.Join(root, "dle-admin-ui", "user-data")
				write(t, filepath.Join(locked, "keep.txt"), []byte("foreign content"))
			}
			// Restore permissions on renamed leftovers before TempDir's cleanup runs.
			t.Cleanup(func() {
				_ = filepath.WalkDir(filepath.Dir(root), func(p string, d fs.DirEntry, err error) error {
					if err == nil && d.IsDir() {
						return os.Chmod(p, 0o755)
					}
					return err
				})
			})
			if err := os.Chmod(locked, 0o555); err != nil {
				t.Fatal(err)
			}
			var err error
			if action == "uninstall" {
				err = Uninstall(root)
			} else {
				err = Install(root, "v2", action == "force")
			}
			if err == nil || !strings.Contains(err.Error(), "cleanup of "+filepath.Dir(root)) || !strings.Contains(err.Error(), "failed") {
				t.Fatalf("expected cleanup error naming remaining temporary files, got %v", err)
			}
			leftovers, globErr := filepath.Glob(filepath.Join(filepath.Dir(root), ".dleplugin-skills-*"))
			if globErr != nil || len(leftovers) != 1 {
				t.Fatalf("leftovers = %v, error %v", leftovers, globErr)
			}
			if inside, _ := filepath.Glob(filepath.Join(root, ".dleplugin-skills-*")); len(inside) != 0 {
				t.Fatalf("temporary copies remain inside skills directory: %v", inside)
			}
			if !strings.Contains(err.Error(), leftovers[0]) {
				t.Fatalf("cleanup error lacks recovery path: %v", err)
			}
		})
	}
}
