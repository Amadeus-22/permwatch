package backoff

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func noSleep(context.Context, time.Duration) error { return nil }

func TestDo(t *testing.T) {
	transient := Retryable(errors.New("status 503"))
	permanent := errors.New("status 400")

	tests := []struct {
		name      string
		retries   int
		results   []error // one per attempt; nil means success
		wantCalls int
		wantErr   error
	}{
		{"succeeds first time", 3, []error{nil}, 1, nil},
		{"succeeds after transient failures", 3, []error{transient, transient, nil}, 3, nil},
		{"stops on a permanent error", 3, []error{permanent}, 1, permanent},
		{"gives up after the retries", 2, []error{transient, transient, transient}, 3, transient},
		{"zero retries tries once", 0, []error{transient}, 1, transient},
		{"wrapped retryable is retried", 1, []error{fmt.Errorf("fetch: %w", transient), nil}, 2, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got, err := Do(context.Background(), Policy{Base: time.Millisecond, Max: time.Millisecond}, tc.retries, noSleep,
				func(context.Context) (int, error) {
					res := tc.results[calls]
					calls++
					if res != nil {
						return 0, res
					}
					return 42, nil
				})
			if calls != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", calls, tc.wantCalls)
			}
			if tc.wantErr == nil {
				if err != nil || got != 42 {
					t.Fatalf("got (%d, %v), want (42, nil)", got, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDoStopsWhenTheWaitIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := Do(ctx, Policy{Base: time.Hour, Max: time.Hour}, 3, nil, func(context.Context) (int, error) {
		calls++
		return 0, Retryable(errors.New("status 503"))
	})
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("calls = %d, err = %v; want 1 call and context.Canceled", calls, err)
	}
}

func TestRetryableNil(t *testing.T) {
	if Retryable(nil) != nil || IsRetryable(nil) {
		t.Fatal("Retryable(nil) must stay nil and not retryable")
	}
}
