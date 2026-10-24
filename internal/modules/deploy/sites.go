package deploy

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/deploysites"
	"github.com/thecoooler/dleplugin/internal/dleadmin"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

func registryPaths(rt *app.Runtime) (global, project string, err error) {
	global = filepath.Join(filepath.Dir(rt.Config.Path()), deploysites.Filename)
	wd, err := rt.WorkDir()
	if err != nil {
		return "", "", err
	}
	root, err := plugin.FindRoot(wd)
	if err == nil {
		project = filepath.Join(root, deploysites.Filename)
	}
	return global, project, nil
}

func sitesCommand(rt *app.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use: "sites", Short: "Manage saved DLE sites",
		Long: "Manage sites in the project's dleplugin-deploy.yaml. Use --global for the file next to the global config. Passwords are never written to the file: a password checked by add or edit is saved in the OS credential store, or in credentials.yaml next to the global config when the store is unavailable or " + CredentialStoreEnv + "=file, unless it comes from --password-env, which saves only an environment variable name.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(listSitesCommand(rt), showSiteCommand(rt), writeSiteCommand(rt, false), writeSiteCommand(rt, true), removeSiteCommand(rt))
	return cmd
}

func listSitesCommand(rt *app.Runtime) *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List effective sites and their source", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			global, project, err := registryPaths(rt)
			if err != nil {
				return err
			}
			entries, err := deploysites.Effective(global, project)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				rt.UI.Info("No saved sites. Use dleplugin deploy sites add HOST [--global].")
				return nil
			}
			w := tabwriter.NewWriter(rt.UI.Out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "HOST\tLOGIN\tSOURCE\tCONFIG")
			for _, host := range deploysites.Hosts(entries) {
				e := entries[host]
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", host, e.Site.Login, e.Scope, e.Path)
			}
			return w.Flush()
		},
	}
}

func showSiteCommand(rt *app.Runtime) *cobra.Command {
	return &cobra.Command{
		Use: "show HOST", Short: "Show an effective site's connection settings", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			global, project, err := registryPaths(rt)
			if err != nil {
				return err
			}
			entries, err := deploysites.Effective(global, project)
			if err != nil {
				return err
			}
			host, err := deploysites.Host(args[0])
			if err != nil {
				return err
			}
			e, ok := entries[host]
			if !ok {
				return fmt.Errorf("site %q is not saved", host)
			}
			admin, err := e.Site.AdminURL(host)
			if err != nil {
				return err
			}
			env := e.Site.PasswordEnv
			if env == "" {
				env = PasswordEnv
			}
			c, err := credentials(rt)
			if err != nil {
				return err
			}
			saved := "no"
			if _, loc, err := c.Password(host, e.Site.Login); err != nil {
				saved = "unavailable"
				rt.UI.Debugf("%v", err)
			} else if loc == deploysites.InFile {
				saved = "yes (" + c.Path + ")"
			} else if loc != "" {
				saved = "yes (OS credential store)"
			}
			rt.UI.Infof("Host: %s\nAdmin URL: %s\nLogin: %s\nPassword environment: %s\nSaved password: %s\nSource: %s (%s)", host, admin, e.Site.Login, env, saved, e.Scope, e.Path)
			return nil
		},
	}
}

func writeSiteCommand(rt *app.Runtime, edit bool) *cobra.Command {
	verb := "add"
	if edit {
		verb = "edit"
	}
	var o options
	var global bool
	cmd := &cobra.Command{
		Use: verb + " [HOST]", Short: strings.ToUpper(verb[:1]) + verb[1:] + " a site after checking its admin login",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := mutationPath(rt, global)
			if err != nil {
				return err
			}
			d, err := deploysites.Load(path)
			if err != nil {
				return err
			}
			target := ""
			if len(args) > 0 {
				target = args[0]
			}
			if target == "" {
				if o.passwordStdin {
					return fmt.Errorf("--password-stdin requires HOST")
				}
				if target, err = ask(rt, "Site host or admin URL", "HOST argument"); err != nil {
					return err
				}
			}
			host, err := deploysites.Host(target)
			if err != nil {
				return err
			}
			site, exists := d.Sites[host]
			if edit && !exists {
				return fmt.Errorf("site %q is not saved in %s", host, path)
			}
			if !edit && exists {
				return fmt.Errorf("site %q already exists in %s; use sites edit", host, path)
			}
			// Copy options: Cobra commands may be executed more than once.
			settings := o
			if edit {
				if !cmd.Flags().Changed("login") {
					if rt.UI.Interactive() && !settings.passwordStdin {
						settings.login, err = rt.UI.Prompt("Login", &site.Login)
						if err != nil {
							return err
						}
					} else {
						settings.login = site.Login
					}
				}
				if !cmd.Flags().Changed("password-env") {
					settings.passwordEnv = site.PasswordEnv
				}
				if !strings.ContainsAny(target, "/?#") {
					admin, err := site.AdminURL(host)
					if err != nil {
						return err
					}
					if cmd.Flags().Changed("admin-path") {
						admin.Path, admin.RawPath = "", ""
					} else if rt.UI.Interactive() && !settings.passwordStdin {
						def := strings.TrimPrefix(admin.Path, "/")
						settings.adminPath, err = rt.UI.Prompt("Admin script path", &def)
						if err != nil {
							return err
						}
						admin.Path, admin.RawPath = "", ""
					}
					target = admin.String()
				}
			}
			admin, err := dleadmin.SiteURL(target, settings.adminPath)
			if err != nil {
				return err
			}
			if settings.login == "" {
				if settings.passwordStdin {
					return fmt.Errorf("--password-stdin requires --login (or a saved login)")
				}
				if settings.login, err = ask(rt, "Login", "--login"); err != nil {
					return err
				}
			}
			password, source, err := signIn(cmd, rt, dleadmin.New(admin, settings.insecure), settings, host)
			if err != nil {
				return err
			}
			origin := *admin
			origin.Path, origin.RawPath = "", ""
			d.Sites[host] = deploysites.Site{URL: origin.String(), AdminPath: strings.TrimPrefix(admin.Path, "/"), Login: settings.login, PasswordEnv: settings.passwordEnv}
			if err := deploysites.Save(path, d); err != nil {
				return err
			}
			if source != fromEnv && source != fromStore {
				rememberPassword(rt, host, settings.login, password)
			}
			if exists && site.Login != settings.login {
				forgetPassword(rt, host, site.Login)
			}
			rt.UI.Successf("Site %s saved in %s", host, path)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&global, "global", false, "Save in the global deployment config")
	f.StringVarP(&o.login, "login", "u", "", "Admin login")
	f.StringVar(&o.adminPath, "admin-path", "", "Admin script path (default admin.php)")
	f.StringVar(&o.passwordEnv, "password-env", "", "Save the name of the environment variable holding the password")
	f.BoolVar(&o.passwordStdin, "password-stdin", false, "Read the password from stdin for the login check")
	f.BoolVar(&o.insecure, "insecure", false, "Skip TLS certificate verification for the login check")
	return cmd
}

// forgetPassword deletes the saved password of host and login unless a site
// in either scope still uses it.
func forgetPassword(rt *app.Runtime, host, login string) {
	global, project, err := registryPaths(rt)
	if err != nil {
		return
	}
	for _, path := range []string{global, project} {
		if path == "" {
			continue
		}
		d, err := deploysites.Load(path)
		if err != nil || d.Sites[host].Login == login {
			return
		}
	}
	c, err := credentials(rt)
	if err == nil {
		err = c.DeletePassword(host, login)
	}
	if err != nil {
		rt.UI.Warningf("%v", err)
	}
}

func mutationPath(rt *app.Runtime, useGlobal bool) (string, error) {
	global, project, err := registryPaths(rt)
	if err != nil {
		return "", err
	}
	if useGlobal {
		return global, nil
	}
	if project == "" {
		return "", fmt.Errorf("project configuration requires a dleplugin.yml project; use --global outside a project")
	}
	return project, nil
}

func removeSiteCommand(rt *app.Runtime) *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use: "remove HOST", Short: "Remove a saved site from the selected config", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := mutationPath(rt, global)
			if err != nil {
				return err
			}
			d, err := deploysites.Load(path)
			if err != nil {
				return err
			}
			host, err := deploysites.Host(args[0])
			if err != nil {
				return err
			}
			if _, ok := d.Sites[host]; !ok {
				return fmt.Errorf("site %q is not saved in %s", host, path)
			}
			login := d.Sites[host].Login
			delete(d.Sites, host)
			if err := deploysites.Save(path, d); err != nil {
				return err
			}
			forgetPassword(rt, host, login)
			rt.UI.Successf("Site %s removed from %s", host, path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Remove from the global deployment config")
	return cmd
}

func chooseSite(rt *app.Runtime, entries map[string]deploysites.Entry) (string, error) {
	if len(entries) == 0 || !rt.UI.Interactive() {
		return ask(rt, "Site host or admin URL", "HOST argument")
	}
	hosts := deploysites.Hosts(entries)
	for i, host := range hosts {
		rt.UI.Infof("%d. %s (%s)", i+1, host, entries[host].Scope)
	}
	rt.UI.Info("0. Enter a host or admin URL manually")
	for {
		answer, err := rt.UI.Prompt("Site number or host", nil)
		if err != nil {
			return "", err
		}
		answer = strings.TrimSpace(answer)
		if i, err := strconv.Atoi(answer); err == nil {
			if i == 0 {
				return ask(rt, "Site host or admin URL", "HOST argument")
			}
			if i > 0 && i <= len(hosts) {
				return hosts[i-1], nil
			}
			rt.UI.Info("Invalid site number")
			continue
		}
		return answer, nil
	}
}
