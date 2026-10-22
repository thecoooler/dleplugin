package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

// Режимы формы входа хоста (dlehost protect). В реестре default хранится
// пустым значением, поэтому старые записи без поля наследуют глобальную настройку.
const (
	ProtectDefault = "default"
	ProtectOn      = "on"
	ProtectOff     = "off"
)

// ProtectSettings — глобальные настройки формы входа из конфига.
type ProtectSettings struct {
	Global     bool          // форма для хостов в режиме default
	SessionTTL time.Duration // срок сессии; 0 — без серверного истечения
}

// shorter: сессии, выданные при old, могут жить дольше, чем разрешает s.
func (s ProtectSettings) shorter(old ProtectSettings) bool {
	return s.SessionTTL > 0 && (old.SessionTTL == 0 || s.SessionTTL < old.SessionTTL)
}

// ParseProtect переводит режим из CLI в значение поля реестра.
func ParseProtect(mode string) (string, error) {
	switch mode {
	case ProtectDefault, "":
		return "", nil
	case ProtectOn, ProtectOff:
		return mode, nil
	}
	return "", fmt.Errorf("режим входа %q: допустимы default, on, off", mode)
}

// ProtectMode — режим хоста для вывода: default, on или off.
func (h Host) ProtectMode() string {
	if h.Protect == "" {
		return ProtectDefault
	}
	return h.Protect
}

// Protected — закрыт ли хост формой входа при глобальной настройке global.
func (h Host) Protected(global bool) bool {
	switch h.Protect {
	case ProtectOn:
		return true
	case ProtectOff:
		return false
	}
	return global
}

// gateMarker есть только в vhost'ах, закрытых формой входа.
const gateMarker = "SetEnv DLEHOST_GATE_SITE "

// Значение cookie: <срок UTC, как %{TIME} в apache>-<256 бит base64url>.
const cookieValueRe = `(([0-9]{14})-[A-Za-z0-9_-]{43})\s*(?:;|$)`

// gateExpr — условие действующей сессии. Имя cookie зависит от схемы так
// же, как в gate.php: на HTTPS (X-Forwarded-Proto: https у публичных
// HTTPS-хостов, как и FastCGI HTTPS=on) принимается только Secure-cookie
// __Host-dlehost_gate, иначе только dlehost_gate. Срок в значении не
// истёк, и для значения есть файл с отпечатком действующего bcrypt-хеша
// в каталоге сессий хоста. Старый пароль не даёт действующую сессию.
func gateExpr(h Host) string {
	plain := `%{HTTP_COOKIE} =~ /(?:^|;)\s*dlehost_gate=` + cookieValueRe + `/`
	cookie := plain
	if strings.HasPrefix(h.ExternalOrigin, "https://") {
		secure := `%{HTTP_COOKIE} =~ /(?:^|;)\s*__Host-dlehost_gate=` + cookieValueRe + `/`
		cookie = fmt.Sprintf(`(req('X-Forwarded-Proto') == 'https' && %s || req('X-Forwarded-Proto') != 'https' && %s)`, secure, plain)
	}
	return fmt.Sprintf(`%s && $2 > %%{TIME} && -f '%s/%s/' . sha1($1) . '-v2-%s'`, cookie, stack.ContainerGateDir, h.Name, h.gateKey)
}

// gateConfig закрывает весь vhost формой входа правилом mod_rewrite уровня
// vhost. Оно выполняется при сопоставлении URL до чтения .htaccess, поэтому
// правила сайта (RewriteEngine Off, Satisfy Any, Require в <If> и т.п.) его
// не отменяют; запрос без сессии уходит в форму. Это единственная проверка:
// отдельного резервного Require нет. Ограничения .htaccess сайта после входа
// действуют как обычно. Без входа доступны только /__dlehost/login и logout.
func gateConfig(h Host, ttl time.Duration) string {
	return fmt.Sprintf(`    # Вход по отдельному паролю сайта (dlehost protect)
    %[1]s%[2]s
    SetEnv DLEHOST_GATE_TTL %[3]d
    AliasMatch "^/__dlehost/(login|logout)$" "%[4]s/gate.php"
    RewriteEngine On
    RewriteCond %%{REQUEST_URI} !^/__dlehost/(login|logout)$
    RewriteCond expr "!(%[5]s)"
    RewriteRule ^ /__dlehost/login [PT,L]
    <LocationMatch "^/__dlehost/(login|logout)$">
        Require all granted
    </LocationMatch>
`, gateMarker, h.Name, int64(ttl/time.Second), stack.ContainerGateApp, gateExpr(h))
}

// gateAliases перехватывает у автохостов 90-auto.conf адреса <имя>.localhost
// и <имя>.*.localhost, которые иначе отдали бы каталог хоста без формы.
func gateAliases(h Host) string {
	return fmt.Sprintf("    ServerAlias %[1]s.localhost %[1]s.*.localhost\n", h.Name)
}

func ensureGateDir(s *stack.Stack, name string) error {
	dir := s.GateDir(name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// От root каталог передаётся пользователю контейнеров (UID/GID из .env).
	if os.Getuid() == 0 {
		uid, err1 := strconv.Atoi(s.Env["UID"])
		gid, err2 := strconv.Atoi(s.Env["GID"])
		if err1 != nil || err2 != nil {
			return fmt.Errorf("UID/GID в .env не заданы: каталог %s нужно передать пользователю контейнеров", dir)
		}
		return os.Chown(dir, uid, gid)
	}
	return nil
}

// clearSessions удаляет все сессии формы входа хоста: без файла сессии
// apache cookie не примет. keep оставляет пустой каталог закрытому хосту
// (при запуске от root php не смог бы создать его сам).
func clearSessions(s *stack.Stack, name string, keep bool) error {
	dir := s.GateDir(name)
	if !keep {
		return os.RemoveAll(dir)
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// applyVhosts приводит vhost'ы hosts к настройкам формы входа, проверяет и
// перезагружает apache, затем выполняет commit (запись реестра или
// конфига). При ошибке на любом шаге прежние vhost'ы возвращаются (заново
// созданные удаляются) и apache перезагружается снова, так что защита не
// числится включённой без применения. Переписываются vhost'ы, у которых
// меняется состояние формы, закрытые vhost'ы с иными параметрами формы и
// отсутствующие vhost'ы хостов реестра (восстанавливаются из реестра, иначе
// закрытый хост остался бы открыт через автохост <имя>.localhost).
// clear — хосты, чьи сессии удаляются после успешного commit.
func applyVhosts(s *stack.Stack, hosts []Host, settings ProtectSettings, commit func() error, clear func(h Host, wasProtected bool) bool, log func(string)) error {
	type change struct {
		h            Host
		path         string
		old          []byte
		existed      bool
		wasProtected bool
		protected    bool
	}
	passwords, err := loadPasswords(s)
	if err != nil {
		return err
	}
	// Сначала проверяем весь набор, чтобы ошибка не оставила частичных изменений.
	for _, h := range hosts {
		if h.Protected(settings.Global) && passwords.effective(h.Name) == "" {
			return missingPassword(h.Name)
		}
	}
	var changes []change
	type clearing struct {
		name string
		keep bool
	}
	var cleared []clearing
	for _, h := range hosts {
		h.gateKey = passwordKey(passwords.effective(h.Name))
		path := vhostPath(s, h.Name)
		protected := h.Protected(settings.Global)
		want := vhostConfig(h, protected, settings.SessionTTL)
		old, err := os.ReadFile(path)
		existed := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("vhost хоста %s: %w", h.Name, err)
		}
		wasProtected := strings.Contains(string(old), gateMarker)
		if existed && wasProtected == protected && (!protected || string(old) == want) {
			if clear(h, wasProtected) {
				cleared = append(cleared, clearing{h.Name, protected})
			}
			continue
		}
		changes = append(changes, change{h, path, old, existed, wasProtected, protected})
		if clear(h, wasProtected) {
			cleared = append(cleared, clearing{h.Name, protected})
		}
	}
	for _, c := range changes {
		if !c.protected {
			continue
		}
		if err := ensureGateDir(s, c.h.Name); err != nil {
			return err
		}
		if err := s.CheckGate(c.h.PHP, c.h.Name); err != nil {
			return err
		}
	}

	restore := func(cause error) error {
		var errs []error
		for _, c := range changes {
			var err error
			if c.existed {
				err = os.WriteFile(c.path, c.old, 0o644)
			} else if err = os.Remove(c.path); errors.Is(err, os.ErrNotExist) {
				err = nil
			}
			if err != nil {
				errs = append(errs, err)
			}
		}
		if len(changes) > 0 {
			if err := s.ReloadApache(); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("%w; вернуть прежние vhost'ы не удалось: %v", cause, errors.Join(errs...))
		}
		return fmt.Errorf("%w (прежние vhost'ы возвращены)", cause)
	}
	for _, c := range changes {
		state := "выключена"
		if c.protected {
			state = "включена"
		}
		switch {
		case !c.existed:
			log("vhost " + c.h.Name + " отсутствовал — восстановлен из реестра, форма входа " + state)
		case c.wasProtected != c.protected:
			log("форма входа " + c.h.Name + ": " + state)
		default:
			log("форма входа " + c.h.Name + ": новые параметры")
		}
		if err := os.WriteFile(c.path, []byte(vhostConfig(c.h, c.protected, settings.SessionTTL)), 0o644); err != nil {
			return restore(err)
		}
	}
	if len(changes) > 0 {
		log("перезагрузка apache")
		if err := s.ReloadApache(); err != nil {
			return restore(err)
		}
	}
	if err := commit(); err != nil {
		return restore(err)
	}
	var errs []error
	for _, c := range cleared {
		log("сессии формы входа " + c.name + " удалены")
		if err := clearSessions(s, c.name, c.keep); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("настройки применены, но удалить сессии не удалось: %w", errors.Join(errs...))
	}
	return nil
}

// SetProtect меняет режим формы входа хоста name: default, on или off.
// При выключении сессии хоста удаляются.
func SetProtect(s *stack.Stack, name, mode string, settings ProtectSettings, log func(string)) (Host, error) {
	value, err := ParseProtect(mode)
	if err != nil {
		return Host{}, err
	}
	hosts, err := loadRegistry(s)
	if err != nil {
		return Host{}, err
	}
	h, ok := hosts[name]
	if !ok {
		return Host{}, fmt.Errorf("хост %s не найден среди созданных через dlehost", name)
	}
	h.Protect = value
	hosts[name] = h
	clear := func(h Host, was bool) bool { return was && !h.Protected(settings.Global) }
	return h, applyVhosts(s, []Host{h}, settings, func() error { return saveRegistry(s, hosts) }, clear, log)
}

// ApplyProtect применяет новые глобальные настройки settings (прежние —
// prev) ко всем хостам реестра; commit сохраняет их в конфиге после
// перезагрузки apache. Сессии удаляются у хостов, где форма выключается,
// и у всех закрытых хостов, если срок сессии стал короче: выданные раньше
// сессии иначе жили бы дольше нового срока.
func ApplyProtect(s *stack.Stack, settings, prev ProtectSettings, commit func() error, log func(string)) error {
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	list := make([]Host, 0, len(hosts))
	for _, h := range hosts {
		list = append(list, h)
	}
	sortHosts(list)
	clear := func(h Host, was bool) bool {
		return was && (!h.Protected(settings.Global) || settings.shorter(prev))
	}
	return applyVhosts(s, list, settings, commit, clear, log)
}

// ResetSessions завершает все сессии формы входа хоста name (или всех
// хостов реестра, если name пустое). Пользователям нужно войти заново.
func ResetSessions(s *stack.Stack, name string, log func(string)) error {
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	if name != "" {
		if _, ok := hosts[name]; !ok {
			return fmt.Errorf("хост %s не найден среди созданных через dlehost", name)
		}
		hosts = map[string]Host{name: hosts[name]}
	}
	var errs []error
	for n := range hosts {
		if _, err := os.Stat(s.GateDir(n)); err == nil {
			log("сессии формы входа " + n + " удалены")
		}
		if err := clearSessions(s, n, true); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
