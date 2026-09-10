package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestIsTransientGitOutput проверяет маркеры stderr git/ssh для retry.
func TestIsTransientGitOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stderr string
		want   bool
	}{
		{
			name:   "ssh handshake reset",
			stderr: "kex_exchange_identification: read: Connection reset by peer\nfatal: Could not read from remote repository.",
			want:   true,
		},
		{
			name:   "maxstartups",
			stderr: "ssh_exchange_identification: Connection closed by remote host\nExceeded MaxStartups",
			want:   true,
		},
		{
			name:   "auth denied is permanent",
			stderr: "Permission denied (publickey).",
			want:   false,
		},
		{
			name:   "missing repo is permanent",
			stderr: "remote: The project you were looking for could not be found.",
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isTransientGitOutput(tc.stderr, nil)
			if got != tc.want {
				t.Fatalf("isTransientGitOutput(%q) = %v, want %v", tc.stderr, got, tc.want)
			}
		})
	}
}

// TestWithNetworkRetryRetriesTransientThenSucceeds проверяет повтор transient-ошибки до успеха.
func TestWithNetworkRetryRetriesTransientThenSucceeds(t *testing.T) {
	t.Parallel()

	client := NewClient(time.Second, t.TempDir(), WithNetworkRetry(2, time.Millisecond))
	var calls atomic.Int32
	err := client.withNetworkRetry(t.Context(), func(context.Context) error {
		n := calls.Add(1)
		if n < 3 {
			return fmt.Errorf("%w: kex_exchange_identification: read: Connection reset by peer", ErrTransientNetwork)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls.Load())
	}
}

// TestWithNetworkRetryDoesNotRetryPermanentError проверяет, что постоянная ошибка не ретраится.
func TestWithNetworkRetryDoesNotRetryPermanentError(t *testing.T) {
	t.Parallel()

	client := NewClient(time.Second, t.TempDir(), WithNetworkRetry(5, time.Millisecond))
	var calls atomic.Int32
	err := client.withNetworkRetry(t.Context(), func(context.Context) error {
		calls.Add(1)
		return errors.New("ошибка git fetch: Permission denied (publickey)")
	})
	if err == nil {
		t.Fatal("expected permanent error")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected single attempt for permanent error, got %d", calls.Load())
	}
}

// TestNetworkSemaphoreLimitsConcurrency проверяет, что второй fetch ждет слот семафора.
func TestNetworkSemaphoreLimitsConcurrency(t *testing.T) {
	t.Parallel()

	client := NewClient(time.Second, t.TempDir(), WithNetworkConcurrency(1), WithNetworkRetry(0, time.Millisecond))

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		_ = client.withNetwork(context.Background(), func() error {
			close(started)
			<-release
			return nil
		})
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first network slot did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	err := client.withNetwork(ctx, func() error { return nil })
	if err == nil {
		t.Fatal("expected second operation to wait on semaphore")
	}
	if !strings.Contains(err.Error(), "ожидание слота сетевой git-операции") {
		t.Fatalf("unexpected wait error: %v", err)
	}

	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("first network operation did not finish")
	}
}

// TestWithNetworkConcurrencyOptionIgnoredWhenNonPositive проверяет, что неположительный лимит не меняет default.
func TestWithNetworkConcurrencyOptionIgnoredWhenNonPositive(t *testing.T) {
	t.Parallel()

	client := NewClient(time.Second, t.TempDir(), WithNetworkConcurrency(0))
	if cap(client.networkSem) != defaultNetworkConcurrency {
		t.Fatalf("expected default concurrency %d, got %d", defaultNetworkConcurrency, cap(client.networkSem))
	}
}
