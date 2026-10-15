// Package diffcmd implements the diff command module.
package diffcmd

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/diff"
	"github.com/thecoooler/dleplugin/internal/paths"
)

type module struct{}

// Module returns the diff command module.
func Module() app.Module { return module{} }

func (module) Name() string { return "diff" }

func (module) Commands(rt *app.Runtime) []*cobra.Command {
	return []*cobra.Command{diffCommand(rt)}
}

func diffCommand(rt *app.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "diff SOURCE1 SOURCE2",
		GroupID: app.GroupRelease,
		Short:   "Compare plugin XML files and projects",
		Long: `Compare two plugin sources: XML files, plugin zip archives, or project
directories containing dleplugin.yml (including their subdirectories).

Projects are built to XML in memory with build.split respected. Zip archives
must contain an XML entry named after the archive. Only XML content is compared;
separately packaged source files are excluded. Version, slug, and mount metadata
are ignored. Differences are printed without failing the command.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := rt.WorkDir()
			if err != nil {
				return err
			}
			var sources [2]string
			for i := range sources {
				sources[i], err = diff.ForDiff(paths.Resolve(wd, args[i]))
				if err != nil {
					return &diffSourceError{source: args[i], path: paths.Resolve(wd, args[i]), err: err}
				}
			}
			rt.UI.Infof("Comparing %s -> %s", args[0], args[1])
			// diff's exit status (differences found) is not an error.
			_, err = diff.RunYAML(cmd.Context(), sources[0], sources[1], cmd.OutOrStdout(), cmd.ErrOrStderr())
			return err
		},
	}
}

// diffSourceError keeps loader errors tied to the argument as typed, without
// repeating the source path already supplied by XML and archive loaders.
type diffSourceError struct {
	source, path string
	err          error
}

func (e *diffSourceError) Error() string {
	if errors.Is(e.err, diff.ErrSourceNotFound) {
		return "Source not found: " + e.source
	}
	message := strings.ReplaceAll(e.err.Error(), e.path, e.source)
	if strings.HasPrefix(message, e.source+": ") || strings.HasPrefix(message, e.source+"!") {
		return message
	}
	return e.source + ": " + message
}

func (e *diffSourceError) Unwrap() error { return e.err }
