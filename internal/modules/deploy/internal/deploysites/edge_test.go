package deploysites

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestPasswordLifecycleAndErrors(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	if p, ok, err := Password("example.com", "admin"); p != "" || ok || err != nil {
		t.Fatalf("missing: %q %v %v", p, ok, err)
	}
	if err := SavePassword("example.com", "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	if p, ok, err := Password("example.com", "admin"); p != "secret" || !ok || err != nil {
		t.Fatalf("saved: %q %v %v", p, ok, err)
	}
	if _, ok, _ := Password("example.com", "other"); ok {
		t.Fatal("password leaked to another login")
	}
	if _, ok, _ := Password("other.com", "admin"); ok {
		t.Fatal("password leaked to another host")
	}
	for range 2 {
		if err := DeletePassword("example.com", "admin"); err != nil {
			t.Fatal(err)
		}
	}
	unavailable := errors.New("keyring unavailable")
	keyring.MockInitWithError(unavailable)
	if _, _, err := Password("example.com", "admin"); !errors.Is(err, unavailable) {
		t.Fatalf("%v", err)
	}
	if err := SavePassword("example.com", "admin", "secret"); !errors.Is(err, unavailable) {
		t.Fatalf("%v", err)
	}
	if err := DeletePassword("example.com", "admin"); !errors.Is(err, unavailable) {
		t.Fatalf("%v", err)
	}
}

func TestSaveReplacementAndFailureCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Filename)
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	d := &Document{Sites: map[string]Site{"example.com": {Login: "admin"}}}
	if err := Save(path, d); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("mode %o", info.Mode().Perm())
		}
	}
	loaded, err := Load(path)
	if err != nil || loaded.Sites["example.com"] != d.Sites["example.com"] {
		t.Fatalf("%v %v", loaded, err)
	}
	// Rename over a directory fails; its contents must survive and temporary files must be removed.
	dest := filepath.Join(dir, "destination")
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dest, "keep")
	if err := os.WriteFile(keep, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Save(dest, d); err == nil {
		t.Fatal("save over directory succeeded")
	}
	if b, err := os.ReadFile(keep); err != nil || string(b) != "keep" {
		t.Fatalf("%q %v", b, err)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".deploy-*.yaml"))
	if err != nil || len(leftovers) > 0 {
		t.Fatalf("%v %v", leftovers, err)
	}
	if err := Save(filepath.Join(path, "child"), d); err == nil {
		t.Fatal("parent file accepted")
	}
}

func TestEffectiveScopesAndErrors(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global")
	project := filepath.Join(dir, "project")
	d := &Document{Sites: map[string]Site{"example.com": {Login: "admin", AdminPath: "panel.php", PasswordEnv: "GLOBAL"}}}
	if err := Save(global, d); err != nil {
		t.Fatal(err)
	}
	entries, err := Effective(global, "")
	if err != nil || entries["example.com"].Scope != "global" {
		t.Fatalf("%v %v", entries, err)
	}
	if entries, err := Effective("", ""); err != nil || len(entries) != 0 {
		t.Fatalf("%v %v", entries, err)
	}
	if err := os.WriteFile(project, []byte("sites: ["), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Effective(global, project); err == nil || !strings.Contains(err.Error(), project) {
		t.Fatalf("%v", err)
	}
	if _, err := Effective(project, global); err == nil {
		t.Fatal("invalid global ignored")
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("directory accepted")
	}
	for _, body := range []string{"", "sites: null\n"} {
		if err := os.WriteFile(project, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if d, err := Load(project); err != nil || d.Sites == nil || len(d.Sites) != 0 {
			t.Fatalf("%v %v", d, err)
		}
	}
}
