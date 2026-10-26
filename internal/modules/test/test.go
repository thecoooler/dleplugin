// Package test implements the test module: Pest tests of a plugin against the
// DataLife Engine trial.
package test

import (
	"errors"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/dockerimg"
	"github.com/thecoooler/dleplugin/internal/modules/test/internal/dletest"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

type module struct{}

// Module returns the test module.
func Module() app.Module { return module{} }

func (module) Name() string { return "test" }

func (module) Commands(rt *app.Runtime) []*cobra.Command {
	return []*cobra.Command{testCommand(rt)}
}

// Images returns the image running the DLE installer and Pest.
func (module) Images() []dockerimg.Image { return []dockerimg.Image{dletest.Image} }

func testCommand(rt *app.Runtime) *cobra.Command {
	var (
		o           dletest.Options
		coverageMin float64
	)
	cmd := &cobra.Command{
		Use:     "test [FILES...]",
		GroupID: app.GroupCheck,
		Short:   "Run the plugin tests",
		Long: `Run the plugin's Pest tests (tests/) in Docker against DataLife Engine.

The free DLE trial is downloaded from dle-news.ru and installed once (MariaDB
in Docker); every run connects the plugin to a fresh copy of the installation
with a fresh database. FILES are relative to the current directory.

The plugin's dependencies (the "dependencies" of dleplugin.yml, plugin names
looked up in dependency_roots, recursively) and the "depends" of dletest.yml
are connected before it. Exits with Pest's exit code.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := rt.WorkDir()
			if err != nil {
				return err
			}
			p, err := plugin.FromDir(wd)
			if err != nil {
				return err
			}
			cfg, err := dletest.LoadConfig(rt.Config)
			if err != nil {
				return err
			}
			conf, err := dletest.LoadPluginConf(p.Root())
			if err != nil {
				return err
			}
			if o.Dependencies, err = dletest.Dependencies(p, conf, cfg.DependencyRoots); err != nil {
				return err
			}
			o.Plugin = p
			o.Files = plugin.NormalizeFiles(args, p.Root(), wd)
			if cmd.Flags().Changed("coverage-min") {
				o.CoverageMin = &coverageMin
			}
			if o.CoverageHTML != "" && !filepath.IsAbs(o.CoverageHTML) {
				o.CoverageHTML = filepath.Join(wd, o.CoverageHTML)
			}
			o.Coverage = o.Coverage || o.CoverageMin != nil || o.CoverageHTML != ""
			o.TrialURL = cfg.TrialURL
			o.StreamBuild = rt.UI.Verbose()
			o.Notify = func(msg string) { rt.UI.Info(msg) }

			code, err := dletest.Run(cmd.Context(), rt.Exec, o)
			if errors.Is(err, dletest.ErrNoTests) {
				rt.UI.Warning("[WARNING] No tests found")
				return nil
			}
			if err != nil {
				return err
			}
			if code != 0 {
				return &app.SilentExit{Code: code}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.Filter, "filter", "f", "", "Run only tests matching the pattern")
	f.BoolVarP(&o.UpdateSnapshots, "update-snapshots", "u", false, "Update test snapshots")
	f.BoolVar(&o.Coverage, "coverage", false, "Show a code coverage report")
	f.Float64Var(&coverageMin, "coverage-min", 0, "Fail when coverage is below this percentage (implies --coverage)")
	f.StringVar(&o.CoverageHTML, "coverage-html", "", "Write an HTML coverage report to this directory (implies --coverage)")
	f.BoolVar(&o.Refresh, "refresh-dle", false, "Download and install the DLE trial again")
	return cmd
}
