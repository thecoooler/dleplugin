// Package stack — docker-стек dlehost: встроенные файлы, .env и вызовы docker compose.
package stack

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Stack — развёрнутый docker-стек: www/, config/apache/sites/, docker-compose.yml.
type Stack struct {
	Dir            string
	BaseDomain     string
	ExternalOrigin string
	Env            map[string]string
	// Updated — служебные файлы стека только что созданы или обновлены.
	Updated bool
}

// Open разворачивает встроенный стек в dir и читает его .env. Каталог
// берётся из DLEHOST_DIR, затем dir, иначе DefaultDir.
func Open(dir string) (*Stack, error) {
	if env := os.Getenv("DLEHOST_DIR"); env != "" {
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
		return nil, fmt.Errorf("подготовка стека в %s: %w", dir, err)
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

// unquote снимает парные кавычки "…" или '…' вокруг значения.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// Пути внутри стека.
func (s *Stack) WWWDir() string       { return filepath.Join(s.Dir, "www") }
func (s *Stack) SitesDir() string     { return filepath.Join(s.Dir, "config", "apache", "sites") }
func (s *Stack) RegistryPath() string { return filepath.Join(s.Dir, "data", "hosts.json") }

// ContainerWWWDir — точка монтирования www/ в контейнерах apache и php.
const ContainerWWWDir = "/var/www"

func (s *Stack) HostDir(name string) string { return filepath.Join(s.WWWDir(), name) }

// Форма входа dlehost protect и её сессии внутри контейнеров apache и php.
const (
	ContainerGateApp = "/opt/dlehost/gate"
	ContainerGateDir = "/var/lib/dlehost/gate"
)

// StagingDir — каталог подготовки файлов новых хостов вне www/.
func (s *Stack) StagingDir() string { return filepath.Join(s.Dir, "data", "staging") }

// GateDir — каталог сессий формы входа хоста (data/gate/<имя>).
func (s *Stack) GateDir(name string) string { return filepath.Join(s.Dir, "data", "gate", name) }

// Domain — домен нового управляемого хоста.
func (s *Stack) Domain(name string) string {
	base := s.BaseDomain
	if base == "" {
		base = "localhost"
	}
	return name + "." + base
}

// HostURL — публичный URL нового хоста. Внешний origin не зависит от HTTP_PORT.
func (s *Stack) HostURL(domain string) string {
	if s.ExternalOrigin != "" {
		u, _ := url.Parse(s.ExternalOrigin) // проверено Config.ValidateRouting
		authority := domain
		if u.Port() != "" {
			authority = net.JoinHostPort(domain, u.Port())
		}
		return u.Scheme + "://" + authority + "/"
	}
	return s.LocalHostURL(domain)
}

// LocalHostURL — прямой HTTP-адрес apache, в том числе для старых хостов.
func (s *Stack) LocalHostURL(domain string) string {
	port := s.HTTPPort()
	if port == "80" {
		return "http://" + domain + "/"
	}
	return "http://" + domain + ":" + port + "/"
}

// Compose выполняет docker compose, вывод показывается только при ошибке.
func (s *Stack) Compose(stdin io.Reader, args ...string) error {
	cmd := s.composeCmd(args...)
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose %s: %s", args[0], errorLines(stderr.String(), err))
	}
	return nil
}

func (s *Stack) composeCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("docker", append([]string{"compose",
		"--project-directory", s.Dir,
		"-f", filepath.Join(s.Dir, "docker-compose.yml")}, args...)...)
	cmd.Dir = s.Dir
	return cmd
}

// ComposeAttached запускает docker compose с выводом в терминал.
func (s *Stack) ComposeAttached(args ...string) error {
	cmd := s.composeCmd(args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// errorLines оставляет из вывода docker compose только строки с ошибками
// (без прогресса вида "Container ... Running").
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

// MariaDB выполняет клиент mariadb от root внутри контейнера db.
// Пароль root берётся из окружения контейнера.
func (s *Stack) MariaDB(stdin io.Reader, args ...string) error {
	script := `MYSQL_PWD="$MARIADB_ROOT_PASSWORD" exec mariadb -uroot "$@"`
	return s.Compose(stdin, append([]string{"exec", "-T", "db", "sh", "-c", script, "--"}, args...)...)
}

// SQL выполняет запрос от root без выбора базы.
func (s *Stack) SQL(query string) error {
	return s.MariaDB(strings.NewReader(query))
}

// ReloadApache проверяет конфиг apache и плавно перезагружает его.
func (s *Stack) ReloadApache() error {
	if err := s.Compose(nil, "exec", "-T", "apache", "apache2ctl", "configtest"); err != nil {
		return err
	}
	return s.Compose(nil, "exec", "-T", "apache", "apache2ctl", "graceful")
}

// CheckGate проверяет от имени www-data, что запущенные apache и php<ver>
// видят форму входа и каталог сессий хоста name, а php может в него писать.
// Без dlehost up после обновления бинарника монтирований ещё нет.
func (s *Stack) CheckGate(phpVersion, name string) error {
	dir := ContainerGateDir + "/" + name
	checks := map[string]string{
		"apache": "test -r " + ContainerGateApp + "/gate.php && test -x " + dir,
		"php" + strings.ReplaceAll(phpVersion, ".", ""): "test -r " + ContainerGateApp + "/gate.php && test -r /opt/dlehost/protect/passwords.json && test -w " + dir + " && test -x " + dir,
	}
	for _, service := range []string{"apache", "php" + strings.ReplaceAll(phpVersion, ".", "")} {
		if err := s.Compose(nil, "exec", "-T", "-u", "www-data", service, "sh", "-c", checks[service]); err != nil {
			return fmt.Errorf("контейнер %s не видит форму входа или каталог сессий %s (выполните dlehost up; владелец data/gate — UID из .env): %w", service, dir, err)
		}
	}
	return nil
}

// SetDotEnv меняет (или добавляет) KEY=VALUE в .env, сохраняя остальные строки.
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

// HTTPPort — порт apache из .env.
func (s *Stack) HTTPPort() string {
	if port := s.Env["HTTP_PORT"]; port != "" {
		return port
	}
	return "80" // значение по умолчанию в docker-compose.yml
}
