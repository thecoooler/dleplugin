package stack

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// WithInstaller serves staging in a disposable PHP container on a random loopback
// port, using the stack's PHP image and DB network. Apache never sees install.php.
func (s *Stack) WithInstaller(ctx context.Context, staging, php string, run func(string) error) (err error) {
	// A real static file avoids PHP's index.php fallback for nonexistent paths.
	probe, err := os.CreateTemp(staging, "dlehost-ready-*.txt")
	if err != nil {
		return err
	}
	probe.Close()
	defer os.Remove(probe.Name())
	if err := os.Chmod(probe.Name(), 0o644); err != nil {
		return err
	}
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	name := "dlehost-install-" + hex.EncodeToString(token[:])
	defer func() {
		out, stopErr := exec.Command("docker", "rm", "-f", name).CombinedOutput()
		if stopErr != nil && !strings.Contains(string(out), "No such container") {
			err = errors.Join(err, fmt.Errorf("удаление контейнера установщика: %s", out))
		}
	}()
	if err := s.Compose(nil, "run", "--rm", "--no-deps", "-d", "--name", name,
		"--user", s.Env["UID"]+":"+s.Env["GID"],
		"--publish", "127.0.0.1::8080", "--volume", staging+":/install",
		"--workdir", "/install", "--entrypoint", "php",
		"php"+strings.ReplaceAll(php, ".", ""), "-S", "0.0.0.0:8080", "-t", "/install"); err != nil {
		return err
	}
	out, err := exec.Command("docker", "port", name, "8080/tcp").Output()
	if err != nil {
		return fmt.Errorf("порт установщика: %w", err)
	}
	address := strings.TrimSpace(string(out))
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return fmt.Errorf("неожиданный адрес установщика: %q", address)
	}
	// Docker's port proxy can accept TCP before PHP starts listening. Wait for a
	// static HTTP response without running PHP or retrying installation requests.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	deadline := time.Now().Add(20 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := client.Head("http://" + address + "/" + filepath.Base(probe.Name()))
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if err == nil {
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("PHP установщика не ответил на HTTP-проверку готовности: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return run("http://" + address + "/")
}
