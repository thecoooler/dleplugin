// Package ui implements terminal output (status lines, colors) and prompts.
package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const (
	colorGreen  = "\x1b[32m"
	colorRed    = "\x1b[31m"
	colorYellow = "\x1b[33m"
	colorReset  = "\x1b[0m"
)

// ErrAborted is returned by prompts when input ends before an answer is read.
var ErrAborted = errors.New("aborted")

// UI writes user-facing messages and reads interactive answers.
type UI struct {
	// Out receives regular output, Err receives diagnostics, In provides answers.
	Out, Err io.Writer
	In       io.Reader

	verbose     bool
	outColor    bool
	errColor    bool
	interactive bool
	reader      *bufio.Reader
}

// New creates a UI. Colors are enabled per stream only when the stream is a
// terminal and the NO_COLOR environment variable is empty.
func New(in io.Reader, out, err io.Writer) *UI {
	noColor := os.Getenv("NO_COLOR") != ""
	return &UI{
		In: in, Out: out, Err: err,
		outColor: !noColor && isTerminal(out),
		errColor: !noColor && isTerminal(err),

		interactive: inTerminal(in),
	}
}

func inTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// SetVerbose enables or disables debug output.
func (u *UI) SetVerbose(v bool) { u.verbose = v }

// Verbose reports whether verbose output is enabled.
func (u *UI) Verbose() bool { return u.verbose }

// Interactive reports whether In is a terminal, i.e. a user can answer
// prompts.
func (u *UI) Interactive() bool { return u.interactive }

// SetInteractive overrides terminal detection (for tests).
func (u *UI) SetInteractive(v bool) { u.interactive = v }

func paint(enabled bool, color, s string) string {
	if !enabled {
		return s
	}
	return color + s + colorReset
}

// Success prints "✔ msg" in green to Out.
func (u *UI) Success(msg string) {
	fmt.Fprintln(u.Out, paint(u.outColor, colorGreen, "✔ "+msg))
}

// Successf is the formatted variant of Success.
func (u *UI) Successf(format string, args ...any) { u.Success(fmt.Sprintf(format, args...)) }

// Error prints "✖ msg" in red to Err.
func (u *UI) Error(msg string) {
	fmt.Fprintln(u.Err, paint(u.errColor, colorRed, "✖ "+msg))
}

// Errorf is the formatted variant of Error.
func (u *UI) Errorf(format string, args ...any) { u.Error(fmt.Sprintf(format, args...)) }

// Warning prints "⚠ msg" in yellow to Err.
func (u *UI) Warning(msg string) {
	fmt.Fprintln(u.Err, paint(u.errColor, colorYellow, "⚠ "+msg))
}

// Warningf is the formatted variant of Warning.
func (u *UI) Warningf(format string, args ...any) { u.Warning(fmt.Sprintf(format, args...)) }

// Info prints msg unchanged to Out.
func (u *UI) Info(msg string) { fmt.Fprintln(u.Out, msg) }

// Infof is the formatted variant of Info.
func (u *UI) Infof(format string, args ...any) { u.Info(fmt.Sprintf(format, args...)) }

// CompactError prints "✖ Error: msg" in red to Err.
func (u *UI) CompactError(msg string) {
	fmt.Fprintln(u.Err, paint(u.errColor, colorRed, "✖ Error: "+msg))
}

// Debugf prints to Err only when verbose output is enabled.
func (u *UI) Debugf(format string, args ...any) {
	if !u.verbose {
		return
	}
	fmt.Fprintln(u.Err, fmt.Sprintf(format, args...))
}

func (u *UI) readLine() (string, error) {
	if u.reader == nil {
		in := u.In
		if in == nil {
			in = strings.NewReader("")
		}
		u.reader = bufio.NewReader(in)
	}
	line, err := u.reader.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", ErrAborted
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Prompt asks for a value with click semantics. An empty answer returns def
// when it is non-nil and otherwise asks again. End of input yields ErrAborted.
func (u *UI) Prompt(label string, def *string) (string, error) {
	for {
		if def != nil {
			fmt.Fprintf(u.Out, "%s [%s]: ", label, *def)
		} else {
			fmt.Fprintf(u.Out, "%s: ", label)
		}
		line, err := u.readLine()
		if err != nil {
			fmt.Fprintln(u.Out)
			return "", err
		}
		if line == "" {
			if def != nil {
				return *def, nil
			}
			continue
		}
		return line, nil
	}
}

// PromptSecret reads a line like Prompt without a default. Echo is
// suppressed when In is a terminal.
func (u *UI) PromptSecret(label string) (string, error) {
	f, ok := u.In.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return u.Prompt(label, nil)
	}
	for {
		fmt.Fprintf(u.Out, "%s: ", label)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(u.Out)
		if err != nil {
			return "", ErrAborted
		}
		if len(b) > 0 {
			return string(b), nil
		}
	}
}

// Confirm asks a yes/no question. Empty input returns def; invalid input
// prints an error line and asks again.
func (u *UI) Confirm(label string, def bool) (bool, error) {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	for {
		fmt.Fprintf(u.Out, "%s %s: ", label, hint)
		line, err := u.readLine()
		if err != nil {
			fmt.Fprintln(u.Out)
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(u.Out, "Error: invalid input")
	}
}
