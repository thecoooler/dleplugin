package static

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/thecoooler/dleplugin/internal/process"
)

type phpstanConf struct {
	Parameters phpstanParams `yaml:"parameters"`
	Includes   []string      `yaml:"includes"`
}

type phpstanParams struct {
	Level           string   `yaml:"level"`
	Paths           []string `yaml:"paths"`
	TmpDir          string   `yaml:"tmpDir"`
	BootstrapFiles  []string `yaml:"bootstrapFiles"`
	ScanDirectories []string `yaml:"scanDirectories"`
	ExcludePaths    any      `yaml:"excludePaths"`
	PHPVersion      int      `yaml:"phpVersion,omitempty"`
}

// writePHPStanConf writes workDir/phpstan.neon (YAML is valid NEON).
// phpVersion is PHPStan's phpVersion (80100 for 8.1); 0 keeps the container's.
func writePHPStanConf(workDir string, conf *PluginConf, includes []string, phpVersion int) error {
	c := phpstanConf{
		Parameters: phpstanParams{
			Level:           "max",
			Paths:           []string{"src"},
			TmpDir:          "./.phpstan",
			BootstrapFiles:  []string{path.Join(containerToolsDir, "bootstrap.php")},
			ScanDirectories: append([]string{}, includes...),
			ExcludePaths:    []string{},
			PHPVersion:      phpVersion,
		},
		Includes: []string{"phar://phpstan.phar/conf/bleedingEdge.neon"},
	}
	if conf.ExcludePaths != nil {
		c.Parameters.ExcludePaths = conf.ExcludePaths
	}
	if conf.Strict {
		c.Includes = append(c.Includes, path.Join(containerToolsDir, "vendor/phpstan/phpstan-strict-rules/rules.neon"))
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return err
	}
	p := filepath.Join(workDir, "phpstan.neon")
	if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	return os.WriteFile(p, buf.Bytes(), 0o644)
}

// runPHPStan analyses files (relative to the plugin root) and returns the
// findings with container paths.
func runPHPStan(ctx context.Context, runner *process.Runner, rt *Runtime, conf *PluginConf, files, includes []string, phpVersion int) ([]Message, error) {
	if err := writePHPStanConf(rt.WorkDir, conf, includes, phpVersion); err != nil {
		return nil, fmt.Errorf("write phpstan.neon: %w", err)
	}
	args := append([]string{
		path.Join(containerToolsDir, "vendor", "bin", "phpstan"),
		"analyze", "--memory-limit", "512M", "--error-format=json", "--no-progress",
		"-c", "phpstan.neon",
	}, files...)
	out, err := runTool(ctx, runner, rt, args)
	if err != nil {
		return nil, err
	}
	return parsePHPStan(out, args)
}

// ParsePHPVersion converts "8.1" or "8.1.2" to PHPStan's phpVersion number
// (80100, 80102).
func ParsePHPVersion(s string) (int, error) {
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("invalid PHP version %q: expected MAJOR.MINOR or MAJOR.MINOR.PATCH", s)
	}
	limits := []int{9, 99, 99}
	v := 0
	for i := range 3 {
		n := 0
		if i < len(parts) {
			var err error
			if n, err = strconv.Atoi(parts[i]); err != nil || n < 0 || n > limits[i] || parts[i] != strconv.Itoa(n) {
				return 0, fmt.Errorf("invalid PHP version %q: expected MAJOR.MINOR or MAJOR.MINOR.PATCH", s)
			}
		}
		v = v*100 + n
	}
	if v < 70000 {
		return 0, fmt.Errorf("invalid PHP version %q: PHP 7.0 or later is required", s)
	}
	return v, nil
}

// runTool runs args in the container. Exit codes 0 and 1 (findings) are
// accepted, but stderr output with exit code 0 is treated as a failure.
func runTool(ctx context.Context, runner *process.Runner, rt *Runtime, args []string) (*process.Result, error) {
	argv := rt.Command(args)
	res, err := runner.Run(ctx, process.Command{Name: argv[0], Args: argv[1:]})
	var ee *process.ExitError
	switch {
	case errors.As(err, &ee) && res.ExitCode == 1:
		return res, nil
	case err != nil:
		return nil, err
	case len(res.Stderr) > 0:
		return nil, &process.ExitError{CmdLine: strings.Join(argv, " "), Result: res}
	}
	return res, nil
}

type phpstanFile struct {
	Messages []struct {
		Message string  `json:"message"`
		Line    *int    `json:"line"`
		Tip     *string `json:"tip"`
	} `json:"messages"`
}

// parsePHPStan reads PHPStan's JSON report keeping the file order.
func parsePHPStan(res *process.Result, args []string) ([]Message, error) {
	invalid := func() error {
		return fmt.Errorf("Command returned invalid JSON: %s\nSTDOUT: %s\nSTDERR: %s",
			strings.Join(args, " "), res.Stdout, res.Stderr)
	}
	var report struct {
		Files json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(res.Stdout, &report); err != nil {
		return nil, invalid()
	}
	files := bytes.TrimSpace(report.Files)
	if len(files) == 0 || files[0] != '{' {
		return nil, nil // absent, null or [] when there are no findings
	}
	dec := json.NewDecoder(bytes.NewReader(files))
	if _, err := dec.Token(); err != nil {
		return nil, invalid()
	}
	var out []Message
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, invalid()
		}
		name, _ := tok.(string)
		var f phpstanFile
		if err := dec.Decode(&f); err != nil {
			return nil, invalid()
		}
		for _, m := range f.Messages {
			out = append(out, Message{Provider: "phpstan", File: name, Line: m.Line, Message: m.Message, Tip: m.Tip})
		}
	}
	return out, nil
}
