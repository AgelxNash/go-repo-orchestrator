package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// envNameRe разрешает имена переменных окружения POSIX-вида: буква/подчёркивание,
// затем буквы/цифры/подчёркивания.
var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type envLookup func(string) (string, bool)

// ReadFileWithEnvExpansion читает YAML-файл конфигурации и возвращает его
// байты с развёрнутыми плейсхолдерами ${VAR} и ${VAR:-default} (синтаксис
// docker-compose). Строки без плейсхолдеров не меняются; незаданная переменная
// без значения по умолчанию — ошибка конфигурации, чтобы секреты не превращались
// в пустые строки молча.
func ReadFileWithEnvExpansion(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("прочитать конфиг: %w", err)
	}

	expanded, err := ExpandYAMLEnv(raw, os.LookupEnv)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return expanded, nil
}

// ExpandYAMLEnv разворачивает плейсхолдеры во всех строковых значениях
// YAML-документа и возвращает сериализованное дерево.
func ExpandYAMLEnv(raw []byte, lookup envLookup) ([]byte, error) {
	var tree any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("прочитать конфиг: %w", err)
	}
	if tree == nil {
		return raw, nil
	}

	expanded, err := expandEnvNode(tree, "", lookup)
	if err != nil {
		return nil, err
	}

	out, err := yaml.Marshal(expanded)
	if err != nil {
		return nil, fmt.Errorf("собрать конфиг после подстановки: %w", err)
	}

	return out, nil
}

func expandEnvNode(node any, path string, lookup envLookup) (any, error) {
	switch typed := node.(type) {
	case map[string]any:
		expanded := make(map[string]any, len(typed))
		for key, value := range typed {
			next, err := expandEnvNode(value, joinEnvPath(path, key), lookup)
			if err != nil {
				return nil, err
			}
			expanded[key] = next
		}
		return expanded, nil
	case []any:
		expanded := make([]any, len(typed))
		for idx, value := range typed {
			next, err := expandEnvNode(value, fmt.Sprintf("%s[%d]", path, idx), lookup)
			if err != nil {
				return nil, err
			}
			expanded[idx] = next
		}
		return expanded, nil
	case string:
		return expandEnvString(typed, path, lookup)
	default:
		return node, nil
	}
}

func joinEnvPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// expandEnvString разворачивает ${VAR} и ${VAR:-default}; `$$` экранирует
// литеральный `$`. Одиночный `$` без плейсхолдера остаётся как есть
// (актуально для regex-правил, где `$` — якорь конца строки).
func expandEnvString(s, path string, lookup envLookup) (string, error) {
	if !strings.Contains(s, "$") {
		return s, nil
	}

	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '$' {
			out.WriteByte(s[i])
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '$' {
			out.WriteByte('$')
			i += 2
			continue
		}
		if i+1 >= len(s) || s[i+1] != '{' {
			out.WriteByte(s[i])
			i++
			continue
		}

		end := strings.IndexByte(s[i+2:], '}')
		if end < 0 {
			return "", fmt.Errorf("ключ %s: незакрытый плейсхолдер %q (экранируйте как $$, если нужен литеральный $)", path, s[i:])
		}

		body := s[i+2 : i+2+end]
		name, defaultValue, hasDefault := strings.Cut(body, ":-")
		if !envNameRe.MatchString(name) {
			return "", fmt.Errorf("ключ %s: некорректное имя переменной окружения %q в плейсхолдере", path, name)
		}

		value, ok := lookup(name)
		if !ok || (hasDefault && value == "") {
			if !hasDefault && !ok {
				return "", fmt.Errorf("ключ %s: переменная окружения %s не задана", path, name)
			}
			value = defaultValue
		}
		out.WriteString(value)
		i += 2 + end + 1
	}

	return out.String(), nil
}
