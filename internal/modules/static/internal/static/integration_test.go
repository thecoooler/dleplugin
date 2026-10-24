package static

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thecoooler/dleplugin/internal/process"
)

// TestDockerSetCookie checks the bundled DLE stub with real PHPStan.
// Opt in with DLEPLUGIN_DOCKER_TEST=1, as for the runtime integration tests.
func TestDockerSetCookie(t *testing.T) {
	if os.Getenv("DLEPLUGIN_DOCKER_TEST") != "1" {
		t.Skip("set DLEPLUGIN_DOCKER_TEST=1 to run real Docker integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	var output bytes.Buffer
	runner := process.NewRunner(process.Options{Out: &output, Err: &output})
	image, err := EnsureImage(ctx, runner, false, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatalf("EnsureImage: %v\n%s", err, &output)
	}
	dir := filepath.Join(root, "plugin")
	write(t, filepath.Join(dir, "src", "cookies.php"), `<?php
set_cookie('toolbar', 'enabled', 365);
set_cookie('toolbar', 'session', 0);
set_cookie('toolbar', '', -1);
`)
	write(t, filepath.Join(dir, "src", "invalid.php"), `<?php
set_cookie('toolbar', 'enabled');
set_cookie([], 'enabled', 365);
echo set_cookie('toolbar', 'enabled', 365);
`)
	ws, err := NewWorkspace(dir, "cookies", image, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"8.4", "7.4"} {
		t.Run(version, func(t *testing.T) {
			phpVersion, err := ParsePHPVersion(version)
			if err != nil {
				t.Fatal(err)
			}
			conf := &PluginConf{Strict: true}
			messages, err := runPHPStan(ctx, runner, ws.Runtime, conf, []string{"src/cookies.php"}, nil, phpVersion)
			if err != nil || len(messages) != 0 {
				t.Fatalf("valid calls: %v, %v\n%s", messages, err, &output)
			}
			messages, err = runPHPStan(ctx, runner, ws.Runtime, conf, []string{"src/invalid.php"}, nil, phpVersion)
			if err != nil || len(messages) != 3 {
				t.Fatalf("invalid calls: %v, %v\n%s", messages, err, &output)
			}
			for i, want := range []string{"invoked with 2 parameters, 3 required", "expects string, array", "Result of function set_cookie (void) is used"} {
				if !strings.Contains(messages[i].Message, want) {
					t.Errorf("message %d = %q, want %q", i, messages[i].Message, want)
				}
			}
		})
	}
}
