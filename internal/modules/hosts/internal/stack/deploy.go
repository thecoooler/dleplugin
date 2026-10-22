package stack

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Файлы docker-стека встроены в бинарник и раскладываются в каталог стека
// при первом запуске. Файлы из files/www/ — заготовки: пишутся один раз и
// дальше принадлежат пользователю. Остальные — служебные: при обновлении
// бинарника перезаписываются, а исчезнувшие из новой версии удаляются.
//
//go:embed files
var stackFS embed.FS

const (
	stackRoot = "files"
	// stackManifest хранит хеш служебных файлов и их список.
	stackManifest = ".dlehost-stack"
	// stackApplied хранит хеш служебных файлов, с которыми последний раз
	// выполнялся dlehost up.
	stackApplied = ".dlehost-applied"
)

// DefaultDir — $XDG_DATA_HOME/dlehost, по умолчанию ~/.local/share/dlehost.
func DefaultDir() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "dlehost"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "dlehost"), nil
}

type stackFile struct {
	rel  string // путь относительно каталога стека, через "/"
	data []byte
	seed bool // заготовка в www/
}

func embeddedStack() ([]stackFile, error) {
	var files []stackFile
	err := fs.WalkDir(stackFS, stackRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := stackFS.ReadFile(p)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, stackRoot+"/")
		files = append(files, stackFile{rel: rel, data: data, seed: strings.HasPrefix(rel, "www/")})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, err
}

// ensure создаёт или обновляет стек в dir и возвращает true,
// если служебные файлы были (пере)записаны.
func ensure(dir string) (bool, error) {
	files, err := embeddedStack()
	if err != nil {
		return false, err
	}
	sum := sha256.New()
	var managed []string
	for _, f := range files {
		if !f.seed {
			fmt.Fprintf(sum, "%s\x00%d\x00", f.rel, len(f.data))
			sum.Write(f.data)
			managed = append(managed, f.rel)
		}
	}
	hash := hex.EncodeToString(sum.Sum(nil))

	for _, d := range []string{"www", "data", "data/gate", "data/protect", "logs/apache"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o755); err != nil {
			return false, err
		}
	}
	if err := writeDefaultDotEnv(filepath.Join(dir, ".env")); err != nil {
		return false, err
	}

	manifestPath := filepath.Join(dir, stackManifest)
	prevHash, prevFiles, err := readStackManifest(manifestPath)
	if err != nil {
		return false, err
	}
	fresh := prevHash == ""
	update := prevHash != hash
	for _, f := range files {
		target := filepath.Join(dir, filepath.FromSlash(f.rel))
		_, statErr := os.Stat(target)
		exists := statErr == nil
		if f.seed && (!fresh || exists) || !f.seed && !update && exists {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(target, f.data, 0o644); err != nil {
			return false, err
		}
	}
	if !update {
		return false, nil
	}
	for _, rel := range prevFiles {
		if !slices.Contains(managed, rel) {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		}
	}
	manifest := hash + "\n" + strings.Join(managed, "\n") + "\n"
	return true, os.WriteFile(manifestPath, []byte(manifest), 0o644)
}

// Pending сообщает, что служебные файлы изменились после последнего
// dlehost up. Правки php.ini, my.cnf и vhost'ов compose сам не замечает:
// контейнеры нужно пересоздать. hash передаётся в MarkApplied после up,
// чтобы не отметить применённой версию, обновлённую во время up.
func (s *Stack) Pending() (hash string, pending bool, err error) {
	hash, _, err = readStackManifest(filepath.Join(s.Dir, stackManifest))
	if err != nil {
		return "", false, err
	}
	applied, err := os.ReadFile(filepath.Join(s.Dir, stackApplied))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	return hash, strings.TrimSpace(string(applied)) != hash, nil
}

// MarkApplied запоминает, что служебные файлы с хешем hash применены.
func (s *Stack) MarkApplied(hash string) error {
	return os.WriteFile(filepath.Join(s.Dir, stackApplied), []byte(hash+"\n"), 0o644)
}

func readStackManifest(p string) (hash string, files []string, err error) {
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, rel := range lines[1:] {
		// не даём манифесту указать на файлы вне стека
		if rel = strings.TrimSpace(rel); filepath.IsLocal(filepath.FromSlash(rel)) {
			files = append(files, rel)
		}
	}
	return strings.TrimSpace(lines[0]), files, nil
}

// defaultDotEnv — ключи нового .env по порядку; "" — пустая строка, "#…" — комментарий.
var defaultDotEnv = []struct{ key, value string }{
	{"#", "Создано dlehost; файл не перезаписывается при обновлении"},
	{"#", "Значение по умолчанию для нового .env можно задать переменной DLEHOST_<КЛЮЧ>"},
	{"#", "Имя проекта docker compose и префикс имён контейнеров"},
	{"COMPOSE_PROJECT_NAME", "dle"},
	{"MARIADB_VERSION", "11.4"},
	{"", ""},
	{"#", "Адрес для портов apache и MariaDB; 127.0.0.1 — только с этой машины"},
	{"BIND_ADDRESS", "0.0.0.0"},
	{"HTTP_PORT", "80"},
	{"DB_PORT", "3307"},
	{"", ""},
	{"DB_ROOT_PASSWORD", "root"},
	{"DB_NAME", "dle"},
	{"DB_USER", "dle"},
	{"DB_PASSWORD", "dle"},
	{"", ""},
	{"#", "Владелец файлов в www/ внутри контейнеров"},
	{"UID", ""},
	{"GID", ""},
}

// writeDefaultDotEnv создаёт .env стека, если его нет. UID/GID — текущего
// пользователя, чтобы файлы в www/ принадлежали ему.
func writeDefaultDotEnv(p string) error {
	if _, err := os.Stat(p); err == nil || !errors.Is(err, os.ErrNotExist) {
		return err
	}
	uid, gid := os.Getuid(), os.Getgid()
	// Windows (-1) или root: php-fpm не запускает пул от UID/GID 0
	if uid <= 0 || gid <= 0 {
		uid, gid = 1000, 1000
	}
	var b strings.Builder
	var err error
	for _, kv := range defaultDotEnv {
		switch kv.key {
		case "":
			b.WriteString("\n")
			continue
		case "#":
			b.WriteString("# " + kv.value + "\n")
			continue
		}
		value := kv.value
		switch kv.key {
		case "UID":
			value = strconv.Itoa(uid)
		case "GID":
			value = strconv.Itoa(gid)
		}
		if v, ok := os.LookupEnv("DLEHOST_" + kv.key); ok {
			if value, err = dotEnvValue(v); err != nil {
				return fmt.Errorf("DLEHOST_%s: %w", kv.key, err)
			}
		}
		b.WriteString(kv.key + "=" + value + "\n")
	}
	// в .env пароли БД
	return os.WriteFile(p, []byte(b.String()), 0o600)
}

var plainDotEnvValue = regexp.MustCompile(`^[A-Za-z0-9_./:@+,-]*$`)

// dotEnvValue записывает значение так, чтобы docker compose прочитал его
// буквально: без подстановки ${…} и обрезки по #.
func dotEnvValue(v string) (string, error) {
	if plainDotEnvValue.MatchString(v) {
		return v, nil
	}
	if strings.ContainsAny(v, "'\n\r") {
		return "", errors.New("значение не должно содержать одинарную кавычку или перевод строки")
	}
	return "'" + v + "'", nil
}
