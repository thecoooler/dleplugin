package dockerimg

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thecoooler/dleplugin/internal/process"
)

func TestEnsure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake docker")
	}
	img := Image{Name: "tool", Title: "test", Dockerfile: []byte("FROM scratch\n"), Files: map[string][]byte{"z.lock": []byte("locked"), "a.json": []byte("{}")}}
	remote := "registry.test/owner/" + img.Tag()
	inspect := "image inspect " + img.Tag()
	list := "image ls --format " + tagsFormat
	pull, tag, build := "pull "+remote, "tag "+remote+" "+img.Tag(), "build --tag "+img.Tag()+" -"
	untag := "image rm " + remote
	pulling := "Pulling test image " + remote + "..."
	fallback := "Could not pull " + remote + " (pull denied); building it locally."
	building := "Building test image " + img.Tag() + "..."
	for _, tc := range []struct {
		name                       string
		missing, noPull, fail      bool
		stream, disabled, noRemote bool
		commands, notices          []string
	}{
		{name: "cached", commands: []string{inspect, list}},
		{name: "pulled", missing: true, commands: []string{inspect, pull, tag, untag, list}, notices: []string{pulling, "Test image pulled."}},
		{name: "build", missing: true, noPull: true, commands: []string{inspect, pull, build, list}, notices: []string{pulling, fallback, building, "Test image built successfully."}},
		{name: "stream", missing: true, noPull: true, stream: true, commands: []string{inspect, pull, build, list}, notices: []string{pulling, "Could not pull " + remote + "; building it locally.", building, "Test image built successfully."}},
		{name: "disabled", missing: true, disabled: true, commands: []string{inspect, build, list}, notices: []string{building, "Test image built successfully."}},
		{name: "failure", missing: true, noPull: true, fail: true, commands: []string{inspect, pull, build}, notices: []string{pulling, fallback, building}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_LOG"
case "$1" in
 image) case "$2" in
  inspect) exit "$INSPECT_EXIT" ;;
  ls) printf '%s' "${IMAGE_LIST:-}"; exit 0 ;;
  rm) exit 0 ;;
 esac; exit 2 ;;
 ps) printf '%s' "${PS_OUT:-}" ;;
 pull) printf 'pull output'; printf 'pull progress\npull denied\n' >&2; exit "$PULL_EXIT" ;;
 tag) exit 0 ;;
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
			t.Setenv(RegistryEnv, "registry.test/owner/")
			if tc.disabled {
				t.Setenv(RegistryEnv, "")
			}
			t.Setenv("INSPECT_EXIT", "0")
			if tc.missing {
				t.Setenv("INSPECT_EXIT", "1")
			}
			t.Setenv("PULL_EXIT", "0")
			if tc.noPull {
				t.Setenv("PULL_EXIT", "1")
			}
			t.Setenv("BUILD_EXIT", "0")
			if tc.fail {
				t.Setenv("BUILD_EXIT", "12")
			}
			var out, stderr bytes.Buffer
			runner := process.NewRunner(process.Options{Out: &out, Err: &stderr})
			var notices []string
			got, err := Ensure(context.Background(), runner, img, tc.stream, func(s string) { notices = append(notices, s) })
			if tc.fail {
				if err == nil || !strings.Contains(err.Error(), "Failed to build Docker image "+img.Tag()) || !strings.Contains(err.Error(), "build diagnostic") {
					t.Fatalf("build error = %v", err)
				}
			} else if err != nil || got != img.Tag() {
				t.Fatalf("Ensure = %q, %v", got, err)
			}
			b, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); !reflect.DeepEqual(lines, tc.commands) {
				t.Errorf("commands = %q, want %q", lines, tc.commands)
			}
			if !reflect.DeepEqual(notices, tc.notices) {
				t.Errorf("notifications = %q, want %q", notices, tc.notices)
			}
			sent, err := os.ReadFile(archive)
			if !slices.Contains(tc.commands, build) {
				if !os.IsNotExist(err) {
					t.Errorf("no build expected, context: %v", err)
				}
				if !tc.missing {
					// Nil callbacks are supported too.
					if _, err := Ensure(context.Background(), runner, img, false, nil); err != nil {
						t.Fatal(err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want, err := img.buildContext()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(sent, want) {
				t.Error("Docker stdin differs from deterministic tar context")
			}
			if tc.stream && (out.String() != "pull outputbuild output" || stderr.String() != "pull progress\npull denied\nbuild diagnostic") {
				t.Errorf("stream = %q, %q", out.String(), stderr.String())
			}
		})
	}
}

func TestEnsurePullTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake docker")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n image) exit 1 ;;\n pull) exec sleep 5 ;;\n build) cat > /dev/null; exit 0 ;;\nesac\nexit 2\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(RegistryEnv, "registry.test")
	defer func(d time.Duration) { pullTimeout = d }(pullTimeout)
	pullTimeout = 50 * time.Millisecond
	img := Image{Name: "tool", Title: "test", Dockerfile: []byte("FROM scratch\n")}
	var notices []string
	tag, err := Ensure(context.Background(), process.NewRunner(process.Options{}), img, false, func(s string) { notices = append(notices, s) })
	if err != nil || tag != img.Tag() {
		t.Fatalf("Ensure = %q, %v", tag, err)
	}
	if len(notices) < 2 || !strings.HasPrefix(notices[1], "Could not pull registry.test/"+img.Tag()) {
		t.Errorf("notifications = %q", notices)
	}
}

func TestRemote(t *testing.T) {
	img := Image{Name: "tool", Dockerfile: []byte("FROM scratch\n")}
	t.Setenv(RegistryEnv, "")
	os.Unsetenv(RegistryEnv)
	if got := img.Remote(); got != Registry+"/"+img.Tag() {
		t.Errorf("default remote = %q", got)
	}
	t.Setenv(RegistryEnv, "mirror.test/tools/")
	if got := img.Remote(); got != "mirror.test/tools/"+img.Tag() {
		t.Errorf("override remote = %q", got)
	}
	t.Setenv(RegistryEnv, "")
	if got := img.Remote(); got != "" {
		t.Errorf("disabled remote = %q", got)
	}
}

func TestWriteContext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ctx")
	img := Image{Name: "tool", Dockerfile: []byte("FROM scratch\n"), Files: map[string][]byte{"composer.lock": []byte("locked")}}
	if err := img.WriteContext(dir); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"Dockerfile": "FROM scratch\n", "composer.lock": "locked"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != want {
			t.Errorf("%s = %q, %v", name, b, err)
		}
	}
}

func TestEnsureDockerUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Ensure(context.Background(), process.NewRunner(process.Options{}), Image{}, false, nil)
	if err == nil || !strings.Contains(err.Error(), "Docker not found") {
		t.Errorf("error = %v", err)
	}
}
