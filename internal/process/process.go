// Package process runs external commands (docker, composer, php, mysql, ...).
package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Options supplies output sinks and an optional completion observer.
type Options struct {
	Out, Err   io.Writer
	OnComplete func(Command, *Result)
}

// Runner executes commands without depending on a terminal or CLI package.
type Runner struct{ options Options }

func NewRunner(options Options) *Runner { return &Runner{options: options} }

// Command describes an external command.
type Command struct {
	Name string
	Args []string
	// Dir is the working directory ("" = current).
	Dir string
	// Env entries (KEY=VALUE) are appended to os.Environ().
	Env []string
	// Stdin is optional input.
	Stdin io.Reader
	// Stream inherits the parent's stdout/stderr instead of capturing them.
	Stream bool
	// Graceful lets the process stop on its own when ctx is canceled (a
	// terminal Ctrl-C reaches it directly): it gets SIGTERM after GraceDelay
	// and is killed KillDelay later. Otherwise cancellation kills it at once.
	Graceful bool
}

// Delays of Command.Graceful cancellation.
var (
	GraceDelay = 2 * time.Second
	KillDelay  = 10 * time.Second
)

// Result is the outcome of a finished command.
type Result struct {
	Stdout, Stderr []byte
	ExitCode       int
	Duration       time.Duration
}

// ExitError is returned when a command exits with a non-zero status.
type ExitError struct {
	// CmdLine is the printable command line.
	CmdLine string
	Result  *Result
}

// Error formats the failure with exit code and captured output.
func (e *ExitError) Error() string {
	return fmt.Sprintf("Command failed: %s\nExit code: %d\nSTDERR: %s\nSTDOUT: %s",
		e.CmdLine, e.Result.ExitCode, strings.TrimSpace(string(e.Result.Stderr)), strings.TrimSpace(string(e.Result.Stdout)))
}

// String returns the printable command line.
func (c Command) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Run executes c. A non-zero exit returns *ExitError (the Result is returned
// as well); a failure to start the process is returned wrapped.
func (r *Runner) Run(ctx context.Context, c Command) (*Result, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	cmd.Stdin = c.Stdin
	var terminated atomic.Bool
	if c.Graceful {
		cmd.Cancel = func() error {
			// Signal is a no-op once Wait has reaped the process.
			p := cmd.Process
			time.AfterFunc(GraceDelay, func() {
				if p.Signal(syscall.SIGTERM) == nil {
					terminated.Store(true)
				}
			})
			return nil
		}
		cmd.WaitDelay = GraceDelay + KillDelay
	}
	var stdout, stderr bytes.Buffer
	if c.Stream {
		cmd.Stdout, cmd.Stderr = r.options.Out, r.options.Err
	} else {
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
	}
	start := time.Now()
	err := cmd.Run()
	res := &Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Duration: time.Since(start)}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return res, fmt.Errorf("run %s: %w", c.String(), err)
		}
		// A graceful process that exited before SIGTERM reports its own status.
		if ctx.Err() != nil && (!c.Graceful || terminated.Load()) {
			return res, fmt.Errorf("run %s: %w", c.String(), ctx.Err())
		}
	}
	if r.options.OnComplete != nil {
		r.options.OnComplete(c, res)
	}
	if err != nil {
		return res, &ExitError{CmdLine: c.String(), Result: res}
	}
	return res, nil
}

// Shell runs script with `sh -c` in dir.
func (r *Runner) Shell(ctx context.Context, script, dir string) (*Result, error) {
	return r.Run(ctx, Command{Name: "sh", Args: []string{"-c", script}, Dir: dir})
}

// Output runs c and returns its trimmed stdout.
func (r *Runner) Output(ctx context.Context, c Command) (string, error) {
	res, err := r.Run(ctx, c)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}
