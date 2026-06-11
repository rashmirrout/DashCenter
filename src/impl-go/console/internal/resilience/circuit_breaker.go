// Package resilience provides production-grade fault tolerance
// primitives for the dashw BFF: circuit breaker and rate limiter.
package resilience

import (
"errors"
"sync"
"time"
)

// ErrCircuitOpen is returned when the circuit breaker is in OPEN state.
// Callers should serve stale cached data when they receive this error.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CBState represents the circuit breaker state.
type CBState int

const (
CBClosed   CBState = iota // Normal — requests pass through
CBOpen                    // Tripped — requests fail fast
CBHalfOpen                // Testing — one request allowed through
)

// String returns a human-readable state name.
func (s CBState) String() string {
switch s {
case CBClosed:
return "closed"
case CBOpen:
return "open"
case CBHalfOpen:
return "half_open"
default:
return "unknown"
}
}

// CircuitBreaker implements the circuit breaker pattern for dashd calls.
//
// State machine:
//
//	CLOSED → (threshold failures within window) → OPEN
//	OPEN → (after resetTimeout) → HALF_OPEN
//	HALF_OPEN → (success) → CLOSED
//	HALF_OPEN → (failure) → OPEN
type CircuitBreaker struct {
mu               sync.Mutex
state            CBState
failures         int
lastFailure      time.Time
failureThreshold int
failureWindow    time.Duration
resetTimeout     time.Duration
}

// NewCircuitBreaker creates a circuit breaker.
//
//   - failureThreshold: number of failures before opening (e.g., 5)
//   - resetTimeout: time in OPEN before trying HALF_OPEN (e.g., 30s)
func NewCircuitBreaker(failureThreshold int, resetTimeout time.Duration) *CircuitBreaker {
return &CircuitBreaker{
state:            CBClosed,
failureThreshold: failureThreshold,
failureWindow:    resetTimeout, // failures must occur within this window
resetTimeout:     resetTimeout,
}
}

// Call executes fn if the circuit is closed or half-open. Returns
// ErrCircuitOpen if the circuit is open (caller should serve stale cache).
//
// On success: resets failure count, transitions to CLOSED.
// On failure: increments failure count, may transition to OPEN.
func (cb *CircuitBreaker) Call(fn func() error) error {
cb.mu.Lock()

switch cb.state {
case CBOpen:
// Check if reset timeout has elapsed
if time.Since(cb.lastFailure) > cb.resetTimeout {
cb.state = CBHalfOpen
cb.mu.Unlock()
// Fall through to execute fn
} else {
cb.mu.Unlock()
return ErrCircuitOpen
}

case CBHalfOpen:
cb.mu.Unlock()
// Allow one request through

case CBClosed:
cb.mu.Unlock()
// Normal operation
}

// Execute the function
err := fn()

cb.mu.Lock()
defer cb.mu.Unlock()

if err != nil {
cb.recordFailure()
return err
}

// Success — reset to closed
cb.state = CBClosed
cb.failures = 0
return nil
}

// recordFailure increments the failure counter and may open the circuit.
// Must be called with mu held.
func (cb *CircuitBreaker) recordFailure() {
now := time.Now()

// If we were in HALF_OPEN, any failure immediately reopens
if cb.state == CBHalfOpen {
cb.state = CBOpen
cb.lastFailure = now
return
}

// If the last failure was outside the window, reset the counter
if !cb.lastFailure.IsZero() && now.Sub(cb.lastFailure) > cb.failureWindow {
cb.failures = 0
}

cb.failures++
cb.lastFailure = now

if cb.failures >= cb.failureThreshold {
cb.state = CBOpen
}
}

// State returns the current circuit breaker state (thread-safe).
func (cb *CircuitBreaker) State() CBState {
cb.mu.Lock()
defer cb.mu.Unlock()
return cb.state
}

// Failures returns the current failure count (thread-safe).
func (cb *CircuitBreaker) Failures() int {
cb.mu.Lock()
defer cb.mu.Unlock()
return cb.failures
}

// Reset forces the circuit breaker back to CLOSED state.
// Useful for testing or manual recovery.
func (cb *CircuitBreaker) Reset() {
cb.mu.Lock()
defer cb.mu.Unlock()
cb.state = CBClosed
cb.failures = 0
cb.lastFailure = time.Time{}
}