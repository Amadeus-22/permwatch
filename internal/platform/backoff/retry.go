package backoff

import (
	"context"
	"errors"
	"time"
)

// retryableError marks an error that is worth another attempt.
type retryableError struct{ err error }

func (r retryableError) Error() string { return r.err.Error() }
func (r retryableError) Unwrap() error { return r.err }

// Retryable marks err as transient (a network error, 429 or 5xx), so Do tries
// again. Errors that are not marked stop the retries at once.
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return retryableError{err}
}

// IsRetryable reports whether err, or an error it wraps, was marked Retryable.
func IsRetryable(err error) bool {
	var r retryableError
	return errors.As(err, &r)
}

// Sleep waits for d or until ctx is done, whichever comes first.
func Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Do calls fn until it succeeds, returns an error that is not Retryable, or has
// been retried retries times, waiting p.Delay between attempts with sleep. A nil
// sleep uses Sleep. It returns the last error from fn, or the context error if
// the wait was interrupted.
func Do[T any](ctx context.Context, p Policy, retries int, sleep func(context.Context, time.Duration) error, fn func(context.Context) (T, error)) (T, error) {
	if sleep == nil {
		sleep = Sleep
	}
	var zero T
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, p.Delay(attempt)); err != nil {
				return zero, err
			}
		}
		value, err := fn(ctx)
		if err == nil {
			return value, nil
		}
		lastErr = err
		if !IsRetryable(err) {
			break
		}
	}
	return zero, lastErr
}
