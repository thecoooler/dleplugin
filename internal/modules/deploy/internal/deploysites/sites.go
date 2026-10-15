// Package deploysites stores DLE deployment connections keyed by host.
// Project records replace global records as a whole.
package deploysites

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/thecoooler/dleplugin/internal/config"
	"github.com/thecoooler/dleplugin/internal/dleadmin"

	"gopkg.in/yaml.v3"
)

// Filename is shared by project and global deployment configurations.
const Filename = "dleplugin-deploy.yaml"

// Site contains connection settings, without the admin password.
type Site struct {
	URL         string `yaml:"url,omitempty"`
	AdminPath   string `yaml:"admin_path,omitempty"`
	Login       string `yaml:"login"`
	PasswordEnv string `yaml:"password_env,omitempty"`
}

// Document is the deployment configuration file.
type Document struct {
	Sites map[string]Site `yaml:"sites"`
}

// Entry identifies an effective site and its source file.
type Entry struct {
	Host, Path, Scope string
	Site              Site
}

// Host returns the case-normalized host and port of a deployment target.
func Host(target string) (string, error) {
	u, err := dleadmin.SiteURL(target, "")
	if err != nil {
		return "", err
	}
	if u.User != nil {
		return "", errors.New("site URL must not contain credentials")
	}
	return strings.TrimSuffix(strings.ToLower(u.Host), "."), nil
}

// AdminURL resolves a site's settings and verifies they belong to host.
func (s Site) AdminURL(host string) (*url.URL, error) {
	target := s.URL
	if target == "" {
		target = host
	}
	u, err := dleadmin.SiteURL(target, s.AdminPath)
	if err != nil {
		return nil, err
	}
	key, err := Host(u.String())
	if err != nil {
		return nil, err
	}
	if key != host {
		return nil, fmt.Errorf("site %q points to a different host %q", host, key)
	}
	return u, nil
}

// Load reads a document; a missing file is an empty registry.
func Load(path string) (*Document, error) {
	d := &Document{Sites: make(map[string]Site)}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read deployment config %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(d); err != nil && err != io.EOF {
		return nil, fmt.Errorf("read deployment config %s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("deployment config %s must contain one YAML document", path)
	}
	if d.Sites == nil {
		d.Sites = make(map[string]Site)
	}
	for host, site := range d.Sites {
		key, err := Host(host)
		if err != nil || key != host || strings.ContainsAny(host, "/?#") {
			return nil, fmt.Errorf("deployment config %s: invalid host key %q", path, host)
		}
		if _, err := site.AdminURL(host); err != nil {
			return nil, fmt.Errorf("deployment config %s: %w", path, err)
		}
		if strings.TrimSpace(site.Login) == "" {
			return nil, fmt.Errorf("deployment config %s: site %q is missing login", path, host)
		}
	}
	return d, nil
}

// Save atomically replaces a document, keeping its contents private.
func Save(path string, d *Document) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create deployment config directory: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".deploy-*.yaml")
	if err != nil {
		return fmt.Errorf("create deployment config: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temporary deployment config: %w", err)
	}
	if err := config.SaveYAMLFile(tmp, d, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("save deployment config %s: %w", path, err)
	}
	return nil
}

// Effective reads both scopes. A project record completely shadows a global one.
// An empty projectPath means the invocation is outside a plugin project.
func Effective(globalPath, projectPath string) (map[string]Entry, error) {
	entries := make(map[string]Entry)
	for _, source := range []struct{ path, scope string }{{globalPath, "global"}, {projectPath, "project"}} {
		if source.path == "" {
			continue
		}
		d, err := Load(source.path)
		if err != nil {
			return nil, err
		}
		for host, site := range d.Sites {
			entries[host] = Entry{Host: host, Path: source.path, Scope: source.scope, Site: site}
		}
	}
	return entries, nil
}

// Hosts returns registry keys in stable order.
func Hosts(entries map[string]Entry) []string {
	hosts := make([]string, 0, len(entries))
	for host := range entries {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}
