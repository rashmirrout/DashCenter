package resilience

import (
"errors"
"testing"
"time"
)

var errDashd = errors.New("dashd unavailable")

func TestCBState_String(t *testing.T) {
tests := []struct {
s    CBState
want string
}{
{CBClosed, "closed"},
{CBOpen, "open"},
{CBHalfOpen, "half_open"},
{CBState(99), "unknown"},
}
for _, tt := range tests {
if got := tt.s.String(); got != tt.want {
t.Errorf("CBState(%d).String() = %q, want %q", tt.s, got, tt.want)
}
}
}

func TestCB_ClosedByDefault(t *testing.T) {
cb := NewCircuitBreaker(3, time.Second)
if cb.State() != CBClosed {
t.Errorf("state = %v, want Closed", cb.State())
}
if cb.Failures() != 0 {
t.Errorf("failures = %d, want 0", cb.Failures())
}
}

func TestCB_SuccessPassesThrough(t *testing.T) {
cb := NewCircuitBreaker(3, time.Second)

called := false
err := cb.Call(func() error {
called = true
return nil
})

if err != nil {
t.Errorf("Call returned error: %v", err)
}
if !called {
t.Error("fn was not called")
}
if cb.State() != CBClosed {
t.Errorf("state = %v, want Closed after success", cb.State())
}
}

func TestCB_FailureIncrementsCounter(t *testing.T) {
cb := NewCircuitBreaker(3, time.Second)

_ = cb.Call(func() error { return errDashd })

if cb.Failures() != 1 {
t.Errorf("failures = %d, want 1", cb.Failures())
}
if cb.State() != CBClosed {
t.Errorf("state = %v, want Closed (below threshold)", cb.State())
}
}

func TestCB_OpensAfterThreshold(t *testing.T) {
cb := NewCircuitBreaker(3, time.Second)

for i := 0; i < 3; i++ {
_ = cb.Call(func() error { return errDashd })
}

if cb.State() != CBOpen {
t.Errorf("state = %v, want Open after 3 failures", cb.State())
}
}

func TestCB_OpenRejectsRequests(t *testing.T) {
cb := NewCircuitBreaker(3, time.Second)

// Trip the circuit
for i := 0; i < 3; i++ {
_ = cb.Call(func() error { return errDashd })
}

// Next call should be rejected
err := cb.Call(func() error {
t.Error("fn should not be called when circuit is open")
return nil
})

if !errors.Is(err, ErrCircuitOpen) {
t.Errorf("err = %v, want ErrCircuitOpen", err)
}
}

func TestCB_TransitionsToHalfOpenAfterTimeout(t *testing.T) {
cb := NewCircuitBreaker(2, 50*time.Millisecond)

// Trip the circuit
_ = cb.Call(func() error { return errDashd })
_ = cb.Call(func() error { return errDashd })

if cb.State() != CBOpen {
t.Fatalf("state = %v, want Open", cb.State())
}

// Wait for reset timeout
time.Sleep(80 * time.Millisecond)

// Next call should go through (half-open)
called := false
err := cb.Call(func() error {
called = true
return nil
})

if err != nil {
t.Errorf("Call returned error: %v", err)
}
if !called {
t.Error("fn should be called in half-open state")
}
if cb.State() != CBClosed {
t.Errorf("state = %v, want Closed after successful half-open call", cb.State())
}
}

func TestCB_HalfOpenFailureReopens(t *testing.T) {
cb := NewCircuitBreaker(2, 50*time.Millisecond)

// Trip the circuit
_ = cb.Call(func() error { return errDashd })
_ = cb.Call(func() error { return errDashd })

// Wait for reset timeout
time.Sleep(80 * time.Millisecond)

// Half-open call fails → reopen
_ = cb.Call(func() error { return errDashd })

if cb.State() != CBOpen {
t.Errorf("state = %v, want Open after half-open failure", cb.State())
}
}

func TestCB_SuccessResetsCounter(t *testing.T) {
cb := NewCircuitBreaker(3, time.Second)

_ = cb.Call(func() error { return errDashd })
_ = cb.Call(func() error { return errDashd })

// Success resets
_ = cb.Call(func() error { return nil })

if cb.Failures() != 0 {
t.Errorf("failures = %d, want 0 after success", cb.Failures())
}
if cb.State() != CBClosed {
t.Errorf("state = %v, want Closed after success", cb.State())
}
}

func TestCB_Reset(t *testing.T) {
cb := NewCircuitBreaker(2, time.Second)

_ = cb.Call(func() error { return errDashd })
_ = cb.Call(func() error { return errDashd })

cb.Reset()

if cb.State() != CBClosed {
t.Errorf("state = %v, want Closed after Reset", cb.State())
}
if cb.Failures() != 0 {
t.Errorf("failures = %d, want 0 after Reset", cb.Failures())
}
}

func TestCB_FailureWindowReset(t *testing.T) {
cb := NewCircuitBreaker(3, 50*time.Millisecond)

// Two failures
_ = cb.Call(func() error { return errDashd })
_ = cb.Call(func() error { return errDashd })

// Wait for failure window to expire
time.Sleep(80 * time.Millisecond)

// This failure should start a new window (counter reset to 1)
_ = cb.Call(func() error { return errDashd })

if cb.Failures() != 1 {
t.Errorf("failures = %d, want 1 (window reset)", cb.Failures())
}
if cb.State() != CBClosed {
t.Errorf("state = %v, want Closed (only 1 failure in new window)", cb.State())
}
}

func TestCB_ReturnsOriginalError(t *testing.T) {
cb := NewCircuitBreaker(5, time.Second)

customErr := errors.New("custom dashd error")
err := cb.Call(func() error { return customErr })

if !errors.Is(err, customErr) {
t.Errorf("err = %v, want %v (original error should pass through)", err, customErr)
}
}