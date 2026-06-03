// Package faults injects configurable failure modes into the simulator.
//
// Configured via the admin API:
//
//	POST /admin/faults  {"op":"Apply","mode":"error","count":1,"message":"injected"}
//	POST /admin/faults  {"op":"Get","mode":"delay","delay_ms":500,"count":3}
//	POST /admin/faults  {"op":"*","mode":"drop","count":1}            # all ops
//	DELETE /admin/faults                                              # clear all
//
// op values match DashApi RPC names: Apply, Delete, Get, List, Subscribe,
// GetCounters. "*" matches any.
package faults

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Mode is the failure behavior.
type Mode string

const (
	ModeError Mode = "error"
	ModeDelay Mode = "delay"
	ModeDrop  Mode = "drop"
)

// Spec is a single configured fault.
type Spec struct {
	Op      string `json:"op"`
	Mode    Mode   `json:"mode"`
	Count   int    `json:"count,omitempty"`
	DelayMs int    `json:"delay_ms,omitempty"`
	Message string `json:"message,omitempty"`
}

// Injector holds active fault specs.
type Injector struct {
	mu    sync.Mutex
	specs []*Spec
}

// New returns an empty Injector.
func New() *Injector { return &Injector{} }

// Add registers a fault spec.
func (i *Injector) Add(s Spec) error {
	if s.Op == "" {
		return errors.New("fault: op is required")
	}
	switch s.Mode {
	case ModeError, ModeDrop:
		if s.Message == "" {
			s.Message = "injected " + string(s.Mode)
		}
	case ModeDelay:
		if s.DelayMs <= 0 {
			return errors.New("fault: delay_ms must be > 0 for mode=delay")
		}
	default:
		return fmt.Errorf("fault: unknown mode %q", s.Mode)
	}
	if s.Count == 0 {
		s.Count = 1
	}
	i.mu.Lock()
	i.specs = append(i.specs, &s)
	i.mu.Unlock()
	return nil
}

// List returns a snapshot of current specs.
func (i *Injector) List() []Spec {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]Spec, 0, len(i.specs))
	for _, s := range i.specs {
		out = append(out, *s)
	}
	return out
}

// Clear removes every spec.
func (i *Injector) Clear() {
	i.mu.Lock()
	i.specs = nil
	i.mu.Unlock()
}

// Apply consumes a matching spec and either returns an error, sleeps, or
// returns nil to continue normally.
func (i *Injector) Apply(op string) error {
	i.mu.Lock()
	var match *Spec
	for idx, s := range i.specs {
		if s.Op == op || s.Op == "*" {
			match = s
			if s.Count > 0 {
				s.Count--
				if s.Count == 0 {
					i.specs = append(i.specs[:idx], i.specs[idx+1:]...)
				}
			}
			break
		}
	}
	i.mu.Unlock()

	if match == nil {
		return nil
	}
	switch match.Mode {
	case ModeDelay:
		time.Sleep(time.Duration(match.DelayMs) * time.Millisecond)
		return nil
	case ModeError, ModeDrop:
		return errors.New(match.Message)
	}
	return nil
}
