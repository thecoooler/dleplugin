// Package host manages DLE hosts in the stack: create, remove, list, port.
package host

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thecoooler/dleplugin/internal/dleinstall"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/dist"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

// Host is a host registry entry (data/hosts.json in the stack directory).
type Host struct {
	Name           string    `json:"name"`
	DomainName     string    `json:"domain,omitempty"`
	ExternalOrigin string    `json:"external_origin,omitempty"`
	DLE            string    `json:"dle"`
	PHP            string    `json:"php"`
	Database       string    `json:"database"`
	CreatedAt      time.Time `json:"created_at"`
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

// URL uses the stored origin; old entries keep local addressing.
func (h Host) URL(s *stack.Stack) string {
	if h.ExternalOrigin != "" {
		return h.ExternalOrigin + "/"
	}
	return s.LocalHostURL(h.Domain())
}

var hostNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,40}[a-z0-9])?$`)

func validateHostName(name string) error {
	if !hostNameRe.MatchString(name) {
		return fmt.Errorf("invalid name %q: lower-case latin letters, digits and hyphens, up to 42 characters", name)
	}
	if name == "localhost" || name == "any" || strings.HasPrefix(name, "php") && isDigits(name[3:]) {
		return fmt.Errorf("name %q is reserved by the apache auto hosts", name)
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

// Row is a list row: a managed host from the registry or a foreign directory in www/.
type Row struct {
	Host
	Managed bool
}

// List returns the registry hosts and the other directories in www/.
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

// Path returns the files directory of host <name> on the machine or, with container,
// inside the containers. It also works for www/ directories not created by hosts add.
func Path(s *stack.Stack, name string, container bool) (string, error) {
	if !hostNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid name %q", name)
	}
	dir := s.HostDir(name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("host %s not found: no directory %s", name, dir)
	}
	if container {
		return stack.ContainerWWWDir + "/" + name, nil
	}
	return dir, nil
}

func vhostPath(s *stack.Stack, name string) string {
	return filepath.Join(s.SitesDir(), "60-host-"+name+".conf")
}

// An explicit vhost with a fixed PHP version, included before the auto hosts.
func vhostConfig(h Host) string {
	ver := strings.ReplaceAll(h.PHP, ".", "")
	forwarded := ""
	if strings.HasPrefix(h.ExternalOrigin, "https://") {
		forwarded = `    # Only behind a trusted TLS-terminating proxy; the backend must not be public.
    ProxyFCGISetEnvIf "req('X-Forwarded-Proto') == 'https'" HTTPS "on"
    ProxyFCGISetEnvIf "req('X-Forwarded-Proto') == 'https'" REQUEST_SCHEME "https"
`
	}
	return fmt.Sprintf(`# Created by dleplugin hosts; do not edit by hand (DLE %s, PHP %s)
<VirtualHost *:80>
    ServerName %s
    DocumentRoot %s/%s
    Use PHP %s
%s    ErrorLog  ${APACHE_LOG_DIR}/%s-error.log
    CustomLog ${APACHE_LOG_DIR}/%s-access.log combined
</VirtualHost>
`, h.DLE, h.PHP, h.Domain(), stack.ContainerWWWDir, h.Name, ver, forwarded, h.Name, h.Name)
}

// AddOptions are the parameters of a new host.
type AddOptions struct {
	Context     context.Context
	Name        string
	DLE         string
	DistArchive string // direct path to a DLE distribution ZIP
	PHP         string
	DefaultPHP  string   // configured default or auto
	PHPVersions []string // allowed PHP versions
	DistsDir    string
	Admin       Credentials
	AdminPath   string       // desired administrator filename; empty keeps the distribution default
	SiteOffline string       // on (default, closed) or off (open)
	Warn        func(string) // optional warnings before installation
}

// AddResult is the created host and its address.
type AddResult struct {
	Host Host
	URL  string
}

// Add creates a host from a distribution; everything created is rolled back on error.
func Add(s *stack.Stack, opts AddOptions, log func(string)) (*AddResult, error) {
	name, dleVersion, phpVersion := opts.Name, opts.DLE, opts.PHP
	if opts.AdminPath != "" {
		if err := ValidateSetting("admin_path", opts.AdminPath); err != nil {
			return nil, err
		}
	}
	if err := opts.Admin.Validate(); err != nil {
		return nil, err
	}
	if err := validateHostName(name); err != nil {
		return nil, err
	}
	offline := opts.SiteOffline
	if offline == "" {
		offline = "on"
	}
	if err := ValidateSetting("site_offline", offline); err != nil {
		return nil, err
	}
	hosts, err := loadRegistry(s)
	if err != nil {
		return nil, err
	}
	if _, ok := hosts[name]; ok {
		return nil, fmt.Errorf("host %s already exists", name)
	}
	hostDir := s.HostDir(name)
	if _, err := os.Stat(hostDir); err == nil {
		return nil, fmt.Errorf("directory %s already exists", hostDir)
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
		return nil, fmt.Errorf("DLE distribution %s: %w (see dleplugin hosts versions)", dleVersion, err)
	}
	defer archive.Close()

	distInfo, err := dist.Inspect(&archive.Reader)
	if err != nil {
		return nil, fmt.Errorf("distribution %s: %w", filepath.Base(archivePath), err)
	}
	if dleVersion != "" && distInfo.Version != dleVersion {
		return nil, fmt.Errorf("archive %s contains DLE %s, but %s was requested", filepath.Base(archivePath), distInfo.Version, dleVersion)
	}
	dleVersion = distInfo.Version

	phpVersion = selectPHP(dleVersion, phpVersion, opts.DefaultPHP)
	warn := opts.Warn
	if warn == nil {
		warn = func(string) {}
	}
	if !compatiblePHP(dleVersion, phpVersion) {
		warn(fmt.Sprintf("PHP %s may not be compatible with DLE %s", phpVersion, dleVersion))
	}
	if phpVersion == "7.4" {
		warn("PHP 7.4 is end-of-life and vulnerable to CVE-2026-109618 and CVE-2026-109619; do not expose this stack to untrusted networks")
	}
	if !slices.Contains(opts.PHPVersions, phpVersion) {
		return nil, fmt.Errorf("PHP version %s is not available, allowed: %s", phpVersion, strings.Join(opts.PHPVersions, ", "))
	}
	h := Host{
		Name:       name,
		DomainName: s.Domain(name),
		DLE:        dleVersion,
		PHP:        phpVersion,
		Database:   "dle_" + strings.ReplaceAll(name, "-", "_"),
		CreatedAt:  time.Now(),
	}
	if s.ExternalOrigin != "" {
		h.ExternalOrigin = strings.TrimSuffix(s.HostURL(h.Domain()), "/")
	}
	password, err := stack.NewPassword()
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

	// Prepare files outside www/ and publish them after the vhost is ready.
	if err := os.MkdirAll(s.StagingDir(), 0o700); err != nil {
		return fail(err)
	}
	staging, err := os.MkdirTemp(s.StagingDir(), name+"-")
	if err != nil {
		return fail(err)
	}
	rollback = append(rollback, func() { os.RemoveAll(staging) })
	log("unpacking DLE " + dleVersion)
	if err := dist.ExtractPrefix(archive, staging, distInfo.Prefix); err != nil {
		return fail(err)
	}

	log("creating database " + h.Database)
	if err := s.SQL(createDatabaseSQL(h.Database)); err != nil {
		return fail(err)
	}
	rollback = append(rollback, func() { s.SQL(dropDatabaseSQL(h.Database)) })
	if err := s.SQL(createUserSQL(h.Database, password)); err != nil {
		return fail(err)
	}
	rollback = append(rollback, func() { s.SQL(dropUserSQL(h.Database)) })

	url := h.URL(s)
	log("running the standard DLE " + dleVersion + " installer over HTTP")
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
		return dleinstall.Run(ctx, dleinstall.Options{URL: endpoint, PublicURL: url, DBHost: dbServiceHost,
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
	if err := setSiteOffline(staging, offline); err != nil {
		return fail(err)
	}

	if opts.AdminPath != "" {
		if err := setAdminPath(staging, opts.AdminPath, os.Rename); err != nil {
			return fail(err)
		}
	}

	log("vhost apache, PHP " + phpVersion)
	vhost := vhostPath(s, name)
	if err := os.WriteFile(vhost, []byte(vhostConfig(h)), 0o644); err != nil {
		return fail(err)
	}
	rollback = append(rollback, func() {
		// Keep the vhost until published files are removed.
		if _, err := os.Stat(hostDir); err == nil {
			return
		}
		os.Remove(vhost)
		s.ReloadApache()
	})
	if err := s.ReloadApache(); err != nil {
		return fail(err)
	}

	log("publishing files to " + hostDir)
	if err := os.Rename(staging, hostDir); err != nil {
		return fail(err)
	}
	// Rollback runs in reverse order: files are removed before the vhost.
	rollback = append(rollback, func() { os.RemoveAll(hostDir) })

	hosts[name] = h
	if err := saveRegistry(s, hosts); err != nil {
		return fail(err)
	}
	return &AddResult{Host: h, URL: url}, nil
}

// Remove removes a host created by hosts add: database, files and vhost.
// Running remove again continues from the failed step.
func Remove(s *stack.Stack, name string, log func(string)) error {
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	h, ok := hosts[name]
	if !ok {
		return fmt.Errorf("host %s is not among the hosts created by hosts add", name)
	}

	log("dropping database " + h.Database)
	if err := s.SQL(dropDatabaseSQL(h.Database) + dropUserSQL(h.Database)); err != nil {
		return err
	}

	log("removing files")
	if err := os.RemoveAll(s.HostDir(name)); err != nil {
		return err
	}

	log("removing vhost")
	if err := os.Remove(vhostPath(s, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.ReloadApache(); err != nil {
		return err
	}

	delete(hosts, name)
	return saveRegistry(s, hosts)
}

// The database name equals the user name; both consist of [a-z0-9_],
// so no escaping is needed.
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

// The database host seen from the PHP containers: the MariaDB service name in docker-compose.
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

// SetHTTPPort switches the Apache port of the whole stack and updates
// http_home_url of the hosts without an external origin.
func SetHTTPPort(s *stack.Stack, port int, log func(string)) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d", port)
	}
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	prev := s.HTTPPort()

	log("HTTP_PORT=" + strconv.Itoa(port) + " in .env")
	if err := s.SetDotEnv("HTTP_PORT", strconv.Itoa(port)); err != nil {
		return err
	}
	log("applying stack configuration")
	if err := s.Compose(nil, "up", "-d"); err != nil {
		if rerr := s.SetDotEnv("HTTP_PORT", prev); rerr != nil {
			return fmt.Errorf("%w; restoring port %s in .env failed: %v", err, prev, rerr)
		}
		if rerr := s.Compose(nil, "up", "-d"); rerr != nil {
			return fmt.Errorf("%w; restoring port %s failed too: %v", err, prev, rerr)
		}
		return fmt.Errorf("%w (port restored to %s)", err, prev)
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

// clearPageCache removes the DLE page cache (engine/cache/*.tmp): it holds
// absolute links with the old address.
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

// SetBindAddress switches the Apache bind address of the whole stack.
func SetBindAddress(s *stack.Stack, address string, log func(string)) error {
	ip := net.ParseIP(address)
	if ip == nil {
		return fmt.Errorf("invalid address %q: expected an IP address (e.g. 127.0.0.1 or 0.0.0.0)", address)
	}
	norm := ip.String()
	prev := s.BindAddress()

	log("BIND_ADDRESS=" + norm + " in .env")
	if err := s.SetDotEnv("BIND_ADDRESS", norm); err != nil {
		return err
	}
	log("applying stack configuration")
	if err := s.Compose(nil, "up", "-d"); err != nil {
		if rerr := s.SetDotEnv("BIND_ADDRESS", prev); rerr != nil {
			return fmt.Errorf("%w; restoring bind address %s in .env failed: %v", err, prev, rerr)
		}
		if rerr := s.Compose(nil, "up", "-d"); rerr != nil {
			return fmt.Errorf("%w; restoring bind address %s failed too: %v", err, prev, rerr)
		}
		return fmt.Errorf("%w (bind address restored to %s)", err, prev)
	}
	return nil
}
