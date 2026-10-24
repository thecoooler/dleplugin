// Package dletest runs a plugin's Pest tests against the free DataLife Engine
// trial: the trial is downloaded and installed once (MariaDB in Docker), and
// every run connects the plugin and its dependencies to a fresh copy of the
// installation and runs Pest in a PHP container.
package dletest

import (
	"context"
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thecoooler/dleplugin/internal/dockerimg"
	"github.com/thecoooler/dleplugin/internal/modules/test/internal/dle"
	"github.com/thecoooler/dleplugin/internal/plugin"
	"github.com/thecoooler/dleplugin/internal/process"
)

//go:embed assets/Dockerfile
var dockerfile []byte

//go:embed assets/composer.json
var composerJSON []byte

//go:embed assets/composer.lock
var composerLock []byte

//go:embed assets/bootstrap.php
var bootstrapPHP []byte

//go:embed assets/phpunit.xml
var phpunitXML string

// Image is the PHP image running the installer and Pest.
var Image = dockerimg.Image{Name: "dleplugin-test", Title: "test", Dockerfile: dockerfile, Files: map[string][]byte{"composer.json": composerJSON, "composer.lock": composerLock}}

// containerRoot is the DLE root inside the Pest container; the image keeps
// Pest in containerRoot/vendor.
const containerRoot = "/dle"

// pestBin is Pest inside the image.
const pestBin = containerRoot + "/vendor/bin/pest"

// Options configure a test run.
type Options struct {
	// Plugin is the tested plugin; Dependencies are connected before it.
	Plugin       *plugin.Plugin
	Dependencies []*plugin.Plugin
	// Files are paths relative to the plugin root (all tests when empty).
	Files           []string
	Filter          string
	UpdateSnapshots bool
	Coverage        bool
	// CoverageMin fails the run below this percentage (nil = no minimum).
	CoverageMin *float64
	// CoverageHTML is an absolute directory for the HTML report ("" = none).
	CoverageHTML string
	// TrialURL overrides the trial download URL.
	TrialURL string
	// Refresh downloads and installs the trial again.
	Refresh bool
	// StreamBuild shows docker build output.
	StreamBuild bool
	// Notify receives progress messages; may be nil.
	Notify func(string)
	// HTTPClient downloads the trial (http.DefaultClient when nil).
	HTTPClient *http.Client
}

func (o *Options) notify(msg string) {
	if o.Notify != nil {
		o.Notify(msg)
	}
}

// ErrNoTests is returned when the plugin has no tests directory.
var ErrNoTests = errors.New("No tests found")

// Run runs the tests and returns Pest's exit code. Setup failures are
// returned as errors.
func Run(ctx context.Context, runner *process.Runner, o Options) (int, error) {
	testsDir := filepath.Join(o.Plugin.Root(), "tests")
	if fi, err := os.Stat(testsDir); err != nil || !fi.IsDir() {
		return 0, ErrNoTests
	}
	if o.CoverageHTML != "" {
		if err := os.MkdirAll(o.CoverageHTML, 0o755); err != nil {
			return 0, err
		}
	}
	image, err := dockerimg.Ensure(ctx, runner, Image, o.StreamBuild, o.Notify)
	if err != nil {
		return 0, err
	}
	base, err := prepareBase(ctx, runner, image, &o)
	if err != nil {
		return 0, err
	}
	site, err := newRunSite(base, o.Plugin)
	if err != nil {
		return 0, err
	}

	e, err := startEnv(ctx, runner)
	php := e.Name + "-php"
	defer e.Close(ctx, php)
	if err != nil {
		return 0, fmt.Errorf("start test database: %w", err)
	}
	dump, err := os.ReadFile(filepath.Join(base, "dump.sql"))
	if err != nil {
		return 0, err
	}
	if err := e.Import(ctx, dump); err != nil {
		return 0, fmt.Errorf("import test database: %w", err)
	}
	if err := connectPlugins(ctx, site, e.DBAddr, append(append([]*plugin.Plugin{}, o.Dependencies...), o.Plugin)); err != nil {
		return 0, err
	}
	if err := writeRunFiles(site, o.Plugin); err != nil {
		return 0, err
	}

	args := append([]string{"run", "--rm", "--name", php, "--network", e.Name, "--env", "HOME=/tmp"}, userArgs()...)
	ms, err := mounts(site, o)
	if err != nil {
		return 0, err
	}
	for _, m := range ms {
		args = append(args, "--volume", m)
	}
	args = append(args, "--workdir", containerRoot, image)
	args = append(args, pestArgs(o)...)
	_, err = runner.Run(ctx, process.Command{Name: "docker", Args: args, Stream: true})
	var ee *process.ExitError
	if errors.As(err, &ee) {
		return ee.Result.ExitCode, nil
	}
	if err != nil {
		return 0, err
	}
	return 0, nil
}

// pestArgs returns the command run in the container.
func pestArgs(o Options) []string {
	args := []string{"php"}
	if o.Coverage {
		// Plugin files are symlinked from /dle to their host paths. PCOV's
		// default /dle scope would silently exclude the actual source files.
		args = append(args, "-d", "pcov.enabled=1", "-d", "pcov.directory="+filepath.Join(o.Plugin.Root(), "src"))
	}
	args = append(args, pestBin, "--display-incomplete", "--colors=always", "--profile")
	if o.Filter != "" {
		args = append(args, "--filter", o.Filter)
	}
	if o.Coverage {
		args = append(args, "--coverage")
		if o.CoverageMin != nil {
			args = append(args, "--min="+strconv.FormatFloat(*o.CoverageMin, 'f', -1, 64))
		}
		if o.CoverageHTML != "" {
			args = append(args, "--coverage-html="+o.CoverageHTML)
		}
	}
	args = append(args, "-c", "phpunit.xml")
	if o.UpdateSnapshots {
		args = append(args, "--update-snapshots")
	}
	return append(args, o.Files...)
}

// mounts returns the volumes of the Pest container. Every entry of the run
// site is mounted into containerRoot (next to the image's vendor/), and the
// plugin's tests/ at containerRoot/tests, so Pest uses containerRoot as the
// project root. Plugin directories are mounted at their host paths, where the
// symlinks of the site point.
func mounts(site string, o Options) ([]string, error) {
	entries, err := os.ReadDir(site)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Name() == "vendor" || e.Name() == "tests" {
			continue
		}
		out = append(out, filepath.Join(site, e.Name())+":"+containerRoot+"/"+e.Name())
	}
	out = append(out, filepath.Join(o.Plugin.Root(), "tests")+":"+containerRoot+"/tests")

	type mount struct {
		path string
		ro   bool
	}
	ms := []mount{{o.Plugin.Root(), false}}
	for _, d := range o.Dependencies {
		ms = append(ms, mount{d.Root(), true})
	}
	if o.CoverageHTML != "" {
		ms = append(ms, mount{o.CoverageHTML, false})
	}
	// Parents first; drop paths inside an already mounted directory.
	sort.SliceStable(ms, func(i, j int) bool { return len(ms[i].path) < len(ms[j].path) })
	var kept []string
	for _, m := range ms {
		inside := false
		for _, k := range kept {
			if m.path == k || strings.HasPrefix(m.path, k+string(filepath.Separator)) {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		kept = append(kept, m.path)
		v := m.path + ":" + m.path
		if m.ro {
			v += ":ro"
		}
		out = append(out, v)
	}
	return out, nil
}

// prepareBase downloads and installs the trial unless an installation of the
// current archive exists, and returns its directory (site/ and dump.sql).
func prepareBase(ctx context.Context, runner *process.Runner, image string, o *Options) (string, error) {
	root, err := CacheRoot()
	if err != nil {
		return "", err
	}
	archive := filepath.Join(root, "dle_trial.zip")
	if _, err := os.Stat(archive); err != nil || o.Refresh {
		url := o.TrialURL
		if url == "" {
			url = TrialURL
		}
		client := o.HTTPClient
		if client == nil {
			client = &http.Client{Timeout: 10 * time.Minute}
		}
		o.notify("Downloading DLE trial from " + url + "...")
		if err := Download(ctx, client, url, archive); err != nil {
			return "", err
		}
	}
	sum, err := FileHash(archive)
	if err != nil {
		return "", err
	}
	base := filepath.Join(root, "dle-"+sum[:12])
	ready := filepath.Join(base, ".ready")
	if _, err := os.Stat(ready); err == nil && !o.Refresh {
		return base, nil
	}

	o.notify("Installing DLE trial (once per DLE release)...")
	if err := os.RemoveAll(base); err != nil {
		return "", err
	}
	site := filepath.Join(base, "site")
	if err := Unpack(archive, site); err != nil {
		return "", err
	}
	if err := installBase(ctx, runner, image, base, site); err != nil {
		return "", err
	}
	if err := os.WriteFile(ready, []byte(Version(site)+"\n"), 0o644); err != nil {
		return "", err
	}
	if v := Version(site); v != "" {
		o.notify("DLE " + v + " installed.")
	}
	return base, nil
}

// installBase runs the installer with PHP's built-in server and saves the
// database dump.
func installBase(ctx context.Context, runner *process.Runner, image, base, site string) error {
	e, err := startEnv(ctx, runner)
	web := e.Name + "-web"
	defer e.Close(ctx, web)
	if err != nil {
		return fmt.Errorf("start install database: %w", err)
	}
	args := append([]string{"run", "--detach", "--name", web, "--network", e.Name, "--env", "HOME=/tmp"}, userArgs()...)
	args = append(args, "--volume", site+":"+site, "--workdir", site, "--publish", "127.0.0.1::8000",
		image, "php", "-S", "0.0.0.0:8000")
	if _, err := e.docker(ctx, args...); err != nil {
		return err
	}
	addr, err := e.publishedAddr(ctx, web, "8000/tcp")
	if err != nil {
		return err
	}
	installURL := "http://" + addr + "/install.php"
	if err := e.waitFor(ctx, "DLE installer", func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, installURL, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}); err != nil {
		return err
	}
	if err := Install(ctx, "http://"+addr+"/", site); err != nil {
		return err
	}
	if err := SetSiteURL(site, SiteURL); err != nil {
		return err
	}
	dump, err := e.Dump(ctx)
	if err != nil {
		return fmt.Errorf("dump test database: %w", err)
	}
	if err := os.WriteFile(filepath.Join(base, "dump.sql"), dump, 0o644); err != nil {
		return err
	}
	// The installer normally deletes itself.
	if err := os.Remove(filepath.Join(site, "install.php")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// newRunSite replaces <CacheRoot>/run/<slug>-<hash>/ with a copy of the
// installed site and returns it.
func newRunSite(base string, p *plugin.Plugin) (string, error) {
	root, err := CacheRoot()
	if err != nil {
		return "", err
	}
	sum := md5.Sum([]byte(p.Root()))
	dir := filepath.Join(root, "run", p.Slug()+"-"+hex.EncodeToString(sum[:])[:6])
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("clean %s: %w", dir, err)
	}
	if err := copyTree(filepath.Join(base, "site"), dir); err != nil {
		return "", fmt.Errorf("copy DLE: %w", err)
	}
	return dir, nil
}

// connectPlugins places the plugins into site (symlinks) and registers them
// in the test database, so DLE applies their file patches.
func connectPlugins(ctx context.Context, site, dbAddr string, plugins []*plugin.Plugin) error {
	cfg := dle.DBConfig{Host: dbAddr, Name: dbName, User: dbUser, Pass: dbPass, Prefix: dbPrefix, UserPrefix: dbPrefix}
	registry, closeRegistry, err := openRegistry(cfg)
	if err != nil {
		return err
	}
	defer closeRegistry()
	host := &dle.Host{Path: site, Registry: registry}
	for _, p := range plugins {
		if err := host.Connect(ctx, p); err != nil {
			return fmt.Errorf("connect %s: %w", p.Slug(), err)
		}
	}
	return nil
}

// openRegistry keeps database creation separate from filesystem connection.
var openRegistry = func(cfg dle.DBConfig) (dle.Registry, func(), error) {
	db, err := dle.OpenMySQL(cfg)
	if err != nil {
		return nil, nil, err
	}
	return dle.NewSQLRegistry(db, cfg), func() { _ = db.Close() }, nil
}

// writeRunFiles writes bootstrap.php and phpunit.xml into site.
func writeRunFiles(site string, p *plugin.Plugin) error {
	if err := os.WriteFile(filepath.Join(site, "bootstrap.php"), bootstrapPHP, 0o644); err != nil {
		return err
	}
	// Coverage is reported for the real files: the site only links them.
	xml := strings.ReplaceAll(phpunitXML, "{{.Source}}", xmlEscape(filepath.Join(p.Root(), "src")))
	return os.WriteFile(filepath.Join(site, "phpunit.xml"), []byte(xml), 0o644)
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func xmlEscape(s string) string { return xmlEscaper.Replace(s) }

// copyTree copies the regular files, directories and symlinks of src into
// dst, keeping permission bits.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			return copyFile(p, target, info.Mode().Perm())
		}
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
