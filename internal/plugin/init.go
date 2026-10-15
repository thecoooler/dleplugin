package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/thecoooler/dleplugin/internal/manifest"
)

// Init writes a new dleplugin.yml into dir, creating dir when it is
// missing, and loads the plugin. An empty namespace defaults to the slug.
func Init(dir, slug, name, description, namespace string) (*Plugin, error) {
	if err := manifest.ValidateSlug(slug); err != nil {
		return nil, err
	}
	if namespace == "" {
		if err := manifest.ValidateNamespace(slug); err != nil {
			return nil, fmt.Errorf("%w: the slug is not a namespace, set one explicitly", err)
		}
		namespace = slug
	}
	if err := manifest.ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, Marker)
	if _, err := os.Lstat(path); err == nil {
		return nil, errors.New("dleplugin.yml already exists")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create directory %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(renderManifest(slug, name, description, namespace)), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", Marker, err)
	}
	return Load(dir)
}

// renderManifest renders the default manifest.
func renderManifest(slug, name, description, namespace string) string {
	var b strings.Builder
	b.WriteString("files: {}\n")
	b.WriteString("slug: " + manifest.YAMLScalar(slug) + "\n")
	b.WriteString("name: " + manifest.YAMLScalar(name) + "\n")
	b.WriteString("description: " + manifest.YAMLScalar(description) + "\n")
	b.WriteString("namespace: " + manifest.YAMLScalar(namespace) + "\n")
	b.WriteString("mount: " + manifest.YAMLScalar(manifest.NamespaceMount(namespace)) + "\n")
	b.WriteString(`build:
  split: false
extras:
  admin_section: false
  autoloader:
  - false
  - false
  configs: []
alias: {}
`)
	for _, section := range []string{"mysql", "php"} {
		b.WriteString(section + ":\n")
		for _, hook := range []string{"install", "upgrade", "enable", "disable", "delete"} {
			b.WriteString("  " + hook + ": ''\n")
		}
	}
	return b.String()
}
