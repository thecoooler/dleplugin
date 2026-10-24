package app_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestPackageImportRules(t *testing.T) {
	// go list's Imports excludes _test.go files: service tests may use command
	// helpers, but production code must respect both dependency boundaries.
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = "../.."
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, &stderr)
	}

	const module = "github.com/thecoooler/dleplugin/"
	const internal = module + "internal/"
	const modules = internal + "modules/"
	const mygo = "github.com/egoist/mygo"
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var pkg struct {
			ImportPath string
			Imports    []string
		}
		if err := dec.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode go list: %v", err)
		}
		path := pkg.ImportPath
		command := path == internal+"app" ||
			strings.HasPrefix(path, module+"cmd/") ||
			(strings.HasPrefix(path, modules) && !strings.Contains(strings.TrimPrefix(path, modules), "/"))
		shared := strings.HasPrefix(path, internal) && !strings.HasPrefix(path, modules)
		// The GUI is a frontend: it runs commands through app, without cobra.
		gui := path == internal+"gui" || path == module+"cmd/dleplugin-gui"
		for _, imp := range pkg.Imports {
			if !command && !(gui && imp == internal+"app") && (imp == internal+"app" || imp == internal+"ui" || imp == "github.com/spf13/cobra") {
				t.Errorf("%s: non-command package imports %s", path, imp)
			}
			if shared && strings.HasPrefix(imp, modules) {
				t.Errorf("%s: shared package imports module package %s", path, imp)
			}
			if !gui && strings.HasPrefix(imp, mygo) {
				t.Errorf("%s: only the GUI may import %s", path, imp)
			}
		}
	}
}

func TestCLIDoesNotLinkGUI(t *testing.T) {
	// The CLI and the GUI build separately: the CLI binary must not carry
	// the GUI toolkit.
	cmd := exec.Command("go", "list", "-deps", "./cmd/dleplugin")
	cmd.Dir = "../.."
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, &stderr)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "github.com/egoist/mygo") || dep == "github.com/thecoooler/dleplugin/internal/gui" {
			t.Errorf("cmd/dleplugin depends on %s", dep)
		}
	}
}
