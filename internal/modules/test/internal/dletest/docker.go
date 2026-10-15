package dletest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/thecoooler/dleplugin/internal/process"
)

// MariaDBImage is the database server of the test environment.
const MariaDBImage = "mariadb:11.4@sha256:1292844148b311e4ed4300022a996d39083f415a963e970cf47cad1b3b18e3a6"

// readyTimeout limits waiting for the database and the installer server.
var readyTimeout = 2 * time.Minute

// pollInterval is the delay between readiness checks.
var pollInterval = 500 * time.Millisecond

// env is a Docker network with a MariaDB container reachable as "db".
type env struct {
	runner *process.Runner
	// Name prefixes the network and container names.
	Name string
	// DBAddr is the database address published on the host (127.0.0.1:port).
	DBAddr string
}

func newName() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "dleplugin-test-" + hex.EncodeToString(b)
}

func (e *env) docker(ctx context.Context, args ...string) (string, error) {
	return e.runner.Output(ctx, process.Command{Name: "docker", Args: args})
}

func (e *env) dbContainer() string { return e.Name + "-db" }

// startEnv creates the network and starts the database. The caller must
// Close the env, also on error.
func startEnv(ctx context.Context, runner *process.Runner) (*env, error) {
	e := &env{runner: runner, Name: newName()}
	if _, err := e.docker(ctx, "network", "create", e.Name); err != nil {
		return e, err
	}
	if _, err := e.docker(ctx, "run", "--detach", "--name", e.dbContainer(),
		"--network", e.Name, "--network-alias", dbHost,
		"--tmpfs", "/var/lib/mysql",
		"--env", "MARIADB_ROOT_PASSWORD="+dbPass,
		"--env", "MARIADB_DATABASE="+dbName,
		"--env", "MARIADB_USER="+dbUser,
		"--env", "MARIADB_PASSWORD="+dbPass,
		"--publish", "127.0.0.1::3306",
		MariaDBImage,
		"--character-set-server=utf8mb4", "--collation-server=utf8mb4_unicode_ci",
		// Non-strict like typical DLE hosting: DLE 21 tables have NOT NULL
		// columns without defaults that plugin registration does not fill.
		"--sql-mode=NO_ENGINE_SUBSTITUTION",
	); err != nil {
		return e, err
	}
	addr, err := e.publishedAddr(ctx, e.dbContainer(), "3306/tcp")
	if err != nil {
		return e, err
	}
	e.DBAddr = addr
	// The image's entrypoint restarts the server after initialization;
	// --connect checks the final server over TCP.
	err = e.waitFor(ctx, "database", func() error {
		_, err := e.docker(ctx, "exec", e.dbContainer(), "healthcheck.sh", "--connect", "--innodb_initialized")
		return err
	})
	return e, err
}

// publishedAddr returns the host address of a published container port.
func (e *env) publishedAddr(ctx context.Context, container, port string) (string, error) {
	out, err := e.docker(ctx, "port", container, port)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		host, p, err := net.SplitHostPort(strings.TrimSpace(line))
		if err == nil && strings.Count(host, ":") == 0 {
			return net.JoinHostPort(host, p), nil
		}
	}
	return "", fmt.Errorf("docker port %s %s: unexpected output %q", container, port, out)
}

// waitFor polls check until it succeeds or readyTimeout passes.
func (e *env) waitFor(ctx context.Context, what string, check func() error) error {
	deadline := time.Now().Add(readyTimeout)
	for {
		err := check()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s is not ready after %s: %w", what, readyTimeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// Import loads an SQL dump into the test database.
func (e *env) Import(ctx context.Context, dump []byte) error {
	_, err := e.runner.Run(ctx, process.Command{
		Name:  "docker",
		Args:  []string{"exec", "--interactive", e.dbContainer(), "mariadb", "--user=" + dbUser, "--password=" + dbPass, dbName},
		Stdin: bytes.NewReader(dump),
	})
	return err
}

// Dump returns an SQL dump of the test database.
func (e *env) Dump(ctx context.Context) ([]byte, error) {
	res, err := e.runner.Run(ctx, process.Command{
		Name: "docker",
		Args: []string{"exec", e.dbContainer(), "mariadb-dump", "--user=" + dbUser, "--password=" + dbPass,
			"--single-transaction", "--skip-dump-date", dbName},
	})
	if err != nil {
		return nil, err
	}
	return res.Stdout, nil
}

// userArgs runs containers as the current user so created files stay
// writable.
func userArgs() []string {
	if uid, gid := os.Getuid(), os.Getgid(); uid >= 0 && gid >= 0 {
		return []string{"--user", strconv.Itoa(uid) + ":" + strconv.Itoa(gid)}
	}
	return nil
}

// Close removes the containers and the network. It ignores ctx cancellation
// so that interrupted runs are cleaned up too.
func (e *env) Close(ctx context.Context, containers ...string) error {
	ctx = context.WithoutCancel(ctx)
	var errs []error
	names := append([]string{e.dbContainer()}, containers...)
	if _, err := e.docker(ctx, append([]string{"rm", "--force", "--volumes"}, names...)...); err != nil {
		errs = append(errs, err)
	}
	if _, err := e.docker(ctx, "network", "rm", e.Name); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
