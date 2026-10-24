package gui

import (
	"context"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/thecoooler/dleplugin/internal/app"
)

// Runner executes dleplugin commands in process, exactly as the CLI does,
// with the output sent to a writer instead of a terminal.
type Runner struct {
	// ConfigPath is the global config file (config.DefaultPath).
	ConfigPath string
	// Modules are the compiled-in CLI modules.
	Modules []app.Module

	// mu serializes runs: the root command touches cobra's package state.
	mu sync.Mutex
}

// Run executes args with dir as the working directory and stdin as the
// standard input, and returns the exit code. The input is not a terminal,
// so commands never prompt: GUI forms pass every value as a flag.
func (r *Runner) Run(ctx context.Context, dir string, args []string, stdin string, out io.Writer) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	rt := app.NewRuntime(strings.NewReader(stdin), out, out, r.ConfigPath)
	rt.SetWorkDir(dir)
	return app.Execute(ctx, rt, args, r.Modules...)
}

// ansi matches terminal escape sequences that tools such as docker print.
var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\a\x1b]*(?:\a|\x1b\\)`)

// lineWriter splits written bytes into lines without terminal escapes. A
// carriage return rewrites the current line, as progress output does.
// Complete lines go to emit; Flush emits a trailing partial line.
type lineWriter struct {
	mu      sync.Mutex
	partial string
	emit    func(lines []string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	text := strings.ReplaceAll(w.partial+string(p), "\r\n", "\n")
	parts := strings.Split(text, "\n")
	w.partial = parts[len(parts)-1]
	var lines []string
	for _, line := range parts[:len(parts)-1] {
		lines = append(lines, cleanLine(line))
	}
	if len(lines) > 0 {
		w.emit(lines)
	}
	return len(p), nil
}

// Flush emits the last line when it did not end with a newline.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.partial != "" {
		w.emit([]string{cleanLine(w.partial)})
		w.partial = ""
	}
}

// cleanLine drops escape sequences and keeps what follows the last carriage
// return.
func cleanLine(s string) string {
	s = ansi.ReplaceAllString(s, "")
	if i := strings.LastIndexByte(strings.TrimRight(s, "\r"), '\r'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimRight(s, "\r")
}

// errorLine returns the message of the last "✖ Error: ..." line of a command's
// output with the lines after it (messages may span several), or its last
// line when it has none.
func errorLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if msg, ok := strings.CutPrefix(lines[i], "✖ Error: "); ok {
			rest := []string{msg}
			for _, line := range lines[i+1:] {
				if strings.HasPrefix(line, "  caused by ") {
					break
				}
				rest = append(rest, line)
			}
			return strings.TrimSpace(strings.Join(rest, "\n"))
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return strings.TrimPrefix(s, "✖ ")
		}
	}
	return ""
}
