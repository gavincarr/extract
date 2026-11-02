package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var keyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseFile reads a shell-style KEY=value file. Values are literal, as with
// Config::Tiny: quotes are stripped but nothing is expanded. Later
// duplicate keys override earlier ones. Errors name the file, and for
// syntax errors the line number.
func parseFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read config file %s: %w", path, err)
	}
	env := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		key, val, ok, err := parseLine(strings.TrimSuffix(line, "\r"))
		if err != nil {
			return nil, fmt.Errorf("config file %s line %d: %w", path, i+1, err)
		}
		if ok {
			env[key] = val
		}
	}
	return env, nil
}

// parseLine parses one line; ok is false for blank and comment lines.
func parseLine(line string) (key, val string, ok bool, err error) {
	s := strings.TrimLeft(line, " \t")
	if s == "" || s[0] == '#' {
		return "", "", false, nil
	}
	if rest, found := strings.CutPrefix(s, "export"); found && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
		s = strings.TrimLeft(rest, " \t")
	}
	k, v, found := strings.Cut(s, "=")
	if !found {
		return "", "", false, fmt.Errorf("expected KEY=value")
	}
	key = strings.TrimSpace(k)
	if !keyRE.MatchString(key) {
		return "", "", false, fmt.Errorf("invalid key %q", key)
	}
	val, err = parseValue(strings.TrimLeft(v, " \t"))
	if err != nil {
		return "", "", false, err
	}
	return key, val, true, nil
}

func parseValue(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	var val, rest string
	switch v[0] {
	case '\'':
		end := strings.IndexByte(v[1:], '\'')
		if end < 0 {
			return "", fmt.Errorf("unterminated single quote")
		}
		val, rest = v[1:1+end], v[2+end:]
	case '"':
		var b strings.Builder
		i, closed := 1, false
		for i < len(v) {
			c := v[i]
			if c == '\\' && i+1 < len(v) && (v[i+1] == '"' || v[i+1] == '\\') {
				b.WriteByte(v[i+1])
				i += 2
				continue
			}
			i++
			if c == '"' {
				closed = true
				break
			}
			b.WriteByte(c)
		}
		if !closed {
			return "", fmt.Errorf("unterminated double quote")
		}
		val, rest = b.String(), v[i:]
	default:
		if i := strings.Index(v, " #"); i >= 0 {
			v = v[:i]
		}
		if i := strings.Index(v, "\t#"); i >= 0 {
			v = v[:i]
		}
		return strings.TrimRight(v, " \t"), nil
	}
	rest = strings.TrimLeft(rest, " \t")
	if rest != "" && rest[0] != '#' {
		return "", fmt.Errorf("unexpected text after closing quote: %q", rest)
	}
	return val, nil
}
