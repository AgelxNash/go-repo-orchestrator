package usecase

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrInvalidBranchName возвращается, если имя ветки содержит недопустимые
// символы: control-символы (CR/LF/NUL/TAB), подозрительные комбинации
// раскрытия % в cmd.exe или символы, нарушающие git check-ref-format.
var ErrInvalidBranchName = errors.New("недопустимое имя ветки")

// maxBranchNameLength ограничивает длину refName для защиты от раздувания
// команд и переполнения буфера.
const maxBranchNameLength = 200

// sanitizeBranchName проверяет имя ветки на безопасность перед встраиванием
// в команды. Возвращает нормализованное имя или ошибку. Пустая строка
// отклоняется, как и имена с control-символами, последовательностями
// для cmd.exe-expansion (`%var%`) и слишком длинные.
func sanitizeBranchName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("%w: пустое имя", ErrInvalidBranchName)
	}
	if len(trimmed) > maxBranchNameLength {
		return "", fmt.Errorf("%w: длиннее %d символов", ErrInvalidBranchName, maxBranchNameLength)
	}

	for _, r := range trimmed {
		if r == 0x7F {
			return "", fmt.Errorf("%w: содержит DEL (0x7F)", ErrInvalidBranchName)
		}
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: содержит control-символ U+%04X", ErrInvalidBranchName, r)
		}
	}

	// Git умеет работать со многими символами, но для cmd.exe раскрытие
	// `%var%` опасно (например, `%PATH%`, `%TEMP%`). Также отклоним
	// backtick, чтобы снизить подозрительную поверхность.
	if strings.Contains(trimmed, "%") {
		return "", fmt.Errorf("%w: содержит '%%' (cmd.exe expansion)", ErrInvalidBranchName)
	}
	if strings.ContainsRune(trimmed, '`') {
		return "", fmt.Errorf("%w: содержит backtick", ErrInvalidBranchName)
	}

	return trimmed, nil
}

// escapeForBat подготавливает значение для безопасного включения в
// двойные кавычки внутри .bat/.cmd файла. Помимо удвоения кавычек
// экранируются символы, имеющие спец. значение в cmd.exe:
//   - `^` — escape-символ, удваивается;
//   - `%` — переменные среды, заменяются на `%%` (если встречается);
//   - `!` — delayed expansion, заменяется на `^!`;
//   - `&`, `|`, `<`, `>` — управляющие символы, оборачиваются кавычками
//     с экранированием. Поскольку вход уже внутри кавычек (двойных),
//     достаточно удвоить внутренние `"`.
//
// Результат рассчитан на встраивание в уже закавыченную строку и не
// содержит самих кавычек в начале/конце.
func escapeForBat(value string) string {
	value = strings.ReplaceAll(value, `"`, `""`)
	value = strings.ReplaceAll(value, "^", "^^")
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, "!", "^!")
	value = strings.ReplaceAll(value, "&", `^&`)
	value = strings.ReplaceAll(value, "|", `^|`)
	value = strings.ReplaceAll(value, "<", `^<`)
	value = strings.ReplaceAll(value, ">", `^>`)
	return value
}

// quoteForBat оборачивает значение в двойные кавычки и применяет
// escapeForBat. Используется для построения готового аргумента BAT.
func quoteForBat(value string) string {
	return `"` + escapeForBat(value) + `"`
}
