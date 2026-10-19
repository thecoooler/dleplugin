package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/config"
)

// Module is a compiled-in feature contributing top-level commands.
type Module interface {
	// Name returns the module name ("init", "build", "deploy", ...).
	Name() string
	// Commands returns the module's top-level commands.
	Commands(rt *Runtime) []*cobra.Command
}

// Command groups of the root help, listed in workflow order. Commands within
// a group keep their registration order; commands without a known group are
// listed under "Additional Commands".
const (
	GroupProject = "project"
	GroupCheck   = "check"
	GroupRelease = "release"
	GroupAgents  = "agents"
)

var groups = []*cobra.Group{
	{ID: GroupProject, Title: "Plugin project:"},
	{ID: GroupCheck, Title: "Checks:"},
	{ID: GroupRelease, Title: "Build and deploy:"},
	{ID: GroupAgents, Title: "AI agents:"},
}

// versioned is implemented by modules that have their own version.
type versioned interface{ Version() string }

// NewRootCommand builds the root command and registers modules in order.
// Rejected modules are recorded on rt (see Runtime.Modules).
func NewRootCommand(rt *Runtime, modules ...Module) *cobra.Command {
	root := &cobra.Command{
		Use:               "dleplugin",
		Short:             "CLI tools for developing DLE plugins",
		Long:              "CLI tools for developing DLE plugins",
		Version:           rt.Version,
		SilenceErrors:     true,
		SilenceUsage:      true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	cobra.EnableCommandSorting = false
	root.PersistentFlags().BoolP("verbose", "v", false, "Show verbose output including full tracebacks")
	root.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
		if v, err := cmd.Flags().GetBool("verbose"); err == nil && v {
			rt.UI.SetVerbose(true)
		}
	}

	rt.modules = nil
	reserved := map[string]bool{"help": true}

	counts := map[string]int{}
	for _, m := range modules {
		counts[m.Name()]++
	}
	for _, m := range modules {
		info := ModuleInfo{Name: m.Name(), Version: rt.Version}
		if v, ok := m.(versioned); ok {
			info.Version = v.Version()
		}
		if n := counts[m.Name()]; n > 1 {
			info.Err = fmt.Errorf("duplicate module name provided by %d modules", n)
			rt.modules = append(rt.modules, info)
			continue
		}
		cmds := m.Commands(rt)
		var conflicts []string
		for _, c := range cmds {
			if reserved[c.Name()] {
				conflicts = append(conflicts, "'"+c.Name()+"'")
			}
		}
		if len(conflicts) > 0 {
			sort.Strings(conflicts)
			info.Err = fmt.Errorf("command conflict: %s", strings.Join(conflicts, ", "))
			rt.modules = append(rt.modules, info)
			continue
		}
		for _, c := range cmds {
			reserved[c.Name()] = true
			root.AddCommand(c)
		}
		rt.modules = append(rt.modules, info)
	}
	addGroups(root)
	return root
}

// addGroups adds the groups that have commands; commands of unknown groups
// move to "Additional Commands".
func addGroups(root *cobra.Command) {
	used := map[string]bool{}
	for _, c := range root.Commands() {
		known := false
		for _, g := range groups {
			known = known || g.ID == c.GroupID
		}
		if !known {
			c.GroupID = ""
		}
		used[c.GroupID] = true
	}
	for _, g := range groups {
		if used[g.ID] {
			root.AddGroup(g)
		}
	}
}

// RequireConfig wraps run so that the core config is loaded first (a missing
// file is created and reported as an error). The loaded config is available
// through rt.CoreConfig().
func RequireConfig(rt *Runtime, run func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		core, err := rt.Config.Core(true)
		if err != nil {
			return err
		}
		rt.core = core
		return run(cmd, args)
	}
}

// OptionalConfig wraps run so that the core config is loaded when present.
// A missing file is ignored (rt.CoreConfig() stays nil); other config errors
// are returned.
func OptionalConfig(rt *Runtime, run func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		core, err := rt.Config.Core(false)
		if err != nil && !errors.Is(err, config.ErrNotFound) {
			return err
		}
		rt.core = core
		return run(cmd, args)
	}
}

// SilentExit makes Execute return Code without printing an error; the
// command has already reported the failure itself.
type SilentExit struct{ Code int }

func (e *SilentExit) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// Execute runs the CLI with args and returns the process exit code.
func Execute(ctx context.Context, rt *Runtime, args []string, modules ...Module) int {
	root := NewRootCommand(rt, modules...)
	root.SetArgs(args)
	root.SetIn(rt.UI.In)
	root.SetOut(rt.UI.Out)
	root.SetErr(rt.UI.Err)
	if err := root.ExecuteContext(ctx); err != nil {
		var silent *SilentExit
		if errors.As(err, &silent) {
			return silent.Code
		}
		rt.UI.CompactError(err.Error())
		if rt.UI.Verbose() {
			for e := errors.Unwrap(err); e != nil; e = errors.Unwrap(e) {
				fmt.Fprintf(rt.UI.Err, "  caused by [%T]: %v\n", e, e)
			}
		}
		return 1
	}
	return 0
}

// Main is the program entry point; it never returns.
func Main(modules ...Module) {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, modules...))
}

func run(args []string, in io.Reader, out, errOut io.Writer, modules ...Module) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	path, err := config.DefaultPath()
	if err != nil {
		path = "" // config-dependent commands will fail with a read error
	}
	rt := NewRuntime(in, out, errOut, path)
	return Execute(ctx, rt, args, modules...)
}
