// Package file implements a file-system-backed DesiredStore.
//
// Disk layout:
//
//	<state_dir>/<namespace>/<kind>/<name>.json
//
// Each JSON file is an envelope:
//
//	{"namespace":"…","kind":"…","name":"…","generation":N,"updated_at":"…","spec":{…}}
package file

import (
"context"
	"encoding/json"
	"fmt"
"log/slog"
"os"
"path/filepath"
"sort"
"strings"
"sync"
"time"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// envelope is the on-disk JSON schema.
type envelope struct {
Namespace string          `json:"namespace"`
Kind      string          `json:"kind"`
Name      string          `json:"name"`
Generation int64          `json:"generation"`
UpdatedAt  time.Time      `json:"updated_at"`
Spec       json.RawMessage `json:"spec"`
}

// FileStore implements store.DesiredStore using the local filesystem.
type FileStore struct {
dir    string
mu     sync.RWMutex
index  map[store.ObjectKey]*store.StoredSpec
subs   map[chan store.DesiredEvent]struct{}
closed bool
}

// Open loads an existing state directory (creating it if needed) and
// rebuilds the in-memory index from all .json files found.
func Open(dir string) (*FileStore, error) {
if err := os.MkdirAll(dir, 0o750); err != nil {
return nil, fmt.Errorf("filestore: mkdir %s: %w", dir, err)
}
fs := &FileStore{
dir:   dir,
index: make(map[store.ObjectKey]*store.StoredSpec),
subs:  make(map[chan store.DesiredEvent]struct{}),
}
if err := fs.loadIndex(); err != nil {
return nil, err
}
return fs, nil
}

// loadIndex walks the state directory and populates the in-memory index.
func (s *FileStore) loadIndex() error {
return filepath.WalkDir(s.dir, func(path string, d os.DirEntry, err error) error {
if err != nil {
return err
}
if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
return nil
}
data, err := os.ReadFile(path)
if err != nil {
return fmt.Errorf("filestore: read %s: %w", path, err)
}
var env envelope
if err := json.Unmarshal(data, &env); err != nil {
return fmt.Errorf("filestore: parse %s: %w", path, err)
}
key := store.ObjectKey{
Namespace: env.Namespace,
Kind:      env.Kind,
Name:      env.Name,
}
s.index[key] = &store.StoredSpec{
Key:        key,
Generation: env.Generation,
Data:       env.Spec,
UpdatedAt:  env.UpdatedAt,
}
return nil
})
}

// Put creates or replaces a spec. Returns the new generation.
func (s *FileStore) Put(ctx context.Context, key store.ObjectKey, spec any, expectedGeneration int64) (int64, error) {
s.mu.Lock()
defer s.mu.Unlock()

if s.closed {
return 0, store.ErrClosed
}

current, exists := s.index[key]

// Generation check.
if expectedGeneration > 0 {
if !exists {
return 0, fmt.Errorf("%w: key %s does not exist (expected gen %d)",
store.ErrGenerationMismatch, key, expectedGeneration)
}
if current.Generation != expectedGeneration {
return 0, fmt.Errorf("%w: key %s has gen %d, expected %d",
store.ErrGenerationMismatch, key, current.Generation, expectedGeneration)
}
}

// Serialize spec.
raw, err := json.Marshal(spec)
if err != nil {
return 0, fmt.Errorf("filestore: marshal spec for %s: %w", key, err)
}

var newGen int64 = 1
if exists {
newGen = current.Generation + 1
}

now := time.Now().UTC()
env := envelope{
Namespace:  key.Namespace,
Kind:       key.Kind,
Name:       key.Name,
Generation: newGen,
UpdatedAt:  now,
Spec:       json.RawMessage(raw),
}

encoded, err := json.MarshalIndent(env, "", "  ")
if err != nil {
return 0, fmt.Errorf("filestore: encode envelope for %s: %w", key, err)
}

// Atomic write: tmp + rename.
finalPath := s.specPath(key)
if err := os.MkdirAll(filepath.Dir(finalPath), 0o750); err != nil {
return 0, fmt.Errorf("filestore: mkdir for %s: %w", key, err)
}
tmpPath := finalPath + ".tmp"
if err := os.WriteFile(tmpPath, encoded, 0o640); err != nil {
return 0, fmt.Errorf("filestore: write tmp for %s: %w", key, err)
}
if err := os.Rename(tmpPath, finalPath); err != nil {
_ = os.Remove(tmpPath)
return 0, fmt.Errorf("filestore: rename for %s: %w", key, err)
}

// Update index.
stored := &store.StoredSpec{
Key:        key,
Generation: newGen,
Data:       raw,
UpdatedAt:  now,
}
s.index[key] = stored

// Broadcast.
s.broadcast(store.DesiredEvent{
Type: store.EventPut,
Key:  key,
Spec: stored,
})

return newGen, nil
}

// Delete removes a spec. Returns ErrNotFound if absent.
func (s *FileStore) Delete(ctx context.Context, key store.ObjectKey) error {
s.mu.Lock()
defer s.mu.Unlock()

if s.closed {
return store.ErrClosed
}

if _, exists := s.index[key]; !exists {
return store.ErrNotFound
}

path := s.specPath(key)
if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
slog.Warn("filestore: remove file failed (proceeding with index delete)",
"path", path, "error", err)
}

delete(s.index, key)

s.broadcast(store.DesiredEvent{
Type: store.EventDelete,
Key:  key,
})

return nil
}

// Get returns a stored spec. Returns ErrNotFound if absent.
func (s *FileStore) Get(ctx context.Context, key store.ObjectKey) (*store.StoredSpec, error) {
s.mu.RLock()
defer s.mu.RUnlock()

if s.closed {
return nil, store.ErrClosed
}

sp, ok := s.index[key]
if !ok {
return nil, store.ErrNotFound
}
// Return a copy.
cp := *sp
return &cp, nil
}

// List returns all specs for (namespace, kind), sorted by Name.
func (s *FileStore) List(ctx context.Context, namespace, kind string) ([]*store.StoredSpec, error) {
s.mu.RLock()
defer s.mu.RUnlock()

if s.closed {
return nil, store.ErrClosed
}

var result []*store.StoredSpec
for k, sp := range s.index {
if k.Namespace == namespace && k.Kind == kind {
cp := *sp
result = append(result, &cp)
}
}
sort.Slice(result, func(i, j int) bool {
return result[i].Key.Name < result[j].Key.Name
})
if result == nil {
result = []*store.StoredSpec{}
}
return result, nil
}

// Watch returns a channel that first receives snapshots of all current specs
// as EventPut events, then live mutations.
func (s *FileStore) Watch(ctx context.Context) (<-chan store.DesiredEvent, error) {
s.mu.Lock()
defer s.mu.Unlock()

if s.closed {
return nil, store.ErrClosed
}

ch := make(chan store.DesiredEvent, 64)

// Snapshot: send current state.
for _, sp := range s.index {
ev := store.DesiredEvent{Type: store.EventPut, Key: sp.Key, Spec: sp}
select {
case ch <- ev:
default:
close(ch)
return nil, fmt.Errorf("filestore: subscriber too slow during snapshot")
}
}

s.subs[ch] = struct{}{}

// Cleanup goroutine: on ctx cancel, remove subscriber and close ch.
go func() {
<-ctx.Done()
s.mu.Lock()
defer s.mu.Unlock()
if _, ok := s.subs[ch]; ok {
delete(s.subs, ch)
close(ch)
}
}()

return ch, nil
}

// Close releases resources. Idempotent.
func (s *FileStore) Close() error {
s.mu.Lock()
defer s.mu.Unlock()

if s.closed {
return nil
}
s.closed = true
for ch := range s.subs {
close(ch)
delete(s.subs, ch)
}
s.index = nil
return nil
}

// broadcast sends an event to all subscribers. Called under write lock.
func (s *FileStore) broadcast(ev store.DesiredEvent) {
for ch := range s.subs {
select {
case ch <- ev:
default:
// Back-pressure: send EventResync sentinel so consumer knows to re-list.
select {
case ch <- store.DesiredEvent{Type: store.EventResync}:
default:
// Channel completely stuck — consumer will catch up via 30s tick.
}
slog.Warn("filestore: subscriber too slow, sent EventResync")
}
}
}

// specPath returns the filesystem path for a given key.
func (s *FileStore) specPath(key store.ObjectKey) string {
return filepath.Join(s.dir, key.Namespace, key.Kind, key.Name+".json")
}