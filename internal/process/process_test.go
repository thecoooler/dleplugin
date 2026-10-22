package process

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunOK(t *testing.T) {
	r := NewRunner(Options{})
	res, err := r.Run(context.Background(), Command{Name: "true"})
	if err != nil || res.ExitCode != 0 {
		t.Fatal(res, err)
	}
}

func TestRunFail(t *testing.T) {
	r := NewRunner(Options{})
	_, err := r.Shell(context.Background(), "echo out; echo err >&2; exit 3", "")
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Result.ExitCode != 3 {
		t.Fatalf("err = %v", err)
	}
	want := "Command failed: sh -c echo out; echo err >&2; exit 3\nExit code: 3\nSTDERR: err\nSTDOUT: out"
	if err.Error() != want {
		t.Errorf("got %q", err.Error())
	}
}

func TestFalse(t *testing.T) {
	var ee *ExitError
	if _, err := NewRunner(Options{}).Run(context.Background(), Command{Name: "false"}); !errors.As(err, &ee) || ee.Result.ExitCode != 1 {
		t.Fatal(err)
	}
}

func TestStartFailure(t *testing.T) {
	_, err := NewRunner(Options{}).Run(context.Background(), Command{Name: "definitely-not-a-command-xyz"})
	var ee *ExitError
	if err == nil || errors.As(err, &ee) {
		t.Fatalf("err = %v", err)
	}
}

func TestOutputEnvDirStdin(t *testing.T) {
	r := NewRunner(Options{})
	out, err := r.Output(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo $FOO; pwd; cat"}, Dir: "/", Env: []string{"FOO=bar"}, Stdin: strings.NewReader("in")})
	if err != nil || out != "bar\n/\nin" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCompletionObserver(t *testing.T) {
	called := false
	r := NewRunner(Options{OnComplete: func(command Command, result *Result) {
		called = true
		if command.String() != "sh -c echo hi" || string(result.Stdout) != "hi\n" || result.ExitCode != 0 {
			t.Fatalf("completion: %v, %+v", command, result)
		}
	}})
	if _, err := r.Shell(context.Background(), "echo hi", ""); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("missing completion notification")
	}
}

func TestStream(t *testing.T) {
	var o, e bytes.Buffer
	if _, err := NewRunner(Options{Out: &o, Err: &e}).Run(context.Background(), Command{Name: "echo", Args: []string{"x"}, Stream: true}); err != nil {
		t.Fatal(err)
	}
	if o.String() != "x\n" {
		t.Errorf("out = %q", o.String())
	}
}

func TestGracefulCancel(t *testing.T) {
	defer func(grace, kill time.Duration) { GraceDelay, KillDelay = grace, kill }(GraceDelay, KillDelay)
	GraceDelay, KillDelay = 500*time.Millisecond, time.Second
	r := NewRunner(Options{})
	for _, tc := range []struct {
		name, script string
		min, max     time.Duration
	}{
		// Exits on its own within GraceDelay, as after a terminal Ctrl-C.
		{"own exit", "sleep 0.1; exit 7", 0, 500 * time.Millisecond},
		{"terminated", "exec sleep 30", 500 * time.Millisecond, 1400 * time.Millisecond},
		{"killed", "trap '' TERM; while :; do sleep 0.05; done", 1500 * time.Millisecond, 10 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(50*time.Millisecond, cancel)
			start := time.Now()
			_, err := r.Run(ctx, Command{Name: "sh", Args: []string{"-c", tc.script}, Graceful: true})
			var ee *ExitError
			if tc.name == "own exit" {
				if !errors.As(err, &ee) || ee.Result.ExitCode != 7 {
					t.Fatalf("err = %v", err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v", err)
			}
			if d := time.Since(start); d < tc.min || d > tc.max {
				t.Fatalf("took %v", d)
			}
		})
	}
}
