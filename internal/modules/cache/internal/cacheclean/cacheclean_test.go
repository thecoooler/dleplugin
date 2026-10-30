package cacheclean

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thecoooler/dleplugin/internal/process"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func atime(t *testing.T, path string, mod time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func entryNames(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = string(e.Kind) + " " + e.Name
	}
	return out
}

// layout creates run sites, bases and static work directories under root and
// returns the current base path. Mtimes are set relative to now.
func layout(t *testing.T, root string, now time.Time) string {
	t.Helper()
	write(t, filepath.Join(root, "test", "run", "demo-abc", "index.php"), "0123456789")
	write(t, filepath.Join(root, "test", "run", "other", "index.php"), strings.Repeat("x", 20))
	atime(t, filepath.Join(root, "test", "run", "demo-abc"), now.Add(-time.Hour))
	atime(t, filepath.Join(root, "test", "run", "other"), now.Add(-time.Hour))

	current := filepath.Join(root, "test", "dle-current")
	write(t, filepath.Join(current, "site", "index.php"), strings.Repeat("c", 100))
	write(t, filepath.Join(root, "test", "dle-old", "site", "index.php"), strings.Repeat("o", 50))
	write(t, filepath.Join(root, "test", "dle-older", "site", "index.php"), strings.Repeat("e", 70))
	atime(t, current, now.Add(-time.Minute))
	atime(t, filepath.Join(root, "test", "dle-old"), now.Add(-2*time.Hour))
	atime(t, filepath.Join(root, "test", "dle-older"), now.Add(-3*time.Hour))
	write(t, filepath.Join(root, "test", "keep.txt"), "keep")

	write(t, filepath.Join(root, "static", "Plugin-abc", "phpstan.neon"), strings.Repeat("s", 30))
	write(t, filepath.Join(root, "static", "bootstrap.php"), "bootstrap")
	atime(t, filepath.Join(root, "static", "Plugin-abc"), now.Add(-time.Hour))
	atime(t, filepath.Join(root, "static", "bootstrap.php"), now.Add(-time.Hour))
	return current
}

func TestCleanKeepsCurrentBase(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	current := layout(t, root, now)
	res, err := Clean(context.Background(), nil, Options{Root: root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"DLE base dle-old", "DLE base dle-older", "run site demo-abc", "run site other",
		"static cache Plugin-abc", "static cache bootstrap.php"}
	if got := entryNames(res.Removed); !reflect.DeepEqual(got, want) {
		t.Errorf("removed = %q, want %q", got, want)
	}
	if res.Freed != 50+70+10+20+30+9 {
		t.Errorf("freed = %d", res.Freed)
	}
	for _, path := range []string{current, filepath.Join(root, "test", "keep.txt")} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s removed: %v", path, err)
		}
	}
	for _, path := range []string{filepath.Join(root, "test", "run"), filepath.Join(root, "static")} {
		if entries, err := os.ReadDir(path); err != nil {
			t.Errorf("%s: %v", path, err)
		} else if len(entries) != 0 {
			t.Errorf("%s remains: %v", path, entries)
		}
	}
}

func TestCleanUsesTrialArchiveForCurrentBase(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	dir := filepath.Join(root, "test")
	write(t, filepath.Join(dir, "dle_trial.zip"), "trial")
	sum := sha256.Sum256([]byte("trial"))
	current := filepath.Join(dir, "dle-"+hex.EncodeToString(sum[:])[:12])
	write(t, filepath.Join(current, "site", "index.php"), "current")
	newest := filepath.Join(dir, "dle-newest")
	write(t, filepath.Join(newest, "site", "index.php"), "newest")
	// The newest directory is not the one installed from the archive.
	atime(t, current, now.Add(-2*time.Hour))
	atime(t, newest, now.Add(-time.Minute))
	res, err := Clean(context.Background(), nil, Options{Root: root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"DLE base dle-newest"}; !reflect.DeepEqual(entryNames(res.Removed), want) {
		t.Errorf("removed = %q, want %q", entryNames(res.Removed), want)
	}
	if _, err := os.Stat(filepath.Join(current, "site")); err != nil {
		t.Errorf("current base removed: %v", err)
	}
}

func TestCleanOlderThan(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	layout(t, root, now)
	// Fresh entries survive; the current base and keep.txt are untouched.
	write(t, filepath.Join(root, "test", "run", "fresh", "index.php"), "fresh")
	atime(t, filepath.Join(root, "test", "run", "fresh"), now.Add(-time.Hour))
	write(t, filepath.Join(root, "static", "Fresh-abc", "phpstan.neon"), "fresh")
	atime(t, filepath.Join(root, "static", "Fresh-abc"), now.Add(-time.Hour))
	for _, name := range []string{"demo-abc", "other"} {
		atime(t, filepath.Join(root, "test", "run", name), now.Add(-48*time.Hour))
	}
	atime(t, filepath.Join(root, "static", "Plugin-abc"), now.Add(-48*time.Hour))
	atime(t, filepath.Join(root, "test", "dle-old"), now.Add(-time.Hour))
	atime(t, filepath.Join(root, "test", "dle-older"), now.Add(-48*time.Hour))

	res, err := Clean(context.Background(), nil, Options{Root: root, OlderThan: 24 * time.Hour, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"DLE base dle-older", "run site demo-abc", "run site other", "static cache Plugin-abc"}
	if got := entryNames(res.Removed); !reflect.DeepEqual(got, want) {
		t.Errorf("removed = %q, want %q", got, want)
	}
	for _, path := range []string{
		filepath.Join(root, "test", "dle-old"),
		filepath.Join(root, "test", "run", "fresh"),
		filepath.Join(root, "static", "Fresh-abc"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s removed: %v", path, err)
		}
	}
}

func TestCleanDryRun(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	layout(t, root, now)
	res, err := Clean(context.Background(), nil, Options{Root: root, Now: now, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 6 || res.Freed != 189 {
		t.Errorf("result = %+v", res)
	}
	for _, path := range []string{
		filepath.Join(root, "test", "run", "demo-abc"),
		filepath.Join(root, "test", "dle-old"),
		filepath.Join(root, "static", "Plugin-abc"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("dry run removed %s: %v", path, err)
		}
	}
}

func TestCleanAll(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	layout(t, root, now)
	write(t, filepath.Join(root, "test", "dle_trial.zip"), "trial")
	res, err := Clean(context.Background(), nil, Options{Root: root, All: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"DLE base dle-current", "DLE base dle-old", "DLE base dle-older",
		"run site demo-abc", "run site other",
		"static cache Plugin-abc", "static cache bootstrap.php", "trial archive dle_trial.zip"}
	if got := entryNames(res.Removed); !reflect.DeepEqual(got, want) {
		t.Errorf("removed = %q, want %q", got, want)
	}
	if res.Freed != 100+50+70+10+20+30+9+5 {
		t.Errorf("freed = %d", res.Freed)
	}
	for _, path := range []string{filepath.Join(root, "test", "dle-current"), filepath.Join(root, "test", "dle_trial.zip")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s remains: %v", path, err)
		}
	}
}

func TestCleanMissingRoots(t *testing.T) {
	root := t.TempDir()
	res, err := Clean(context.Background(), nil, Options{Root: root})
	if err != nil || len(res.Removed) != 0 || res.Freed != 0 {
		t.Fatalf("empty cache = %+v, %v", res, err)
	}
}

const cleanDocker = `#!/bin/sh
case "$1" in
 image) case "$2" in
  ls) printf '%s' "${IMAGE_LIST:-}" ;;
  inspect) printf '%s' "${IMAGE_SIZE:-0}" ;;
  rm) printf '%s\n' "$3" >> "${RM_LOG}" ;;
 esac; exit 0 ;;
 ps) case "$*" in *333333333333*) printf 'container-id' ;; esac; exit 0 ;;
esac
exit 2
`

func fakeDocker(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake docker")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(cleanDocker), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RM_LOG", filepath.Join(dir, "rm.log"))
	t.Setenv("IMAGE_SIZE", "100")
	t.Setenv("IMAGE_LIST", strings.Join([]string{
		"dleplugin-test:111111111111|2023-01-01 10:00:00 +0000 UTC",
		"dleplugin-test:222222222222|2024-01-02 09:00:00 +0000 UTC",
		"dleplugin-test:333333333333|2023-01-01 10:00:00 +0000 UTC",
		"dleplugin-hosts-php85:latest|2023-01-01 10:00:00 +0000 UTC",
		"other:tag|2023-01-01 10:00:00 +0000 UTC",
	}, "\n"))
}

func TestCleanDockerImages(t *testing.T) {
	fakeDocker(t)
	root := t.TempDir()
	now := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	res, err := Clean(context.Background(), process.NewRunner(process.Options{}), Options{
		Root: root, Docker: true, OlderThan: 24 * time.Hour, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"image dleplugin-test:111111111111"}; !reflect.DeepEqual(entryNames(res.Removed), want) {
		t.Errorf("removed = %q, want %q", entryNames(res.Removed), want)
	}
	if res.Freed != 100 {
		t.Errorf("freed = %d", res.Freed)
	}
	if want := []string{"image dleplugin-test:333333333333"}; !reflect.DeepEqual(entryNames(res.Kept), want) {
		t.Errorf("kept = %q, want %q", entryNames(res.Kept), want)
	}
	log, err := os.ReadFile(os.Getenv("RM_LOG"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(log)); got != "dleplugin-test:111111111111" {
		t.Errorf("removed images = %q", got)
	}

	// A dry run reports the same removals without touching Docker.
	res, err = Clean(context.Background(), process.NewRunner(process.Options{}), Options{
		Root: root, Docker: true, OlderThan: 24 * time.Hour, Now: now, DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(os.Getenv("RM_LOG")); strings.TrimSpace(string(got)) != "dleplugin-test:111111111111" {
		t.Errorf("dry run removed images: %q", got)
	}
	if len(res.Removed) != 1 {
		t.Errorf("dry run removed = %q", entryNames(res.Removed))
	}
}

func TestCleanDockerMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	res, err := Clean(context.Background(), process.NewRunner(process.Options{}), Options{Root: root, Docker: true})
	if err == nil || !strings.Contains(err.Error(), "Docker not found") || len(res.Errors) != 1 {
		t.Fatalf("err = %v, errors = %v", err, res.Errors)
	}
}
