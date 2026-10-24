// Command dleplugin-gui is the desktop app for developing DLE plugins: a
// native window over the commands of the dleplugin CLI.
package main

import (
	"log"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/gui"
	"github.com/thecoooler/dleplugin/internal/modules/buildcmd"
	"github.com/thecoooler/dleplugin/internal/modules/check"
	"github.com/thecoooler/dleplugin/internal/modules/convertcmd"
	"github.com/thecoooler/dleplugin/internal/modules/deploy"
	"github.com/thecoooler/dleplugin/internal/modules/diffcmd"
	"github.com/thecoooler/dleplugin/internal/modules/hosts"
	"github.com/thecoooler/dleplugin/internal/modules/initcmd"
	"github.com/thecoooler/dleplugin/internal/modules/skills"
	"github.com/thecoooler/dleplugin/internal/modules/static"
	"github.com/thecoooler/dleplugin/internal/modules/test"
)

func main() {
	if err := gui.Main(modules()...); err != nil {
		log.Fatal(err)
	}
}

// modules lists the CLI modules the app runs commands of; keep it in step
// with cmd/dleplugin.
func modules() []app.Module {
	return []app.Module{initcmd.Module(), buildcmd.Module(), convertcmd.Module(), diffcmd.Module(), deploy.Module(), static.Module(), test.Module(), check.Module(), hosts.Module(), skills.Module()}
}
