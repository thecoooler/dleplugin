package dist

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func createTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z := zip.NewWriter(f)
	for name, content := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
}

func createCleanDist(t *testing.T, path, prefix, version string, extra map[string]string) {
	t.Helper()
	files := map[string]string{
		prefix + "install.php":     fmt.Sprintf("<?php\n$config = array(\n'version_id' => '%s',\n'skin' => 'Default',\n);\n", version),
		prefix + "index.php":       "<?php echo 'hello';",
		prefix + "engine/init.php": "<?php echo 'init';",
	}
	if prefix != "" {
		files["Documentation/readme.txt"] = "docs"
	}
	for k, v := range extra {
		files[k] = v
	}
	createTestZip(t, path, files)
}

func TestInspectAndDetectVersion(t *testing.T) {
	for _, prefix := range []string{"", "upload/", "dle14_1/upload/", "deep/sub/folder/upload/"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "dist.zip")
			createCleanDist(t, path, prefix, "21.0", nil)

			info, err := InspectFile(path)
			if err != nil {
				t.Fatalf("inspect valid: %v", err)
			}
			if info.Version != "21.0" {
				t.Errorf("version: got %s, want 21.0", info.Version)
			}
			if info.Prefix != prefix {
				t.Errorf("prefix: got %s, want %s", info.Prefix, prefix)
			}

			r, err := zip.OpenReader(path)
			if err != nil {
				t.Fatal(err)
			}
			ver, err := DetectVersion(&r.Reader)
			r.Close()
			if err != nil || ver != "21.0" {
				t.Errorf("DetectVersion: got %s, %v", ver, err)
			}
		})
	}

	t.Run("disambiguates multiple install.php by index.php", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "multi-install.zip")
		// The real site is in upload/ (has index.php and engine/), while docs has a dummy install.php
		createCleanDist(t, path, "upload/", "21.0", map[string]string{
			"Documentation/samples/install.php": "not real installer",
		})
		info, err := InspectFile(path)
		if err != nil {
			t.Fatalf("failed to disambiguate: %v", err)
		}
		if info.Prefix != "upload/" || info.Version != "21.0" {
			t.Fatalf("wrong disambiguation: prefix=%s, ver=%s", info.Prefix, info.Version)
		}
	})

	t.Run("rejects without install.php", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "no-install.zip")
		createTestZip(t, path, map[string]string{"index.php": "<?php", "engine/init.php": "<?php"})
		if _, err := InspectFile(path); err == nil {
			t.Fatal("accepted zip without install.php")
		}
	})

	t.Run("rejects with installed config in subfolder", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "installed.zip")
		createCleanDist(t, path, "upload/", "21.0", map[string]string{
			"upload/engine/data/config.php": "<?php",
		})
		if _, err := InspectFile(path); err == nil {
			t.Fatal("accepted zip with engine/data/config.php")
		}
	})

	t.Run("rejects with installed dbconfig", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "installed.zip")
		createCleanDist(t, path, "custom/upload/", "21.0", map[string]string{
			"custom/upload/engine/data/dbconfig.php": "<?php",
		})
		if _, err := InspectFile(path); err == nil {
			t.Fatal("accepted zip with dbconfig.php")
		}
	})

	t.Run("rejects without index.php", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "no-index.zip")
		createTestZip(t, path, map[string]string{
			"install.php":     "$config = array('version_id' => '21.0');",
			"engine/init.php": "<?php",
		})
		if _, err := InspectFile(path); err == nil {
			t.Fatal("accepted zip without index.php")
		}
	})

	t.Run("rejects without engine directory", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "no-engine.zip")
		createTestZip(t, path, map[string]string{
			"install.php": "$config = array('version_id' => '21.0');",
			"index.php":   "<?php",
		})
		if _, err := InspectFile(path); err == nil {
			t.Fatal("accepted zip without engine/")
		}
	})

	t.Run("rejects unsupported legacy version", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "old.zip")
		createCleanDist(t, path, "", "10.0", nil)
		if _, err := InspectFile(path); err == nil {
			t.Fatal("accepted version 10.0")
		}
	})
}

func TestResolveAndList(t *testing.T) {
	dir := t.TempDir()

	// 11.0 with install.php in root
	createCleanDist(t, filepath.Join(dir, "11.0.zip"), "", "11.0", nil)
	// 11.1 with install.php in upload/
	createCleanDist(t, filepath.Join(dir, "11.1.zip"), "upload/", "11.1", nil)
	// 12.0 with install.php in nested subfolder
	createCleanDist(t, filepath.Join(dir, "12.0.zip"), "dle_12.0/upload/", "12.0", nil)
	// 21.0 with install.php in upload/
	createCleanDist(t, filepath.Join(dir, "21.0.zip"), "upload/", "21.0", nil)

	// _upload.zip archives are NOT supported and must not be indexed by List or resolved
	createCleanDist(t, filepath.Join(dir, "15.0_upload.zip"), "", "15.0", nil)

	// Archive with mismatched internal version (named 20.0.zip but has version 19.0 inside)
	createCleanDist(t, filepath.Join(dir, "20.0.zip"), "", "19.0", nil)

	// Custom named archive (must NOT be found by Resolve without path, but inspected directly)
	createCleanDist(t, filepath.Join(dir, "custom_dle_bundle.zip"), "", "13.0", nil)

	// Dump archives (without install.php, with configs) — should be completely ignored
	createTestZip(t, filepath.Join(dir, "18.0_dump.zip"), map[string]string{
		"index.php":              "<?php",
		"engine/data/config.php": "<?php",
		"dump.sql":               "INSERT INTO ...",
	})
	createTestZip(t, filepath.Join(dir, "19.0.zip"), map[string]string{
		"index.php":                "<?php",
		"engine/data/dbconfig.php": "<?php",
	})

	// Non-clean nested archive without install.php
	createTestZip(t, filepath.Join(dir, "14.2.zip"), map[string]string{
		"14.2/dle14_2.zip": "binary data",
	})

	versions, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"11.0", "11.1", "12.0", "21.0"}
	if !reflect.DeepEqual(versions, expected) {
		t.Fatalf("List: got %v, want %v", versions, expected)
	}

	path21, err := Resolve(dir, "21.0")
	if err != nil || filepath.Base(path21) != "21.0.zip" {
		t.Fatalf("Resolve 21.0: got %s, %v", path21, err)
	}

	path12, err := Resolve(dir, "12.0")
	if err != nil || filepath.Base(path12) != "12.0.zip" {
		t.Fatalf("Resolve 12.0: got %s, %v", path12, err)
	}

	// _upload.zip must NOT be resolved
	if _, err := Resolve(dir, "15.0"); err == nil {
		t.Fatal("Resolve should not accept 15.0_upload.zip")
	}

	// Resolve must reject archive with version mismatch
	if _, err := Resolve(dir, "20.0"); err == nil {
		t.Fatal("Resolve accepted archive with version mismatch")
	}

	// Resolve must not scan arbitrary named archives in dir
	if _, err := Resolve(dir, "13.0"); err == nil {
		t.Fatal("Resolve should not find custom_dle_bundle.zip when looking for 13.0")
	}
	customInfo, err := InspectFile(filepath.Join(dir, "custom_dle_bundle.zip"))
	if err != nil || customInfo.Version != "13.0" {
		t.Fatalf("direct InspectFile custom archive: %v, %v", customInfo, err)
	}

	// Resolve dumps must fail
	if _, err := Resolve(dir, "18.0"); err == nil {
		t.Fatal("Resolve accepted dump archive for 18.0")
	}
	if _, err := Resolve(dir, "19.0"); err == nil {
		t.Fatal("Resolve accepted archive with dbconfig for 19.0")
	}
	if _, err := Resolve(dir, "14.2"); err == nil {
		t.Fatal("Resolve accepted nested archive for 14.2")
	}
}

func TestExtract(t *testing.T) {
	for _, prefix := range []string{"", "upload/", "dle_nested/upload/"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "21.0.zip")
			createCleanDist(t, path, prefix, "21.0", nil)

			archive, err := zip.OpenReader(path)
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(dir, "site")
			err = Extract(archive, dest)
			archive.Close()
			if err != nil {
				t.Fatal(err)
			}

			if _, err := os.Stat(filepath.Join(dest, "install.php")); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dest, "index.php")); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dest, "Documentation")); !os.IsNotExist(err) {
				t.Fatal("extracted Documentation outside of prefix")
			}
		})
	}

	t.Run("rejects zip slip attempt", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "evil.zip")
		createTestZip(t, path, map[string]string{
			"install.php":     "$config = array('version_id' => '21.0');",
			"index.php":       "<?php",
			"engine/init.php": "<?php",
			"../evil.txt":     "payload",
		})
		archive, err := zip.OpenReader(path)
		if err != nil {
			t.Fatal(err)
		}
		defer archive.Close()
		dest := filepath.Join(dir, "site")
		err = Extract(archive, dest)
		if err == nil {
			t.Fatal("expected error on zip slip, got nil")
		}
	})
}
