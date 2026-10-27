package stack

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The Docker stack files are embedded in the binary and written to the stack
// directory on first run. Files from files/www/ are seeds: written once and
// then owned by the user. The rest are service files: overwritten when the
// binary is updated, and removed when a new version no longer has them.
//
//go:embed files
var stackFS embed.FS

const (
	stackRoot = "files"
	// Only the exact former seed is replaced; user-edited pages remain untouched.
	unsafeLocalhostSeedHash = "37664b57e9c4d36ee1a55fcd7b508a290bfefbecb25d5a598c8ab5a43a734551"
	// stackManifest holds the hash of the service files and their list.
	stackManifest = ".dleplugin-hosts-stack"
	// stackApplied holds the hash of the service files the last hosts up
	// ran with.
	stackApplied = ".dleplugin-hosts-applied"
)

// DefaultDir is $XDG_DATA_HOME/dleplugin/hosts, by default ~/.local/share/dleplugin/hosts.
func DefaultDir() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "dleplugin", "hosts"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "dleplugin", "hosts"), nil
}

type stackFile struct {
	rel  string // path relative to the stack directory, with "/"
	data []byte
	seed bool // seed in www/
}

func embeddedStack() ([]stackFile, error) {
	var files []stackFile
	err := fs.WalkDir(stackFS, stackRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := stackFS.ReadFile(p)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, stackRoot+"/")
		files = append(files, stackFile{rel: rel, data: data, seed: strings.HasPrefix(rel, "www/")})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, err
}

// ensure creates or updates the stack in dir and returns true when the
// service files were (re)written.
func ensure(dir string) (bool, error) {
	files, err := embeddedStack()
	if err != nil {
		return false, err
	}
	sum := sha256.New()
	var managed []string
	for _, f := range files {
		if !f.seed {
			fmt.Fprintf(sum, "%s\x00%d\x00", f.rel, len(f.data))
			sum.Write(f.data)
			managed = append(managed, f.rel)
		}
	}
	hash := hex.EncodeToString(sum.Sum(nil))

	for _, d := range []string{"www", "data", "logs/apache"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o755); err != nil {
			return false, err
		}
	}
	if err := writeDefaultDotEnv(filepath.Join(dir, ".env")); err != nil {
		return false, err
	}

	manifestPath := filepath.Join(dir, stackManifest)
	prevHash, prevFiles, err := readStackManifest(manifestPath)
	if err != nil {
		return false, err
	}
	fresh := prevHash == ""
	update := prevHash != hash
	for _, f := range files {
		target := filepath.Join(dir, filepath.FromSlash(f.rel))
		if f.rel == "www/localhost/index.php" {
			if err := replaceUnsafeLocalhostSeed(target, f.data); err != nil {
				return false, err
			}
		}
		_, statErr := os.Stat(target)
		exists := statErr == nil
		if f.seed && (!fresh || exists) || !f.seed && !update && exists {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(target, f.data, 0o644); err != nil {
			return false, err
		}
	}
	if !update {
		return false, nil
	}
	for _, rel := range prevFiles {
		if !slices.Contains(managed, rel) {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		}
	}
	manifest := hash + "\n" + strings.Join(managed, "\n") + "\n"
	return true, os.WriteFile(manifestPath, []byte(manifest), 0o644)
}

func replaceUnsafeLocalhostSeed(path string, replacement []byte) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != unsafeLocalhostSeedHash {
		return nil
	}
	return os.WriteFile(path, replacement, info.Mode().Perm())
}

// Pending reports that the service files changed after the last hosts up.
// Compose does not notice edits of php.ini, my.cnf and vhosts by itself: the
// containers must be recreated. hash is passed to MarkApplied after up so
// that a version updated during up is not marked as applied.
func (s *Stack) Pending() (hash string, pending bool, err error) {
	hash, _, err = readStackManifest(filepath.Join(s.Dir, stackManifest))
	if err != nil {
		return "", false, err
	}
	applied, err := os.ReadFile(filepath.Join(s.Dir, stackApplied))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	return hash, strings.TrimSpace(string(applied)) != hash, nil
}

// MarkApplied records that the service files with hash were applied.
func (s *Stack) MarkApplied(hash string) error {
	return os.WriteFile(filepath.Join(s.Dir, stackApplied), []byte(hash+"\n"), 0o644)
}

func readStackManifest(p string) (hash string, files []string, err error) {
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, rel := range lines[1:] {
		// keep the manifest from pointing outside the stack
		if rel = strings.TrimSpace(rel); filepath.IsLocal(filepath.FromSlash(rel)) {
			files = append(files, rel)
		}
	}
	return strings.TrimSpace(lines[0]), files, nil
}

// defaultDotEnv lists the keys of a new .env in order; "" is an empty line, "#…" a comment.
var defaultDotEnv = []struct{ key, value string }{
	{"#", "Created by dleplugin hosts; not overwritten on updates"},
	{"#", "Defaults of a new .env can be set with DLEPLUGIN_HOSTS_<KEY> variables"},
	{"#", "docker compose project name and container name prefix"},
	{"COMPOSE_PROJECT_NAME", "dleplugin-hosts"},
	{"MARIADB_VERSION", "11.4"},
	{"", ""},
	{"#", "Apache bind address"},
	{"BIND_ADDRESS", "127.0.0.1"},
	{"HTTP_PORT", "80"},
	{"#", "MariaDB bind address, independent of Apache"},
	{"DB_BIND_ADDRESS", "127.0.0.1"},
	{"DB_PORT", "3307"},
	{"", ""},
	{"DB_ROOT_PASSWORD", ""},
	{"DB_NAME", "dle"},
	{"DB_USER", "dle"},
	{"DB_PASSWORD", ""},
	{"", ""},
	{"#", "Owner of the www/ files inside the containers"},
	{"UID", ""},
	{"GID", ""},
}

// writeDefaultDotEnv creates the stack .env when missing. UID/GID are the
// current user's, so that the www/ files belong to them.
func writeDefaultDotEnv(p string) error {
	if _, err := os.Stat(p); err == nil || !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Lstat(p); err == nil {
		return errors.New(".env is a dangling symbolic link; restore the original file")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// MariaDB initialization variables do not change an existing data directory.
	// Generating a replacement .env would silently create unusable credentials.
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(p), "data", "mariadb"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(entries) != 0 {
		return errors.New(".env is missing but MariaDB data exists; restore the original .env before starting the stack")
	}
	registry, err := os.ReadFile(filepath.Join(filepath.Dir(p), "data", "hosts.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		var hosts []json.RawMessage
		if json.Unmarshal(registry, &hosts) != nil || len(hosts) != 0 {
			return errors.New(".env is missing but the hosts registry exists; restore the original .env and MariaDB data")
		}
	}
	uid, gid := os.Getuid(), os.Getgid()
	// Windows (-1) or root: php-fpm does not run a pool as UID/GID 0
	if uid <= 0 || gid <= 0 {
		uid, gid = 1000, 1000
	}
	var b strings.Builder
	for _, kv := range defaultDotEnv {
		switch kv.key {
		case "":
			b.WriteString("\n")
			continue
		case "#":
			b.WriteString("# " + kv.value + "\n")
			continue
		}
		value := kv.value
		switch kv.key {
		case "UID":
			value = strconv.Itoa(uid)
		case "GID":
			value = strconv.Itoa(gid)
		}
		if v, ok := os.LookupEnv("DLEPLUGIN_HOSTS_" + kv.key); ok {
			if v == "" && (kv.key == "DB_ROOT_PASSWORD" || kv.key == "DB_PASSWORD") {
				return fmt.Errorf("DLEPLUGIN_HOSTS_%s must not be empty", kv.key)
			}
			if value, err = dotEnvValue(v); err != nil {
				return fmt.Errorf("DLEPLUGIN_HOSTS_%s: %w", kv.key, err)
			}
		} else if kv.key == "DB_ROOT_PASSWORD" || kv.key == "DB_PASSWORD" {
			if value, err = NewPassword(); err != nil {
				return fmt.Errorf("generate %s: %w", kv.key, err)
			}
		}
		b.WriteString(kv.key + "=" + value + "\n")
	}
	// Publish only a fully written file, without replacing a concurrent creator.
	f, err := os.CreateTemp(filepath.Dir(p), ".env-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(b.String()); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err := os.Link(f.Name(), p); err != nil {
		if errors.Is(err, os.ErrExist) {
			// The competing creator must have published a usable file.
			_, err = os.Stat(p)
		}
		return err
	}
	return nil
}

// NewPassword returns 192 bits of cryptographically random data as hex.
func NewPassword() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

var plainDotEnvValue = regexp.MustCompile(`^[A-Za-z0-9_./:@+,-]*$`)

// dotEnvValue quotes a value so that docker compose reads it literally:
// without ${…} interpolation or truncation at #.
func dotEnvValue(v string) (string, error) {
	if plainDotEnvValue.MatchString(v) {
		return v, nil
	}
	if strings.ContainsAny(v, "'\n\r") {
		return "", errors.New("the value must not contain a single quote or a line break")
	}
	return "'" + v + "'", nil
}
