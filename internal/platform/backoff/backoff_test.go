package backoff

import (
	"testing"
	"time"
)

func TestDelayGrowsAndIsCapped(t *testing.T) {
	p := Policy{Base: 100 * time.Millisecond, Max: time.Second}
	tests := []struct {
		attempt  int
		min, max time.Duration
	}{
		{0, 50 * time.Millisecond, 100 * time.Millisecond},
		{1, 50 * time.Millisecond, 100 * time.Millisecond},
		{2, 100 * time.Millisecond, 200 * time.Millisecond},
		{4, 400 * time.Millisecond, 800 * time.Millisecond},
		{5, 500 * time.Millisecond, time.Second},
		{60, 500 * time.Millisecond, time.Second},
	}
	for _, tt := range tests {
		for range 50 {
			if d := p.Delay(tt.attempt); d < tt.min || d > tt.max {
				t.Fatalf("Delay(%d) = %s, want in [%s, %s]", tt.attempt, d, tt.min, tt.max)
			}
		}
	}
}
