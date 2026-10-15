// Package check implements the check module: verifies that the plugin's file
// patches apply to given DLE directories.
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/dlexml"
	"github.com/thecoooler/dleplugin/internal/modules/check/internal/check"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

type module struct{}

// Module returns the check module.
func Module() app.Module { return module{} }

func (module) Name() string { return "check" }

func (module) Commands(rt *app.Runtime) []*cobra.Command {
	return []*cobra.Command{checkCommand(rt)}
}

func checkCommand(rt *app.Runtime) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:     "check DLE_DIR...",
		GroupID: app.GroupCheck,
		Short:   "Check the manifest's file patches against DLE directories",
		Long: `Check that the file patches of dleplugin.yml apply to the given DLE
directories: every patched file exists and the code searched by
before/after/replace operations is found in it (whitespace and case are
ignored). Generated patches (autoloader, admin section) and extra
configs are included; nothing is written.

DLE_DIR is a DLE installation or unpacked distribution (the directory with
engine/inc/include/init.php); several directories are checked one by one.
Use --file to check a ready plugin .xml or .zip instead of the plugin in the
current directory.

Code found more than once is a warning. Exits with code 1 when a patch does
not apply to some directory.`,
		Example: `  dleplugin check ~/dle/18.1
  dleplugin check ~/dists/*
  dleplugin check -f dist/my_plugin.xml ~/dle/17.0 ~/dle/18.1`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := rt.WorkDir()
			if err != nil {
				return err
			}
			ops, err := operations(wd, file)
			if err != nil {
				return err
			}
			dists := make([]check.Dist, len(args))
			for i, dir := range args {
				if dists[i], err = check.OpenDist(absPath(wd, dir)); err != nil {
					return err
				}
			}
			failed := 0
			for _, d := range dists {
				r, err := check.Run(d, ops)
				if err != nil {
					return err
				}
				if r.Failed() {
					failed++
				}
				printReport(rt, r)
			}
			if failed > 0 {
				rt.UI.Errorf("Patches do not apply to %d of %d DLE directories", failed, len(dists))
				return &app.SilentExit{Code: 1}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Check a ready plugin file (.xml or .zip) instead of dleplugin.yml")
	return cmd
}

// operations returns the operations of file, or of the plugin around wd when
// file is empty.
func operations(wd, file string) ([]check.Operation, error) {
	if file == "" {
		p, err := plugin.FromDir(wd)
		if err != nil {
			return nil, err
		}
		return check.FromInfo(p.Info()), nil
	}
	file = absPath(wd, file)
	if st, err := os.Stat(file); err != nil {
		return nil, fmt.Errorf("plugin file: %w", err)
	} else if st.IsDir() {
		return nil, fmt.Errorf("plugin file %s is a directory", file)
	}
	root, err := dlexml.ParsePluginFile(file)
	if err != nil {
		return nil, fmt.Errorf("read plugin file %s: %w", file, err)
	}
	return check.FromXML(root), nil
}

func absPath(wd, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(wd, p)
}

func printReport(rt *app.Runtime, r check.Report) {
	label := r.Dist.Dir
	if r.Dist.Version != "" {
		label = "DLE " + r.Dist.Version + " (" + r.Dist.Dir + ")"
	}
	status := "OK"
	if r.Failed() {
		status = "ERROR"
	}
	rt.UI.Infof("%s: %s", label, status)
	for _, p := range r.Problems {
		switch p.Kind {
		case check.FileNotFound:
			rt.UI.Infof("  FILE_NOT_FOUND: %s", p.File)
		case check.Duplicate:
			rt.UI.Infof("  [WARNING] DUPLICATE in %s: %s (%d matches)", p.File, strconv.Quote(p.Search), p.Count)
		case check.EmptySearch:
			rt.UI.Infof("  EMPTY_SEARCH in %s", p.File)
		default:
			rt.UI.Infof("  %s in %s: %s", p.Kind, p.File, strconv.Quote(p.Search))
		}
	}
}
