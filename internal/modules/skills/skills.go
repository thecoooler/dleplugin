// Package skills implements explicit installation of bundled AI agent skills.
package skills

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/skills/internal/bundle"
)

type module struct{}

// Module returns the skills command module.
func Module() app.Module    { return module{} }
func (module) Name() string { return "skills" }
func (module) Commands(rt *app.Runtime) []*cobra.Command {
	root := &cobra.Command{
		Use: "skills", GroupID: app.GroupAgents, Short: "Manage bundled AI agent skills",
		Long: "List or explicitly install both bundled skills for Codex or Claude Code.\nNo network or dleplugin configuration is required. Project scope uses the current directory.",
	}
	root.AddCommand(&cobra.Command{
		Use: "list", Short: "List skills bundled with this CLI", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			rt.UI.Infof("Bundled skills (CLI %s; source %s, with bundled fixes):", rt.Version, bundle.Source)
			for _, name := range bundle.Names() {
				rt.UI.Infof("  %s", name)
			}
			return nil
		},
	})
	for _, remove := range []bool{false, true} {
		var agent string
		var global, force bool
		use, short := "install", "Install or update both bundled skills"
		if remove {
			use, short = "uninstall", "Remove unchanged skills managed by this CLI"
		}
		cmd := &cobra.Command{
			Use: use, Short: short, Args: cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				wd, err := rt.WorkDir()
				if err != nil {
					return err
				}
				var home string
				if global {
					home, err = os.UserHomeDir()
					if err != nil {
						return err
					}
				}
				target, err := bundle.Target(wd, home, agent, global)
				if err != nil {
					return err
				}
				if remove {
					if err := bundle.Uninstall(target); err != nil {
						return err
					}
					rt.UI.Successf("Managed skills removed from %s", target)
				} else {
					for _, change := range bundle.Downgrades(target, rt.Version) {
						rt.UI.Warningf("Downgrading %s skills from CLI %s to %s", change.Name, change.From, change.To)
					}
					if err := bundle.Install(target, rt.Version, force); err != nil {
						return err
					}
					rt.UI.Successf("Installed dleplugin and dle-admin-ui (CLI %s) in %s", rt.Version, target)
				}
				return nil
			},
		}
		cmd.Flags().StringVar(&agent, "agent", "", "Target agent: codex or claude (required)")
		cmd.Flags().BoolVar(&global, "global", false, "Use the user's home directory instead of the current project")
		if !remove {
			cmd.Flags().BoolVar(&force, "force", false, "Replace modified or unmanaged skill directories (discards their contents)")
		}
		root.AddCommand(cmd)
	}
	return []*cobra.Command{root}
}
