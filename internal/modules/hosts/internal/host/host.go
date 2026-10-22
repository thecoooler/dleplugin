// Package host — хосты DLE в стеке: создание, удаление, список, порт.
package host

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/dist"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/installer"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

// Host — запись реестра хостов (data/hosts.json в каталоге стека).
type Host struct {
	gateKey string // отпечаток действующего хеша, только для генерации vhost

	Name           string    `json:"name"`
	DomainName     string    `json:"domain,omitempty"`
	ExternalOrigin string    `json:"external_origin,omitempty"`
	DLE            string    `json:"dle"`
	PHP            string    `json:"php"`
	Database       string    `json:"database"`
	CreatedAt      time.Time `json:"created_at"`
	// Protect — режим формы входа: "" (default, по глобальной настройке), on, off.
	Protect string `json:"protect,omitempty"`
}

func (h Host) Domain() string {
	if h.DomainName != "" {
		return h.DomainName
	}
	if h.Name == "localhost" {
		return h.Name
	}
	return h.Name + ".localhost"
}

// URL использует сохранённый origin; старые записи сохраняют локальную адресацию.
func (h Host) URL(s *stack.Stack) string {
	if h.ExternalOrigin != "" {
		return h.ExternalOrigin + "/"
	}
	return s.LocalHostURL(h.Domain())
}

var hostNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,40}[a-z0-9])?$`)

func validateHostName(name string) error {
	if !hostNameRe.MatchString(name) {
		return fmt.Errorf("некорректное имя %q: латиница в нижнем регистре, цифры и дефис, до 42 символов", name)
	}
	if name == "localhost" || name == "any" || strings.HasPrefix(name, "php") && isDigits(name[3:]) {
		return fmt.Errorf("имя %q зарезервировано автохостами apache", name)
	}
	return nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func loadRegistry(s *stack.Stack) (map[string]Host, error) {
	hosts := map[string]Host{}
	data, err := os.ReadFile(s.RegistryPath())
	if errors.Is(err, os.ErrNotExist) {
		return hosts, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Host
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", s.RegistryPath(), err)
	}
	for _, h := range list {
		// Неизвестный режим нельзя молча считать default: защита могла бы выключиться.
		if _, err := ParseProtect(h.Protect); err != nil || h.Protect == ProtectDefault {
			return nil, fmt.Errorf("%s: хост %s: недопустимый protect %q (on, off или без поля)", s.RegistryPath(), h.Name, h.Protect)
		}
		hosts[h.Name] = h
	}
	return hosts, nil
}

func saveRegistry(s *stack.Stack, hosts map[string]Host) error {
	list := make([]Host, 0, len(hosts))
	for _, h := range hosts {
		list = append(list, h)
	}
	sortHosts(list)
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.RegistryPath()), 0o755); err != nil {
		return err
	}
	tmp := s.RegistryPath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.RegistryPath())
}

func sortHosts(list []Host) {
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
}

// Row — строка списка: управляемый хост из реестра или «чужой» каталог в www/.
type Row struct {
	Host
	Managed bool
}

// List — хосты из реестра и остальные каталоги в www/.
func List(s *stack.Stack) ([]Row, error) {
	hosts, err := loadRegistry(s)
	if err != nil {
		return nil, err
	}
	var rows []Row
	for _, h := range hosts {
		rows = append(rows, Row{Host: h, Managed: true})
	}
	entries, err := os.ReadDir(s.WWWDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		if _, ok := hosts[e.Name()]; ok || !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		row := Row{Host: Host{Name: e.Name()}}
		if info, err := e.Info(); err == nil {
			row.CreatedAt = info.ModTime()
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

// Path — каталог файлов хоста <name> на машине или, с container, внутри
// контейнеров. Подходит и для каталогов www/, созданных не через dlehost.
func Path(s *stack.Stack, name string, container bool) (string, error) {
	if !hostNameRe.MatchString(name) {
		return "", fmt.Errorf("некорректное имя %q", name)
	}
	dir := s.HostDir(name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("хост %s не найден: нет каталога %s", name, dir)
	}
	if container {
		return stack.ContainerWWWDir + "/" + name, nil
	}
	return dir, nil
}

func vhostPath(s *stack.Stack, name string) string {
	return filepath.Join(s.SitesDir(), "60-host-"+name+".conf")
}

// Явный vhost с фиксированной версией PHP. Файлы 60-* подключаются раньше
// 90-auto.conf, поэтому домен обслуживается выбранной версией. protected
// закрывает хост формой входа, включая его адреса автохостов; ttl — срок
// сессии формы (0 — без серверного истечения).
func vhostConfig(h Host, protected bool, ttl time.Duration) string {
	ver := strings.ReplaceAll(h.PHP, ".", "")
	forwarded := ""
	if strings.HasPrefix(h.ExternalOrigin, "https://") {
		forwarded = `    # Только за доверенным TLS-terminating proxy; backend не должен быть публичным.
    ProxyFCGISetEnvIf "req('X-Forwarded-Proto') == 'https'" HTTPS "on"
    ProxyFCGISetEnvIf "req('X-Forwarded-Proto') == 'https'" REQUEST_SCHEME "https"
`
	}
	aliases, gate := "", ""
	if protected {
		aliases, gate = gateAliases(h), gateConfig(h, ttl)
	}
	return fmt.Sprintf(`# Создано dlehost — не редактируйте вручную (DLE %s, PHP %s)
<VirtualHost *:80>
    ServerName %s
%s    DocumentRoot %s/%s
    Use PHP %s
%s%s    ErrorLog  ${APACHE_LOG_DIR}/%s-error.log
    CustomLog ${APACHE_LOG_DIR}/%s-access.log combined
</VirtualHost>
`, h.DLE, h.PHP, h.Domain(), aliases, stack.ContainerWWWDir, h.Name, ver, forwarded, gate, h.Name, h.Name)
}

// AddOptions — параметры нового хоста.
type AddOptions struct {
	Context     context.Context
	Name        string
	DLE         string
	DistArchive string // прямой путь к ZIP-архиву дистрибутива DLE
	PHP         string
	DefaultPHP  string   // предпочтение конфига; используется только при совместимости с DLE
	PHPVersions []string // допустимые версии PHP
	DistsDir    string
	Admin       Credentials
	Protect     string          // default, on или off
	Settings    ProtectSettings // глобальные настройки формы входа
}

// AddResult — созданный хост и его адрес.
type AddResult struct {
	Host Host
	URL  string
}

// Add создаёт хост из дистрибутива; при ошибке всё созданное откатывается.
func Add(s *stack.Stack, opts AddOptions, log func(string)) (*AddResult, error) {
	name, dleVersion, phpVersion := opts.Name, opts.DLE, opts.PHP
	if err := opts.Admin.Validate(); err != nil {
		return nil, err
	}
	if err := validateHostName(name); err != nil {
		return nil, err
	}
	protect, err := ParseProtect(opts.Protect)
	if err != nil {
		return nil, err
	}
	passwords, err := loadPasswords(s)
	if err != nil {
		return nil, err
	}
	if (Host{Protect: protect}).Protected(opts.Settings.Global) && passwords.effective(name) == "" {
		return nil, missingPassword(name)
	}
	hosts, err := loadRegistry(s)
	if err != nil {
		return nil, err
	}
	if _, ok := hosts[name]; ok {
		return nil, fmt.Errorf("хост %s уже существует", name)
	}
	hostDir := s.HostDir(name)
	if _, err := os.Stat(hostDir); err == nil {
		return nil, fmt.Errorf("каталог %s уже существует", hostDir)
	}

	var archivePath string
	if opts.DistArchive != "" {
		archivePath = opts.DistArchive
	} else {
		if opts.DistsDir == "" {
			return nil, dist.ErrNoDir
		}
		p, err := dist.Resolve(opts.DistsDir, dleVersion)
		if err != nil {
			return nil, err
		}
		archivePath = p
	}

	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("дистрибутив DLE %s: %w (см. dlehost versions)", dleVersion, err)
	}
	defer archive.Close()

	distInfo, err := dist.Inspect(&archive.Reader)
	if err != nil {
		return nil, fmt.Errorf("дистрибутив %s: %w", filepath.Base(archivePath), err)
	}
	if dleVersion != "" && distInfo.Version != dleVersion {
		return nil, fmt.Errorf("в архиве %s обнаружена версия DLE %s, но запрошена %s", filepath.Base(archivePath), distInfo.Version, dleVersion)
	}
	dleVersion = distInfo.Version

	phpVersion = selectPHP(dleVersion, phpVersion, opts.DefaultPHP)
	if !slices.Contains(opts.PHPVersions, phpVersion) {
		return nil, fmt.Errorf("версия PHP %s недоступна, допустимые: %s", phpVersion, strings.Join(opts.PHPVersions, ", "))
	}
	if dist.VersionLess(phpVersion, minimumInstallerPHP(dleVersion)) {
		return nil, fmt.Errorf("чистая установка DLE %s требует PHP >= %s", dleVersion, minimumInstallerPHP(dleVersion))
	}

	h := Host{
		Name:       name,
		DomainName: s.Domain(name),
		DLE:        dleVersion,
		PHP:        phpVersion,
		Database:   "dle_" + strings.ReplaceAll(name, "-", "_"),
		CreatedAt:  time.Now(),
		Protect:    protect,
	}
	protected := h.Protected(opts.Settings.Global)
	h.gateKey = passwordKey(passwords.effective(name))
	if s.ExternalOrigin != "" {
		h.ExternalOrigin = strings.TrimSuffix(s.HostURL(h.Domain()), "/")
	}
	password, err := randomHex(16)
	if err != nil {
		return nil, err
	}

	var rollback []func()
	fail := func(err error) (*AddResult, error) {
		for i := len(rollback) - 1; i >= 0; i-- {
			rollback[i]()
		}
		return nil, err
	}

	if protected {
		log("каталог сессий формы входа")
		rollback = append(rollback, func() { os.RemoveAll(s.GateDir(name)) })
		// Сессии удалённого ранее хоста с тем же именем не должны подойти новому.
		if err := os.RemoveAll(s.GateDir(name)); err != nil {
			return fail(err)
		}
		if err := ensureGateDir(s, name); err != nil {
			return fail(err)
		}
		if err := s.CheckGate(phpVersion, name); err != nil {
			return fail(err)
		}
	}

	// Файлы готовятся вне www/ и публикуются переименованием только после
	// vhost: иначе до его появления каталог отдавал бы автохост
	// <имя>.localhost без формы входа.
	if err := os.MkdirAll(s.StagingDir(), 0o700); err != nil {
		return fail(err)
	}
	staging, err := os.MkdirTemp(s.StagingDir(), name+"-")
	if err != nil {
		return fail(err)
	}
	rollback = append(rollback, func() { os.RemoveAll(staging) })
	log("распаковка DLE " + dleVersion)
	if err := dist.ExtractPrefix(archive, staging, distInfo.Prefix); err != nil {
		return fail(err)
	}

	log("создание базы " + h.Database)
	if err := s.SQL(createDatabaseSQL(h.Database)); err != nil {
		return fail(err)
	}
	rollback = append(rollback, func() { s.SQL(dropDatabaseSQL(h.Database)) })
	if err := s.SQL(createUserSQL(h.Database, password)); err != nil {
		return fail(err)
	}
	rollback = append(rollback, func() { s.SQL(dropUserSQL(h.Database)) })

	url := h.URL(s)
	log("стандартный установщик DLE " + dleVersion + " через HTTP")
	if err := prepareInstallerFiles(s, staging); err != nil {
		return fail(err)
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	err = s.WithInstaller(ctx, staging, phpVersion, func(endpoint string) error {
		return installer.Run(ctx, installer.Options{URL: endpoint, PublicURL: url, DBHost: dbServiceHost,
			DBName: h.Database, DBUser: h.Database, DBPassword: password,
			Login: opts.Admin.Login, Password: opts.Admin.Password, Email: "admin@example.com",
			RemoveInstaller: func() error { return os.Remove(filepath.Join(staging, "install.php")) }}, log)
	})
	if err != nil {
		return fail(err)
	}
	if err := finishInstallation(staging); err != nil {
		return fail(err)
	}

	log("vhost apache, PHP " + phpVersion)
	vhost := vhostPath(s, name)
	if protected {
		log("форма входа: включена")
	}
	if err := os.WriteFile(vhost, []byte(vhostConfig(h, protected, opts.Settings.SessionTTL)), 0o644); err != nil {
		return fail(err)
	}
	rollback = append(rollback, func() {
		// Пока опубликованные файлы не удалены, vhost их закрывает.
		if _, err := os.Stat(hostDir); err == nil {
			return
		}
		os.Remove(vhost)
		s.ReloadApache()
	})
	if err := s.ReloadApache(); err != nil {
		return fail(err)
	}

	log("публикация файлов в " + hostDir)
	if err := os.Rename(staging, hostDir); err != nil {
		return fail(err)
	}
	// Откат идёт в обратном порядке: файлы удаляются раньше vhost.
	rollback = append(rollback, func() { os.RemoveAll(hostDir) })

	hosts[name] = h
	if err := saveRegistry(s, hosts); err != nil {
		return fail(err)
	}
	return &AddResult{Host: h, URL: url}, nil
}

// Remove удаляет хост, созданный через dlehost: базу, файлы и vhost.
// vhost удаляется последним: пока файлы хоста есть, его vhost (и форма
// входа, если включена) обслуживает и адреса автохостов, поэтому при
// ошибке удаления базы или файлов хост не становится открытым. Повторный
// rm продолжает с места ошибки.
func Remove(s *stack.Stack, name string, log func(string)) error {
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	h, ok := hosts[name]
	if !ok {
		return fmt.Errorf("хост %s не найден среди созданных через dlehost", name)
	}

	log("удаление базы " + h.Database)
	if err := s.SQL(dropDatabaseSQL(h.Database) + dropUserSQL(h.Database)); err != nil {
		return err
	}

	log("удаление файлов")
	if err := os.RemoveAll(s.HostDir(name)); err != nil {
		return err
	}

	log("удаление vhost")
	if err := os.Remove(vhostPath(s, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.ReloadApache(); err != nil {
		return err
	}

	if err := clearSessions(s, name, false); err != nil {
		log("не удалось удалить сессии формы входа: " + err.Error())
	}
	passwords, err := loadPasswords(s)
	if err != nil {
		return err
	}
	if _, own := passwords.Hosts[name]; own {
		delete(passwords.Hosts, name)
		if err := savePasswords(s, passwords); err != nil {
			return err
		}
	}
	delete(hosts, name)
	return saveRegistry(s, hosts)
}

// Имя базы совпадает с именем пользователя; оба состоят из [a-z0-9_],
// поэтому экранирование не требуется.
func createDatabaseSQL(db string) string {
	return fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", db)
}

func createUserSQL(user, password string) string {
	return fmt.Sprintf(
		"CREATE USER '%[1]s'@'%%' IDENTIFIED BY '%[2]s';\n"+
			"GRANT ALL PRIVILEGES ON `%[1]s`.* TO '%[1]s'@'%%';\n",
		user, password)
}

func dropDatabaseSQL(db string) string {
	return fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", db)
}

func dropUserSQL(user string) string {
	return fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%';", user)
}

// Хост БД, видимый из контейнеров PHP, — имя сервиса MariaDB в docker-compose.
const dbServiceHost = "db"

var homeURLRe = regexp.MustCompile(`('http_home_url'\s*=>\s*)(['"]).*?(['"])`)

func setHomeURL(hostDir, url string) error {
	return replaceInFile(filepath.Join(hostDir, "engine", "data", "config.php"), func(s string) string {
		return homeURLRe.ReplaceAllString(s, "${1}${2}"+url+"${3}")
	})
}

func replaceInFile(path string, fn func(string) string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fn(string(data))), 0o644)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SetHTTPPort переключает порт apache для всего стека и обновляет
// http_home_url у хостов dlehost без внешнего origin.
func SetHTTPPort(s *stack.Stack, port int, log func(string)) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("некорректный порт %d", port)
	}
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	prev := s.HTTPPort()

	log("HTTP_PORT=" + strconv.Itoa(port) + " в .env")
	if err := s.SetDotEnv("HTTP_PORT", strconv.Itoa(port)); err != nil {
		return err
	}
	log("пересоздание apache")
	if err := s.Compose(nil, "up", "-d", "apache"); err != nil {
		s.SetDotEnv("HTTP_PORT", prev)
		if rerr := s.Compose(nil, "up", "-d", "apache"); rerr != nil {
			return fmt.Errorf("%w; вернуть порт %s тоже не удалось: %v", err, prev, rerr)
		}
		return fmt.Errorf("%w (порт возвращён на %s)", err, prev)
	}

	for _, h := range hosts {
		if h.ExternalOrigin != "" {
			continue
		}
		log("http_home_url " + h.Name)
		hostDir := s.HostDir(h.Name)
		if err := setHomeURL(hostDir, h.URL(s)); err != nil {
			return fmt.Errorf("%s: %w", h.Name, err)
		}
		if err := clearPageCache(hostDir); err != nil {
			return fmt.Errorf("%s: %w", h.Name, err)
		}
	}
	return nil
}

// clearPageCache удаляет кэш страниц DLE (engine/cache/*.tmp) — в нём
// сохранены абсолютные ссылки со старым адресом.
func clearPageCache(hostDir string) error {
	files, err := filepath.Glob(filepath.Join(hostDir, "engine", "cache", "*.tmp"))
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}
