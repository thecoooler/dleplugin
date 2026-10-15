// Package diff compares plugin sources by converting their XML to YAML and
// running the system diff tool.
package diff

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thecoooler/dleplugin/internal/build"
	"github.com/thecoooler/dleplugin/internal/convert"
	"github.com/thecoooler/dleplugin/internal/dlexml"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

var (
	// ErrSourceNotFound means the source argument itself does not exist.
	ErrSourceNotFound = errors.New("source not found")

	slugRe  = regexp.MustCompile(`^slug: .*\n`)
	mountRe = regexp.MustCompile(`\nmount: .*\n`)
)

// ForDiff normalizes an XML file, plugin zip archive, or project directory to
// YAML without derived slug and mount lines. Project XML uses an empty version
// and honors build.split, so separately distributed source files are excluded.
func ForDiff(source string) (string, error) {
	root, err := sourceXML(source)
	if err != nil {
		return "", err
	}
	y, err := convert.FromElement(root, "plugin", "", "")
	if err != nil {
		return "", err
	}
	y = slugRe.ReplaceAllString(y, "")
	return mountRe.ReplaceAllString(y, "\n"), nil
}

func sourceXML(source string) (*dlexml.Element, error) {
	st, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %w", ErrSourceNotFound, err)
		}
		return nil, err
	}
	if st.Mode().IsRegular() {
		return dlexml.ParsePluginFile(source)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("source must be a regular file or project directory")
	}
	p, err := plugin.FromDir(source)
	if err != nil {
		return nil, err
	}
	files, err := p.Files()
	if err != nil {
		return nil, err
	}
	x, err := build.XML(p.Info(), files, "")
	if err != nil {
		return nil, fmt.Errorf("build project XML: %w", err)
	}
	return dlexml.Parse(strings.NewReader(x))
}

// RunYAML compares two normalized YAML texts, streaming the diff output. It
// returns diff's exit code (0 or 1 are not errors).
func RunYAML(ctx context.Context, y1, y2 string, stdout, stderr io.Writer) (int, error) {
	tmp, err := os.MkdirTemp("", "dleplugin-diff-")
	if err != nil {
		return 0, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	one := filepath.Join(tmp, "one.yaml")
	two := filepath.Join(tmp, "two.yaml")
	if err := os.WriteFile(one, []byte(y1), 0o644); err != nil {
		return 0, fmt.Errorf("write %s: %w", one, err)
	}
	if err := os.WriteFile(two, []byte(y2), 0o644); err != nil {
		return 0, fmt.Errorf("write %s: %w", two, err)
	}

	cmd := exec.CommandContext(ctx, "diff", "--color=auto", one, two)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if code := ee.ExitCode(); code == 1 {
			return 1, nil
		} else {
			return code, fmt.Errorf("diff exited with code %d", code)
		}
	}
	return 0, fmt.Errorf("run diff: %w", err)
}
