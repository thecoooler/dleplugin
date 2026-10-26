// Package stack manages the hosts Docker stack: embedded files, .env and docker compose calls.
package stack

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/thecoooler/dleplugin/internal/process"
)

// Stack is a deployed Docker stack: www/, config/apache/sites/, docker-compose.yml.
type Stack struct {
	Dir            string
	BaseDomain     string
	ExternalOrigin string
	Env            map[string]string
	// Updated reports that the stack service files were just created or updated.
	Updated bool
	// Exec runs docker; nil uses a runner writing to os.Stdout and os.Stderr.
	Exec *process.Runner
}

// Open deploys the embedded stack into dir and reads its .env. The directory
// is DLEPLUGIN_HOSTS_DIR, then dir, otherwise DefaultDir.
func Open(dir string) (*Stack, error) {
	if env := os.Getenv("DLEPLUGIN_HOSTS_DIR"); env != "" {
		dir = env
	}
	if dir == "" {
		var err error
		if dir, err = DefaultDir(); err != nil {
			return nil, err
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	updated, err := ensure(dir)
	if err != nil {
		return nil, fmt.Errorf("prepare the stack in %s: %w", dir, err)
	}
	env, err := readDotEnv(filepath.Join(dir, ".env"))
	if err != nil {
		return nil, err
	}
	return &Stack{Dir: dir, Env: env, Updated: updated}, nil
}

func readDotEnv(path string) (map[string]string, error) {
	env := map[string]string{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return env, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		env[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
	}
	return env, sc.Err()
}

// unquote strips matching "…" or '…' quotes around a value.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// Paths inside the stack.
func (s *Stack) WWWDir() string       { return filepath.Join(s.Dir, "www") }
func (s *Stack) SitesDir() string     { return filepath.Join(s.Dir, "config", "apache", "sites") }
func (s *Stack) RegistryPath() string { return filepath.Join(s.Dir, "data", "hosts.json") }

// ContainerWWWDir is the mount point of www/ in the apache and php containers.
const ContainerWWWDir = "/var/www"

func (s *Stack) HostDir(name string) string { return filepath.Join(s.WWWDir(), name) }

// The protect login form and its sessions inside the apache and php containers.
const (
	ContainerGateApp = "/opt/dleplugin/hosts/gate"
	ContainerGateDir = "/var/lib/dleplugin/hosts/gate"
)

// StagingDir is where files of new hosts are prepared outside www/.
func (s *Stack) StagingDir() string { return filepath.Join(s.Dir, "data", "staging") }

// GateDir is the login form session directory of a host (data/gate/<name>).
func (s *Stack) GateDir(name string) string { return filepath.Join(s.Dir, "data", "gate", name) }

// Domain returns the domain of a new managed host.
func (s *Stack) Domain(name string) string {
	base := s.BaseDomain
	if base == "" {
		base = "localhost"
	}
	return name + "." + base
}

// HostURL returns the public URL of a new host. The external origin does not depend on HTTP_PORT.
func (s *Stack) HostURL(domain string) string {
	if s.ExternalOrigin != "" {
		u, _ := url.Parse(s.ExternalOrigin) // checked by Config.ValidateRouting
		authority := domain
		if u.Port() != "" {
			authority = net.JoinHostPort(domain, u.Port())
		}
		return u.Scheme + "://" + authority + "/"
	}
	return s.LocalHostURL(domain)
}

// LocalHostURL returns the direct Apache HTTP address, also used for old hosts.
func (s *Stack) LocalHostURL(domain string) string {
	port := s.HTTPPort()
	if port == "80" {
		return "http://" + domain + "/"
	}
	return "http://" + domain + ":" + port + "/"
}

// Compose runs docker compose; output is shown only on error.
func (s *Stack) Compose(stdin io.Reader, args ...string) error {
	c := s.composeCmd(args...)
	c.Stdin = stdin
	res, err := s.runner().Run(context.Background(), c)
	var exit *process.ExitError
	if errors.As(err, &exit) {
		return fmt.Errorf("docker compose %s: %s", args[0], errorLines(string(res.Stderr), err))
	}
	return err
}

func (s *Stack) composeCmd(args ...string) process.Command {
	return process.Command{Name: "docker", Args: append([]string{"compose",
		"--project-directory", s.Dir,
		"-f", filepath.Join(s.Dir, "docker-compose.yml")}, args...), Dir: s.Dir}
}

func (s *Stack) runner() *process.Runner {
	if s.Exec == nil {
		s.Exec = process.NewRunner(process.Options{Out: os.Stdout, Err: os.Stderr})
	}
	return s.Exec
}

// ComposeAttached runs docker compose with stdin and the runner's output
// streams. A terminal Ctrl-C reaches docker compose directly, so on ctx
// cancellation it first gets time to stop its containers gracefully.
// A non-zero exit returns *process.ExitError.
func (s *Stack) ComposeAttached(ctx context.Context, stdin io.Reader, args ...string) error {
	c := s.composeCmd(args...)
	c.Stdin, c.Stream, c.Graceful = stdin, true, true
	_, err := s.runner().Run(ctx, c)
	return err
}

// errorLines keeps only the error lines of docker compose output
// (without progress such as "Container ... Running").
func errorLines(output string, err error) string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(strings.ToLower(line), "error") {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	if len(lines) == 0 {
		if msg := strings.TrimSpace(output); msg != "" {
			return msg
		}
		return err.Error()
	}
	return strings.Join(lines, "\n")
}

// MariaDB runs the mariadb client as root inside the db container.
// The root password comes from the container environment.
func (s *Stack) MariaDB(stdin io.Reader, args ...string) error {
	script := `MYSQL_PWD="$MARIADB_ROOT_PASSWORD" exec mariadb -uroot "$@"`
	return s.Compose(stdin, append([]string{"exec", "-T", "db", "sh", "-c", script, "--"}, args...)...)
}

// SQL runs a query as root without selecting a database.
func (s *Stack) SQL(query string) error {
	return s.MariaDB(strings.NewReader(query))
}

// ReloadApache checks the Apache config and reloads it gracefully.
func (s *Stack) ReloadApache() error {
	if err := s.Compose(nil, "exec", "-T", "apache", "apache2ctl", "configtest"); err != nil {
		return err
	}
	return s.Compose(nil, "exec", "-T", "apache", "apache2ctl", "graceful")
}

// CheckGate checks as www-data that the running apache and php<ver> see the
// login form and the session directory of host name, and that php can write
// to it. Without hosts up after a binary update the mounts do not exist yet.
func (s *Stack) CheckGate(phpVersion, name string) error {
	dir := ContainerGateDir + "/" + name
	checks := map[string]string{
		"apache": "test -r " + ContainerGateApp + "/gate.php && test -x " + dir,
		"php" + strings.ReplaceAll(phpVersion, ".", ""): "test -r " + ContainerGateApp + "/gate.php && test -r /opt/dleplugin/hosts/protect/passwords.json && test -w " + dir + " && test -x " + dir,
	}
	for _, service := range []string{"apache", "php" + strings.ReplaceAll(phpVersion, ".", "")} {
		if err := s.Compose(nil, "exec", "-T", "-u", "www-data", service, "sh", "-c", checks[service]); err != nil {
			return fmt.Errorf("container %s does not see the login form or the session directory %s (run dleplugin hosts up; data/gate must be owned by the UID from .env): %w", service, dir, err)
		}
	}
	return nil
}

// SetDotEnv changes (or adds) KEY=VALUE in .env, keeping the other lines.
func (s *Stack) SetDotEnv(key, value string) error {
	path := filepath.Join(s.Dir, ".env")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	found := false
	for i, line := range lines {
		if k, _, ok := strings.Cut(strings.TrimSpace(line), "="); ok && strings.TrimSpace(k) == key {
			lines[i] = key + "=" + value
			found = true
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	s.Env[key] = value
	return nil
}

// HTTPPort returns the Apache port from .env.
func (s *Stack) HTTPPort() string {
	if port := s.Env["HTTP_PORT"]; port != "" {
		return port
	}
	return "80" // default in docker-compose.yml
}
