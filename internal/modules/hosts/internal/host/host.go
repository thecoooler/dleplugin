// Package host manages DLE hosts in the stack: create, remove, list, port.
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

	"github.com/thecoooler/dleplugin/internal/dleinstall"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/dist"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

// Host is a host registry entry (data/hosts.json in the stack directory).
type Host struct {
	gateKey string // fingerprint of the effective hash, only for vhost generation

	Name           string    `json:"name"`
	DomainName     string    `json:"domain,omitempty"`
	ExternalOrigin string    `json:"external_origin,omitempty"`
	DLE            string    `json:"dle"`
	PHP            string    `json:"php"`
	Database       string    `json:"database"`
	CreatedAt      time.Time `json:"created_at"`
	// Protect is the login form mode: "" (default, follows the global setting), on, off.
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
		// An unknown mode must not silently become default: protection could turn off.
		if _, err := ParseProtect(h.Protect); err != nil || h.Protect == ProtectDefault {
			return nil, fmt.Errorf("%s: host %s: invalid protect %q (on, off or no field)", s.RegistryPath(), h.Name, h.Protect)
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

// An explicit vhost with a fixed PHP version. 60-* files are included before
// 90-auto.conf, so the domain is served by the chosen version. protected
// puts the login form in front of the host, including its auto host addresses;
// ttl is the form session lifetime (0 means no server-side expiry).
func vhostConfig(h Host, protected bool, ttl time.Duration) string {
	ver := strings.ReplaceAll(h.PHP, ".", "")
	forwarded := ""
	if strings.HasPrefix(h.ExternalOrigin, "https://") {
		forwarded = `    # Only behind a trusted TLS-terminating proxy; the backend must not be public.
    ProxyFCGISetEnvIf "req('X-Forwarded-Proto') == 'https'" HTTPS "on"
    ProxyFCGISetEnvIf "req('X-Forwarded-Proto') == 'https'" REQUEST_SCHEME "https"
`
	}
	aliases, gate := "", ""
	if protected {
		aliases, gate = gateAliases(h), gateConfig(h, ttl)
	}
	return fmt.Sprintf(`# Created by dleplugin hosts; do not edit by hand (DLE %s, PHP %s)
<VirtualHost *:80>
    ServerName %s
%s    DocumentRoot %s/%s
    Use PHP %s
%s%s    ErrorLog  ${APACHE_LOG_DIR}/%s-error.log
    CustomLog ${APACHE_LOG_DIR}/%s-access.log combined
</VirtualHost>
`, h.DLE, h.PHP, h.Domain(), aliases, stack.ContainerWWWDir, h.Name, ver, forwarded, gate, h.Name, h.Name)
}

// AddOptions are the parameters of a new host.
type AddOptions struct {
	Context     context.Context
	Name        string
	DLE         string
	DistArchive string // direct path to a DLE distribution ZIP
	PHP         string
	DefaultPHP  string   // config preference; used only when compatible with DLE
	PHPVersions []string // allowed PHP versions
	DistsDir    string
	Admin       Credentials
	SiteOffline string          // on (default, closed) or off (open)
	Protect     string          // default, on or off
	Settings    ProtectSettings // global login form settings
}

// AddResult is the created host and its address.
type AddResult struct {
	Host Host
	URL  string
}

// Add creates a host from a distribution; everything created is rolled back on error.
func Add(s *stack.Stack, opts AddOptions, log func(string)) (*AddResult, error) {
	name, dleVersion, phpVersion := opts.Name, opts.DLE, opts.PHP
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
	if !slices.Contains(opts.PHPVersions, phpVersion) {
		return nil, fmt.Errorf("PHP version %s is not available, allowed: %s", phpVersion, strings.Join(opts.PHPVersions, ", "))
	}
	if dist.VersionLess(phpVersion, minimumInstallerPHP(dleVersion)) {
		return nil, fmt.Errorf("a clean install of DLE %s requires PHP >= %s", dleVersion, minimumInstallerPHP(dleVersion))
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
		log("login form session directory")
		rollback = append(rollback, func() { os.RemoveAll(s.GateDir(name)) })
		// Sessions of a previously removed host with the same name must not work for the new one.
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

	// Files are prepared outside www/ and published by renaming only after the
	// vhost exists: otherwise the directory would be served by the
	// <name>.localhost auto host without the login form.
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

	log("vhost apache, PHP " + phpVersion)
	vhost := vhostPath(s, name)
	if protected {
		log("login form: on")
	}
	if err := os.WriteFile(vhost, []byte(vhostConfig(h, protected, opts.Settings.SessionTTL)), 0o644); err != nil {
		return fail(err)
	}
	rollback = append(rollback, func() {
		// The vhost protects the published files until they are removed.
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
// The vhost goes last: while the host files exist, its vhost (and the login
// form, when enabled) also serves the auto host addresses, so a failure to
// remove the database or files does not leave the host open. Running
// remove again continues from the failed step.
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

	if err := clearSessions(s, name, false); err != nil {
		log("could not remove the login form sessions: " + err.Error())
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

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
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
	log("recreating apache")
	if err := s.Compose(nil, "up", "-d", "apache"); err != nil {
		s.SetDotEnv("HTTP_PORT", prev)
		if rerr := s.Compose(nil, "up", "-d", "apache"); rerr != nil {
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
