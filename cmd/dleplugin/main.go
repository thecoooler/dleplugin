// Command dleplugin is a CLI for developing DLE plugins.
package main

import (
	"github.com/thecoooler/dleplugin/internal/app"
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
	app.Main(modules()...)
}

// modules lists the compiled-in modules in registration order.
func modules() []app.Module {
	return []app.Module{initcmd.Module(), buildcmd.Module(), convertcmd.Module(), diffcmd.Module(), deploy.Module(), static.Module(), test.Module(), check.Module(), hosts.Module(), skills.Module()}
}
