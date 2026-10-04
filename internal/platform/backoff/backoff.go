// Package backoff computes jittered exponential retry delays.
package backoff

import (
	"math/rand/v2"
	"time"
)

// Policy is an exponential backoff with jitter: attempt n waits a random duration
// in [d/2, d] where d = min(Base*2^(n-1), Max). Jitter keeps many clients that
// failed together from retrying together.
type Policy struct {
	Base time.Duration
	Max  time.Duration
}

// Delay returns the wait before retry number attempt (1-based).
func (p Policy) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := p.Base
	for i := 1; i < attempt && d < p.Max; i++ {
		d *= 2
	}
	if d > p.Max {
		d = p.Max
	}
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + rand.N(half+1)
}
