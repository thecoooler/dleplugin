package deploysites

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestPasswordLifecycleAndErrors(t *testing.T) {
	unavailable := errors.New("keyring unavailable")
	for _, tc := range []struct {
		mode    string
		broken  bool
		want    Location
		failing bool
	}{
		{mode: "", want: InKeyring},
		{mode: StoreAuto, want: InKeyring},
		{mode: StoreKeyring, want: InKeyring},
		{mode: StoreFile, want: InFile},
		{mode: StoreAuto, broken: true, want: InFile},
		{mode: StoreFile, broken: true, want: InFile},
		{mode: StoreKeyring, broken: true, failing: true},
	} {
		t.Run(fmt.Sprintf("%s broken=%v", tc.mode, tc.broken), func(t *testing.T) {
			keyring.MockInit()
			t.Cleanup(keyring.MockInit)
			if tc.broken {
				keyring.MockInitWithError(unavailable)
			}
			c, err := NewCredentials(t.TempDir(), tc.mode)
			if err != nil {
				t.Fatal(err)
			}
			if tc.failing {
				if _, _, err := c.Password("example.com", "admin"); !errors.Is(err, unavailable) {
					t.Fatalf("%v", err)
				}
				if _, _, err := c.SavePassword("example.com", "admin", "secret"); !errors.Is(err, unavailable) {
					t.Fatalf("%v", err)
				}
				if err := c.DeletePassword("example.com", "admin"); !errors.Is(err, unavailable) {
					t.Fatalf("%v", err)
				}
				if _, err := os.Stat(c.Path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("keyring mode wrote the file: %v", err)
				}
				return
			}
			if p, loc, err := c.Password("example.com", "admin"); p != "" || loc != "" || err != nil {
				t.Fatalf("missing: %q %v %v", p, loc, err)
			}
			loc, fallback, err := c.SavePassword("example.com", "admin", "secret")
			if err != nil || loc != tc.want || (fallback != nil) != (tc.broken && tc.mode == StoreAuto) {
				t.Fatalf("save: %v %v %v", loc, fallback, err)
			}
			if p, loc, err := c.Password("example.com", "admin"); p != "secret" || loc != tc.want || err != nil {
				t.Fatalf("saved: %q %v %v", p, loc, err)
			}
			if _, loc, _ := c.Password("example.com", "other"); loc != "" {
				t.Fatal("password leaked to another login")
			}
			if _, loc, _ := c.Password("other.com", "admin"); loc != "" {
				t.Fatal("password leaked to another host")
			}
			if tc.want == InFile && runtime.GOOS != "windows" {
				info, err := os.Stat(c.Path)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("%v %v", info, err)
				}
			}
			for range 2 {
				if err := c.DeletePassword("example.com", "admin"); err != nil {
					t.Fatal(err)
				}
			}
			if _, loc, _ := c.Password("example.com", "admin"); loc != "" {
				t.Fatal("password not deleted")
			}
		})
	}
	if _, err := NewCredentials(t.TempDir(), "vault"); err == nil || !strings.Contains(err.Error(), "vault") {
		t.Fatalf("%v", err)
	}
}

func TestCredentialsFile(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	dir := t.TempDir()
	auto, _ := NewCredentials(dir, StoreAuto)
	file, _ := NewCredentials(dir, StoreFile)
	// A password saved to the file while the store was unavailable moves to the
	// store on the next auto save.
	if _, _, err := file.SavePassword("example.com", "admin", "old"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := file.SavePassword("example.com", "editor", "kept"); err != nil {
		t.Fatal(err)
	}
	if p, loc, err := auto.Password("example.com", "admin"); p != "old" || loc != InFile || err != nil {
		t.Fatalf("%q %v %v", p, loc, err)
	}
	if loc, _, err := auto.SavePassword("example.com", "admin", "new"); loc != InKeyring || err != nil {
		t.Fatalf("%v %v", loc, err)
	}
	if _, loc, _ := file.Password("example.com", "admin"); loc != "" {
		t.Fatal("stale file password kept")
	}
	if p, _, _ := file.Password("example.com", "editor"); p != "kept" {
		t.Fatalf("other login lost: %q", p)
	}
	b, err := os.ReadFile(file.Path)
	if err != nil || string(b) != "sites:\n  example.com:\n    editor: kept\n" {
		t.Fatalf("%q %v", b, err)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".credentials-*.yaml"))
	if err != nil || len(leftovers) > 0 {
		t.Fatalf("%v %v", leftovers, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(file.Path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := file.Password("example.com", "editor"); err == nil || !strings.Contains(err.Error(), "chmod 600") {
			t.Fatalf("readable file accepted: %v", err)
		}
		if err := os.Chmod(file.Path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{"passwords: {}\n", "sites:\n  example.com: hunter2\n"} {
		if err := os.WriteFile(file.Path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, err := file.Password("example.com", "editor")
		if err == nil || !strings.Contains(err.Error(), file.Path) || strings.Contains(err.Error(), "hunter2") {
			t.Fatalf("invalid file %q: %v", body, err)
		}
	}
	// A stale file copy that cannot be cleaned up does not fail a keyring save.
	if loc, fallback, err := auto.SavePassword("example.com", "admin", "new"); loc != InKeyring || fallback != nil || err != nil {
		t.Fatalf("%v %v %v", loc, fallback, err)
	}
	if runtime.GOOS != "windows" {
		target := filepath.Join(dir, "target.yaml")
		if err := os.Rename(file.Path, target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, file.Path); err != nil {
			t.Fatal(err)
		}
		if _, _, err := file.Password("example.com", "editor"); err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("symlink accepted: %v", err)
		}
		if err := os.Remove(file.Path); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(file.Path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, loc, err := file.Password("example.com", "editor"); loc != "" || err != nil {
		t.Fatalf("empty file: %v %v", loc, err)
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
