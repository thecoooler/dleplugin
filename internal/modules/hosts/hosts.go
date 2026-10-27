// Package hosts implements the hosts module: DLE sites in a local Docker
// stack (Apache, PHP-FPM, MariaDB) built from files embedded in the binary.
package hosts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/dist"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/host"
	config "github.com/thecoooler/dleplugin/internal/modules/hosts/internal/hostconfig"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
	"github.com/thecoooler/dleplugin/internal/process"
)

type module struct{}

// Module returns the hosts module.
func Module() app.Module { return module{} }

func (module) Name() string { return "hosts" }

func (module) Commands(rt *app.Runtime) []*cobra.Command {
	return []*cobra.Command{hostsCommand(rt)}
}

func hostsCommand(rt *app.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "hosts",
		GroupID: app.GroupHosts,
		Short:   "Manage DLE sites in a local Docker stack",
		Long: `Manage DLE sites in a local Docker stack (Apache, PHP 8.5/7.4, MariaDB).

The stack files are embedded in the binary; only Docker with Compose and
DLE distributions are needed. The hosts settings are kept separately from the
dleplugin config, in ~/.config/dlehost/config.yml (DLEHOST_CONFIG), and the
stack lives in ~/.local/share/dlehost (DLEHOST_DIR or project_dir).`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(upCmd(rt), downCmd(rt), composeCmd(rt), listCmd(rt), addCmd(rt), removeCmd(rt),
		getCmd(rt), setCmd(rt), pathCmd(rt), portCmd(rt), versionsCmd(rt), configCmd(rt))
	return cmd
}

// env loads the config and prepares the stack directory.
func env(rt *app.Runtime) (*config.Config, *stack.Stack, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	s, err := openStack(rt, cfg)
	if err != nil {
		return nil, nil, err
	}
	if s.Updated {
		rt.UI.Warningf("Stack files in %s were created or updated; apply them with: dleplugin hosts up", s.Dir)
	}
	return cfg, s, nil
}

// stackOnly is env without the hint about dleplugin hosts up.
func stackOnly(rt *app.Runtime) (*stack.Stack, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return openStack(rt, cfg)
}

func openStack(rt *app.Runtime, cfg *config.Config) (*stack.Stack, error) {
	if err := cfg.ValidateRouting(); err != nil {
		return nil, err
	}
	s, err := stack.Open(cfg.ProjectDir)
	if err != nil {
		return nil, err
	}
	s.BaseDomain, s.ExternalOrigin = cfg.BaseDomain, cfg.ExternalOrigin
	s.Exec = rt.Exec
	return s, nil
}

// stepper returns the progress logger handed to the services.
func stepper(rt *app.Runtime) func(string) {
	return func(msg string) { rt.UI.Info("→ " + msg) }
}

func upCmd(rt *app.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Build and start the Docker stack (running it again applies updates)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			s, err := openStack(rt, cfg)
			if err != nil {
				return err
			}
			hash, pending, err := s.Pending()
			if err != nil {
				return err
			}
			upArgs := []string{"up", "-d", "--build", "--remove-orphans"}
			if pending {
				// configs are mounted into the containers; compose does not see their changes
				upArgs = append(upArgs, "--force-recreate")
			}
			if err := s.ComposeAttached(cmd.Context(), nil, upArgs...); err != nil {
				return err
			}
			if err := s.MarkApplied(hash); err != nil {
				return err
			}
			rt.UI.Success("Stack is running: " + s.LocalHostURL("localhost"))
			return nil
		},
	}
}

func downCmd(rt *app.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the Docker stack (sites and databases are kept)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := stackOnly(rt)
			if err != nil {
				return err
			}
			return s.ComposeAttached(cmd.Context(), nil, "down")
		},
	}
}

func composeCmd(rt *app.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:                "compose [DOCKER_COMPOSE_ARGS...]",
		Short:              "Run docker compose in the stack directory",
		Example:            "  dleplugin hosts compose ps\n  dleplugin hosts compose logs -f apache",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := stackOnly(rt)
			if err != nil {
				return err
			}
			err = s.ComposeAttached(cmd.Context(), rt.UI.In, takeInheritedFlags(cmd, args)...)
			var exit *process.ExitError
			if errors.As(err, &exit) {
				return &app.SilentExit{Code: exit.Result.ExitCode}
			}
			return err
		},
	}
}

// takeInheritedFlags applies the leading dleplugin flags (such as -v) that
// cobra leaves in args when flag parsing is disabled and returns the rest.
// Only flags without a value are taken; anything else belongs to compose.
func takeInheritedFlags(cmd *cobra.Command, args []string) []string {
	flags := cmd.InheritedFlags()
	for len(args) > 0 {
		sets, ok := inheritedFlagArg(flags, args[0])
		if !ok {
			break
		}
		for _, set := range sets {
			if err := cmd.Flags().Set(set[0], set[1]); err != nil {
				return args
			}
		}
		args = args[1:]
	}
	if pre := cmd.Root().PersistentPreRun; pre != nil {
		pre(cmd, args)
	}
	return args
}

// inheritedFlagArg parses arg as boolean-style flags of flags with pflag
// syntax (--name, --name=value, -x, -xy, -x=value) and returns name/value
// pairs; ok is false unless the whole argument consists of such flags.
func inheritedFlagArg(flags *pflag.FlagSet, arg string) (sets [][2]string, ok bool) {
	if long, isLong := strings.CutPrefix(arg, "--"); isLong {
		name, value, hasValue := strings.Cut(long, "=")
		f := flags.Lookup(name)
		if name == "" || f == nil || f.NoOptDefVal == "" {
			return nil, false
		}
		if !hasValue {
			value = f.NoOptDefVal
		}
		return [][2]string{{f.Name, value}}, true
	}
	shorts, isShort := strings.CutPrefix(arg, "-")
	if !isShort || shorts == "" {
		return nil, false
	}
	for i := 0; i < len(shorts); i++ {
		f := flags.ShorthandLookup(shorts[i : i+1])
		if f == nil || f.NoOptDefVal == "" {
			return nil, false
		}
		value := f.NoOptDefVal
		if rest := shorts[i+1:]; strings.HasPrefix(rest, "=") {
			value, i = rest[1:], len(shorts)
		}
		sets = append(sets, [2]string{f.Name, value})
	}
	return sets, true
}

func listCmd(rt *app.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List hosts",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := env(rt)
			if err != nil {
				return err
			}
			rows, err := host.List(s)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				rt.UI.Info("No hosts")
				return nil
			}
			w := tabwriter.NewWriter(rt.UI.Out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "HOST\tURL\tDLE\tPHP\tCREATED")
			for _, r := range rows {
				dle, php, url := "-", "-", r.URL(s)
				if r.Managed {
					dle, php = r.DLE, r.PHP
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Name, url, dle, php, r.CreatedAt.Local().Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}
}

func addCmd(rt *app.Runtime) *cobra.Command {
	var dle, distArchive, php, adminLogin, adminPassword, siteOffline string
	cmd := &cobra.Command{
		Use:   "add NAME",
		Short: "Create the host NAME.<base_domain> with DLE",
		Example: "  dleplugin hosts add shop --dle 19.0\n" +
			"  dleplugin hosts add demo --dist ~/dists/11.0.zip",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			credentials := host.Credentials{
				Login:    credentialValue(cmd, "admin-login", adminLogin, "DLEHOST_ADMIN_LOGIN"),
				Password: credentialValue(cmd, "admin-password", adminPassword, "DLEHOST_ADMIN_PASSWORD"),
			}
			if err := credentials.Validate(); err != nil {
				return err
			}
			if cmd.Flags().Changed("site-offline") {
				if err := host.ValidateSetting("site_offline", siteOffline); err != nil {
					return err
				}
			}
			cfg, s, err := env(rt)
			if err != nil {
				return err
			}
			if distArchive != "" {
				abs, err := filepath.Abs(distArchive)
				if err != nil {
					return err
				}
				distArchive = abs
			}
			if dle == "" && distArchive == "" {
				dle = cfg.DefaultDLE
			}
			if dle == "" && distArchive == "" {
				return fmt.Errorf("specify --dle, --dist or set default_dle (list: dleplugin hosts versions)")
			}
			if !cmd.Flags().Changed("site-offline") {
				siteOffline = config.Keys["default_site_offline"].Get(cfg)
			}
			res, err := host.Add(s, host.AddOptions{
				Context:     cmd.Context(),
				Name:        args[0],
				DLE:         dle,
				DistArchive: distArchive,
				PHP:         php,
				DefaultPHP:  cfg.DefaultPHP,
				PHPVersions: cfg.PHPVersions,
				DistsDir:    cfg.DistsDir,
				Admin:       credentials,
				AdminPath:   cfg.AdminPath(),
				SiteOffline: siteOffline,
				Warn:        rt.UI.Warning,
			}, stepper(rt))
			if err != nil {
				return err
			}
			adminPath, err := host.GetSetting(s, res.Host.Name, "admin_path")
			if err != nil {
				return err
			}
			rt.UI.Info("")
			rt.UI.Successf("Host %s created (DLE %s, PHP %s)", res.Host.Name, res.Host.DLE, res.Host.PHP)
			rt.UI.Info("URL:      " + res.URL)
			rt.UI.Info("Admin:    " + res.URL + adminPath)
			rt.UI.Info("Login:    " + credentials.Login)
			rt.UI.Info("Offline:  " + siteOffline)
			rt.UI.Info("Database: " + res.Host.Database)
			rt.UI.Info("Files:    " + s.HostDir(res.Host.Name))
			return nil
		},
	}
	cmd.Flags().StringVar(&dle, "dle", "", "DLE version (default: default_dle)")
	cmd.Flags().StringVar(&distArchive, "dist", "", "path to a DLE distribution ZIP (instead of <version>.zip in dists_dir)")
	cmd.Flags().StringVar(&php, "php", "", "PHP version (default: chosen by DLE version; auto or an explicit version)")
	cmd.Flags().StringVar(&adminLogin, "admin-login", "admin", "administrator login (DLEHOST_ADMIN_LOGIN)")
	cmd.Flags().StringVar(&adminPassword, "admin-password", "admin", "administrator password (DLEHOST_ADMIN_PASSWORD)")
	cmd.Flags().StringVar(&siteOffline, "site-offline", "", "DLE maintenance mode: on (site closed) or off (site open); default: default_site_offline (on)")
	return cmd
}

func getCmd(rt *app.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "get NAME KEY",
		Short:   "Read a host setting from DLE",
		Long:    "Read a host setting from DLE. Available keys: site_offline (on: site closed, off: site open), admin_path (PHP filename, e.g. panel.php).",
		Example: "  dleplugin hosts get shop site_offline\n  dleplugin hosts get shop admin_path",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate the key before preparing the stack.
			if err := host.ValidateSettingKey(args[1]); err != nil {
				return err
			}
			_, s, err := env(rt)
			if err != nil {
				return err
			}
			value, err := host.GetSetting(s, args[0], args[1])
			if err != nil {
				return err
			}
			rt.UI.Info(value)
			return nil
		},
	}
}

func setCmd(rt *app.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "set NAME KEY VALUE",
		Short:   "Change a host setting in DLE",
		Long:    "Change a host setting in DLE. Available keys: site_offline (on: site closed, off: site open), admin_path (PHP filename, e.g. panel.php).\nAdministrators retain access while the site is closed.\nSetting admin_path renames the current admin PHP file and updates DLE configuration. Use admin.php to restore the default.",
		Example: "  dleplugin hosts set shop site_offline on\n  dleplugin hosts set shop site_offline off\n  dleplugin hosts set shop admin_path panel.php\n  dleplugin hosts set shop admin_path admin.php",
		Args:    cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := host.ValidateSetting(args[1], args[2]); err != nil {
				return err
			}
			_, s, err := env(rt)
			if err != nil {
				return err
			}
			if err := host.SetSetting(s, args[0], args[1], args[2]); err != nil {
				return err
			}
			rt.UI.Successf("Host %s: %s = %s", args[0], args[1], args[2])
			return nil
		},
	}
}

func removeCmd(rt *app.Runtime) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove NAME",
		Aliases: []string{"rm"},
		Short:   "Remove a host: files, database, database user and vhost",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := env(rt)
			if err != nil {
				return err
			}
			name := args[0]
			if !yes {
				ok, err := rt.UI.Confirm(fmt.Sprintf("Remove host %s with its database and files?", name), false)
				if err != nil {
					return err
				}
				if !ok {
					rt.UI.Info("Cancelled")
					return nil
				}
			}
			if err := host.Remove(s, name, stepper(rt)); err != nil {
				return err
			}
			rt.UI.Successf("Host %s removed", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func pathCmd(rt *app.Runtime) *cobra.Command {
	var container bool
	cmd := &cobra.Command{
		Use:   "path [NAME]",
		Short: "Print the host files directory (without NAME, the stack directory)",
		Example: "  dleplugin hosts path                   # stack directory\n" +
			"  dleplugin hosts path shop              # host directory on this machine\n" +
			"  dleplugin hosts path shop --container  # the same directory inside the containers\n" +
			"  cd \"$(dleplugin hosts path shop)\"",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := stackOnly(rt)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				if container {
					return fmt.Errorf("--container only applies to a host")
				}
				rt.UI.Info(s.Dir)
				return nil
			}
			path, err := host.Path(s, args[0], container)
			if err != nil {
				return err
			}
			rt.UI.Info(path)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&container, "container", "c", false, "path inside the apache/php containers ("+stack.ContainerWWWDir+"/NAME)")
	return cmd
}

func portCmd(rt *app.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "port [NUMBER]",
		Short: "Show or change the Apache HTTP port (shared by all hosts)",
		Example: "  dleplugin hosts port       # current port\n" +
			"  dleplugin hosts port 80    # http://NAME.localhost/",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := env(rt)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				rt.UI.Info(s.HTTPPort())
				return nil
			}
			port, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid port %q", args[0])
			}
			if err := host.SetHTTPPort(s, port, stepper(rt)); err != nil {
				return err
			}
			rt.UI.Successf("Port %d, example address: %s", port, s.HostURL(s.Domain("NAME")))
			return nil
		},
	}
}

func versionsCmd(rt *app.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "versions",
		Short: "Available DLE (from dists_dir) and PHP versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			dists, err := dist.List(cfg.DistsDir)
			if err != nil {
				return err
			}
			rt.UI.Info("DLE: " + strings.Join(markDefault(dists, cfg.DefaultDLE), " "))
			rt.UI.Info("PHP: " + strings.Join(markDefault(cfg.PHPVersions, cfg.DefaultPHP), " "))
			return nil
		},
	}
}

func markDefault(list []string, def string) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = v
		if v == def {
			out[i] = v + "*"
		}
	}
	return out
}

func configCmd(rt *app.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the hosts config",
		Long:  "Manage the hosts config.\n\nKeys:\n" + configKeysHelp(),
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Show the config",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := config.Load()
				if err != nil {
					return err
				}
				path, _ := config.Path()
				rt.UI.Info("# " + path)
				for _, k := range config.KeyNames() {
					rt.UI.Infof("%s: %s", k, config.Keys[k].Get(cfg))
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "get KEY",
			Short: "Print a key value",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				key, err := config.Lookup(args[0])
				if err != nil {
					return err
				}
				cfg, err := config.Load()
				if err != nil {
					return err
				}
				rt.UI.Info(key.Get(cfg))
				return nil
			},
		},
		&cobra.Command{
			Use:   "set KEY VALUE",
			Short: "Set a key value",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				return updateConfig(rt, args[0], args[1])
			},
		},
		&cobra.Command{
			Use:   "unset KEY",
			Short: "Reset a key",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return updateConfig(rt, args[0], "")
			},
		},
		&cobra.Command{
			Use:   "path",
			Short: "Print the config file path",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				path, err := config.Path()
				if err != nil {
					return err
				}
				rt.UI.Info(path)
				return nil
			},
		},
	)
	return cmd
}

func configKeysHelp() string {
	var b strings.Builder
	for _, k := range config.KeyNames() {
		fmt.Fprintf(&b, "  %-20s %s\n", k, config.Keys[k].Help)
	}
	return b.String()
}

func updateConfig(rt *app.Runtime, name, value string) error {
	key, err := config.Lookup(name)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := key.Set(cfg, value); err != nil {
		return err
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	rt.UI.Infof("%s: %s", name, key.Get(cfg))
	return nil
}

// credentialValue: explicit flag, then the environment variable, then the flag default.
func credentialValue(cmd *cobra.Command, flag, value, variable string) string {
	if cmd.Flags().Changed(flag) {
		return value
	}
	if v, ok := os.LookupEnv(variable); ok {
		return v
	}
	return value
}
