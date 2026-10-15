package dletest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/thecoooler/dleplugin/internal/config"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

// Config is the "test" section of the global config.
type Config struct {
	// TrialURL overrides TrialURL.
	TrialURL        string   `yaml:"trial_url"`
	DependencyRoots []string `yaml:"dependency_roots"`
}

// LoadConfig reads the "test" section. A missing config file yields
// defaults. dependency_roots falls back to the top-level key of the same name.
func LoadConfig(s *config.Store) (*Config, error) {
	c := &Config{}
	err := s.Section("test", c)
	if errors.Is(err, config.ErrNotFound) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if c.DependencyRoots != nil {
		return c, nil
	}
	var top struct {
		DependencyRoots []string `yaml:"dependency_roots"`
	}
	raw, _, err := s.Raw()
	if err != nil {
		return nil, err
	}
	if v, ok := raw["dependency_roots"]; ok && v != nil {
		b, _ := yaml.Marshal(map[string]any{"dependency_roots": v})
		if err := yaml.Unmarshal(b, &top); err != nil {
			return nil, fmt.Errorf("Invalid config file %s: dependency_roots must be a list of paths", s.Path())
		}
	}
	c.DependencyRoots = top.DependencyRoots
	return c, nil
}

// ConfFile is the per-plugin test configuration.
const ConfFile = "dletest.yml"

// PluginConf is dletest.yml.
type PluginConf struct {
	// Depends lists plugins needed only by the tests: paths (relative to the
	// plugin root) or plugin names.
	Depends []string `yaml:"depends"`
}

// LoadPluginConf reads root/dletest.yml; a missing file yields defaults.
func LoadPluginConf(root string) (*PluginConf, error) {
	p := filepath.Join(root, ConfFile)
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &PluginConf{}, nil
	}
	if err != nil {
		return nil, err
	}
	c := &PluginConf{}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", p, err)
	}
	return c, nil
}

// Dependencies returns the plugins to connect before p, each after its own
// dependencies: the "dependencies" of dleplugin.yml (plugin names, looked up
// in roots) and the "depends" of dletest.yml, recursively.
func Dependencies(p *plugin.Plugin, conf *PluginConf, roots []string) ([]*plugin.Plugin, error) {
	r := &resolver{roots: roots, state: map[string]int{p.Root(): visiting}, stack: []string{p.Name()}}
	refs := append([]string{}, p.Info().Dependencies...)
	for _, dep := range conf.Depends {
		dir := dep
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(p.Root(), dep)
		}
		if isPlugin(dir) {
			dp, err := plugin.Load(filepath.Clean(dir))
			if err != nil {
				return nil, fmt.Errorf("load dependency %s: %w", dep, err)
			}
			if err := r.visit(dp); err != nil {
				return nil, err
			}
			continue
		}
		refs = append(refs, dep)
	}
	for _, name := range refs {
		if err := r.visitName(name); err != nil {
			return nil, err
		}
	}
	return r.out, nil
}

const (
	visiting = 1
	done     = 2
)

type resolver struct {
	roots []string
	state map[string]int
	stack []string
	out   []*plugin.Plugin
}

func (r *resolver) visitName(name string) error {
	dp, err := findPlugin(name, r.roots)
	if err != nil {
		return fmt.Errorf("%w (required by %s)", err, r.stack[len(r.stack)-1])
	}
	return r.visit(dp)
}

func (r *resolver) visit(p *plugin.Plugin) error {
	switch r.state[p.Root()] {
	case done:
		return nil
	case visiting:
		return fmt.Errorf("Dependency cycle: %s -> %s", strings.Join(r.stack, " -> "), p.Name())
	}
	r.state[p.Root()] = visiting
	r.stack = append(r.stack, p.Name())
	for _, name := range p.Info().Dependencies {
		if err := r.visitName(name); err != nil {
			return err
		}
	}
	r.stack = r.stack[:len(r.stack)-1]
	r.state[p.Root()] = done
	r.out = append(r.out, p)
	return nil
}

// findPlugin looks a plugin up by name (or slug) in roots: first
// <root>/<name>, then every plugin directory directly inside the roots.
func findPlugin(name string, roots []string) (*plugin.Plugin, error) {
	matches := func(p *plugin.Plugin) bool { return p.Name() == name || p.Slug() == name }
	for _, root := range roots {
		dir := filepath.Join(root, name)
		if !isPlugin(dir) {
			continue
		}
		if p, err := plugin.Load(dir); err == nil && matches(p) {
			return p, nil
		}
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			dir := filepath.Join(root, e.Name())
			if !e.IsDir() || !isPlugin(dir) {
				continue
			}
			if p, err := plugin.Load(dir); err == nil && matches(p) {
				return p, nil
			}
		}
	}
	return nil, fmt.Errorf("Dependency not found: %s (searched dependency_roots: %s)", name, strings.Join(roots, ", "))
}

func isPlugin(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, plugin.Marker))
	return err == nil
}
