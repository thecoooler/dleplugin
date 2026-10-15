package dockerimg

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/process"
)

func TestEnsure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake docker")
	}
	for _, tc := range []struct {
		name                  string
		missing, fail, stream bool
	}{
		{name: "cached"}, {name: "build", missing: true}, {name: "stream", missing: true, stream: true}, {name: "failure", missing: true, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_LOG"
case "$1" in
 image) exit "$INSPECT_EXIT" ;;
 build) cat > "$BUILD_CONTEXT"; printf 'build output'; printf 'build diagnostic' >&2; exit "$BUILD_EXIT" ;;
esac
exit 2
`
			bin := filepath.Join(dir, "docker")
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			log, archive := filepath.Join(dir, "log"), filepath.Join(dir, "context.tar")
			t.Setenv("DOCKER_LOG", log)
			t.Setenv("BUILD_CONTEXT", archive)
			t.Setenv("INSPECT_EXIT", "0")
			if tc.missing {
				t.Setenv("INSPECT_EXIT", "1")
			}
			t.Setenv("BUILD_EXIT", "0")
			if tc.fail {
				t.Setenv("BUILD_EXIT", "12")
			}
			img := Image{Name: "tool", Title: "test", Dockerfile: []byte("FROM scratch\n"), Files: map[string][]byte{"z.lock": []byte("locked"), "a.json": []byte("{}")}}
			var out, stderr bytes.Buffer
			runner := process.NewRunner(process.Options{Out: &out, Err: &stderr})
			var notices []string
			tag, err := Ensure(context.Background(), runner, img, tc.stream, func(s string) { notices = append(notices, s) })
			if tc.fail {
				if err == nil || !strings.Contains(err.Error(), "Failed to build Docker image "+img.Tag()) || !strings.Contains(err.Error(), "build diagnostic") {
					t.Fatalf("build error = %v", err)
				}
			} else if err != nil || tag != img.Tag() {
				t.Fatalf("Ensure = %q, %v", tag, err)
			}
			b, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if lines[0] != "image inspect "+img.Tag() {
				t.Errorf("inspect = %q", lines[0])
			}
			if !tc.missing {
				if len(lines) != 1 || len(notices) != 0 {
					t.Errorf("cached image did work: %q, %q", lines, notices)
				}
				if _, err := os.Stat(archive); !os.IsNotExist(err) {
					t.Errorf("cached image sent a build context: %v", err)
				}
				// Nil callbacks are supported too.
				if _, err := Ensure(context.Background(), runner, img, false, nil); err != nil {
					t.Fatal(err)
				}
				return
			}
			if len(lines) != 2 || lines[1] != "build --tag "+img.Tag()+" -" {
				t.Errorf("build commands = %q", lines)
			}
			got, err := os.ReadFile(archive)
			if err != nil {
				t.Fatal(err)
			}
			want, err := img.buildContext()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Error("Docker stdin differs from deterministic tar context")
			}
			expected := []string{"Building test image " + img.Tag() + "..."}
			if !tc.fail {
				expected = append(expected, "Test image built successfully.")
			}
			if !reflect.DeepEqual(notices, expected) {
				t.Errorf("notifications = %q, want %q", notices, expected)
			}
			if tc.stream && (out.String() != "build output" || stderr.String() != "build diagnostic") {
				t.Errorf("stream = %q, %q", out.String(), stderr.String())
			}
		})
	}
}

func TestEnsureDockerUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Ensure(context.Background(), process.NewRunner(process.Options{}), Image{}, false, nil)
	if err == nil || !strings.Contains(err.Error(), "Docker not found") {
		t.Errorf("error = %v", err)
	}
}
