package host

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/hostconfig"
)

// Tokenize generated PHP configuration without matching keys inside comments or
// other string values. Never execute site PHP to read a setting.
var configTokenRE = regexp.MustCompile(`(?s)//[^\n]*|#[^\n]*|/\*.*?\*/|'(?:\\.|[^'\\])*'|"(?:\\.|[^"\\])*"|=>|\s+|.`)

func readAdminPath(dir string) ([]byte, []int, string, error) {
	path := filepath.Join(dir, "engine/data/config.php")
	if err := regularFile(path); err != nil {
		return nil, nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", fmt.Errorf("read DLE configuration: %w", err)
	}
	var tokens [][]int
	for _, span := range configTokenRE.FindAllIndex(data, -1) {
		token := string(data[span[0]:span[1]])
		if strings.TrimSpace(token) == "" || strings.HasPrefix(token, "//") || strings.HasPrefix(token, "#") || strings.HasPrefix(token, "/*") {
			continue
		}
		tokens = append(tokens, span)
	}
	var match []int
	count := 0
	for i, span := range tokens {
		token := string(data[span[0]:span[1]])
		if token != "'admin_path'" && token != `"admin_path"` {
			continue
		}
		if i+1 >= len(tokens) || string(data[tokens[i+1][0]:tokens[i+1][1]]) != "=>" {
			continue
		}
		count++
		if i+3 >= len(tokens) {
			continue
		}
		v := tokens[i+2]
		literal := data[v[0]:v[1]]
		end := string(data[tokens[i+3][0]:tokens[i+3][1]])
		if len(literal) >= 2 && (literal[0] == '\'' || literal[0] == '"') && literal[len(literal)-1] == literal[0] && (end == "," || end == "]" || end == ")") {
			match = []int{v[0] + 1, v[1] - 1}
		}
	}
	if count != 1 || match == nil {
		return nil, nil, "", fmt.Errorf("%s: expected exactly one literal admin_path setting", path)
	}
	value := string(data[match[0]:match[1]])
	if err := hostconfig.ValidateAdminPath(value); err != nil {
		return nil, nil, "", fmt.Errorf("%s: %w", path, err)
	}
	return data, match, value, nil
}

func regularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: expected a regular file (no symlinks)", path)
	}
	return nil
}

// Stage the complete config before touching the entry point. A hard link claims
// the new filename exclusively and preserves the PHP file's owner and mode.
// Keep it until the config is committed so a failed commit can restore the old
// entry point without copying or truncating either original file.
func setAdminPath(dir, value string, commit func(string, string) error) error {
	data, span, old, err := readAdminPath(dir)
	if err != nil {
		return err
	}
	source, target := filepath.Join(dir, old), filepath.Join(dir, value)
	if err := regularFile(source); err != nil {
		return err
	}
	if old == value {
		return nil
	}
	config := filepath.Join(dir, "engine/data/config.php")
	tmp, err := os.CreateTemp(filepath.Dir(config), ".admin-path-*")
	if err != nil {
		return fmt.Errorf("stage DLE configuration: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := copyConfigMetadata(tmp, config); err != nil {
		return err
	}
	updated := append(bytes.Clone(data[:span[0]]), []byte(value)...)
	updated = append(updated, data[span[1]:]...)
	if _, err := tmp.Write(updated); err != nil {
		return fmt.Errorf("stage DLE configuration: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Link(source, target); err != nil {
		return fmt.Errorf("claim admin_path %s (must not exist): %w", value, err)
	}
	if err := os.Remove(source); err != nil {
		return errors.Join(fmt.Errorf("remove old admin entry point: %w", err), os.Remove(target))
	}
	if err := commit(tmp.Name(), config); err != nil {
		failure := fmt.Errorf("commit DLE configuration: %w", err)
		if rollback := os.Link(target, source); rollback != nil {
			return errors.Join(failure, fmt.Errorf("restore %s: %w; entry point retained at %s", source, rollback, target))
		}
		return errors.Join(failure, os.Remove(target))
	}
	return nil
}
