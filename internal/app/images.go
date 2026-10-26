package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/dockerimg"
)

// toolImagesName is the hidden command listing the modules' tool images.
const toolImagesName = "tool-images"

// imageProvider is implemented by modules whose commands run tool images.
type imageProvider interface{ Images() []dockerimg.Image }

// toolImagesCommand prints the tags Ensure looks for, one per line, and with
// --context writes every build context to DIR/<name>. CI publishes the images
// with it, so the registry tags always match the embedded build contexts.
func toolImagesCommand(rt *Runtime, images []dockerimg.Image) *cobra.Command {
	var contextDir string
	cmd := &cobra.Command{
		Use:    toolImagesName,
		Short:  "Print the tool image tags (used by CI to publish them)",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var tags []string
			for _, img := range images {
				if contextDir != "" {
					if err := img.WriteContext(filepath.Join(contextDir, img.Name)); err != nil {
						return fmt.Errorf("write %s build context: %w", img.Name, err)
					}
				}
				tags = append(tags, img.Tag())
			}
			if len(tags) > 0 {
				fmt.Fprintln(rt.UI.Out, strings.Join(tags, "\n"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&contextDir, "context", "", "Write the build contexts to `DIR`/<name>")
	return cmd
}
