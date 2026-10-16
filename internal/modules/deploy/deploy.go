// Package deploy implements the deploy module: uploading a plugin to a DLE
// site through its admin panel.
package deploy

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/build"
	"github.com/thecoooler/dleplugin/internal/dleadmin"
	"github.com/thecoooler/dleplugin/internal/dlexml"
	"github.com/thecoooler/dleplugin/internal/modules/deploy/internal/deploysites"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

// PasswordEnv is the environment variable holding the admin password.
const PasswordEnv = "DLEPLUGIN_PASSWORD"

type module struct{}

// Module returns the deploy module.
func Module() app.Module { return module{} }

func (module) Name() string { return "deploy" }

func (module) Commands(rt *app.Runtime) []*cobra.Command {
	return []*cobra.Command{deployCommand(rt)}
}

type options struct {
	file          string
	version       string
	login         string
	adminPath     string
	passwordStdin bool
	passwordEnv   string
	allowNew      bool
	dryRun        bool
	insecure      bool
	// savedHost is the registry key when upload uses a saved site.
	savedHost string
}

func deployCommand(rt *app.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use: "deploy", Short: "Manage deployment sites and upload plugins", Args: cobra.NoArgs,
		GroupID: app.GroupRelease,
		RunE:    func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(uploadCommand(rt), sitesCommand(rt))
	return cmd
}

func uploadCommand(rt *app.Runtime) *cobra.Command {
	var o options
	cmd := &cobra.Command{
		Use:   "upload [HOST]",
		Short: "Upload the plugin to a DLE site",
		Long: `Upload the plugin to a DLE site through its admin panel.

Without --file the plugin in the current directory is built first and its
dist/ artifact is uploaded; --version overrides the version from dleplugin.yml. HOST is a domain (https is assumed, http for
*.localhost) or the full admin URL, e.g. https://example.com/panel.php.
Saved sites are read from global and project dleplugin-deploy.yaml files.
Project records replace global records for the same host. Without HOST,
saved sites are offered interactively, alongside manual URL entry.
Missing login and password are asked interactively.

The password is read from --password-stdin, the site's password_env or
--password-env variable (default ` + PasswordEnv + `), the OS credential
store for saved sites, or a secret prompt. A password typed for a saved site
is remembered in the credential store.
An explicit admin URL bypasses saved connection settings.`,
		Example: `  dleplugin deploy upload
  dleplugin deploy upload example.com
  dleplugin deploy upload https://example.com/panel.php -u admin --new
  dleplugin deploy upload example.com -f dist/my_plugin.xml
  echo "$PASS" | dleplugin deploy upload example.com --password-stdin`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host := ""
			if len(args) == 1 {
				host = args[0]
			}
			return run(cmd, rt, o, host)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.file, "file", "f", "", "Upload a ready plugin file (.xml or .zip) instead of building")
	f.StringVar(&o.version, "version", "", "Plugin version written to the built XML (default: version from dleplugin.yml)")
	f.StringVarP(&o.login, "login", "u", "", "Admin login")
	f.StringVar(&o.adminPath, "admin-path", "", "Admin script path (default \"admin.php\")")
	f.BoolVar(&o.passwordStdin, "password-stdin", false, "Read the admin password from stdin")
	f.StringVar(&o.passwordEnv, "password-env", "", "Environment variable holding the admin password")
	f.BoolVar(&o.allowNew, "new", false, "Install the plugin when it is not installed on the site")
	f.BoolVar(&o.dryRun, "dry-run", false, "Log in and look up the plugin without uploading")
	f.BoolVar(&o.insecure, "insecure", false, "Skip TLS certificate verification")
	cmd.MarkFlagsMutuallyExclusive("file", "version")
	return cmd
}

func run(cmd *cobra.Command, rt *app.Runtime, o options, host string) error {
	artifact, err := artifact(rt, o.file, o.version)
	if err != nil {
		return err
	}
	name, err := pluginName(artifact)
	if err != nil {
		return err
	}

	host, o, err = resolveUpload(cmd, rt, o, host)
	if err != nil {
		return err
	}
	admin, err := dleadmin.SiteURL(host, o.adminPath)
	if err != nil {
		return err
	}
	if o.login == "" {
		if o.login, err = ask(rt, "Login", "--login"); err != nil {
			return err
		}
	}

	site := admin.Host
	client := dleadmin.New(admin, o.insecure)
	rt.UI.Debugf("admin URL: %s", client.AdminURL())
	password, source, err := signIn(cmd, rt, client, o, o.savedHost)
	if err != nil {
		return err
	}
	if o.savedHost != "" && source == fromPrompt {
		rememberPassword(rt, o.savedHost, o.login, password)
	}
	rt.UI.Successf("Logged in to %s", site)

	plugins, err := client.Plugins(cmd.Context())
	if err != nil {
		return err
	}
	installed, found, err := dleadmin.FindPlugin(plugins, name)
	if err != nil {
		return err
	}
	if found {
		actualName, err := client.PluginName(cmd.Context(), installed.ID)
		if err != nil {
			return err
		}
		if actualName != name {
			return fmt.Errorf("plugin id %d on %s has name %q in its edit form, expected %q; refusing to update", installed.ID, site, actualName, name)
		}
		rt.UI.Debugf("plugin %q has id %d", name, installed.ID)
	} else if !o.allowNew {
		if o.dryRun || !rt.UI.Interactive() {
			return fmt.Errorf("plugin %q is not installed on %s; use --new to install it", name, site)
		}
		ok, err := rt.UI.Confirm(fmt.Sprintf("Plugin %q is not installed on %s. Install it as new?", name, site), false)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("plugin %q is not installed on %s", name, site)
		}
	}

	if o.dryRun {
		if found {
			rt.UI.Infof("Dry run: plugin %q would be updated on %s (id %d)", name, site, installed.ID)
		} else {
			rt.UI.Infof("Dry run: plugin %q would be installed on %s", name, site)
		}
		return nil
	}

	if err := client.Upload(cmd.Context(), installed.ID, artifact); err != nil {
		return err
	}
	if found {
		rt.UI.Successf("Plugin %q updated on %s", name, site)
	} else {
		rt.UI.Successf("Plugin %q installed on %s", name, site)
	}
	return nil
}

// artifact returns the file to upload: file resolved against the working
// directory, or the freshly built dist/ artifact of the current plugin with
// version (the manifest version when empty).
func artifact(rt *app.Runtime, file, version string) (string, error) {
	wd, err := rt.WorkDir()
	if err != nil {
		return "", err
	}
	if file != "" {
		if !filepath.IsAbs(file) {
			file = filepath.Join(wd, file)
		}
		st, err := os.Stat(file)
		if err != nil {
			return "", fmt.Errorf("plugin file: %w", err)
		}
		if st.IsDir() {
			return "", fmt.Errorf("plugin file %s is a directory", file)
		}
		return file, nil
	}

	p, err := plugin.FromDir(wd)
	if err != nil {
		return "", err
	}
	files, err := p.Files()
	if err != nil {
		return "", err
	}
	if version == "" {
		version = p.Info().Version
	}
	res, err := build.WriteDist(p.Root(), p.Info(), files, version)
	if err != nil {
		return "", err
	}
	rt.UI.Success("Plugin built successfully")
	if res.ArchivePath != "" {
		return res.ArchivePath, nil
	}
	return res.XMLPath, nil
}

// pluginName reads <name> from a plugin .xml or split .zip.
func pluginName(path string) (string, error) {
	root, err := dlexml.ParsePluginFile(path)
	if err != nil {
		return "", fmt.Errorf("read plugin file %s: %w", path, err)
	}
	el := root.Find("name")
	if el == nil {
		return "", fmt.Errorf("plugin file %s has no <name>", path)
	}
	name := strings.TrimSpace(el.Text)
	if len(name) < 3 {
		return "", fmt.Errorf("plugin name %q in %s is too short", name, path)
	}
	return name, nil
}

// ask prompts for a required value, or fails naming flag when input is not
// interactive.
func ask(rt *app.Runtime, label, flag string) (string, error) {
	if !rt.UI.Interactive() {
		return "", fmt.Errorf("missing %s", flag)
	}
	v, err := rt.UI.Prompt(label, nil)
	return strings.TrimSpace(v), err
}

// passwordSource tells where a password came from.
type passwordSource int

const (
	fromStdin passwordSource = iota
	fromEnv
	fromStore
	fromPrompt
)

// readPassword reads the admin password from stdin, the environment, the OS
// credential store (when host is set) or a secret prompt, in that order.
func readPassword(rt *app.Runtime, fromStdinFlag bool, env, host, login string) (string, passwordSource, error) {
	if fromStdinFlag {
		b, err := io.ReadAll(rt.UI.In)
		if err != nil {
			return "", fromStdin, fmt.Errorf("read password from stdin: %w", err)
		}
		p := strings.TrimRight(string(b), "\r\n")
		if p == "" {
			return "", fromStdin, errors.New("password from stdin is empty")
		}
		return p, fromStdin, nil
	}
	if env == "" {
		env = PasswordEnv
	}
	if p := os.Getenv(env); p != "" {
		return p, fromEnv, nil
	}
	if host != "" {
		p, ok, err := deploysites.Password(host, login)
		if err != nil {
			rt.UI.Debugf("%v", err)
		} else if ok {
			rt.UI.Debugf("using the password saved for %s@%s", login, host)
			return p, fromStore, nil
		}
	}
	if !rt.UI.Interactive() {
		return "", fromPrompt, fmt.Errorf("missing password: use --password-stdin or %s", env)
	}
	p, err := rt.UI.PromptSecret("Password")
	return p, fromPrompt, err
}

// signIn logs in with the password from readPassword. A saved password the
// site rejects is asked for again on a terminal. It returns the password that
// worked and its source.
func signIn(cmd *cobra.Command, rt *app.Runtime, client *dleadmin.Client, o options, host string) (string, passwordSource, error) {
	password, source, err := readPassword(rt, o.passwordStdin, o.passwordEnv, host, o.login)
	if err != nil {
		return "", source, err
	}
	err = client.Login(cmd.Context(), o.login, password)
	if errors.Is(err, dleadmin.ErrLogin) && source == fromStore && rt.UI.Interactive() {
		rt.UI.Warning("The saved password was rejected")
		if password, err = rt.UI.PromptSecret("Password"); err != nil {
			return "", fromPrompt, err
		}
		source = fromPrompt
		err = client.Login(cmd.Context(), o.login, password)
	}
	return password, source, err
}

// rememberPassword saves a password in the OS credential store, warning on
// failure: the store may be unavailable (for example, on a headless Linux).
func rememberPassword(rt *app.Runtime, host, login, password string) {
	if err := deploysites.SavePassword(host, login, password); err != nil {
		rt.UI.Warningf("%v; use --password-env instead", err)
		return
	}
	rt.UI.Debugf("password for %s@%s saved", login, host)
}

func resolveUpload(cmd *cobra.Command, rt *app.Runtime, o options, target string) (string, options, error) {
	global, project, err := registryPaths(rt)
	if err != nil {
		return "", o, err
	}
	entries, err := deploysites.Effective(global, project)
	if err != nil {
		return "", o, err
	}
	if target == "" {
		if o.passwordStdin {
			return "", o, errors.New("--password-stdin requires HOST and --login (or a saved login)")
		}
		if target, err = chooseSite(rt, entries); err != nil {
			return "", o, err
		}
	}
	key, err := deploysites.Host(target)
	if err != nil {
		return "", o, err
	}
	// Explicit URLs are one-off connections; bare hosts use saved settings.
	if entry, ok := entries[key]; ok && !strings.ContainsAny(target, "/?#") {
		admin, err := entry.Site.AdminURL(key)
		if err != nil {
			return "", o, err
		}
		if cmd.Flags().Changed("admin-path") {
			admin.Path, admin.RawPath = "", ""
		}
		target = admin.String()
		o.savedHost = key
		if !cmd.Flags().Changed("login") {
			o.login = entry.Site.Login
		}
		if !cmd.Flags().Changed("password-env") {
			o.passwordEnv = entry.Site.PasswordEnv
		}
	}
	if o.passwordStdin && o.login == "" {
		return "", o, errors.New("--password-stdin requires HOST and --login (or a saved login)")
	}
	return target, o, nil
}
