package dispatch

import (
"golang.org/x/time/rate"
)

// newLimiter creates a token-bucket rate limiter for per-DPU Apply/Delete ops.
func newLimiter(opsPerSec float64) *rate.Limiter {
return rate.NewLimiter(rate.Limit(opsPerSec), int(opsPerSec))
}