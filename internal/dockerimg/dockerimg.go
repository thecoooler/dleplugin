// Package dockerimg builds tool images from embedded Dockerfiles. The tag is
// derived from every build-context input so dependency changes yield a new image.
package dockerimg

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"sort"

	"github.com/thecoooler/dleplugin/internal/process"
)

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

// Ensure builds the image unless it exists and returns its tag. stream shows
// the build output; notify (may be nil) receives progress messages.
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
		return tag, nil
	}
	var ee *process.ExitError
	if !errors.As(err, &ee) {
		return "", err
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
	return tag, nil
}

func capitalize(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}
