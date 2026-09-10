package git

import (
	"errors"
	"fmt"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// ErrEmptyClone — локальный клон без checkout: HEAD не резолвится и нет локальных веток.
var ErrEmptyClone = errors.New("репозиторий-оболочка без checkout")

// newEmptyCloneError возвращает ErrEmptyClone с подсказкой, как восстановить checkout.
func newEmptyCloneError() error {
	return fmt.Errorf("%w: HEAD не резолвится и локальных веток нет; выполните fetch и checkout дефолтной ветки", ErrEmptyClone)
}

// isEmptyCloneRepo сообщает, что в репозитории нет локальных веток.
func isEmptyCloneRepo(repo *git.Repository) bool {
	if repo == nil {
		return false
	}
	branches, err := repo.Branches()
	if err != nil {
		return false
	}
	count := 0
	_ = branches.ForEach(func(*plumbing.Reference) error {
		count++
		return nil
	})
	return count == 0
}
