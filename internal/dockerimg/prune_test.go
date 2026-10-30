package dockerimg

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/process"
)

const pruneDocker = `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_LOG"
case "$1" in
 image) case "$2" in
  inspect) printf '%s' "${IMAGE_SIZE:-}"; exit "${INSPECT_EXIT:-0}" ;;
  ls) printf '%s' "${IMAGE_LIST:-}"; exit 0 ;;
  rm) exit "${RM_EXIT:-0}" ;;
 esac; exit 2 ;;
 ps) printf '%s' "${PS_OUT:-}"; exit 0 ;;
 pull) exit 1 ;;
 build) cat > /dev/null; exit 0 ;;
esac
exit 2
`

func fakePruneDocker(t *testing.T) (*process.Runner, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake docker")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(pruneDocker), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	log := filepath.Join(dir, "docker.log")
	t.Setenv("DOCKER_LOG", log)
	t.Setenv("INSPECT_EXIT", "0")
	t.Setenv("IMAGE_LIST", "")
	t.Setenv("PS_OUT", "")
	t.Setenv("RM_EXIT", "0")
	return process.NewRunner(process.Options{}), log
}

func loggedCommands(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestLocalTags(t *testing.T) {
	_, log := fakePruneDocker(t)
	list := strings.Join([]string{
		"dleplugin-static:old|id-static|2024-01-01 10:00:00 +0000 UTC",
		"dleplugin-test:new|id-new|2024-03-01 10:00:00 +0000 UTC",
		"other:tag|id-other|2024-04-01 10:00:00 +0000 UTC",
		"dleplugin-test:<none>|id-none|2024-04-01 10:00:00 +0000 UTC",
		"dleplugin-test:mid|id-mid|2024-02-01 10:00:00 +0000 UTC",
	}, "\n")
	t.Setenv("IMAGE_LIST", list)
	runner := process.NewRunner(process.Options{})
	tags, err := LocalTags(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, tag := range tags {
		refs = append(refs, tag.Reference)
	}
	if want := []string{"other:tag", "dleplugin-test:new", "dleplugin-test:mid", "dleplugin-static:old"}; !reflect.DeepEqual(refs, want) {
		t.Errorf("tags = %q, want %q", refs, want)
	}
	if tags[1].Repository != "dleplugin-test" || tags[1].ID != "id-new" || tags[1].Created.IsZero() {
		t.Errorf("tag = %+v", tags[1])
	}
	if cmds := loggedCommands(t, log); !reflect.DeepEqual(cmds, []string{"image ls --format " + tagsFormat}) {
		t.Errorf("commands = %q", cmds)
	}
}

func TestPruneKeepsRecentTags(t *testing.T) {
	_, log := fakePruneDocker(t)
	img := Image{Name: "tool", Dockerfile: []byte("FROM scratch\n")}
	current := img.Tag()
	list := strings.Join([]string{
		"tool:old|id-old|2024-01-01 10:00:00 +0000 UTC",
		"tool:new|id-new|2024-03-01 10:00:00 +0000 UTC",
		current + "|id-current|2024-04-01 10:00:00 +0000 UTC",
		"tool:mid|id-mid|2024-02-01 10:00:00 +0000 UTC",
	}, "\n")
	t.Setenv("IMAGE_LIST", list)
	var notices []string
	removed, err := Prune(context.Background(), process.NewRunner(process.Options{}), img, KeepTags, func(s string) { notices = append(notices, s) })
	if err != nil {
		t.Fatal(err)
	}
	// KeepTags counts the current version: one previous release survives.
	if !reflect.DeepEqual(removed, []string{"tool:mid", "tool:old"}) {
		t.Errorf("removed = %q", removed)
	}
	if want := []string{"Removed old image tool:mid.", "Removed old image tool:old."}; !reflect.DeepEqual(notices, want) {
		t.Errorf("notices = %q", notices)
	}
	cmds := loggedCommands(t, log)
	want := []string{
		"image ls --format " + tagsFormat,
		"ps --all --quiet --filter ancestor=tool:mid",
		"image rm tool:mid",
		"ps --all --quiet --filter ancestor=tool:old",
		"image rm tool:old",
	}
	if !reflect.DeepEqual(cmds, want) {
		t.Errorf("commands = %q, want %q", cmds, want)
	}
}

func TestPruneKeepsImagesUsedByContainers(t *testing.T) {
	_, log := fakePruneDocker(t)
	img := Image{Name: "tool", Dockerfile: []byte("FROM scratch\n")}
	list := strings.Join([]string{
		"tool:old|id-old|2024-01-01 10:00:00 +0000 UTC",
		"tool:new|id-new|2024-03-01 10:00:00 +0000 UTC",
		"tool:mid|id-mid|2024-02-01 10:00:00 +0000 UTC",
	}, "\n")
	t.Setenv("IMAGE_LIST", list)
	t.Setenv("PS_OUT", "container-id\n")
	var notices []string
	removed, err := Prune(context.Background(), process.NewRunner(process.Options{}), img, KeepTags, func(s string) { notices = append(notices, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %q", removed)
	}
	if want := []string{"Keeping image tool:old (used by a container)."}; !reflect.DeepEqual(notices, want) {
		t.Errorf("notices = %q", notices)
	}
	cmds := loggedCommands(t, log)
	if strings.Contains(strings.Join(cmds, "\n"), "image rm") {
		t.Errorf("in-use image removed: %q", cmds)
	}
}

func TestPruneRemoteAliases(t *testing.T) {
	_, log := fakePruneDocker(t)
	img := Image{Name: "tool", Dockerfile: []byte("FROM scratch\n")}
	list := strings.Join([]string{
		"tool:new|id-new|2024-02-01 10:00:00 +0000 UTC",
		"tool:old|id-old|2024-01-01 10:00:00 +0000 UTC",
		"registry.test/tool:old|id-old|2024-01-01 10:00:00 +0000 UTC",
		img.Tag() + "|id-current|2024-03-01 10:00:00 +0000 UTC",
	}, "\n")
	t.Setenv("IMAGE_LIST", list)
	removed, err := Prune(context.Background(), process.NewRunner(process.Options{}), img, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tool:new", "registry.test/tool:old", "tool:old"}; !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %q, want %q", removed, want)
	}
	cmds := strings.Join(loggedCommands(t, log), "\n")
	for _, ref := range []string{"tool:new", "registry.test/tool:old", "tool:old"} {
		if !strings.Contains(cmds, "image rm "+ref) {
			t.Errorf("%s not removed: %q", ref, cmds)
		}
	}
	if strings.Contains(cmds, "image rm "+img.Tag()) {
		t.Errorf("current image removed: %q", cmds)
	}
}

func TestPruneAliasesShareRetentionSlots(t *testing.T) {
	_, log := fakePruneDocker(t)
	img := Image{Name: "tool", Dockerfile: []byte("FROM scratch\n")}
	list := strings.Join([]string{
		"ghcr.io/registry/tool:" + strings.TrimPrefix(img.Tag(), "tool:") + "|id-current|2024-04-01 10:00:00 +0000 UTC",
		img.Tag() + "|id-current|2024-04-01 10:00:00 +0000 UTC",
		"ghcr.io/registry/tool:new|id-new|2024-03-01 10:00:00 +0000 UTC",
		"tool:new|id-new|2024-03-01 10:00:00 +0000 UTC",
		"tool:mid|id-mid|2024-02-01 10:00:00 +0000 UTC",
		"tool:old|id-old|2024-01-01 10:00:00 +0000 UTC",
	}, "\n")
	t.Setenv("IMAGE_LIST", list)
	removed, err := Prune(context.Background(), process.NewRunner(process.Options{}), img, KeepTags, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Both aliases of the current and previous versions survive; aliases do
	// not consume the two version slots.
	if want := []string{"tool:mid", "tool:old"}; !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %q, want %q", removed, want)
	}
	cmds := strings.Join(loggedCommands(t, log), "\n")
	for _, ref := range []string{img.Tag(), "ghcr.io/registry/tool:" + strings.TrimPrefix(img.Tag(), "tool:"), "tool:new", "ghcr.io/registry/tool:new"} {
		if strings.Contains(cmds, "image rm "+ref) {
			t.Errorf("kept %s removed: %q", ref, cmds)
		}
	}
}

func TestPruneSkipsUnknownCreated(t *testing.T) {
	_, log := fakePruneDocker(t)
	img := Image{Name: "tool", Dockerfile: []byte("FROM scratch\n")}
	list := strings.Join([]string{
		"tool:new|id-new|2024-03-01 10:00:00 +0000 UTC",
		"tool:mid|id-mid|2024-02-01 10:00:00 +0000 UTC",
		"tool:unknown|id-unknown|not-a-date",
	}, "\n")
	t.Setenv("IMAGE_LIST", list)
	removed, err := Prune(context.Background(), process.NewRunner(process.Options{}), img, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tool:mid"}; !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %q, want %q", removed, want)
	}
	cmds := strings.Join(loggedCommands(t, log), "\n")
	if strings.Contains(cmds, "image rm tool:unknown") || strings.Contains(cmds, "ancestor=tool:unknown") {
		t.Errorf("unknown creation time pruned: %q", cmds)
	}
}

func TestPruneFailureIsBestEffortInEnsure(t *testing.T) {
	_, log := fakePruneDocker(t)
	img := Image{Name: "tool", Title: "test", Dockerfile: []byte("FROM scratch\n")}
	t.Setenv("INSPECT_EXIT", "1")
	t.Setenv("RM_EXIT", "1")
	t.Setenv("IMAGE_LIST", strings.Join([]string{
		"tool:old|id-old|2024-01-01 10:00:00 +0000 UTC",
		"tool:new|id-new|2024-03-01 10:00:00 +0000 UTC",
		"tool:mid|id-mid|2024-02-01 10:00:00 +0000 UTC",
	}, "\n"))
	tag, err := Ensure(context.Background(), process.NewRunner(process.Options{}), img, false, nil)
	if err != nil || tag != img.Tag() {
		t.Fatalf("Ensure = %q, %v", tag, err)
	}
	for _, want := range []string{"build --tag " + img.Tag() + " -", "image rm tool:old"} {
		if !strings.Contains(strings.Join(loggedCommands(t, log), "\n"), want) {
			t.Errorf("commands lack %q: %q", want, log)
		}
	}
}

func TestImageSize(t *testing.T) {
	_, log := fakePruneDocker(t)
	t.Setenv("IMAGE_SIZE", "12345")
	n, err := ImageSize(context.Background(), process.NewRunner(process.Options{}), "tool:old")
	if err != nil || n != 12345 {
		t.Fatalf("ImageSize = %d, %v", n, err)
	}
	t.Setenv("IMAGE_SIZE", "1.2GB")
	if _, err := ImageSize(context.Background(), process.NewRunner(process.Options{}), "tool:old"); err == nil || !strings.Contains(err.Error(), "parse image size") {
		t.Errorf("invalid size: %v", err)
	}
	cmds := loggedCommands(t, log)
	if len(cmds) != 2 || cmds[0] != "image inspect --format {{.Size}} tool:old" {
		t.Errorf("commands = %q", cmds)
	}
}

func TestPruneEmptyList(t *testing.T) {
	_, log := fakePruneDocker(t)
	img := Image{Name: "tool", Dockerfile: []byte("FROM scratch\n")}
	removed, err := Prune(context.Background(), process.NewRunner(process.Options{}), img, KeepTags, nil)
	if err != nil || len(removed) != 0 {
		t.Fatalf("Prune = %q, %v", removed, err)
	}
	if cmds := loggedCommands(t, log); !reflect.DeepEqual(cmds, []string{"image ls --format " + tagsFormat}) {
		t.Errorf("commands = %q", cmds)
	}
}
