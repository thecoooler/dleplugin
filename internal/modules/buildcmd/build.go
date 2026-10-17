// Package buildcmd implements the build command module.
package buildcmd

import (
	"errors"
	"strings"

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
	var version, upgradeURL string
	cmd := &cobra.Command{
		Use:     "build",
		GroupID: app.GroupRelease,
		Short:   "Build the plugin",
		Long: `Build the plugin into dist/.

The plugin version (<version> in the XML) is --version when given, otherwise
the version field of dleplugin.yml (empty when unset). The upgrade URL
(<upgradeurl> in the XML) is --upgrade-url, otherwise the upgrade_url field of
dleplugin.yml; only https URLs are accepted.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			u, err := effectiveUpgradeURL(cmd, upgradeURL, p.Info().UpgradeURL)
			if err != nil {
				return err
			}
			if _, err := build.WriteDist(p.Root(), p.Info(), files, version, u); err != nil {
				return err
			}
			rt.UI.Success("Plugin built successfully")
			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "plugin version written to the XML (default: version from dleplugin.yml)")
	cmd.Flags().StringVar(&upgradeURL, "upgrade-url", "", "plugin upgrade URL written to the XML (default: upgrade_url from dleplugin.yml)")
	return cmd
}

// effectiveUpgradeURL returns the trimmed --upgrade-url, or manifestURL when
// the flag is unset. A flag given as blank is an error.
func effectiveUpgradeURL(cmd *cobra.Command, flag, manifestURL string) (string, error) {
	flag = strings.TrimSpace(flag)
	if !cmd.Flags().Changed("upgrade-url") {
		return manifestURL, nil
	}
	if flag == "" {
		return "", errors.New("--upgrade-url is empty")
	}
	return flag, nil
}
