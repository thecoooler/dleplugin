package stack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/thecoooler/dleplugin/internal/process"
)

type publishedPort struct {
	service, containerPort, key, port string
}

// CheckPorts checks the host ports before compose starts building or recreating
// containers. A running container belonging to this stack may keep its port.
func (s *Stack) CheckPorts(ctx context.Context) error {
	address := s.composeValue("BIND_ADDRESS", "0.0.0.0")
	ports := []publishedPort{
		{"apache", "80/tcp", "HTTP_PORT", s.composeValue("HTTP_PORT", "80")},
		{"db", "3306/tcp", "DB_PORT", s.composeValue("DB_PORT", "3306")},
	}
	for i, p := range ports {
		n, err := strconv.Atoi(p.port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("invalid %s %q: expected a port from 1 to 65535", p.key, p.port)
		}
		ports[i].port = strconv.Itoa(n)
	}
	if ports[0].port == ports[1].port {
		return fmt.Errorf("HTTP_PORT and DB_PORT both use port %s; configure different ports in %s", ports[0].port, filepath.Join(s.Dir, ".env"))
	}
	for _, p := range ports {
		endpoint := net.JoinHostPort(address, p.port)
		listener, err := net.Listen("tcp", endpoint)
		if err == nil {
			listener.Close()
			continue
		}
		busy := errors.Is(err, syscall.EADDRINUSE)
		if errors.Is(err, os.ErrPermission) {
			// Windows may report access denied for an exclusive socket. A
			// successful connection distinguishes it from a privileged port.
			dialAddress := address
			if address == "0.0.0.0" {
				dialAddress = "127.0.0.1"
			} else if address == "::" {
				dialAddress = "::1"
			}
			conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort(dialAddress, p.port), 250*time.Millisecond)
			if dialErr == nil {
				conn.Close()
				busy = true
			} else {
				continue
			}
		}
		if !busy {
			return fmt.Errorf("check %s (%s): %w", p.key, endpoint, err)
		}
		if s.ownsPort(ctx, address, p) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		hint := fmt.Sprintf("change %s in %s", p.key, filepath.Join(s.Dir, ".env"))
		if p.key == "HTTP_PORT" {
			hint = "free the port or choose another with: dleplugin hosts port <number>"
		} else {
			hint = "free the port or " + hint
		}
		return fmt.Errorf("%s port %s for %s is already in use; %s", p.key, endpoint, p.service, hint)
	}
	return nil
}

// composeValue follows Compose's environment-over-.env precedence and its
// ${KEY:-default} handling for empty values in the embedded compose file.
func (s *Stack) composeValue(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		value = s.Env[key]
	}
	if value == "" {
		return fallback
	}
	return value
}

type dockerPortBinding struct {
	HostIP   string
	HostPort string
}

func (s *Stack) ownsPort(ctx context.Context, address string, p publishedPort) bool {
	name := s.composeValue("COMPOSE_PROJECT_NAME", "dleplugin-hosts") + "-" + p.service
	// Restrict the output to metadata: full container inspection includes secrets.
	format := "{{.State.Running}}\t{{index .Config.Labels \"com.docker.compose.project.working_dir\"}}\t{{index .Config.Labels \"com.docker.compose.service\"}}\t{{json .NetworkSettings.Ports}}"
	out, err := s.runner().Output(ctx, process.Command{Name: "docker", Args: []string{"inspect", "--format", format, name}, Dir: s.Dir})
	if err != nil {
		return false
	}
	return matchesOwnedPort(out, s.Dir, address, p)
}

func matchesOwnedPort(output, dir, address string, p publishedPort) bool {
	parts := strings.SplitN(output, "\t", 4)
	if len(parts) != 4 || parts[0] != "true" || filepath.Clean(parts[1]) != filepath.Clean(dir) || parts[2] != p.service {
		return false
	}
	var ports map[string][]dockerPortBinding
	if err := json.Unmarshal([]byte(parts[3]), &ports); err != nil {
		return false
	}
	for _, binding := range ports[p.containerPort] {
		if binding.HostIP == address && binding.HostPort == p.port {
			return true
		}
	}
	return false
}
