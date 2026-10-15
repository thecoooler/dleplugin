package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type entryModule struct{}

func (entryModule) Name() string { return "entry-test" }
func (entryModule) Commands(*Runtime) []*cobra.Command {
	return []*cobra.Command{
		{Use: "ok", RunE: func(*cobra.Command, []string) error { return nil }},
		{Use: "fail", RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("outer: %w", fmt.Errorf("middle: %w", errors.New("root cause")))
		}},
	}
}

func TestRunExitAndDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		code    int
		want    string
		verbose bool
	}{
		{[]string{"ok"}, 0, "", false},
		{[]string{"--unknown"}, 1, "✖ Error: unknown flag: --unknown", false},
		{[]string{"fail"}, 1, "✖ Error: outer: middle: root cause", false},
		{[]string{"-v", "fail"}, 1, "caused by [*errors.errorString]: root cause", true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, diag bytes.Buffer
			code := run(tc.args, strings.NewReader(""), &out, &diag, entryModule{})
			if code != tc.code || !strings.Contains(diag.String(), tc.want) {
				t.Fatalf("code %d, diagnostic %q", code, &diag)
			}
			if !tc.verbose && strings.Contains(diag.String(), "caused by") {
				t.Fatal("verbose chain without -v")
			}
			if tc.verbose && !strings.Contains(diag.String(), "caused by [*fmt.wrapError]: middle: root cause") {
				t.Fatal(diag.String())
			}
		})
	}
}

func TestRuntimeProcessDiagnostics(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		var out, diag bytes.Buffer
		rt := NewRuntime(strings.NewReader(""), &out, &diag, "config.yaml")
		rt.UI.SetVerbose(verbose)
		if rt.Version != Version || rt.Config.Path() != "config.yaml" || rt.CoreConfig() != nil {
			t.Fatal("runtime defaults")
		}
		if _, err := rt.Exec.Shell(context.Background(), "printf output; printf diagnostic >&2", ""); err != nil {
			t.Fatal(err)
		}
		if verbose {
			for _, want := range []string{"-> 0", "STDERR: diagnostic", "STDOUT: output"} {
				if !strings.Contains(diag.String(), want) {
					t.Fatalf("missing %q: %s", want, &diag)
				}
			}
		} else if diag.Len() != 0 {
			t.Fatal(diag.String())
		}
		if _, err := rt.Exec.Shell(context.Background(), ":", ""); err != nil {
			t.Fatal(err)
		}
	}
}
