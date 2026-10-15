// Package convertcmd implements the convert command module.
package convertcmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/convert"
	"github.com/thecoooler/dleplugin/internal/paths"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

type module struct{}

// Module returns the convert command module.
func Module() app.Module { return module{} }

func (module) Name() string { return "convert" }

func (module) Commands(rt *app.Runtime) []*cobra.Command {
	return []*cobra.Command{convertCommand(rt)}
}

func convertCommand(rt *app.Runtime) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{
		Use:     "convert XML_PATH [PLUGIN_PATH]",
		GroupID: app.GroupProject,
		Short:   "Convert a plugin XML file to dleplugin.yml",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			wd, err := rt.WorkDir()
			if err != nil {
				return err
			}
			pluginPath := "."
			if len(args) > 1 {
				pluginPath = args[1]
			}
			xmlPath := paths.Resolve(wd, args[0])
			dir := paths.Resolve(wd, pluginPath)
			ymlPath := filepath.Join(dir, plugin.Marker)
			srcPath := filepath.Join(dir, "src")
			for _, p := range []string{ymlPath, srcPath} {
				if _, err := os.Lstat(p); err == nil {
					return errors.New("dleplugin.yml or the src directory already exists")
				}
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create directory %s: %w", dir, err)
			}
			yml, err := convert.Convert(xmlPath, srcPath, namespace)
			if err != nil {
				return err
			}
			if err := os.WriteFile(ymlPath, []byte(yml), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", ymlPath, err)
			}
			rt.UI.Success("File dleplugin.yml was created")
			return nil
		},
	}
	cmd.Flags().StringVar(&namespace, "namespace", "", `PHP namespace of the plugin classes, e.g. Acme\Plugin (default: from the plugin file paths)`)
	return cmd
}
