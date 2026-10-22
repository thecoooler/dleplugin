package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	config "github.com/thecoooler/dleplugin/internal/modules/hosts/internal/hostconfig"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/dist"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/host"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "dlehost",
		Short:         "Управление хостами DLE в docker-стеке",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(upCmd(), downCmd(), composeCmd(), listCmd(), addCmd(), removeCmd(), protectCmd(), pathCmd(), portCmd(), versionsCmd(), configCmd())
	return root
}

// env загружает конфиг и готовит каталог стека.
func env() (*config.Config, *stack.Stack, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	s, err := openStack(cfg)
	if err != nil {
		return nil, nil, err
	}
	if s.Updated {
		fmt.Fprintf(os.Stderr, "Файлы стека в %s созданы или обновлены — примените: dlehost up\n", s.Dir)
	}
	return cfg, s, nil
}

// stackOnly — как env, но без подсказки про dlehost up.
func stackOnly() (*stack.Stack, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return openStack(cfg)
}

func openStack(cfg *config.Config) (*stack.Stack, error) {
	if err := cfg.ValidateRouting(); err != nil {
		return nil, err
	}
	s, err := stack.Open(cfg.ProjectDir)
	if err != nil {
		return nil, err
	}
	s.BaseDomain, s.ExternalOrigin = cfg.BaseDomain, cfg.ExternalOrigin
	return s, nil
}

func step(msg string) { fmt.Println("→", msg) }

func upCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Собрать и запустить docker-стек (повторный запуск применяет обновления)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			s, err := openStack(cfg)
			if err != nil {
				return err
			}
			if err := host.CheckProtectPasswords(s, protectSettings(cfg)); err != nil {
				return err
			}
			hash, pending, err := s.Pending()
			if err != nil {
				return err
			}
			upArgs := []string{"up", "-d", "--build", "--remove-orphans"}
			if pending {
				// конфиги смонтированы в контейнеры, compose их изменений не видит
				upArgs = append(upArgs, "--force-recreate")
			}
			if err := s.ComposeAttached(upArgs...); err != nil {
				return err
			}
			if err := host.ApplyProtect(s, protectSettings(cfg), protectSettings(cfg), func() error { return nil }, step); err != nil {
				return err
			}
			if err := s.MarkApplied(hash); err != nil {
				return err
			}
			fmt.Println("\nСтек запущен:", s.LocalHostURL("localhost"))
			return nil
		},
	}
}

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Остановить docker-стек (сайты и базы сохраняются)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := stackOnly()
			if err != nil {
				return err
			}
			return s.ComposeAttached("down")
		},
	}
}

func composeCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "compose [аргументы docker compose]",
		Short:              "Выполнить docker compose в каталоге стека",
		Example:            "  dlehost compose ps\n  dlehost compose logs -f apache",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := stackOnly()
			if err != nil {
				return err
			}
			err = s.ComposeAttached(args...)
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				os.Exit(exit.ExitCode())
			}
			return err
		},
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Список хостов",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, s, err := env()
			if err != nil {
				return err
			}
			rows, err := host.List(s)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Println("Хостов нет")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ХОСТ\tURL\tDLE\tPHP\tВХОД\tСОЗДАН")
			for _, r := range rows {
				dle, php, url, gate := "-", "-", r.URL(s), "-"
				if r.Managed {
					dle, php, gate = r.DLE, r.PHP, protectState(r.Host, cfg.Protect)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, url, dle, php, gate, r.CreatedAt.Local().Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}
}

func addCmd() *cobra.Command {
	var dle, distArchive, php, adminLogin, adminPassword, protect string
	cmd := &cobra.Command{
		Use:   "add <имя>",
		Short: "Создать хост <имя>.<base_domain> с DLE",
		Example: "  dlehost add shop --dle 19.0\n" +
			"  dlehost add demo --dle 11.0\n" +
			"  dlehost add stage --protect on   # сайт только после ввода пароля protect",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			credentials := host.Credentials{
				Login:    credentialValue(cmd, "admin-login", adminLogin, "DLEHOST_ADMIN_LOGIN"),
				Password: credentialValue(cmd, "admin-password", adminPassword, "DLEHOST_ADMIN_PASSWORD"),
			}
			if err := credentials.Validate(); err != nil {
				return err
			}
			if _, err := host.ParseProtect(protect); err != nil {
				return err
			}
			cfg, s, err := env()
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
				return fmt.Errorf("укажите --dle, --dist или задайте default_dle (список: dlehost versions)")
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			res, err := host.Add(s, host.AddOptions{
				Context:     ctx,
				Name:        args[0],
				DLE:         dle,
				DistArchive: distArchive,
				PHP:         php,
				DefaultPHP:  cfg.DefaultPHP,
				PHPVersions: cfg.PHPVersions,
				DistsDir:    cfg.DistsDir,
				Admin:       credentials,
				Protect:     protect,
				Settings:    protectSettings(cfg),
			}, step)
			if err != nil {
				return err
			}
			fmt.Printf("\nХост %s создан (DLE %s, PHP %s)\n", res.Host.Name, res.Host.DLE, res.Host.PHP)
			fmt.Println("URL:   ", res.URL)
			fmt.Println("Админка:", res.URL+"admin.php")
			fmt.Println("Логин: ", credentials.Login)
			fmt.Println("Вход:  ", protectState(res.Host, cfg.Protect))
			fmt.Println("База:  ", res.Host.Database)
			fmt.Println("Файлы: ", s.HostDir(res.Host.Name))
			return nil
		},
	}
	cmd.Flags().StringVar(&dle, "dle", "", "версия DLE (по умолчанию default_dle)")
	cmd.Flags().StringVar(&distArchive, "dist", "", "путь к ZIP-архиву дистрибутива DLE (альтернатива поиску <версия>.zip в dists_dir)")
	cmd.Flags().StringVar(&php, "php", "", "версия PHP (по умолчанию автоматически по DLE; auto или явная версия)")
	cmd.Flags().StringVar(&adminLogin, "admin-login", "admin", "логин администратора (DLEHOST_ADMIN_LOGIN, по умолчанию admin)")
	cmd.Flags().StringVar(&adminPassword, "admin-password", "admin", "пароль администратора (DLEHOST_ADMIN_PASSWORD, по умолчанию admin)")
	cmd.Flags().StringVar(&protect, "protect", host.ProtectDefault, "форма входа перед сайтом: default (глобальная настройка), on, off")
	return cmd
}

func removeCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove <имя>",
		Aliases: []string{"rm"},
		Short:   "Удалить хост: файлы, базу, пользователя БД и vhost",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := env()
			if err != nil {
				return err
			}
			name := args[0]
			if !yes && !confirm(fmt.Sprintf("Удалить хост %s вместе с базой и файлами?", name)) {
				fmt.Println("Отменено")
				return nil
			}
			if err := host.Remove(s, name, step); err != nil {
				return err
			}
			fmt.Printf("Хост %s удалён\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "не спрашивать подтверждение")
	return cmd
}

func protectCmd() *cobra.Command {
	var inherit bool
	cmd := &cobra.Command{
		Use:   "protect [on|off|reset|password] | protect <имя> <default|on|off|reset|password>",
		Short: "Форма входа по отдельному паролю перед всем сайтом",
		Long: `Форма закрывает весь сайт отдельным паролем, без связи с учётками DLE.
protect password задаёт общий пароль; protect <имя> password — пароль хоста.
protect <имя> password --default возвращает хост к общему паролю.
Пароль читается с терминала без эха с подтверждением, либо одной строкой из stdin.
Смена пароля завершает сессии затронутых хостов. Без пароля включение запрещено.

Глобальная настройка (по умолчанию off) действует на хосты в режиме default;
режимы on и off хоста её не учитывают. Срок сессии — protect_session_ttl
(по умолчанию never). Без аргументов — настройки и наличие паролей.`,
		Example: "  dlehost protect               # глобальная настройка и хосты\n" +
			"  dlehost protect on            # включить для хостов в режиме default\n" +
			"  dlehost protect shop off      # shop открыт при любой глобальной\n" +
			"  dlehost protect shop default  # shop снова следует глобальной\n" +
			"  dlehost protect shop reset    # завершить все сессии формы shop\n" +
			"  dlehost config set protect_session_ttl 30d\n" +
			"  выход: https://shop.example.com/__dlehost/logout",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			passwordCommand := len(args) == 1 && args[0] == "password" || len(args) == 2 && args[1] == "password"
			if inherit && (!passwordCommand || len(args) != 2) {
				return fmt.Errorf("--default допустим только для protect <имя> password")
			}
			if passwordCommand {
				cfg, s, err := env()
				if err != nil {
					return err
				}
				name := ""
				if len(args) == 2 {
					name = args[0]
				}
				var password []byte
				if !inherit {
					password, err = readProtectPassword(cmd.InOrStdin(), cmd.ErrOrStderr())
					if err != nil {
						return err
					}
					defer clear(password)
				}
				return host.SetPassword(s, name, password, inherit, protectSettings(cfg), step)
			}
			switch len(args) {
			case 1:
				if args[0] == protectReset {
					_, s, err := env()
					if err != nil {
						return err
					}
					return host.ResetSessions(s, "", step)
				}
				if args[0] != host.ProtectOn && args[0] != host.ProtectOff {
					return fmt.Errorf("глобально — dlehost protect on|off|reset|password, для хоста — dlehost protect %s <default|on|off|reset|password>", args[0])
				}
				return updateConfig("protect", args[0])
			case 2:
				if _, err := host.ParseProtect(args[1]); err != nil && args[1] != protectReset {
					return err
				}
				cfg, s, err := env()
				if err != nil {
					return err
				}
				if args[1] == protectReset {
					return host.ResetSessions(s, args[0], step)
				}
				h, err := host.SetProtect(s, args[0], args[1], protectSettings(cfg), step)
				if err != nil {
					return err
				}
				fmt.Printf("%s: %s\n", h.Name, protectState(h, cfg.Protect))
				return nil
			}
			cfg, s, err := env()
			if err != nil {
				return err
			}
			rows, err := host.List(s)
			if err != nil {
				return err
			}
			globalPassword, ownPasswords, err := host.PasswordStatus(s)
			if err != nil {
				return err
			}
			fmt.Println("Глобальный пароль:", passwordPresence(globalPassword))
			fmt.Println("Глобально:", config.Keys["protect"].Get(cfg))
			fmt.Println("Срок сессии:", config.Keys["protect_session_ttl"].Get(cfg))
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ХОСТ\tРЕЖИМ\tВХОД\tСВОЙ ПАРОЛЬ")
			for _, r := range rows {
				if r.Managed {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, r.ProtectMode(), onOff(r.Protected(cfg.Protect)), passwordPresence(ownPasswords[r.Name]))
				}
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&inherit, "default", false, "сбросить пароль хоста к глобальному")
	return cmd
}

const protectReset = "reset"

func protectSettings(cfg *config.Config) host.ProtectSettings {
	return host.ProtectSettings{Global: cfg.Protect, SessionTTL: cfg.SessionTTL()}
}

// protectState — итоговое состояние формы входа; для default с пометкой.
func protectState(h host.Host, global bool) string {
	state := onOff(h.Protected(global))
	if h.ProtectMode() == host.ProtectDefault {
		return state + " (default)"
	}
	return state
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func pathCmd() *cobra.Command {
	var container bool
	cmd := &cobra.Command{
		Use:   "path [имя]",
		Short: "Путь к каталогу файлов хоста (без имени — к каталогу стека)",
		Example: "  dlehost path                   # каталог стека\n" +
			"  dlehost path shop              # каталог хоста на этой машине\n" +
			"  dlehost path shop --container  # тот же каталог внутри контейнеров\n" +
			"  cd (dlehost path shop)         # fish; в bash/zsh: cd \"$(dlehost path shop)\"",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := stackOnly()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				if container {
					return fmt.Errorf("--container применим только к хосту")
				}
				fmt.Println(s.Dir)
				return nil
			}
			path, err := host.Path(s, args[0], container)
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&container, "container", "c", false, "путь внутри контейнеров apache/php ("+stack.ContainerWWWDir+"/<имя>)")
	return cmd
}

func portCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "port [номер]",
		Short: "Показать или сменить HTTP-порт apache (общий для всех хостов)",
		Example: "  dlehost port       # текущий порт\n" +
			"  dlehost port 80    # http://<имя>.localhost/",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := env()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				fmt.Println(s.HTTPPort())
				return nil
			}
			port, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("некорректный порт %q", args[0])
			}
			if err := host.SetHTTPPort(s, port, step); err != nil {
				return err
			}
			fmt.Println("Порт", port, "— пример адреса:", s.HostURL(s.Domain("<имя>")))
			return nil
		},
	}
}

func confirm(question string) bool {
	fmt.Print(question, " [y/N]: ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes" || answer == "д" || answer == "да"
}

func versionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "versions",
		Short: "Доступные версии DLE (из dists_dir) и PHP",
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
			fmt.Println("DLE:", strings.Join(markDefault(dists, cfg.DefaultDLE), " "))
			fmt.Println("PHP:", strings.Join(markDefault(cfg.PHPVersions, cfg.DefaultPHP), " "))
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

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Управление конфигом CLI",
		Long:  "Управление конфигом CLI.\n\nКлючи:\n" + configKeysHelp(),
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Показать конфиг",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := config.Load()
				if err != nil {
					return err
				}
				path, _ := config.Path()
				fmt.Println("#", path)
				for _, k := range config.KeyNames() {
					fmt.Printf("%s: %s\n", k, config.Keys[k].Get(cfg))
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "get <ключ>",
			Short: "Значение ключа",
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
				fmt.Println(key.Get(cfg))
				return nil
			},
		},
		&cobra.Command{
			Use:   "set <ключ> <значение>",
			Short: "Установить значение ключа",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				return updateConfig(args[0], args[1])
			},
		},
		&cobra.Command{
			Use:   "unset <ключ>",
			Short: "Сбросить ключ",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return updateConfig(args[0], "")
			},
		},
		&cobra.Command{
			Use:   "path",
			Short: "Путь к файлу конфига",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				path, err := config.Path()
				if err != nil {
					return err
				}
				fmt.Println(path)
				return nil
			},
		},
	)
	return cmd
}

func configKeysHelp() string {
	var b strings.Builder
	for _, k := range config.KeyNames() {
		fmt.Fprintf(&b, "  %-12s %s\n", k, config.Keys[k].Help)
	}
	return b.String()
}

func updateConfig(name, value string) error {
	key, err := config.Lookup(name)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	prev := protectSettings(cfg)
	if err := key.Set(cfg, value); err != nil {
		return err
	}
	if name == "protect" || name == "protect_session_ttl" {
		// Сначала vhost'ы хостов, затем конфиг; при ошибке — откат.
		s, err := openStack(cfg)
		if err != nil {
			return err
		}
		if name == "protect" && cfg.Protect {
			set, _, err := host.PasswordStatus(s)
			if err != nil {
				return err
			}
			if !set {
				return fmt.Errorf("не задан глобальный пароль: выполните dlehost protect password")
			}
		}
		if err := host.ApplyProtect(s, protectSettings(cfg), prev, func() error { return config.Save(cfg) }, step); err != nil {
			return err
		}
	} else if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("%s: %s\n", name, key.Get(cfg))
	return nil
}

// credentialValue: явный флаг → переменная окружения → значение флага по умолчанию.
func credentialValue(cmd *cobra.Command, flag, value, variable string) string {
	if cmd.Flags().Changed(flag) {
		return value
	}
	if v, ok := os.LookupEnv(variable); ok {
		return v
	}
	return value
}

func passwordPresence(set bool) string {
	if set {
		return "задан"
	}
	return "не задан"
}

// Пароль никогда не принимается аргументом команды или флагом.
func readProtectPassword(in io.Reader, out io.Writer) ([]byte, error) {
	var password []byte
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(out, "Пароль protect: ")
		var err error
		password, err = term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return nil, err
		}
		fmt.Fprint(out, "Повторите пароль: ")
		confirm, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(out)
		defer clear(confirm)
		if err != nil {
			clear(password)
			return nil, err
		}
		if !bytes.Equal(password, confirm) {
			clear(password)
			return nil, fmt.Errorf("пароли не совпадают")
		}
	} else {
		// Лимит чтения исключает неограниченное выделение памяти; пробелы сохраняются.
		data, err := io.ReadAll(io.LimitReader(in, 75))
		if err != nil {
			return nil, err
		}
		password = bytes.TrimSuffix(bytes.TrimSuffix(data, []byte("\n")), []byte("\r"))
	}
	if len(password) == 0 || len(password) > 72 || bytes.ContainsAny(password, "\r\n\x00") {
		clear(password)
		return nil, fmt.Errorf("пароль должен быть одной строкой от 1 до 72 байт без NUL")
	}
	return password, nil
}
