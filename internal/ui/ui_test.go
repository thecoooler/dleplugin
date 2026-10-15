package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func newTest(in string) (*UI, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return New(strings.NewReader(in), &out, &errb), &out, &errb
}

func TestMessages(t *testing.T) {
	u, out, errb := newTest("")
	u.Success("ok")
	u.Info("plain")
	u.Error("bad")
	u.Warning("hmm")
	u.CompactError("boom")
	if out.String() != "✔ ok\nplain\n" {
		t.Errorf("out = %q", out.String())
	}
	if errb.String() != "✖ bad\n⚠ hmm\n✖ Error: boom\n" {
		t.Errorf("err = %q", errb.String())
	}
}

func TestDebugGating(t *testing.T) {
	u, _, errb := newTest("")
	u.Debugf("x %d", 1)
	if errb.Len() != 0 {
		t.Fatal("debug printed when not verbose")
	}
	u.SetVerbose(true)
	u.Debugf("x %d", 1)
	if errb.String() != "x 1\n" || !u.Verbose() {
		t.Errorf("err = %q", errb.String())
	}
}

func TestPrompt(t *testing.T) {
	u, out, _ := newTest("\nvalue\n\n")
	def := "d"
	got, _ := u.Prompt("Name", &def)
	if got != "d" {
		t.Errorf("got %q", got)
	}
	got, err := u.Prompt("Name", nil)
	if got != "value" || err != nil {
		t.Errorf("got %q %v", got, err)
	}
	if !strings.HasPrefix(out.String(), "Name [d]: Name: ") {
		t.Errorf("out = %q", out.String())
	}
	if _, err := u.Prompt("X", nil); !errors.Is(err, ErrAborted) || err.Error() != "aborted" {
		t.Errorf("err = %v", err)
	}
}

func TestPromptEmptyDefault(t *testing.T) {
	u, out, _ := newTest("\n")
	def := ""
	got, _ := u.Prompt("L", &def)
	if got != "" || out.String() != "L []: " {
		t.Errorf("got %q out %q", got, out.String())
	}
}

func TestConfirm(t *testing.T) {
	u, out, _ := newTest("maybe\nYES\n\nn\n")
	if v, _ := u.Confirm("Go", false); !v {
		t.Error("want yes")
	}
	if v, _ := u.Confirm("Go", true); !v {
		t.Error("want default true")
	}
	if v, _ := u.Confirm("Go", true); v {
		t.Error("want no")
	}
	if !strings.Contains(out.String(), "Error: invalid input\n") || !strings.Contains(out.String(), "Go [y/N]: ") || !strings.Contains(out.String(), "Go [Y/n]: ") {
		t.Errorf("out = %q", out.String())
	}
	if _, err := u.Confirm("Go", true); !errors.Is(err, ErrAborted) {
		t.Errorf("err = %v", err)
	}
}
