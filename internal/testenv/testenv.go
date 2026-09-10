// Package testenv содержит хелперы изоляции окружения для тестов.
package testenv

import "os"

import "strings"

// GitSanitizedEnv возвращает окружение без переменных с префиксом GIT_,
// чтобы дочерние git-процессы тестов не наследовали GIT_DIR/GIT_WORK_TREE и
// прочее из внешнего контекста запуска (например, pre-push хук worktree
// получает GIT_DIR на реальный репозиторий — без очистки тестовые git-команды
// выполняются в нём, а не во временном каталоге теста).
func GitSanitizedEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "GIT_") {
			continue
		}
		env = append(env, entry)
	}
	return env
}
