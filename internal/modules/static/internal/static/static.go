// Package static runs PHP static analysis (PHPStan) for a plugin inside
// Docker and resolves the dependency include paths.
package static

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/thecoooler/dleplugin/internal/config"
	"github.com/thecoooler/dleplugin/internal/process"
)

// ConfFile is the per-plugin static analysis config.
const ConfFile = "dlestatic.yml"

// DefaultIgnoreErrors are message patterns that are always ignored.
var DefaultIgnoreErrors = []string{
	"Call to method .* of internal class .* from outside its root namespace Pest",
}

// Config is the "static" section of the global config.
type Config struct {
	Dependencies    map[string]string `yaml:"dependencies"`
	DependencyRoots []string          `yaml:"dependency_roots"`
}

// LoadConfig reads the "static" section. A missing config file yields
// defaults. dependency_roots falls back to the top-level key of the same name.
func LoadConfig(s *config.Store) (*Config, error) {
	c := &Config{}
	err := s.Section("static", c)
	if errors.Is(err, config.ErrNotFound) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if c.DependencyRoots != nil {
		return c, nil
	}
	raw, _, err := s.Raw()
	if err != nil {
		return nil, err
	}
	v, ok := raw["dependency_roots"]
	if !ok || v == nil {
		return c, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("Invalid config file %s: dependency_roots must be a list of paths", s.Path())
	}
	for _, item := range list {
		p, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("Invalid config file %s: dependency_roots must be a list of paths", s.Path())
		}
		c.DependencyRoots = append(c.DependencyRoots, p)
	}
	return c, nil
}

// PluginConf is the plugin's dlestatic.yml.
type PluginConf struct {
	Dependencies []string      `yaml:"dependencies"`
	ExcludePaths *ExcludePaths `yaml:"excludePaths"`
	Strict       bool          `yaml:"strict"`
	IgnoreErrors []string      `yaml:"ignoreErrors"`
}

// ExcludePaths mirrors PHPStan's excludePaths parameter.
type ExcludePaths struct {
	Analyse []string `yaml:"analyse"`
}

// LoadPluginConf reads root/dlestatic.yml; a missing file yields defaults.
func LoadPluginConf(root string) (*PluginConf, error) {
	c := &PluginConf{}
	data, err := os.ReadFile(filepath.Join(root, ConfFile))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ConfFile, err)
	}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("%s validation error: %w", ConfFile, err)
	}
	if c.ExcludePaths != nil && c.ExcludePaths.Analyse == nil {
		return nil, fmt.Errorf("%s validation error: excludePaths.analyse is required", ConfFile)
	}
	return c, nil
}

// ReadSlug returns the slug from root/dleplugin.yml without validating the
// rest of the manifest.
func ReadSlug(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "dleplugin.yml"))
	if err != nil {
		return "", fmt.Errorf("read dleplugin.yml: %w", err)
	}
	var m struct {
		Slug string `yaml:"slug"`
	}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("parse dleplugin.yml: %w", err)
	}
	if m.Slug == "" {
		return "", errors.New("dleplugin.yml has no slug")
	}
	return m.Slug, nil
}

// IncludePaths returns the src/ directory of every dependency. A dependency is looked up in deps by name,
// then as <root>/<name> in roots; unresolved ones are reported as warnings.
func IncludePaths(conf *PluginConf, deps map[string]string, roots []string) (paths, warnings []string) {
	for _, dep := range conf.Dependencies {
		dir, ok := deps[dep]
		if !ok {
			for _, root := range roots {
				candidate := filepath.Join(root, dep)
				if _, err := os.Stat(candidate); err == nil {
					dir, ok = candidate, true
					break
				}
			}
		}
		if !ok {
			warnings = append(warnings, "Dependency not found: "+dep)
			continue
		}
		paths = append(paths, filepath.Join(dir, "src"))
	}
	return paths, warnings
}

// Message is a single analysis finding.
type Message struct {
	Provider string  `json:"provider"`
	File     string  `json:"file"`
	Line     *int    `json:"line"`
	Message  string  `json:"message"`
	Tip      *string `json:"tip"`
}

// FilterIgnored drops messages matching DefaultIgnoreErrors or the plugin's
// ignoreErrors (unanchored regular expressions).
func FilterIgnored(messages []Message, conf *PluginConf) ([]Message, error) {
	var res []*regexp.Regexp
	for _, p := range append(append([]string(nil), DefaultIgnoreErrors...), conf.IgnoreErrors...) {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid ignoreErrors pattern %q in %s: %w", p, ConfFile, err)
		}
		res = append(res, re)
	}
	out := messages[:0:0]
	for _, m := range messages {
		ignored := false
		for _, re := range res {
			if re.MatchString(m.Message) {
				ignored = true
				break
			}
		}
		if !ignored {
			out = append(out, m)
		}
	}
	return out, nil
}

// Options configures Run.
type Options struct {
	// PluginDir is the plugin root.
	PluginDir string
	// Files are paths relative to PluginDir ("src" when empty).
	Files []string
	// InvocationDir resolves relative include paths.
	InvocationDir   string
	Dependencies    map[string]string
	DependencyRoots []string
	// PHPVersion is the PHP version to analyse for in PHPStan's format
	// (80100 for 8.1, see ParsePHPVersion); 0 uses the container's PHP.
	PHPVersion int
	// StreamBuild shows docker build output.
	StreamBuild bool
	// Notify receives progress messages (image build); may be nil.
	Notify func(string)
}

// Result is the outcome of an analysis run.
type Result struct {
	Messages []Message
	// Errors maps a failed provider to its error.
	Errors   map[string]string
	Warnings []string
}

// Run analyses the plugin with PHPStan in Docker. Provider failures are
// reported in Result.Errors; setup failures are returned.
func Run(ctx context.Context, runner *process.Runner, o Options) (*Result, error) {
	slug, err := ReadSlug(o.PluginDir)
	if err != nil {
		return nil, err
	}
	conf, err := LoadPluginConf(o.PluginDir)
	if err != nil {
		return nil, err
	}
	includes, warnings := IncludePaths(conf, o.Dependencies, o.DependencyRoots)
	image, err := EnsureImage(ctx, runner, o.StreamBuild, o.Notify)
	if err != nil {
		return nil, err
	}
	ws, err := NewWorkspace(o.PluginDir, slug, image, includes, o.InvocationDir)
	if err != nil {
		return nil, err
	}

	files := o.Files
	if len(files) == 0 {
		files = []string{"src"}
	}
	res := &Result{Errors: map[string]string{}, Warnings: append(warnings, ws.Warnings...)}
	messages, err := runPHPStan(ctx, runner, ws.Runtime, conf, files, ws.IncludePaths, o.PHPVersion)
	if err != nil {
		res.Errors["PHPStanProvider"] = err.Error()
	}
	ws.Runtime.NormalizePaths(messages, o.PluginDir)
	if res.Messages, err = FilterIgnored(messages, conf); err != nil {
		return nil, err
	}
	return res, nil
}

// Group is the messages reported for one file and line.
type Group struct {
	File     string
	Line     *int
	Messages []Message
}

// GroupMessages groups messages by file (in order of first appearance) and
// line (in order of first appearance within the file).
func GroupMessages(messages []Message) [][]Group {
	var files [][]Group
	fileIdx := map[string]int{}
	for _, m := range messages {
		fi, ok := fileIdx[m.File]
		if !ok {
			fi = len(files)
			fileIdx[m.File] = fi
			files = append(files, nil)
		}
		groups := files[fi]
		found := false
		for i := range groups {
			if sameLine(groups[i].Line, m.Line) {
				groups[i].Messages = append(groups[i].Messages, m)
				found = true
				break
			}
		}
		if !found {
			groups = append(groups, Group{File: m.File, Line: m.Line, Messages: []Message{m}})
		}
		files[fi] = groups
	}
	return files
}

func sameLine(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// SortLinesDesc orders a file's groups by line, highest first (unknown lines
// last).
func SortLinesDesc(groups []Group) []Group {
	out := append([]Group(nil), groups...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Line, out[j].Line
		if a == nil || b == nil {
			return a != nil
		}
		return *a > *b
	})
	return out
}
