// Package initcmd implements the init command module.
package initcmd

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/manifest"
	"github.com/thecoooler/dleplugin/internal/paths"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

type module struct{}

// Module returns the init command module.
func Module() app.Module { return module{} }

func (module) Name() string { return "init" }

func (module) Commands(rt *app.Runtime) []*cobra.Command {
	return []*cobra.Command{initCommand(rt)}
}

type fields struct{ slug, name, description, namespace string }

func (f *fields) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.slug, "slug", "", "Plugin slug")
	cmd.Flags().StringVar(&f.name, "name", "", "Plugin display name")
	cmd.Flags().StringVar(&f.description, "description", "", "Plugin description")
	cmd.Flags().StringVar(&f.namespace, "namespace", "", `PHP namespace of the plugin classes, e.g. Acme\Plugin (default: the slug)`)
}

// prompt asks for the values missing from flags. The slug defaults to
// defSlug, the name and the namespace to the slug (the namespace only when
// the slug is a valid PHP name).
func (f *fields) prompt(rt *app.Runtime, defSlug string) error {
	var err error
	if f.slug == "" {
		if f.slug, err = rt.UI.Prompt("Plugin slug", &defSlug); err != nil {
			return err
		}
	}
	if f.name == "" {
		if f.name, err = rt.UI.Prompt("Plugin display name", &f.slug); err != nil {
			return err
		}
	}
	if f.description == "" {
		empty := ""
		if f.description, err = rt.UI.Prompt("Plugin description", &empty); err != nil {
			return err
		}
	}
	if f.namespace == "" {
		// A slug that is not a PHP name cannot be the default namespace.
		def := &f.slug
		if manifest.ValidateNamespace(f.slug) != nil {
			def = nil
		}
		if f.namespace, err = rt.UI.Prompt("PHP namespace", def); err != nil {
			return err
		}
	}
	return nil
}

func initCommand(rt *app.Runtime) *cobra.Command {
	var f fields
	cmd := &cobra.Command{
		Use:     "init [DIR]",
		GroupID: app.GroupProject,
		Short:   "Initialize a plugin project",
		Long: `Write dleplugin.yml into DIR, creating the directory when it is missing.
DIR defaults to the current directory; its name is the default slug.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			wd, err := rt.WorkDir()
			if err != nil {
				return err
			}
			dir := wd
			if len(args) > 0 {
				dir = paths.Resolve(wd, args[0])
			}
			if err := f.prompt(rt, filepath.Base(dir)); err != nil {
				return err
			}
			if _, err := plugin.Init(dir, f.slug, f.name, f.description, f.namespace); err != nil {
				return err
			}
			rt.UI.Successf("Plugin \"%s\" initialized in %s", f.name, dir)
			return nil
		},
	}
	f.addFlags(cmd)
	return cmd
}
