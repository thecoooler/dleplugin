// Package app wires the dleplugin CLI: runtime services, module registration,
// the root command and error rendering.
package app

import (
	"io"
	"os"
	"strings"

	"github.com/thecoooler/dleplugin/internal/config"
	"github.com/thecoooler/dleplugin/internal/process"
	"github.com/thecoooler/dleplugin/internal/ui"
)

// Version is the binary version; override with
// -ldflags "-X github.com/thecoooler/dleplugin/internal/app.Version=...".
var Version = "dev"

// Runtime is the dependency container handed to modules.
type Runtime struct {
	// UI handles terminal I/O.
	UI *ui.UI
	// Config is the lazily loaded global config.
	Config *config.Store
	// Exec runs external commands.
	Exec *process.Runner
	// Version is the binary version.
	Version string

	workDir string
	core    *config.Core
	modules []ModuleInfo
}

// ModuleInfo describes a module offered for registration.
type ModuleInfo struct {
	// Name and Version identify the module.
	Name, Version string
	// Err is non-nil when the module was rejected.
	Err error
}

// NewRuntime builds a Runtime with the given streams and config file path.
func NewRuntime(in io.Reader, out, errOut io.Writer, configPath string) *Runtime {
	u := ui.New(in, out, errOut)
	return &Runtime{
		UI:     u,
		Config: config.NewStore(configPath),
		Exec: process.NewRunner(process.Options{
			Out: u.Out, Err: u.Err,
			OnComplete: func(command process.Command, result *process.Result) {
				if !u.Verbose() {
					return
				}
				u.Debugf("[%.2fs] %s -> %d", result.Duration.Seconds(), command, result.ExitCode)
				if s := strings.TrimSpace(string(result.Stderr)); s != "" {
					u.Debugf("STDERR: %s", s)
				}
				if s := strings.TrimSpace(string(result.Stdout)); s != "" {
					u.Debugf("STDOUT: %s", s)
				}
			},
		}),
		Version: Version,
	}
}

// WorkDir returns the invocation directory (os.Getwd unless overridden).
func (rt *Runtime) WorkDir() (string, error) {
	if rt.workDir != "" {
		return rt.workDir, nil
	}
	return os.Getwd()
}

// SetWorkDir overrides the invocation directory (for tests).
func (rt *Runtime) SetWorkDir(dir string) { rt.workDir = dir }

// CoreConfig returns the core config loaded by RequireConfig/OptionalConfig,
// or nil when it has not been loaded.
func (rt *Runtime) CoreConfig() *config.Core { return rt.core }

// Modules returns all modules offered for registration, in order, including
// rejected ones.
func (rt *Runtime) Modules() []ModuleInfo { return append([]ModuleInfo(nil), rt.modules...) }

// ModuleErrors returns the load error of each rejected module, keyed by name
// (the first error wins for duplicate names).
func (rt *Runtime) ModuleErrors() map[string]error {
	m := map[string]error{}
	for _, mi := range rt.modules {
		if mi.Err != nil {
			if _, ok := m[mi.Name]; !ok {
				m[mi.Name] = mi.Err
			}
		}
	}
	return m
}
