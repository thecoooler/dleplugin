package ui

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestPromptSecretPiped(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		aborted     bool
	}{
		{"secret\n", "secret", false}, {"\n\r\nsecret", "secret", false}, {"", "", true}, {"\n", "", true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			u, out, _ := newTest(tc.input)
			got, err := u.PromptSecret("Password")
			if got != tc.want || errors.Is(err, ErrAborted) != tc.aborted {
				t.Fatalf("got %q, %v", got, err)
			}
			if strings.Contains(out.String(), "secret") {
				t.Fatal("password echoed")
			}
		})
	}
	u := New(nil, io.Discard, io.Discard)
	if _, err := u.PromptSecret("Password"); !errors.Is(err, ErrAborted) {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("file-secret\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	u = New(f, io.Discard, io.Discard)
	if got, err := u.PromptSecret("Password"); got != "file-secret" || err != nil {
		t.Fatalf("%q %v", got, err)
	}
}

func TestFormattedMessagesColors(t *testing.T) {
	for _, color := range []bool{false, true} {
		u, out, errb := newTest("")
		u.outColor, u.errColor = color, color
		u.Successf("done %d", 2)
		u.Warningf("warning %d", 3)
		u.Errorf("error %d", 4)
		u.Infof("info %d", 5)
		wantOut := paint(color, colorGreen, "✔ done 2") + "\ninfo 5\n"
		wantErr := paint(color, colorYellow, "⚠ warning 3") + "\n" + paint(color, colorRed, "✖ error 4") + "\n"
		if out.String() != wantOut || errb.String() != wantErr {
			t.Fatalf("out %q err %q", out, errb)
		}
	}
	t.Setenv("NO_COLOR", "1")
	u, _, _ := newTest("")
	if u.outColor || u.errColor {
		t.Fatal("NO_COLOR ignored")
	}
}

func TestNonTerminalDeviceDisablesColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	u := New(strings.NewReader(""), f, f)
	if u.outColor || u.errColor {
		t.Fatal("colors enabled for a non-terminal character device")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if isTerminal(f) {
		t.Fatal("closed file detected as terminal")
	}
}
