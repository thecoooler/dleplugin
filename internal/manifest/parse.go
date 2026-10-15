package manifest

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse decodes and validates dleplugin.yml content.
func Parse(data []byte) (*Info, error) {
	root, err := parseRoot(data)
	if err != nil {
		return nil, err
	}
	d := &decoder{}
	info := d.info(root)
	if err := d.err(); err != nil {
		return nil, err
	}
	return info, nil
}

// ParseFiles decodes only the "files" section of an extra config fragment.
// All other keys are ignored.
func ParseFiles(data []byte) (*OrderedMap[*FilePatch], error) {
	root, err := parseRoot(data)
	if err != nil {
		return nil, err
	}
	d := &decoder{}
	files := d.files(mapGet(root, "files"), "files")
	if err := d.err(); err != nil {
		return nil, err
	}
	return files, nil
}

func parseRoot(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("dleplugin.yml syntax error: %w", err)
	}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	root = deref(root)
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("dleplugin.yml must contain a mapping at the root")
	}
	return root, nil
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// mapGet returns the value for key in a mapping node (last duplicate wins),
// or nil when absent.
func mapGet(m *yaml.Node, key string) *yaml.Node {
	m = deref(m)
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	var found *yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		if deref(m.Content[i]).Value == key {
			found = m.Content[i+1]
		}
	}
	return found
}

func isNull(n *yaml.Node) bool {
	n = deref(n)
	return n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null")
}

type decoder struct {
	issues []string
}

func (d *decoder) fail(path, msg string) {
	d.issues = append(d.issues, path+": "+msg)
}

func (d *decoder) err() error {
	if len(d.issues) == 0 {
		return nil
	}
	return fmt.Errorf("dleplugin.yml validation error: %s", strings.Join(d.issues, "; "))
}

// text reads a scalar as a string. ok is false (and an issue is recorded)
// for null, collections and missing values when required.
func (d *decoder) text(n *yaml.Node, path string, required bool) (string, bool) {
	n = deref(n)
	if n == nil {
		if required {
			d.fail(path, "Field required")
		}
		return "", false
	}
	if n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		d.fail(path, "Input should be a valid string")
		return "", false
	}
	return n.Value, true
}

func (d *decoder) optText(n *yaml.Node, path string) string {
	if n == nil {
		return ""
	}
	s, _ := d.text(n, path, false)
	return s
}

// boolean reads a bool, also accepting yes/no, on/off, y/n, t/f and 1/0.
func (d *decoder) boolean(n *yaml.Node, path string) bool {
	n = deref(n)
	if n == nil {
		return false
	}
	if n.Kind == yaml.ScalarNode && n.Tag != "!!null" {
		switch strings.ToLower(n.Value) {
		case "true", "yes", "on", "y", "t", "1":
			return true
		case "false", "no", "off", "n", "f", "0":
			return false
		}
	}
	d.fail(path, "Input should be a valid boolean")
	return false
}

func trimCode(s string) string { return strings.Trim(s, "\r\n") }

func (d *decoder) info(root *yaml.Node) *Info {
	info := NewInfo()
	info.Slug, _ = d.text(mapGet(root, "slug"), "slug", true)
	if mapGet(root, "slug") != nil {
		if err := ValidateSlug(info.Slug); err != nil {
			d.fail("slug", err.Error())
		}
	}
	info.Name, _ = d.text(mapGet(root, "name"), "name", true)
	info.Description = d.optText(mapGet(root, "description"), "description")
	if n := mapGet(root, "mount"); n != nil {
		info.Mount = NormalizePath(d.optText(n, "mount"))
		if info.Mount != "" {
			if err := ValidateFilePath(info.Mount); err != nil {
				d.fail("mount", err.Error())
			}
		}
	}
	nsNode := mapGet(root, "namespace")
	if nsNode != nil {
		info.Namespace = d.optText(nsNode, "namespace")
		if err := ValidateNamespace(info.Namespace); err != nil {
			d.fail("namespace", err.Error())
		}
	}
	info.Dependencies = d.dependencies(mapGet(root, "dependencies"))
	if b := mapGet(root, "build"); b != nil {
		if n := mapGet(b, "split"); n != nil {
			info.Build.Split = d.boolean(n, "build.split")
		}
	}
	d.extras(mapGet(root, "extras"), &info.Extras)
	d.alias(mapGet(root, "alias"), info)
	info.Files = d.files(mapGet(root, "files"), "files")
	info.MySQL = d.lifecycle(mapGet(root, "mysql"), "mysql")
	info.PHP = d.lifecycle(mapGet(root, "php"), "php")
	d.resolveNamespace(info, nsNode != nil)
	return info
}

// dependencies reads a list of plugin names. DLE stores them comma-separated,
// so a name cannot contain a comma.
func (d *decoder) dependencies(n *yaml.Node) []string {
	n = deref(n)
	if n == nil || isNull(n) {
		return nil
	}
	if n.Kind != yaml.SequenceNode {
		d.fail("dependencies", "Input should be a valid list")
		return nil
	}
	var out []string
	for i, c := range n.Content {
		path := "dependencies." + strconv.Itoa(i)
		s, ok := d.text(c, path, true)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		switch {
		case s == "":
			d.fail(path, "Plugin name must not be empty")
		case strings.Contains(s, ","):
			d.fail(path, "Plugin name must not contain a comma")
		default:
			out = append(out, s)
		}
	}
	return out
}

func (d *decoder) lifecycle(n *yaml.Node, path string) Lifecycle {
	var l Lifecycle
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return l
	}
	read := func(key string) string {
		v := mapGet(n, key)
		if v == nil {
			return ""
		}
		s, _ := d.text(v, path+"."+key, false)
		return trimCode(s)
	}
	l.Install = read("install")
	l.Upgrade = read("upgrade")
	l.Enable = read("enable")
	l.Disable = read("disable")
	l.Delete = read("delete")
	return l
}

func (d *decoder) alias(n *yaml.Node, info *Info) {
	n = deref(n)
	if n == nil || isNull(n) {
		return
	}
	if n.Kind != yaml.MappingNode {
		d.fail("alias", "Input should be a valid dictionary")
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := deref(n.Content[i]).Value
		v, _ := d.text(n.Content[i+1], "alias."+k, false)
		key, value := NormalizePath(k), NormalizePath(v)
		if err := ValidateFilePath(key); err != nil {
			d.fail("alias."+k, err.Error())
		}
		if err := ValidateFilePath(value); err != nil {
			d.fail("alias."+k, err.Error())
		}
		info.Alias.Set(key, value)
	}
}

func (d *decoder) extras(n *yaml.Node, e *Extras) {
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return
	}
	if v := mapGet(n, "admin_section"); v != nil {
		e.AdminSection = d.boolean(v, "extras.admin_section")
	}
	if v := mapGet(n, "autoloader"); v != nil {
		e.Autoloader = autoloader(v)
	}
	if v := deref(mapGet(n, "configs")); v != nil && !isNull(v) {
		if v.Kind != yaml.SequenceNode {
			d.fail("extras.configs", "Input should be a valid list")
		} else {
			for i, c := range v.Content {
				if s, ok := d.text(c, "extras.configs."+strconv.Itoa(i), false); ok {
					e.Configs = append(e.Configs, s)
				}
			}
		}
	}
}

func autoloader(n *yaml.Node) [2]bool {
	var out [2]bool
	n = deref(n)
	if n.Kind == yaml.SequenceNode {
		for i, item := range n.Content {
			if i > 1 {
				break
			}
			out[i] = truthy(item)
		}
		return out
	}
	if truthy(n) {
		out = [2]bool{true, true}
	}
	return out
}

// truthy reports whether a YAML value is non-empty and not false or zero.
func truthy(n *yaml.Node) bool {
	n = deref(n)
	if n == nil {
		return false
	}
	switch n.Kind {
	case yaml.SequenceNode, yaml.MappingNode:
		return len(n.Content) > 0
	case yaml.ScalarNode:
		var v any
		if err := n.Decode(&v); err != nil {
			return n.Value != ""
		}
		switch x := v.(type) {
		case nil:
			return false
		case bool:
			return x
		case int:
			return x != 0
		case int64:
			return x != 0
		case uint64:
			return x != 0
		case float64:
			return x != 0
		case string:
			if n.Style == 0 {
				switch strings.ToLower(x) {
				case "no", "off", "n":
					return false
				case "yes", "on", "y":
					return true
				}
			}
			return x != ""
		}
		return true
	}
	return false
}

func (d *decoder) files(n *yaml.Node, path string) *OrderedMap[*FilePatch] {
	files := NewOrderedMap[*FilePatch]()
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return files
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		name := deref(n.Content[i]).Value
		p := path + "." + name
		if err := ValidateFilePath(name); err != nil {
			d.fail(p, err.Error())
		}
		patch := &FilePatch{Name: name, Actions: []Action{}}
		list := deref(n.Content[i+1])
		if list == nil || list.Kind != yaml.SequenceNode {
			d.fail(p, "Input should be a valid list")
			continue
		}
		for j, item := range list.Content {
			if a, ok := d.action(item, p+"."+strconv.Itoa(j)); ok {
				patch.Actions = append(patch.Actions, a)
			}
		}
		files.Set(name, patch)
	}
	return files
}

func (d *decoder) action(n *yaml.Node, path string) (Action, bool) {
	n = deref(n)
	if n.Kind != yaml.MappingNode || len(n.Content) < 2 {
		d.fail(path, "action must be a non-empty mapping")
		return Action{}, false
	}
	name := deref(n.Content[0]).Value
	payload := n.Content[1]
	p := path + "." + name
	if name == "create" {
		s, ok := d.text(payload, p+".replace", false)
		return Action{Name: name, Replace: trimCode(s)}, ok
	}
	payload = deref(payload)
	if payload.Kind != yaml.MappingNode {
		d.fail(p, "Input should be a valid dictionary")
		return Action{}, false
	}
	search, ok1 := d.text(mapGet(payload, "search"), p+".search", true)
	replace, ok2 := d.text(mapGet(payload, "replace"), p+".replace", true)
	return Action{Name: name, Search: trimCode(search), Replace: trimCode(replace)}, ok1 && ok2
}
