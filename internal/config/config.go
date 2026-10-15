// Package config reads the global dleplugin configuration file and offers
// helpers for modules that keep typed sections or their own YAML files.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrNotFound matches errors caused by a missing config file.
var ErrNotFound = errors.New("config file not found")

// Error is a user-facing configuration error.
type Error struct {
	// Path is the config file the error refers to.
	Path string
	// Message is the complete human-readable message.
	Message string

	notFound bool
}

// Error returns the message.
func (e *Error) Error() string { return e.Message }

// Is reports whether target is ErrNotFound and the file was missing.
func (e *Error) Is(target error) bool { return e.notFound && target == ErrNotFound }

func newErr(path, format string, args ...any) *Error {
	return &Error{Path: path, Message: fmt.Sprintf(format, args...)}
}

func notFoundErr(path string) *Error {
	e := newErr(path, "Config file not found: %s", path)
	e.notFound = true
	return e
}

// DefaultPath returns ~/.config/dleplugin/config.yml.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "dleplugin", "config.yml"), nil
}

// Core holds the settings required by the core commands.
type Core struct {
	HostDir string `yaml:"host_dir"`
}

// DefaultHostPath returns HostDir/dle.localhost.
func (c *Core) DefaultHostPath() string { return filepath.Join(c.HostDir, "dle.localhost") }

// Store lazily reads and caches the global config document.
type Store struct {
	path   string
	loaded bool
	exists bool
	root   *yaml.Node // mapping node; nil when the file is missing
	err    error
}

// NewStore creates a Store for the config file at path.
func NewStore(path string) *Store { return &Store{path: path} }

// Path returns the config file path.
func (s *Store) Path() string { return s.path }

func (s *Store) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	root, exists, err := readMapping(s.path)
	s.root, s.exists, s.err = root, exists, err
}

// readMapping reads a YAML file whose root must be a mapping. A missing file
// returns exists=false; an empty file returns an empty mapping.
func readMapping(path string) (*yaml.Node, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, newErr(path, "Cannot read config file %s: %v", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, true, newErr(path, "Invalid YAML in config file %s: %v", path, err)
	}
	if doc.Kind == 0 {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, true, nil
	}
	n := &doc
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, true, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, true, newErr(path, "Invalid config file %s: root must be a mapping", path)
	}
	return n, true, nil
}

func (s *Store) get() (*yaml.Node, error) {
	s.load()
	if s.err != nil {
		return nil, s.err
	}
	if !s.exists {
		return nil, notFoundErr(s.path)
	}
	return s.root, nil
}

// lookup finds key in a mapping node, resolving aliases.
func lookup(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			v := m.Content[i+1]
			for v.Kind == yaml.AliasNode && v.Alias != nil {
				v = v.Alias
			}
			return v
		}
	}
	return nil
}

func isNull(n *yaml.Node) bool {
	return n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null")
}

func fillMessage(path string, keys []string) string {
	return fmt.Sprintf("Fill required configuration values in %s:\n- %s", path, strings.Join(keys, "\n- "))
}

// Core returns the core settings. When the file is missing and createIfMissing
// is set, a template is written and an *Error describing it is returned.
func (s *Store) Core(createIfMissing bool) (*Core, error) {
	s.load()
	if s.err != nil {
		return nil, s.err
	}
	if !s.exists {
		if !createIfMissing {
			return nil, notFoundErr(s.path)
		}
		if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
			return nil, newErr(s.path, "Cannot create config file %s: %v", s.path, err)
		}
		if err := os.WriteFile(s.path, []byte("host_dir: null\n"), 0o644); err != nil {
			return nil, newErr(s.path, "Cannot create config file %s: %v", s.path, err)
		}
		s.loaded = false
		return nil, newErr(s.path, "Config file was created at %s.\n%s", s.path, fillMessage(s.path, []string{"host_dir"}))
	}
	n := lookup(s.root, "host_dir")
	if isNull(n) {
		return nil, newErr(s.path, "%s", fillMessage(s.path, []string{"host_dir"}))
	}
	var c Core
	if err := n.Decode(&c.HostDir); err != nil {
		return nil, newErr(s.path, "Invalid config file %s: host_dir: %v", s.path, err)
	}
	if c.HostDir == "" {
		return nil, newErr(s.path, "%s", fillMessage(s.path, []string{"host_dir"}))
	}
	return &c, nil
}

// Raw returns the whole document as a generic map. The bool is false when the
// file does not exist.
func (s *Store) Raw() (map[string]any, bool, error) {
	s.load()
	if s.err != nil {
		return nil, s.exists, s.err
	}
	if !s.exists {
		return nil, false, nil
	}
	m := map[string]any{}
	if err := s.root.Decode(&m); err != nil {
		return nil, true, newErr(s.path, "Invalid config file %s: %v", s.path, err)
	}
	return m, true, nil
}

// Section decodes the named top-level section over out, which must be a
// pointer to a struct pre-filled with defaults. Fields tagged
// `required:"true"` must be present and non-null.
func (s *Store) Section(name string, out any) error {
	root, err := s.get()
	if err != nil {
		return err
	}
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("config section %q: out must be a non-nil pointer to a struct", name)
	}
	sec := lookup(root, name)
	if !isNull(sec) {
		if sec.Kind != yaml.MappingNode {
			return newErr(s.path, "Invalid config file %s: %s must be a mapping", s.path, name)
		}
		if err := sec.Decode(out); err != nil {
			return newErr(s.path, "Invalid config file %s: %v", s.path, err)
		}
	} else {
		sec = nil
	}
	missing := missingRequired(rv.Elem().Type(), sec, name)
	if len(missing) > 0 {
		sort.Strings(missing)
		return newErr(s.path, "%s", fillMessage(s.path, missing))
	}
	return nil
}

func missingRequired(t reflect.Type, m *yaml.Node, prefix string) []string {
	var missing []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		key := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if key == "-" {
			continue
		}
		if key == "" {
			key = strings.ToLower(f.Name)
		}
		child := lookup(m, key)
		full := prefix + "." + key
		if f.Tag.Get("required") == "true" && isNull(child) {
			missing = append(missing, full)
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Struct {
			if !isNull(child) && child.Kind != yaml.MappingNode {
				child = nil
			}
			missing = append(missing, missingRequired(ft, child, full)...)
		}
	}
	return missing
}

// SaveYAMLFile writes v as YAML (2-space indent) to path, creating parent
// directories.
func SaveYAMLFile(path string, v any, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(sb.String()), perm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
