package process

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
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
