package cache

import (
"sort"
"testing"
"time"
)

func TestStatus_String(t *testing.T) {
tests := []struct {
status Status
want   string
}{
{Miss, "miss"},
{Hit, "hit"},
{Stale, "stale"},
{Status(99), "miss"}, // unknown defaults to miss
}
for _, tt := range tests {
if got := tt.status.String(); got != tt.want {
t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
}
}
}

func TestCache_SetAndGet_Hit(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

c.Set("key1", []byte("value1"), 5*time.Second, 30*time.Second)

data, status, age := c.Get("key1")
if status != Hit {
t.Errorf("status = %v, want Hit", status)
}
if string(data) != "value1" {
t.Errorf("data = %q, want %q", data, "value1")
}
if age < 0 || age > time.Second {
t.Errorf("age = %v, expected near zero", age)
}
}

func TestCache_Get_Miss(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

data, status, age := c.Get("nonexistent")
if status != Miss {
t.Errorf("status = %v, want Miss", status)
}
if data != nil {
t.Errorf("data = %v, want nil", data)
}
if age != 0 {
t.Errorf("age = %v, want 0", age)
}
}

func TestCache_Get_Stale(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

// Set with 50ms TTL and 5s stale window
c.Set("key1", []byte("stale-value"), 50*time.Millisecond, 5*time.Second)

// Wait for TTL to expire but within stale window
time.Sleep(80 * time.Millisecond)

data, status, _ := c.Get("key1")
if status != Stale {
t.Errorf("status = %v, want Stale", status)
}
if string(data) != "stale-value" {
t.Errorf("data = %q, want %q", data, "stale-value")
}
}

func TestCache_Get_FullyExpired(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

// Set with 10ms TTL and 20ms stale window
c.Set("key1", []byte("expired"), 10*time.Millisecond, 20*time.Millisecond)

// Wait for both TTL and stale window to expire
time.Sleep(50 * time.Millisecond)

data, status, _ := c.Get("key1")
if status != Miss {
t.Errorf("status = %v, want Miss (fully expired)", status)
}
if data != nil {
t.Errorf("data = %v, want nil", data)
}
}

func TestCache_Set_Overwrite(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

c.Set("key1", []byte("v1"), 5*time.Second, 5*time.Second)
c.Set("key1", []byte("v2"), 5*time.Second, 5*time.Second)

data, status, _ := c.Get("key1")
if status != Hit {
t.Errorf("status = %v, want Hit", status)
}
if string(data) != "v2" {
t.Errorf("data = %q, want %q", data, "v2")
}
}

func TestCache_Invalidate(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

c.Set("key1", []byte("v1"), 5*time.Second, 5*time.Second)
c.Set("key2", []byte("v2"), 5*time.Second, 5*time.Second)

c.Invalidate("key1")

_, status1, _ := c.Get("key1")
if status1 != Miss {
t.Errorf("key1 status = %v, want Miss after invalidation", status1)
}

_, status2, _ := c.Get("key2")
if status2 != Hit {
t.Errorf("key2 status = %v, want Hit (not invalidated)", status2)
}
}

func TestCache_InvalidatePattern(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

c.Set("vnet/detail:prod", []byte("v1"), 5*time.Second, 5*time.Second)
c.Set("vnet/canvas:prod", []byte("v2"), 5*time.Second, 5*time.Second)
c.Set("vnet/detail:staging", []byte("v3"), 5*time.Second, 5*time.Second)
c.Set("fleet/summary", []byte("v4"), 5*time.Second, 5*time.Second)

c.InvalidatePattern("vnet/")

if c.Len() != 1 {
t.Errorf("Len = %d, want 1 (only fleet/summary should remain)", c.Len())
}

_, status, _ := c.Get("fleet/summary")
if status != Hit {
t.Error("fleet/summary should still be cached")
}

_, status, _ = c.Get("vnet/detail:prod")
if status != Miss {
t.Error("vnet/detail:prod should be invalidated")
}
}

func TestCache_InvalidatePattern_Wildcard(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

c.Set("key1", []byte("v1"), 5*time.Second, 5*time.Second)
c.Set("key2", []byte("v2"), 5*time.Second, 5*time.Second)

c.InvalidatePattern("*")

if c.Len() != 0 {
t.Errorf("Len = %d, want 0 after wildcard invalidation", c.Len())
}
}

func TestCache_InvalidateKeys(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

c.Set("a", []byte("1"), 5*time.Second, 5*time.Second)
c.Set("b", []byte("2"), 5*time.Second, 5*time.Second)
c.Set("c", []byte("3"), 5*time.Second, 5*time.Second)

c.InvalidateKeys([]string{"a", "c"})

if c.Len() != 1 {
t.Errorf("Len = %d, want 1", c.Len())
}

_, status, _ := c.Get("b")
if status != Hit {
t.Error("key 'b' should still be cached")
}
}

func TestCache_Flush(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

c.Set("key1", []byte("v1"), 5*time.Second, 5*time.Second)
c.Set("key2", []byte("v2"), 5*time.Second, 5*time.Second)
c.Set("key3", []byte("v3"), 5*time.Second, 5*time.Second)

c.Flush()

if c.Len() != 0 {
t.Errorf("Len = %d, want 0 after Flush", c.Len())
}
}

func TestCache_Len(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

if c.Len() != 0 {
t.Errorf("Len = %d, want 0 for empty cache", c.Len())
}

c.Set("a", []byte("1"), 5*time.Second, 5*time.Second)
c.Set("b", []byte("2"), 5*time.Second, 5*time.Second)

if c.Len() != 2 {
t.Errorf("Len = %d, want 2", c.Len())
}
}

func TestCache_Keys(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

c.Set("beta", []byte("2"), 5*time.Second, 5*time.Second)
c.Set("alpha", []byte("1"), 5*time.Second, 5*time.Second)

keys := c.Keys()
sort.Strings(keys)

if len(keys) != 2 {
t.Fatalf("Keys len = %d, want 2", len(keys))
}
if keys[0] != "alpha" || keys[1] != "beta" {
t.Errorf("Keys = %v, want [alpha, beta]", keys)
}
}

func TestCache_BackgroundCleanup(t *testing.T) {
// Use very short cleanup interval
c := New(50 * time.Millisecond)
defer c.Stop()

// Set entry that expires quickly (10ms TTL + 10ms stale = 20ms total)
c.Set("ephemeral", []byte("gone"), 10*time.Millisecond, 10*time.Millisecond)
c.Set("persistent", []byte("stays"), 5*time.Second, 5*time.Second)

// Wait for cleanup to run
time.Sleep(100 * time.Millisecond)

if c.Len() != 1 {
t.Errorf("Len = %d, want 1 (ephemeral should be cleaned up)", c.Len())
}

_, status, _ := c.Get("persistent")
if status != Hit {
t.Error("persistent entry should still be cached")
}
}

func TestCache_Stop(t *testing.T) {
c := New(time.Millisecond) // very fast cleanup

c.Set("key", []byte("val"), 5*time.Second, 5*time.Second)

// Stop should not panic and should terminate the goroutine
c.Stop()

// Cache should still be readable after stop (just no cleanup)
data, status, _ := c.Get("key")
if status != Hit {
t.Error("should still be able to read after Stop")
}
if string(data) != "val" {
t.Errorf("data = %q, want %q", data, "val")
}
}

func TestCache_ConcurrentAccess(t *testing.T) {
c := New(time.Minute)
defer c.Stop()

// Run concurrent reads and writes to verify thread safety
done := make(chan struct{})

// Writer goroutine
go func() {
for i := 0; i < 1000; i++ {
c.Set("key", []byte("value"), time.Second, time.Second)
}
done <- struct{}{}
}()

// Reader goroutine
go func() {
for i := 0; i < 1000; i++ {
c.Get("key")
}
done <- struct{}{}
}()

// Invalidator goroutine
go func() {
for i := 0; i < 100; i++ {
c.InvalidatePattern("k")
}
done <- struct{}{}
}()

// Wait for all goroutines
<-done
<-done
<-done
// If we got here without a race condition panic, the test passes
}