package manifest

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	numberRe  = regexp.MustCompile(`^[-+]?(\d[\d_]*(\.[\d_]*)?|\.\d[\d_]*)([eE][-+]?\d+)?$`)
	baseIntRe = regexp.MustCompile(`^[-+]?0([xX][0-9a-fA-F_]+|[bB][01_]+|[oO]?[0-7_]+)$`)
	sexaRe    = regexp.MustCompile(`^[-+]?\d[\d_]*(:[0-5]?\d)+(\.\d*)?$`)
	dateRe    = regexp.MustCompile(`^\d{4}-\d{1,2}-\d{1,2}([Tt ].*)?$`)
	specialRe = regexp.MustCompile(`^[-+]?\.(inf|Inf|INF)$|^\.(nan|NaN|NAN)$`)
)

// YAMLScalar renders s as a YAML scalar, erring on the side of quoting: plain when safe, single-quoted (” escaping) otherwise,
// and double-quoted when s has newlines or control characters.
func YAMLScalar(s string) string {
	if s == "" {
		return "''"
	}
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || !(unicode.IsPrint(r) || r == ' ') {
			return doubleQuote(s)
		}
	}
	if needsQuote(s) {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return s
}

func needsQuote(s string) bool {
	if s[0] == ' ' || s[len(s)-1] == ' ' || strings.HasSuffix(s, ":") {
		return true
	}
	if strings.Contains(s, ": ") || strings.Contains(s, " #") {
		return true
	}
	if strings.ContainsRune("-?:,[]{}#&*!|>'\"%@`", rune(s[0])) {
		return true
	}
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "y", "n", "null", "~", "=", "<<":
		return true
	}
	// YAML resolves numeric values after removing underscores.
	numeric := strings.ReplaceAll(s, "_", "")
	return numberRe.MatchString(numeric) || baseIntRe.MatchString(numeric) || sexaRe.MatchString(s) ||
		dateRe.MatchString(s) || specialRe.MatchString(s)
}

func doubleQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if unicode.IsPrint(r) {
				b.WriteRune(r)
			} else if r < 0x10000 {
				b.WriteString(`\u` + pad(strconv.FormatInt(int64(r), 16), 4))
			} else {
				b.WriteString(`\U` + pad(strconv.FormatInt(int64(r), 16), 8))
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func pad(s string, n int) string {
	return strings.Repeat("0", n-len(s)) + s
}
