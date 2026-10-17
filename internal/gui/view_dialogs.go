package gui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/thecoooler/dleplugin/internal/plugin"
)

// dialogs builds the dialogs that are open, above the page.
func (a *App) dialogs(c *ui.Context) {
	if a.initAsk {
		switch ui.AlertDialog(c, &a.initAsk, "No dleplugin.yml", "“"+a.initAskDir+"” is not inside a plugin project. Create a new project in it?", "Cancel", "Create project…") {
		case 1:
			a.openNewProject(a.initAskDir)
		}
	}
	if a.removeAsk {
		if pv := a.current(); pv != nil {
			if ui.AlertDialog(c, &a.removeAsk, "Remove “"+pv.title()+"”?", "The project leaves the list; its files stay in "+pv.root+".", "Cancel", "Remove") == 1 {
				a.removeProject(pv.root)
			}
		} else {
			a.removeAsk = false
		}
	}
	if a.siteRemove {
		r := a.siteRemoved
		if ui.AlertDialog(c, &a.siteRemove, "Remove "+r.host+"?", "The site is removed from "+r.path+", with its saved password.", "Cancel", "Remove") == 1 {
			a.removeSite(r)
		}
	}
	ui.Modal(c, &a.initOpen, func() { a.initDialog(c) }).Width(460)
	if ui.Modal(c, &a.siteOpen, func() { a.siteDialog(c) }).Width(480).Dismissed() && a.busy() {
		a.siteOpen = true // the login check is running
	}
}

func dialogTitle(c *ui.Context, title, description string) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, title).FontSize(17).Bold()
		if description != "" {
			ui.Text(c, description).FontSize(12).TextColor(c.Theme().TextMuted)
		}
	})
}

func dialogButtons(c *ui.Context, fn func()) {
	ui.Row(c).Gap(8).Justify(ui.End).Children(fn)
}

func (a *App) initDialog(c *ui.Context) {
	f := &a.initForm
	ui.Column(c).Gap(14).Children(func() {
		dialogTitle(c, "New plugin project", "Writes dleplugin.yml into the folder, creating it when it is missing.")
		ui.Form(c, func() {
			ui.Field(c, "Folder", func() {
				ui.Row(c).Gap(8).Children(func() {
					ui.TextInput(c, &f.dir).Placeholder("/path/to/plugin").Grow(1)
					if ui.Button(c.Key("choose-init-dir"), "Choose…").Clicked() {
						a.chooseInitDir()
					}
				})
			})
			ui.Field(c, "Slug", func() {
				ui.TextInput(c, &f.slug).Placeholder("my_plugin")
			}).Description("The plugin's file name: letters, digits, _ and -.")
			ui.Field(c, "Name", func() { ui.TextInput(c, &f.name).Placeholder(orDefault(f.slug, "the slug")) })
			ui.Field(c, "Description", func() { ui.TextInput(c, &f.description) })
			ui.Field(c, "PHP namespace", func() {
				ui.TextInput(c, &f.namespace).Placeholder(orDefault(f.slug, `Acme\Plugin`))
			})
		})
		errorText(c, a.initErr)
		dialogButtons(c, func() {
			if ui.Button(c.Key("init-cancel"), "Cancel").Clicked() {
				a.initOpen = false
			}
			if ui.PrimaryButton(c.Key("init-create"), "Create").Clicked() {
				a.createProject()
			}
		})
	})
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func (a *App) chooseInitDir() {
	a.background(func() {
		dir, err := a.desk.chooseDirectory("Plugin project folder")
		a.post(func() {
			if err != nil {
				a.initErr = err.Error()
			} else if dir != "" {
				a.initForm.dir = dir
				if strings.TrimSpace(a.initForm.slug) == "" {
					a.initForm.slug = filepath.Base(dir)
				}
			}
		})
	})
}

// createProject writes dleplugin.yml as init does, with the defaults init
// prompts with: the name is the slug, and so is the namespace.
func (a *App) createProject() {
	f := a.initForm
	dir := strings.TrimSpace(f.dir)
	if dir == "" {
		a.initErr = "Choose the project folder."
		return
	}
	if !filepath.IsAbs(dir) {
		a.initErr = "The folder must be an absolute path."
		return
	}
	slug := strings.TrimSpace(f.slug)
	name := strings.TrimSpace(f.name)
	if name == "" {
		name = slug
	}
	p, err := plugin.Init(filepath.Clean(dir), slug, name, strings.TrimSpace(f.description), strings.TrimSpace(f.namespace))
	if err != nil {
		a.initErr = err.Error()
		return
	}
	a.initOpen = false
	a.selectProject(p.Root())
	a.notify("Project "+name+" created", false)
}

func (a *App) siteDialog(c *ui.Context) {
	f := &a.siteForm
	title := "Add site"
	if f.edit {
		title = "Edit " + f.target
	}
	scope := "this project's dleplugin-deploy.yaml"
	if f.global {
		scope = "the global dleplugin-deploy.yaml"
	}
	ui.Column(c).Gap(14).Children(func() {
		dialogTitle(c, title, "The login is checked on the site before it is saved in "+scope+". Passwords go to the OS credential store, never to the file.")
		ui.Form(c, func() {
			ui.Field(c, "Host or admin URL", func() {
				ui.TextInput(c, &f.target).Placeholder("example.com or https://example.com/admin.php").ReadOnly(f.edit).AutoFocus()
			}).Description("https is assumed, http for *.localhost.")
			ui.Field(c, "Admin script", func() {
				ui.TextInput(c, &f.adminPath).Placeholder("admin.php")
			})
			ui.Field(c, "Login", func() { ui.TextInput(c, &f.login) })
			passwordHint := ""
			if f.edit {
				passwordHint = "Leave empty to keep the saved password."
			}
			ui.Field(c, "Password", func() {
				ui.TextInput(c, &f.password).Password()
			}).Description(passwordHint)
			ui.Field(c, "Password variable", func() {
				ui.TextInput(c, &f.passwordEnv).Placeholder(defaultPasswordEnv())
			}).Description("An environment variable holding the password, saved instead of it.")
			if a.current() != nil && !f.edit {
				ui.Field(c, "", func() { ui.Checkbox(c, &f.global, "Save as a global site, for every project") })
			}
			ui.Field(c, "", func() { ui.Checkbox(c, &f.insecure, "Skip TLS certificate verification for the check") })
		})
		errorText(c, a.siteErr)
		dialogButtons(c, func() {
			if a.busy() {
				ui.Spinner(c).Size(16, 16)
				ui.Text(c, "Checking the login…").TextColor(c.Theme().TextMuted)
				ui.Spacer(c)
			}
			if ui.Button(c.Key("site-cancel"), "Cancel").Disabled(a.busy()).Clicked() {
				a.siteOpen = false
			}
			if ui.PrimaryButton(c.Key("site-save"), "Check and save").Disabled(a.busy()).Clicked() {
				a.saveSite()
			}
		})
	})
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
