package dletest

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thecoooler/dleplugin/internal/plugin"
	"github.com/thecoooler/dleplugin/internal/process"
)

// TestDockerIntegration downloads the official trial and uses real containers.
// Opt in with DLEPLUGIN_DOCKER_TEST=1; no DLE source is kept in the repository.
func TestDockerIntegration(t *testing.T) {
	if os.Getenv("DLEPLUGIN_DOCKER_TEST") != "1" {
		t.Skip("set DLEPLUGIN_DOCKER_TEST=1 to run real Docker integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	fixture, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "..", "testdata", "integration", "docker"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "plugin")
	if err := copyTree(fixture, dir); err != nil {
		t.Fatal(err)
	}
	p, err := plugin.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	var networks []string
	runner := process.NewRunner(process.Options{
		Out: &output, Err: &output,
		OnComplete: func(cmd process.Command, _ *process.Result) {
			if cmd.Name == "docker" && len(cmd.Args) == 3 && cmd.Args[0] == "network" && cmd.Args[1] == "create" {
				networks = append(networks, cmd.Args[2])
			}
		},
	})
	min := 100.0
	o := Options{Plugin: p, Coverage: true, CoverageMin: &min, CoverageHTML: filepath.Join(root, "coverage"), Notify: func(s string) { t.Log(s) }}
	for _, name := range []string{"fresh installation", "cached installation", "failed assertion"} {
		t.Run(name, func(t *testing.T) {
			output.Reset()
			wantCode := 0
			if name == "failed assertion" {
				write(t, filepath.Join(dir, "tests", "FailureTest.php"), "<?php it('fails deliberately', fn () => expect(1)->toBe(2));\n")
				o.Files = []string{"tests/FailureTest.php"}
				o.Coverage, o.CoverageMin, o.CoverageHTML = false, nil, ""
				wantCode = 1
			}
			code, err := Run(ctx, runner, o)
			if err != nil || code != wantCode {
				t.Fatalf("Run = %d, %v; want %d\n%s", code, err, wantCode, &output)
			}
			if wantCode == 0 {
				if _, err := os.Stat(filepath.Join(o.CoverageHTML, "index.html")); err != nil {
					t.Fatalf("HTML coverage missing: %v\n%s", err, &output)
				}
			}
			for _, network := range networks {
				for _, args := range [][]string{
					{"container", "ls", "--all", "--filter", "name=" + network, "--format", "{{.ID}}"},
					{"network", "ls", "--filter", "name=" + network, "--format", "{{.ID}}"},
				} {
					out, err := runner.Output(ctx, process.Command{Name: "docker", Args: args})
					if err != nil || strings.TrimSpace(out) != "" {
						t.Errorf("resources remain for %s: %q, %v", network, out, err)
					}
				}
			}
		})
		if t.Failed() {
			break
		}
	}
}
