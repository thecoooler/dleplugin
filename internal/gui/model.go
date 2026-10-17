package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/thecoooler/dleplugin/internal/config"
	"github.com/thecoooler/dleplugin/internal/deploysites"
	"github.com/thecoooler/dleplugin/internal/manifest"
	"github.com/thecoooler/dleplugin/internal/plugin"

	"gopkg.in/yaml.v3"
)

// SettingsFilename is the GUI settings file, kept next to the global config.
const SettingsFilename = "gui.yaml"

// Settings is what the GUI remembers between runs.
type Settings struct {
	// Projects are the plugin projects in the sidebar, in order.
	Projects []ProjectSettings `yaml:"projects"`
	// Verbose runs commands with --verbose.
	Verbose bool `yaml:"verbose,omitempty"`
}

// ProjectSettings is a project in the sidebar and the values its forms keep.
type ProjectSettings struct {
	// Path is the project root, the directory of dleplugin.yml.
	Path string `yaml:"path"`
	// CheckDirs are the DLE directories the patch check runs against.
	CheckDirs []string `yaml:"check_dirs,omitempty"`
	// DeploySite is the host last chosen for upload.
	DeploySite string `yaml:"deploy_site,omitempty"`
}

// LoadSettings reads the settings file; a missing file is empty settings.
func LoadSettings(path string) (*Settings, error) {
	s := &Settings{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, s); err != nil {
		return &Settings{}, fmt.Errorf("invalid settings file %s: %w", path, err)
	}
	// Drop duplicates and empty paths a hand edit may have left.
	var projects []ProjectSettings
	for _, p := range s.Projects {
		if p.Path != "" && s.project(p.Path, projects) == nil {
			projects = append(projects, p)
		}
	}
	s.Projects = projects
	return s, nil
}

// Save writes the settings file.
func (s *Settings) Save(path string) error { return config.SaveYAMLFile(path, s, 0o644) }

func (s *Settings) project(root string, in []ProjectSettings) *ProjectSettings {
	for i := range in {
		if samePath(in[i].Path, root) {
			return &in[i]
		}
	}
	return nil
}

// Project returns the settings of the project at root, or nil.
func (s *Settings) Project(root string) *ProjectSettings { return s.project(root, s.Projects) }

// AddProject adds root unless it is listed already, and reports whether it
// was added.
func (s *Settings) AddProject(root string) bool {
	if s.Project(root) != nil {
		return false
	}
	s.Projects = append(s.Projects, ProjectSettings{Path: root})
	return true
}

// RemoveProject removes root from the list.
func (s *Settings) RemoveProject(root string) {
	s.Projects = slices.DeleteFunc(s.Projects, func(p ProjectSettings) bool { return samePath(p.Path, root) })
}

func samePath(a, b string) bool { return filepath.Clean(a) == filepath.Clean(b) }

// projectRoot returns the plugin project containing dir: the nearest
// directory with dleplugin.yml at or above it.
func projectRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return plugin.FindRoot(abs)
}

// project is a loaded plugin project.
type project struct {
	root  string
	info  *manifest.Info
	files int
	err   error
	dist  []distFile
}

// distFile is a build artifact in dist/.
type distFile struct {
	name string
	size int64
}

// loadProject reads the manifest of the project at root and lists its
// sources and build artifacts.
func loadProject(root string) *project {
	p := &project{root: root}
	pl, err := plugin.Load(root)
	if err != nil {
		p.err = err
		return p
	}
	p.info = pl.Info()
	files, err := pl.Files()
	if err != nil {
		p.err = err
	}
	p.files = len(files)
	p.dist = listDist(root)
	return p
}

// title is the project's name in the sidebar.
func (p *project) title() string {
	if p.info != nil && strings.TrimSpace(p.info.Name) != "" {
		return p.info.Name
	}
	return filepath.Base(p.root)
}

func listDist(root string) []distFile {
	entries, err := os.ReadDir(filepath.Join(root, "dist"))
	if err != nil {
		return nil
	}
	var out []distFile
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, distFile{name: e.Name(), size: info.Size()})
	}
	return out
}

// passwordState tells where an upload to a saved site would take the admin
// password from.
type passwordState int

const (
	passwordNone        passwordState = iota // nowhere: it must be typed
	passwordEnv                              // an environment variable
	passwordStored                           // the OS credential store
	passwordUnavailable                      // the credential store failed
)

func (s passwordState) String() string {
	switch s {
	case passwordEnv:
		return "environment"
	case passwordStored:
		return "saved"
	case passwordUnavailable:
		return "store unavailable"
	}
	return "not saved"
}

// siteRow is a saved deployment site.
type siteRow struct {
	host     string
	site     deploysites.Site
	scope    string // "global" or "project"
	path     string // the file it is saved in
	admin    string // the admin URL
	env      string // the environment variable checked for the password
	password passwordState
}

// registryPaths returns the global deployment file and, for a project, its
// own one.
func registryPaths(configPath, root string) (global, project string) {
	global = filepath.Join(filepath.Dir(configPath), deploysites.Filename)
	if root != "" {
		project = filepath.Join(root, deploysites.Filename)
	}
	return global, project
}

// loadSites returns the sites effective for a project (global records
// replaced by the project's), or the global ones when root is empty. It
// reads the credential store, so call it off the main thread.
func loadSites(configPath, root string) ([]siteRow, error) {
	global, projectPath := registryPaths(configPath, root)
	entries, err := deploysites.Effective(global, projectPath)
	if err != nil {
		return nil, err
	}
	var rows []siteRow
	for _, host := range deploysites.Hosts(entries) {
		e := entries[host]
		row := siteRow{host: host, site: e.Site, scope: e.Scope, path: e.Path}
		if u, err := e.Site.AdminURL(host); err == nil {
			row.admin = u.String()
		}
		row.env = e.Site.PasswordEnv
		if row.env == "" {
			row.env = deploysites.DefaultPasswordEnv
		}
		row.password = sitePassword(host, e.Site.Login, row.env)
		rows = append(rows, row)
	}
	return rows, nil
}

func sitePassword(host, login, env string) passwordState {
	if os.Getenv(env) != "" {
		return passwordEnv
	}
	_, ok, err := deploysites.Password(host, login)
	switch {
	case err != nil:
		return passwordUnavailable
	case ok:
		return passwordStored
	}
	return passwordNone
}

// scopeSites returns the rows saved in one scope.
func scopeSites(rows []siteRow, scope string) []siteRow {
	var out []siteRow
	for _, r := range rows {
		if r.scope == scope {
			out = append(out, r)
		}
	}
	return out
}

// hosts returns the hosts of rows, sorted.
func hosts(rows []siteRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.host)
	}
	sort.Strings(out)
	return out
}

func findSite(rows []siteRow, host string) (siteRow, bool) {
	for _, r := range rows {
		if r.host == host {
			return r, true
		}
	}
	return siteRow{}, false
}
