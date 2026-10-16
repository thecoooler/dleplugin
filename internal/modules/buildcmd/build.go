// Package buildcmd implements the build command module.
package buildcmd

import (
	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/build"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

type module struct{}

// Module returns the build command module.
func Module() app.Module { return module{} }

func (module) Name() string { return "build" }

func (module) Commands(rt *app.Runtime) []*cobra.Command {
	return []*cobra.Command{buildCommand(rt)}
}

func buildCommand(rt *app.Runtime) *cobra.Command {
	var version string
	cmd := &cobra.Command{
		Use:     "build",
		GroupID: app.GroupRelease,
		Short:   "Build the plugin",
		Long: `Build the plugin into dist/.

The plugin version (<version> in the XML) is --version when given, otherwise
the version field of dleplugin.yml (empty when unset).`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			wd, err := rt.WorkDir()
			if err != nil {
				return err
			}
			p, err := plugin.FromDir(wd)
			if err != nil {
				return err
			}
			files, err := p.Files()
			if err != nil {
				return err
			}
			if version == "" {
				version = p.Info().Version
			}
			if _, err := build.WriteDist(p.Root(), p.Info(), files, version); err != nil {
				return err
			}
			rt.UI.Success("Plugin built successfully")
			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "plugin version written to the XML (default: version from dleplugin.yml)")
	return cmd
}
