package static

import (
	"bytes"
	"context"
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/thecoooler/dleplugin/internal/dockerimg"
	"github.com/thecoooler/dleplugin/internal/paths"
	"github.com/thecoooler/dleplugin/internal/process"
)

//go:embed assets/Dockerfile
var dockerfile []byte

//go:embed assets/composer.json
var composerJSON []byte

//go:embed assets/composer.lock
var composerLock []byte

//go:embed assets/bootstrap.php
var bootstrapPHP []byte

// ImageName is the analysis image repository; the tag is derived from the
// complete embedded build context.
const ImageName = "dleplugin-static"

const (
	containerWorkDir       = "/work"
	containerToolsDir      = "/opt/tools"
	containerIncludeDir    = "/include"
	containerTestVendorDir = "/test-vendor"
)

// image is the analysis image.
var image = dockerimg.Image{Name: ImageName, Title: "static analysis", Dockerfile: dockerfile, Files: map[string][]byte{"composer.json": composerJSON, "composer.lock": composerLock}}

// ImageTag returns the content-derived tag of the embedded locked tool image.
func ImageTag() string { return image.Tag() }

// EnsureImage builds the analysis image unless it exists and returns its tag.
func EnsureImage(ctx context.Context, runner *process.Runner, stream bool, notify func(string)) (string, error) {
	return dockerimg.Ensure(ctx, runner, image, stream, notify)
}

// Mount is a bind mount into the analysis container.
type Mount struct {
	Host, Container string
	ReadOnly        bool
}

func (m Mount) arg() string {
	if m.ReadOnly {
		return m.Host + ":" + m.Container + ":ro"
	}
	return m.Host + ":" + m.Container
}

// Runtime describes how to run tools in the analysis container. WorkDir is
// mounted read-write at /work.
type Runtime struct {
	Image   string
	WorkDir string
	Mounts  []Mount
}

// Command returns the full docker argv running args in the container.
func (r *Runtime) Command(args []string) []string {
	cmd := []string{"docker", "run", "--rm", "--pull", "never", "--network", "none", "--env", "HOME=/tmp"}
	if uid, gid := os.Getuid(), os.Getgid(); uid >= 0 && gid >= 0 {
		cmd = append(cmd, "--user", strconv.Itoa(uid)+":"+strconv.Itoa(gid))
	}
	cmd = append(cmd, "--volume", Mount{Host: r.WorkDir, Container: containerWorkDir}.arg())
	for _, m := range r.Mounts {
		cmd = append(cmd, "--volume", m.arg())
	}
	cmd = append(cmd, "--workdir", containerWorkDir, r.Image)
	return append(cmd, args...)
}

// ToHost maps a container path to the host using the most specific mount.
func (r *Runtime) ToHost(p string) string {
	mounts := append([]Mount{{Host: r.WorkDir, Container: containerWorkDir}}, r.Mounts...)
	sort.SliceStable(mounts, func(i, j int) bool {
		return strings.Count(mounts[i].Container, "/") > strings.Count(mounts[j].Container, "/")
	})
	for _, m := range mounts {
		if p == m.Container || strings.HasPrefix(p, m.Container+"/") {
			return m.Host + p[len(m.Container):]
		}
	}
	return p
}

// NormalizePaths rewrites message files to host paths relative to the work
// directory or the plugin root.
func (r *Runtime) NormalizePaths(messages []Message, pluginDir string) {
	for i := range messages {
		f := r.ToHost(messages[i].File)
		f = strings.ReplaceAll(f, r.WorkDir+"/", "")
		messages[i].File = strings.ReplaceAll(f, pluginDir+"/", "")
	}
}

// Workspace is a prepared container runtime for one plugin.
type Workspace struct {
	Runtime *Runtime
	// IncludePaths are the container paths of the mounted include paths.
	IncludePaths []string
	Warnings     []string
}

// WorkRoot is the parent of per-plugin work directories.
func WorkRoot() (string, error) { return paths.CacheDir("static") }

// NewWorkspace creates the work directory <WorkRoot>/<slug>-<hash> and the
// mounts: bootstrap.php, the plugin's src/ and tests/ (read-only), existing
// include paths (relative ones resolved against invocationDir) and the shared
// test Composer vendor directory.
func NewWorkspace(pluginDir, slug, image string, includes []string, invocationDir string) (*Workspace, error) {
	root, err := WorkRoot()
	if err != nil {
		return nil, err
	}
	sum := md5.Sum([]byte(pluginDir))
	workDir := filepath.Join(root, slug+"-"+hex.EncodeToString(sum[:])[:6])
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("create work directory: %w", err)
	}
	bootstrap := filepath.Join(root, "bootstrap.php")
	if cur, err := os.ReadFile(bootstrap); err != nil || !bytes.Equal(cur, bootstrapPHP) {
		if err := os.WriteFile(bootstrap, bootstrapPHP, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", bootstrap, err)
		}
	}

	ws := &Workspace{}
	mounts := []Mount{{Host: bootstrap, Container: path.Join(containerToolsDir, "bootstrap.php"), ReadOnly: true}}
	for _, dir := range []string{"src", "tests"} {
		src := filepath.Join(pluginDir, dir)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Join(workDir, dir), 0o755); err != nil {
			return nil, fmt.Errorf("create work directory: %w", err)
		}
		mounts = append(mounts, Mount{Host: src, Container: path.Join(containerWorkDir, dir), ReadOnly: true})
	}
	for i, p := range includes {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(invocationDir, abs)
		}
		if _, err := os.Stat(abs); err != nil {
			ws.Warnings = append(ws.Warnings, "Include path not found: "+p)
			continue
		}
		c := path.Join(containerIncludeDir, strconv.Itoa(i))
		mounts = append(mounts, Mount{Host: abs, Container: c, ReadOnly: true})
		ws.IncludePaths = append(ws.IncludePaths, c)
	}
	if cache, err := paths.ComposerCacheDir(); err == nil {
		vendor := filepath.Join(cache, "vendor")
		if _, err := os.Stat(vendor); err == nil {
			mounts = append(mounts, Mount{Host: vendor, Container: containerTestVendorDir, ReadOnly: true})
		}
	}
	ws.Runtime = &Runtime{Image: image, WorkDir: workDir, Mounts: mounts}
	return ws, nil
}
