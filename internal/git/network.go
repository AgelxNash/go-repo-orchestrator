package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrTransientNetwork помечает повторно пробуемые сетевые сбои git
// (сброс SSH на handshake, MaxStartups, таймаут).
var ErrTransientNetwork = errors.New("transient network error")

// acquireNetwork занимает слот семафора сетевых git-операций.
func (c *Client) acquireNetwork(ctx context.Context) error {
	if c == nil || c.networkSem == nil {
		return nil
	}
	ctx = ctxOrBackground(ctx)
	select {
	case <-ctx.Done():
		return fmt.Errorf("ожидание слота сетевой git-операции: %w", ctx.Err())
	case c.networkSem <- struct{}{}:
		return nil
	}
}

// releaseNetwork освобождает слот семафора сетевых git-операций.
func (c *Client) releaseNetwork() {
	if c == nil || c.networkSem == nil {
		return
	}
	select {
	case <-c.networkSem:
	default:
	}
}

// withNetwork выполняет op, удерживая один слот семафора на время вызова.
func (c *Client) withNetwork(ctx context.Context, op func() error) error {
	if err := c.acquireNetwork(ctx); err != nil {
		return err
	}
	defer c.releaseNetwork()
	return op()
}

// withNetworkRetry выполняет сетевую git-операцию с ограниченным параллелизмом
// и повторами для transient-сбоев. Слот семафора не удерживается на время backoff,
// чтобы другие репозитории могли продолжить синхронизацию.
func (c *Client) withNetworkRetry(ctx context.Context, op func(context.Context) error) error {
	if op == nil {
		return errors.New("сетевая git-операция не задана")
	}

	attempts := 1
	if c != nil && c.maxRetries > 0 {
		attempts = 1 + c.maxRetries
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			backoff := defaultNetworkRetryBackoff
			if c != nil && c.retryBackoff > 0 {
				backoff = c.retryBackoff * time.Duration(1<<(i-1))
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctxOrBackground(ctx).Done():
				timer.Stop()
				if lastErr != nil {
					return lastErr
				}
				return ctxOrBackground(ctx).Err()
			case <-timer.C:
			}
		}

		lastErr = c.withNetwork(ctx, func() error {
			return op(ctx)
		})
		if lastErr == nil {
			return nil
		}
		if ctxOrBackground(ctx).Err() != nil {
			return lastErr
		}
		if !isTransientNetworkError(lastErr) {
			return lastErr
		}
	}

	return lastErr
}

// isTransientNetworkError сообщает, что ошибка относится к повторно пробуемому сетевому сбою.
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrTransientNetwork) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return false
}

// isTransientGitOutput распознает stderr git/ssh, характерный для сброса handshake и MaxStartups.
func isTransientGitOutput(stderr string, runErr error) bool {
	if errors.Is(runErr, context.DeadlineExceeded) {
		return true
	}

	var b strings.Builder
	b.WriteString(stderr)
	if runErr != nil {
		b.WriteByte(' ')
		b.WriteString(runErr.Error())
	}
	msg := strings.ToLower(b.String())
	for _, marker := range transientGitMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// transientGitMarkers — маркеры stderr git/ssh, которые соответствуют сбросу
// handshake или лимиту одновременных SSH-сессий, а не ошибке ключей/прав.
var transientGitMarkers = []string{
	"kex_exchange_identification",
	"ssh_exchange_identification",
	"connection reset by peer",
	"connection timed out",
	"i/o timeout",
	"operation timed out",
	"broken pipe",
	"connection closed by remote host",
	"maxstartup",
	"exceeded maxstartups",
	"resource temporarily unavailable",
	"temporarily unavailable",
	"early eof",
}
