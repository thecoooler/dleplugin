package host

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
	"golang.org/x/crypto/bcrypt"
)

// Хранилище не связано с конфигом DLE и монтируется в контейнеры только для чтения.
type protectPasswords struct {
	Global string            `json:"global,omitempty"`
	Hosts  map[string]string `json:"hosts"`
}

func passwordPath(s *stack.Stack) string {
	return filepath.Join(s.Dir, "data", "protect", "passwords.json")
}

func loadPasswords(s *stack.Stack) (protectPasswords, error) {
	p := protectPasswords{Hosts: map[string]string{}}
	data, err := os.ReadFile(passwordPath(s))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("не удалось прочитать хранилище паролей protect")
	}
	if p.Hosts == nil {
		p.Hosts = map[string]string{}
	}
	for name, hash := range p.Hosts {
		if !hostNameRe.MatchString(name) || !validProtectHash(hash) {
			return p, fmt.Errorf("некорректный хеш protect хоста %s", name)
		}
	}
	if p.Global != "" && !validProtectHash(p.Global) {
		return p, fmt.Errorf("некорректный глобальный хеш protect")
	}
	return p, nil
}

var protectHashRE = regexp.MustCompile(`^\$2[aby]\$10\$[./A-Za-z0-9]{53}$`)

func validProtectHash(hash string) bool {
	if !protectHashRE.MatchString(hash) {
		return false
	}
	cost, err := bcrypt.Cost([]byte(hash))
	return err == nil && cost == bcrypt.DefaultCost
}

func (p protectPasswords) effective(name string) string {
	if hash := p.Hosts[name]; hash != "" {
		return hash
	}
	return p.Global
}

func passwordKey(hash string) string {
	sum := sha256.Sum256([]byte(hash))
	return hex.EncodeToString(sum[:])
}

func savePasswords(s *stack.Stack, p protectPasswords) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := passwordPath(s)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Переименование внутри смонтированного каталога видно уже работающему PHP.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".passwords-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// PasswordStatus возвращает только наличие паролей, без самих хешей.
func PasswordStatus(s *stack.Stack) (bool, map[string]bool, error) {
	p, err := loadPasswords(s)
	own := map[string]bool{}
	for name := range p.Hosts {
		own[name] = true
	}
	return p.Global != "", own, err
}

// SetPassword задаёт пароль (name пустое — глобальный), либо возвращает
// хост к глобальному паролю (inherit). Открытый пароль существует только в памяти.
func SetPassword(s *stack.Stack, name string, password []byte, inherit bool, settings ProtectSettings, log func(string)) error {
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	if name != "" {
		if _, ok := hosts[name]; !ok {
			return fmt.Errorf("хост %s не найден среди созданных через dlehost", name)
		}
	} else if inherit {
		return fmt.Errorf("--default допустим только для пароля хоста")
	}
	p, err := loadPasswords(s)
	if err != nil {
		return err
	}
	old, err := loadPasswords(s)
	if err != nil {
		return err
	}
	if inherit {
		delete(p.Hosts, name)
	} else {
		if len(password) == 0 || len(password) > 72 {
			return fmt.Errorf("пароль должен содержать от 1 до 72 байт")
		}
		hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("не удалось создать хеш пароля: %w", err)
		}
		if name == "" {
			p.Global = string(hash)
		} else {
			p.Hosts[name] = string(hash)
		}
	}
	// Проверяем все затронутые хосты до записи: сброс не оставит закрытый хост без пароля.
	for _, h := range hosts {
		if (name == "" || h.Name == name) && h.Protected(settings.Global) && p.effective(h.Name) == "" {
			return missingPassword(h.Name)
		}
	}
	// При первом обновлении нужно подключить новый каталог только для чтения.
	// Пароль можно задать до up; старые сессии отзываются сразу.
	legacy := false
	for _, h := range hosts {
		data, err := os.ReadFile(vhostPath(s, h.Name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if strings.Contains(string(data), "Вход по логину и паролю администратора DLE") {
			legacy = true
		}
	}
	if err := savePasswords(s, p); err != nil {
		return err
	}
	if legacy {
		if err := ResetSessions(s, "", log); err != nil {
			return err
		}
		log("пароль сохранён; для перехода со старой формы выполните dlehost up")
		return nil
	}
	err = ApplyProtect(s, settings, settings, func() error { return nil }, log)
	if err != nil {
		if rerr := savePasswords(s, old); rerr != nil {
			return fmt.Errorf("%w; вернуть прежние хеши не удалось: %v", err, rerr)
		}
		return err
	}
	// Пространство файлов сессий уже изменено в vhost; удаляем старые файлы.
	for _, h := range hosts {
		if old.effective(h.Name) != p.effective(h.Name) {
			if err := clearSessions(s, h.Name, h.Protected(settings.Global)); err != nil {
				return fmt.Errorf("пароль изменён, но удалить сессии %s не удалось: %w", h.Name, err)
			}
		}
	}
	log("пароль protect сохранён")
	return nil
}

func missingPassword(name string) error {
	return fmt.Errorf("для закрытого хоста %s не задан пароль: выполните dlehost protect password или dlehost protect %s password", name, name)
}

// CheckProtectPasswords проверяет настройки перед запуском стека. Сессии старой
// формы DLE отзываются даже при незавершённой миграции; сайты остаются закрытыми.
func CheckProtectPasswords(s *stack.Stack, settings ProtectSettings) error {
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	p, err := loadPasswords(s)
	if err != nil {
		return err
	}
	for _, h := range hosts {
		data, err := os.ReadFile(vhostPath(s, h.Name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if strings.Contains(string(data), "Вход по логину и паролю администратора DLE") {
			if err := clearSessions(s, h.Name, true); err != nil {
				return err
			}
		}
	}
	for _, h := range hosts {
		if h.Protected(settings.Global) && p.effective(h.Name) == "" {
			return missingPassword(h.Name)
		}
	}
	return nil
}
