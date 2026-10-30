// Package dockerimg builds tool images from embedded Dockerfiles. The tag is
// derived from every build-context input so dependency changes yield a new image.
// Images published under the same tag in Registry are pulled instead of built.
package dockerimg

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/thecoooler/dleplugin/internal/process"
)

// Registry holds the prebuilt tool images published by CI.
const Registry = "ghcr.io/thecoooler"

// RegistryEnv overrides Registry; an empty value disables pulling.
const RegistryEnv = "DLEPLUGIN_IMAGE_REGISTRY"

// pullTimeout limits a pull before Ensure falls back to building.
var pullTimeout = 10 * time.Minute

// Image is a tool image built from an embedded Dockerfile.
type Image struct {
	// Name is the repository ("dleplugin-static").
	Name string
	// Title names the image in progress messages ("static analysis").
	Title      string
	Dockerfile []byte
	// Files contains the remaining embedded build-context inputs.
	Files map[string][]byte
}

// Tag hashes the Dockerfile and every build-context input in stable order.
func (i Image) Tag() string {
	h := sha256.New()
	h.Write(i.Dockerfile)
	for _, name := range i.fileNames() {
		h.Write([]byte("\x00" + name + "\x00"))
		h.Write(i.Files[name])
	}
	return i.Name + ":" + hex.EncodeToString(h.Sum(nil))[:12]
}

// Remote returns the registry reference of the tag, or "" when pulling is
// disabled.
func (i Image) Remote() string {
	registry, ok := os.LookupEnv(RegistryEnv)
	if !ok {
		registry = Registry
	}
	registry = strings.TrimRight(registry, "/")
	if registry == "" {
		return ""
	}
	return registry + "/" + i.Tag()
}

// WriteContext writes the build context files into dir.
func (i Image) WriteContext(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	files := map[string][]byte{"Dockerfile": i.Dockerfile}
	for name, data := range i.Files {
		files[name] = data
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (i Image) fileNames() []string {
	names := make([]string, 0, len(i.Files))
	for name := range i.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (i Image) buildContext() ([]byte, error) {
	var b bytes.Buffer
	writer := tar.NewWriter(&b)
	names := append([]string{"Dockerfile"}, i.fileNames()...)
	for _, name := range names {
		data := i.Files[name]
		if name == "Dockerfile" {
			data = i.Dockerfile
		}
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			return nil, err
		}
		if _, err := writer.Write(data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// CheckDocker returns an error when the docker CLI is not installed.
func CheckDocker() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("Docker not found. Install it from https://docs.docker.com/get-docker/")
	}
	return nil
}

// Ensure returns the tag of the image: an existing local image, the image
// pulled from the registry (tagged locally), or a local build when the pull
// fails. stream shows the pull and build output; notify (may be nil) receives
// progress messages.
func Ensure(ctx context.Context, runner *process.Runner, img Image, stream bool, notify func(string)) (string, error) {
	if err := CheckDocker(); err != nil {
		return "", err
	}
	if notify == nil {
		notify = func(string) {}
	}
	tag := img.Tag()
	_, err := runner.Run(ctx, process.Command{Name: "docker", Args: []string{"image", "inspect", tag}})
	if err == nil {
		prune(ctx, runner, img, notify)
		return tag, nil
	}
	var ee *process.ExitError
	if !errors.As(err, &ee) {
		return "", err
	}
	if pulled, err := pull(ctx, runner, img, stream, notify); err != nil || pulled {
		if pulled {
			prune(ctx, runner, img, notify)
		}
		return tag, err
	}

	buildContext, err := img.buildContext()
	if err != nil {
		return "", fmt.Errorf("create build context: %w", err)
	}
	notify("Building " + img.Title + " image " + tag + "...")
	res, err := runner.Run(ctx, process.Command{
		Name:   "docker",
		Args:   []string{"build", "--tag", tag, "-"},
		Stdin:  bytes.NewReader(buildContext),
		Stream: stream,
	})
	if errors.As(err, &ee) {
		return "", fmt.Errorf("Failed to build Docker image %s\n%s", tag, res.Stderr)
	}
	if err != nil {
		return "", err
	}
	notify(capitalize(img.Title) + " image built successfully.")
	prune(ctx, runner, img, notify)
	return tag, nil
}

// prune removes older tags once the current image is available. Cleanup is
// best effort: a failed prune must not fail the run that needed the image.
func prune(ctx context.Context, runner *process.Runner, img Image, notify func(string)) {
	_, _ = Prune(ctx, runner, img, KeepTags, notify)
}

// pull fetches the prebuilt image and tags it locally. It reports false when
// the image cannot be pulled, so that Ensure builds it.
func pull(ctx context.Context, runner *process.Runner, img Image, stream bool, notify func(string)) (bool, error) {
	remote := img.Remote()
	if remote == "" {
		return false, nil
	}
	notify("Pulling " + img.Title + " image " + remote + "...")
	pullCtx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()
	res, err := runner.Run(pullCtx, process.Command{Name: "docker", Args: []string{"pull", remote}, Stream: stream})
	if err == nil {
		res, err = runner.Run(ctx, process.Command{Name: "docker", Args: []string{"tag", remote, img.Tag()}})
		if err == nil {
			// The registry reference would keep the image layers alive after
			// the local tag is pruned; the local tag is all that is needed.
			_, _ = runner.Run(ctx, process.Command{Name: "docker", Args: []string{"image", "rm", remote}})
		}
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil {
		notify("Could not pull " + remote + reason(res) + "; building it locally.")
		return false, nil
	}
	notify(capitalize(img.Title) + " image pulled.")
	return true, nil
}

// reason returns the last stderr line of a failed command as " (line)".
func reason(res *process.Result) string {
	if res == nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(res.Stderr)), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return " (" + last + ")"
	}
	return ""
}

func capitalize(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}
