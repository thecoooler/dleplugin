package dle

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/thecoooler/dleplugin/internal/manifest"
)

type fakeProject struct {
	root  string
	info  *manifest.Info
	files []manifest.SourceFile
}

func (f *fakeProject) Root() string                          { return f.root }
func (f *fakeProject) Info() *manifest.Info                  { return f.info }
func (f *fakeProject) Files() ([]manifest.SourceFile, error) { return f.files, nil }

type fakeRegistry struct{ calls *[]string }

func (r fakeRegistry) Add(_ context.Context, i *manifest.Info) error {
	*r.calls = append(*r.calls, "add:"+i.Name)
	return nil
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readLink(t *testing.T, path string) string {
	t.Helper()
	l, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("readlink %s: %v", path, err)
	}
	return l
}

func isLink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// setup returns a Host, project and call log. Resolves symlinks in temp dirs.
func setup(t *testing.T) (*Host, *fakeProject, *[]string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "plugin")
	dle := filepath.Join(base, "dle")
	write(t, filepath.Join(root, "src", "engine", "modules", "a.php"), "A")
	write(t, filepath.Join(root, "src", "b.php"), "B")
	if err := os.MkdirAll(dle, 0o755); err != nil {
		t.Fatal(err)
	}
	info := manifest.NewInfo()
	info.Name = "demo"
	p := &fakeProject{root: root, info: info, files: []manifest.SourceFile{
		{Src: filepath.Join(root, "src", "engine", "modules", "a.php"), Mount: "engine/modules/a.php"},
		{Src: filepath.Join(root, "src", "b.php"), Mount: "b.php"},
	}}
	calls := &[]string{}
	return &Host{Path: dle, Registry: fakeRegistry{calls}}, p, calls
}

func TestConnectSymlink(t *testing.T) {
	h, p, calls := setup(t)
	write(t, filepath.Join(h.Path, "b.php"), "existing")
	write(t, filepath.Join(h.Path, "engine/cache/system/plugins.php"), "x")
	write(t, filepath.Join(h.Path, "engine/cache/system/plugins/k.php"), "x")

	if err := h.Connect(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(h.Path, "engine/modules/a.php")
	if got := readLink(t, a); got != p.files[0].Src {
		t.Errorf("link target %q", got)
	}
	if isLink(filepath.Join(h.Path, "b.php")) {
		t.Error("existing file replaced")
	}
	if !reflect.DeepEqual(*calls, []string{"add:demo"}) {
		t.Errorf("calls %v", *calls)
	}
	for _, f := range []string{"engine/cache/system/plugins.php", "engine/cache/system/plugins"} {
		if _, err := os.Lstat(filepath.Join(h.Path, f)); err == nil {
			t.Errorf("%s not cleared", f)
		}
	}
}

func TestConnectTemplates(t *testing.T) {
	h, p, _ := setup(t)
	write(t, filepath.Join(p.root, "templates/newdir/x.tpl"), "x")
	write(t, filepath.Join(p.root, "templates/olddir/y.tpl"), "y")
	write(t, filepath.Join(p.root, "templates/olddir/z.tpl"), "z")
	write(t, filepath.Join(p.root, "templates/top.tpl"), "t")
	write(t, filepath.Join(h.Path, "templates/Default/olddir/y.tpl"), "mine")

	if err := h.Connect(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	d := filepath.Join(h.Path, "templates/Default")
	if got := readLink(t, filepath.Join(d, "newdir")); got != filepath.Join(p.root, "templates/newdir") {
		t.Errorf("newdir -> %q", got)
	}
	if isLink(filepath.Join(d, "olddir")) {
		t.Error("existing dir replaced")
	}
	if isLink(filepath.Join(d, "olddir/y.tpl")) {
		t.Error("existing file replaced")
	}
	if got := readLink(t, filepath.Join(d, "olddir/z.tpl")); got != filepath.Join(p.root, "templates/olddir/z.tpl") {
		t.Errorf("z.tpl -> %q", got)
	}
	if !isLink(filepath.Join(d, "top.tpl")) {
		t.Error("top.tpl not linked")
	}
}

func TestConnectEnvAndConftest(t *testing.T) {
	h, p, _ := setup(t)
	write(t, filepath.Join(p.root, ".env"), "E")
	if err := h.Connect(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if got := readLink(t, filepath.Join(h.Path, ".env")); got != filepath.Join(p.root, ".env") {
		t.Errorf(".env -> %q", got)
	}
	if _, err := os.Lstat(filepath.Join(h.Path, "conftest.php")); err == nil {
		t.Error("conftest.php should not exist")
	}
}

func TestConnectWithoutRegistry(t *testing.T) {
	h, p, _ := setup(t)
	h.Registry = nil
	if err := h.Connect(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

func TestConnectRejectsUnsafeMountsBeforeWriting(t *testing.T) {
	for _, mount := range []string{"../escape.php", "/escape.php", `..\escape.php`, `C:\escape.php`} {
		t.Run(mount, func(t *testing.T) {
			h, p, calls := setup(t)
			p.files[1].Mount = mount
			if err := h.Connect(context.Background(), p); err == nil {
				t.Fatal("unsafe mount accepted")
			}
			entries, err := os.ReadDir(h.Path)
			if err != nil || len(entries) != 0 || len(*calls) != 0 {
				t.Fatalf("partial connect: %v, %v, %v", entries, *calls, err)
			}
		})
	}
}

func TestConnectRejectsEscapingParentSymlink(t *testing.T) {
	h, p, _ := setup(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(h.Path, "engine")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := h.Connect(context.Background(), p); err == nil {
		t.Fatal("escaping symlink accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside changed: %v, %v", entries, err)
	}
}
