package stack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsure(t *testing.T) {
	dir := t.TempDir()
	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	updated, err := ensure(dir)
	if err != nil || !updated {
		t.Fatalf("first run: updated=%v err=%v", updated, err)
	}
	for _, rel := range []string{"docker-compose.yml", "config/apache/sites/05-fallback.conf", "config/apache/sites/90-auto.conf", "docker/php/Dockerfile", "www/localhost/index.php", ".env"} {
		read(rel)
	}
	for _, d := range []string{"data", "logs/apache"} {
		if info, err := os.Stat(filepath.Join(dir, d)); err != nil || !info.IsDir() {
			t.Fatalf("missing directory %s", d)
		}
	}
	if !strings.Contains(read(".env"), "HTTP_PORT=80") {
		t.Fatal(".env without HTTP_PORT")
	}

	// User files are kept, a removed service file is restored.
	os.WriteFile(filepath.Join(dir, ".env"), []byte("HTTP_PORT=8080\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "www/localhost/index.php"), []byte("mine"), 0o644)
	os.Remove(filepath.Join(dir, "docker-compose.yml"))
	os.RemoveAll(filepath.Join(dir, "www/localhost"))
	if updated, err := ensure(dir); err != nil || updated {
		t.Fatalf("second run: updated=%v err=%v", updated, err)
	}
	read("docker-compose.yml")
	if read(".env") != "HTTP_PORT=8080\n" {
		t.Fatal(".env overwritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "www/localhost")); err == nil {
		t.Fatal("www/ seed restored after removal")
	}

	// New binary version: service files are overwritten, files it no longer
	// has are removed, files outside the manifest stay.
	compose := read("docker-compose.yml")
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(dir, "config/old.conf"), nil, 0o644)
	// An older stack has no safe fallback; updates must install it without touching sites.
	os.Remove(filepath.Join(dir, "config/apache/sites/05-fallback.conf"))
	os.WriteFile(filepath.Join(dir, "config/apache/sites/60-host-shop.conf"), nil, 0o644)
	_, files, err := readStackManifest(filepath.Join(dir, stackManifest))
	if err != nil {
		t.Fatal(err)
	}
	manifest := "oldhash\n" + strings.Join(append(files, "config/old.conf", "../outside"), "\n") + "\n"
	os.WriteFile(filepath.Join(dir, stackManifest), []byte(manifest), 0o644)
	if updated, err := ensure(dir); err != nil || !updated {
		t.Fatalf("update: updated=%v err=%v", updated, err)
	}
	if read("docker-compose.yml") != compose {
		t.Fatal("docker-compose.yml not updated")
	}
	if _, err := os.Stat(filepath.Join(dir, "config/old.conf")); err == nil {
		t.Fatal("stale file not removed")
	}
	read("config/apache/sites/60-host-shop.conf")
	read("config/apache/sites/05-fallback.conf")
}

func TestPending(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pending := func() bool {
		t.Helper()
		_, p, err := s.Pending()
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if !pending() {
		t.Fatal("new stack not marked pending")
	}
	hash, _, err := s.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkApplied(hash); err != nil {
		t.Fatal(err)
	}
	if pending() {
		t.Fatal("stack still pending after MarkApplied")
	}
	// up ran with the service files of the previous binary version.
	os.WriteFile(filepath.Join(s.Dir, stackApplied), []byte("oldhash\n"), 0o644)
	if !pending() {
		t.Fatal("updated stack not marked pending")
	}
}

func TestDotEnvDefaultsFromEnvironment(t *testing.T) {
	t.Setenv("DLEHOST_HTTP_PORT", "8081")
	t.Setenv("DLEHOST_COMPOSE_PROJECT_NAME", "dle-test")
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.HTTPPort() != "8081" || s.Env["COMPOSE_PROJECT_NAME"] != "dle-test" || s.Env["DB_PORT"] != "3307" {
		t.Fatalf(".env: %v", s.Env)
	}
}

func TestDirFromEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DLEHOST_DIR", dir)
	s, err := Open(filepath.Join(dir, "ignored"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Dir != dir {
		t.Fatalf("Dir = %s, want %s", s.Dir, dir)
	}
}

func TestDotEnvSecrets(t *testing.T) {
	const password = `p@ss ${UNSET} #x`
	t.Setenv("DLEHOST_DB_ROOT_PASSWORD", password)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Env["DB_ROOT_PASSWORD"]; got != password {
		t.Fatalf("DB_ROOT_PASSWORD = %q", got)
	}
	info, err := os.Stat(filepath.Join(s.Dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf(".env: mode %o", perm)
	}

	t.Setenv("DLEHOST_DB_ROOT_PASSWORD", "it's")
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("value with a single quote accepted")
	}
}
