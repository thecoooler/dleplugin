package stack

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedDatabaseCredentials(t *testing.T) {
	first, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range []*Stack{first, second} {
		if s.Env["BIND_ADDRESS"] != "127.0.0.1" {
			t.Fatal("HTTP does not default to loopback")
		}
		if s.Env["DB_BIND_ADDRESS"] != "127.0.0.1" {
			t.Fatal("database does not default to loopback")
		}
		for _, key := range []string{"DB_ROOT_PASSWORD", "DB_PASSWORD"} {
			password := s.Env[key]
			decoded, err := hex.DecodeString(password)
			if err != nil || len(decoded) != 24 || seen[password] {
				t.Fatalf("%s must be independently generated from 24 random bytes", key)
			}
			seen[password] = true
		}
	}
	reopened, err := Open(first.Dir)
	if err != nil || reopened.Env["DB_ROOT_PASSWORD"] != first.Env["DB_ROOT_PASSWORD"] || reopened.Env["DB_PASSWORD"] != first.Env["DB_PASSWORD"] {
		t.Fatal("reopening changed database credentials", err)
	}
}

func TestExistingDataRequiresOriginalEnv(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "data", "mariadb")
	if err := os.MkdirAll(db, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(db, "existing"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "restore the original .env") {
		t.Fatal("missing .env with existing database must fail", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Fatal("created .env for existing database")
	}
	old := "BIND_ADDRESS=0.0.0.0\nDB_ROOT_PASSWORD=old\nDB_PASSWORD=old-user\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil || string(got) != old {
		t.Fatal("existing .env changed", err)
	}
}

func TestExplicitDatabaseDefaults(t *testing.T) {
	t.Setenv("DLEPLUGIN_HOSTS_DB_ROOT_PASSWORD", "custom-root")
	t.Setenv("DLEPLUGIN_HOSTS_DB_PASSWORD", "custom-user")
	t.Setenv("DLEPLUGIN_HOSTS_DB_BIND_ADDRESS", "192.0.2.10")
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.Env["DB_ROOT_PASSWORD"] != "custom-root" || s.Env["DB_PASSWORD"] != "custom-user" || s.Env["DB_BIND_ADDRESS"] != "192.0.2.10" {
		t.Fatal("explicit settings were not preserved")
	}
}

func TestComposeDatabaseBinding(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker compose not installed")
	}
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, env, want string
	}{
		{"legacy env", "BIND_ADDRESS=0.0.0.0\n", "127.0.0.1"},
		{"independent address", "BIND_ADDRESS=127.0.0.1\nDB_BIND_ADDRESS=192.0.2.10\n", "192.0.2.10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(s.Dir, ".env"), []byte("COMPOSE_PROJECT_NAME=test\n"+tc.env), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("docker", "compose", "--project-directory", s.Dir, "-f", filepath.Join(s.Dir, "docker-compose.yml"), "config", "--format", "json")
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("compose config: %v", err)
			}
			var config struct {
				Services map[string]struct {
					Ports []struct {
						HostIP    string `json:"host_ip"`
						Published string `json:"published"`
					} `json:"ports"`
				} `json:"services"`
			}
			if err := json.Unmarshal(output, &config); err != nil {
				t.Fatal(err)
			}
			ports := config.Services["db"].Ports
			if len(ports) != 1 || ports[0].HostIP != tc.want || ports[0].Published != "3307" {
				t.Fatalf("unexpected database port binding: %+v", ports)
			}
		})
	}
}

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
	t.Setenv("DLEPLUGIN_HOSTS_HTTP_PORT", "8081")
	t.Setenv("DLEPLUGIN_HOSTS_COMPOSE_PROJECT_NAME", "dle-test")
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.HTTPPort() != "8081" || s.Env["COMPOSE_PROJECT_NAME"] != "dle-test" || s.Env["DB_PORT"] != "3307" {
		t.Fatal(".env did not retain explicit or default non-secret settings")
	}
}

func TestDirFromEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DLEPLUGIN_HOSTS_DIR", dir)
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
	t.Setenv("DLEPLUGIN_HOSTS_DB_ROOT_PASSWORD", password)
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

	t.Setenv("DLEPLUGIN_HOSTS_DB_ROOT_PASSWORD", "it's")
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("value with a single quote accepted")
	}
}
