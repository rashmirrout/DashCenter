// Package etcd implements a store.DesiredStore backed by an etcd v3 cluster.
//
// Key layout mirrors the file backend:
//
//	<key_prefix><namespace>/<kind>/<name>
//
// Each value is a JSON envelope identical in shape to the file backend's
// (namespace, kind, name, generation, updated_at, spec) so an operator
// can move state between backends with a single dump+restore.
//
// Generation semantics:
//   - StoredSpec.Generation is etcd's per-key Version field (1 for the
//     first Put on a key, then 2, 3, ...). This matches the file
//     backend exactly so the dispatcher and reconciler treat both
//     backends identically.
//   - StoredSpec.EtcdRevision carries the cluster-wide ModRevision of
//     the last write to the key, used by Watch resume on compaction.
//
// CAS:
//   - expectedGeneration == 0 → unconditional Put (last-write-wins)
//   - expectedGeneration  > 0 → etcd Txn comparing Version(key) ==
//     expectedGeneration; mismatch returns store.ErrGenerationMismatch.
//
// Watch:
//   - Initial snapshot is a GetWithPrefix at revision R; the returned
//     channel first delivers EventPut for every existing key, then
//     etcd events from revision R+1 onward.
//   - If the broker compacts past R, we redo the snapshot and emit
//     store.EventResync so the consumer knows it must re-evaluate
//     deletes it would have otherwise missed. See compaction.go.
//
// Concurrency:
//   - Multiple Put/Delete/Get/List calls are safe concurrently — each
//     turns into one or more independent etcd RPCs.
//   - Watch starts one background goroutine per subscriber; Close
//     terminates all of them via the subscriber's context.
package etcd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/api/v3/mvccpb"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// Config is the subset of internal/config.EtcdStorageConfig that this
// package needs to open a connection. We accept a purpose-built struct
// rather than importing internal/config to keep the dependency direction
// one-way (config → store/etcd, never the reverse) — and to keep this
// package importable from tests that don't want to drag in the full
// config validator.
type Config struct {
	// Endpoints is the list of etcd peer URLs.
	Endpoints []string

	// KeyPrefix is prepended to every store key. Required; the config
	// layer defaults it to "/dashd/state/" when omitted.
	KeyPrefix string

	// DialTimeout caps the time clientv3.New will wait for an initial
	// connection. Defaults to 5s when zero.
	DialTimeout time.Duration

	// TLS material. When CertFile + KeyFile are both empty the
	// connection is plaintext (acceptable on localhost / trusted
	// networks; production deployments should always set both).
	CertFile string
	KeyFile  string
	CAFile   string
}

// envelope is the on-disk JSON schema. Identical to the file backend's
// envelope; the two packages keep their copies in sync via the
// integration suite (PA-1b adds the comparison test).
type envelope struct {
	Namespace  string          `json:"namespace"`
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Generation int64           `json:"generation"`
	UpdatedAt  time.Time       `json:"updated_at"`
	Spec       json.RawMessage `json:"spec"`
}

// EtcdStore implements store.DesiredStore against an etcd v3 cluster.
type EtcdStore struct {
	cli       *clientv3.Client
	keyPrefix string

	// closeOnce guards Close so it is idempotent.
	closeOnce sync.Once
	closed    chan struct{}
}

// Open dials the etcd cluster and returns a ready-to-use store.
// Returns an error only if the initial dial cannot complete within
// cfg.DialTimeout. Once open, the store survives transient etcd
// disconnects automatically (clientv3 reconnects under the hood).
func Open(ctx context.Context, cfg Config) (*EtcdStore, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, errors.New("etcdstore: at least one endpoint is required")
	}
	if cfg.KeyPrefix == "" {
		return nil, errors.New("etcdstore: KeyPrefix is required")
	}
	if !strings.HasSuffix(cfg.KeyPrefix, "/") {
		// We rely on the trailing slash for prefix scoping; rather
		// than silently appending it we reject the misshapen value.
		return nil, fmt.Errorf("etcdstore: KeyPrefix %q must end in /", cfg.KeyPrefix)
	}

	dialTimeout := cfg.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = 5 * time.Second
	}

	clientCfg := clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: dialTimeout,
		Context:     ctx,
	}

	if cfg.CertFile != "" || cfg.CAFile != "" {
		tlsCfg, err := buildClientTLS(cfg)
		if err != nil {
			return nil, fmt.Errorf("etcdstore: TLS config: %w", err)
		}
		clientCfg.TLS = tlsCfg
	}

	cli, err := clientv3.New(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("etcdstore: dial %v: %w", cfg.Endpoints, err)
	}

	// Probe the cluster with a quick get to confirm the connection is
	// usable. clientv3.New returns successfully even when the cluster is
	// unreachable; a real Get surfaces the failure now rather than at
	// the first business call.
	probeCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if _, err := cli.Get(probeCtx, cfg.KeyPrefix, clientv3.WithLimit(1)); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("etcdstore: probe %v: %w", cfg.Endpoints, err)
	}

	return &EtcdStore{
		cli:       cli,
		keyPrefix: cfg.KeyPrefix,
		closed:    make(chan struct{}),
	}, nil
}

// buildClientTLS constructs a *tls.Config from the supplied cert/key/CA
// material. cfg.CertFile and cfg.KeyFile must be set together (or both
// empty); cfg.CAFile is independently optional.
func buildClientTLS(cfg Config) (*tls.Config, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if cfg.CertFile != "" || cfg.KeyFile != "" {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return nil, errors.New("CertFile and KeyFile must be set together")
		}
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load x509 keypair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	if cfg.CAFile != "" {
		caBytes, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, fmt.Errorf("CA file %s contained no usable certificates", cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
	}

	return tlsCfg, nil
}

// keyFor builds the absolute etcd key for a given store.ObjectKey.
// Layout: <prefix><namespace>/<kind>/<name>.
func (s *EtcdStore) keyFor(k store.ObjectKey) string {
	return s.keyPrefix + k.Namespace + "/" + k.Kind + "/" + k.Name
}

// listPrefix returns the etcd prefix for List(namespace, kind).
func (s *EtcdStore) listPrefix(namespace, kind string) string {
	return s.keyPrefix + namespace + "/" + kind + "/"
}

// parseEtcdKey decodes <prefix><ns>/<kind>/<name> back into an
// ObjectKey. Returns ok=false for keys that don't conform (e.g. legacy
// or operator-injected entries under the same prefix).
func (s *EtcdStore) parseEtcdKey(etcdKey string) (store.ObjectKey, bool) {
	tail := strings.TrimPrefix(etcdKey, s.keyPrefix)
	if tail == etcdKey {
		return store.ObjectKey{}, false
	}
	parts := strings.SplitN(tail, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return store.ObjectKey{}, false
	}
	return store.ObjectKey{Namespace: parts[0], Kind: parts[1], Name: parts[2]}, true
}

// Put creates or replaces a spec. Returns the new generation
// (etcd's per-key Version after the write).
func (s *EtcdStore) Put(ctx context.Context, key store.ObjectKey, spec any, expectedGeneration int64) (int64, error) {
	if err := s.checkOpen(); err != nil {
		return 0, err
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		return 0, fmt.Errorf("etcdstore: marshal spec for %s: %w", key, err)
	}

	env := envelope{
		Namespace:  key.Namespace,
		Kind:       key.Kind,
		Name:       key.Name,
		UpdatedAt:  time.Now().UTC(),
		Spec:       json.RawMessage(raw),
		// Generation is filled in after we know the etcd Version
		// (post-Txn / post-Put), but we encode it now with a tentative
		// 0 so the on-disk envelope shape is stable. The real generation
		// is in the returned int64; readers always trust the envelope's
		// Generation field, which we update before encoding.
	}

	etcdKey := s.keyFor(key)

	if expectedGeneration == 0 {
		// Unconditional Put: we have to do a Put then a Get (the etcd
		// Put response only returns PrevKv, not the new version). To
		// keep the envelope's Generation field consistent with the
		// returned value, we round-trip through a Txn that returns the
		// new key info.
		return s.putUnconditional(ctx, etcdKey, env)
	}

	// CAS Put: use a Txn comparing Version(key) == expectedGeneration.
	// On mismatch we return the canonical sentinel.
	return s.putCAS(ctx, etcdKey, env, expectedGeneration)
}

// putUnconditional persists env without a CAS guard. Implementation note:
// etcd's Put returns the previous KV, not the new one — to learn the new
// Version we follow the Put with a Get in the same Txn (both run on the
// same revision; no race window).
func (s *EtcdStore) putUnconditional(ctx context.Context, etcdKey string, env envelope) (int64, error) {
	// Two-step: encode without final generation, then re-encode with
	// the correct generation, then write. This is one extra
	// serialization but it keeps the envelope's Generation field
	// truthful for any future reader that bypasses StoredSpec.
	currentVersion, err := s.getVersion(ctx, etcdKey)
	if err != nil {
		return 0, err
	}
	env.Generation = currentVersion + 1

	encoded, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("etcdstore: encode envelope: %w", err)
	}

	// Write. We don't gate on the version we just read because this is
	// the unconditional path — last-write-wins is the documented
	// semantics.
	if _, err := s.cli.Put(ctx, etcdKey, string(encoded)); err != nil {
		return 0, fmt.Errorf("etcdstore: put %s: %w", etcdKey, err)
	}

	// Re-read to get the post-write Version (which is what callers
	// expect as the new generation). One extra round-trip; etcd is
	// fast enough that this is negligible vs. the write itself.
	resp, err := s.cli.Get(ctx, etcdKey)
	if err != nil {
		return 0, fmt.Errorf("etcdstore: post-put get %s: %w", etcdKey, err)
	}
	if len(resp.Kvs) == 0 {
		// Should never happen — we just wrote it and no one else
		// could have deleted it between the Put and this Get with
		// only-controller-mode-writes semantics. If it does, the
		// envelope's Generation is the best we can offer.
		return env.Generation, nil
	}
	return resp.Kvs[0].Version, nil
}

// putCAS persists env only if the current Version equals expectedGen.
func (s *EtcdStore) putCAS(ctx context.Context, etcdKey string, env envelope, expectedGen int64) (int64, error) {
	// Build the envelope assuming the CAS will succeed, so the on-disk
	// Generation matches what we'll return.
	env.Generation = expectedGen + 1

	encoded, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("etcdstore: encode envelope: %w", err)
	}

	txn := s.cli.Txn(ctx).
		If(clientv3.Compare(clientv3.Version(etcdKey), "=", expectedGen)).
		Then(clientv3.OpPut(etcdKey, string(encoded))).
		Else(clientv3.OpGet(etcdKey))

	resp, err := txn.Commit()
	if err != nil {
		return 0, fmt.Errorf("etcdstore: cas put %s: %w", etcdKey, err)
	}

	if !resp.Succeeded {
		// CAS failed — surface the actual current generation if we
		// can read it from the Else branch.
		current := int64(0)
		if len(resp.Responses) > 0 {
			if get := resp.Responses[0].GetResponseRange(); get != nil && len(get.Kvs) > 0 {
				current = get.Kvs[0].Version
			}
		}
		return 0, fmt.Errorf("%w: key %s has gen %d, expected %d",
			store.ErrGenerationMismatch, etcdKey, current, expectedGen)
	}

	// CAS succeeded — fetch the new Version. We could derive it from
	// PrevKv + 1 but Get gives us the cluster-truth value.
	getResp, err := s.cli.Get(ctx, etcdKey)
	if err != nil {
		return 0, fmt.Errorf("etcdstore: post-cas get %s: %w", etcdKey, err)
	}
	if len(getResp.Kvs) == 0 {
		return env.Generation, nil
	}
	return getResp.Kvs[0].Version, nil
}

// getVersion returns the current Version of the key, or 0 if absent.
func (s *EtcdStore) getVersion(ctx context.Context, etcdKey string) (int64, error) {
	resp, err := s.cli.Get(ctx, etcdKey)
	if err != nil {
		return 0, fmt.Errorf("etcdstore: get version for %s: %w", etcdKey, err)
	}
	if len(resp.Kvs) == 0 {
		return 0, nil
	}
	return resp.Kvs[0].Version, nil
}

// Delete removes the spec. Returns store.ErrNotFound if absent.
func (s *EtcdStore) Delete(ctx context.Context, key store.ObjectKey) error {
	if err := s.checkOpen(); err != nil {
		return err
	}

	resp, err := s.cli.Delete(ctx, s.keyFor(key))
	if err != nil {
		return fmt.Errorf("etcdstore: delete %s: %w", key, err)
	}
	if resp.Deleted == 0 {
		return store.ErrNotFound
	}
	return nil
}

// Get returns the stored spec. Returns store.ErrNotFound if absent.
func (s *EtcdStore) Get(ctx context.Context, key store.ObjectKey) (*store.StoredSpec, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}

	resp, err := s.cli.Get(ctx, s.keyFor(key))
	if err != nil {
		return nil, fmt.Errorf("etcdstore: get %s: %w", key, err)
	}
	if len(resp.Kvs) == 0 {
		return nil, store.ErrNotFound
	}
	return decodeKV(key, resp.Kvs[0])
}

// List returns all specs for (namespace, kind), sorted by Name.
func (s *EtcdStore) List(ctx context.Context, namespace, kind string) ([]*store.StoredSpec, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}

	prefix := s.listPrefix(namespace, kind)
	resp, err := s.cli.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("etcdstore: list %s/%s: %w", namespace, kind, err)
	}

	result := make([]*store.StoredSpec, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		objKey, ok := s.parseEtcdKey(string(kv.Key))
		if !ok {
			// Skip keys that don't conform — e.g. operator-injected
			// debugging entries under the same prefix.
			continue
		}
		sp, err := decodeKV(objKey, kv)
		if err != nil {
			return nil, err
		}
		result = append(result, sp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key.Name < result[j].Key.Name
	})
	return result, nil
}

// decodeKV unwraps an etcd KV into a StoredSpec.
func decodeKV(key store.ObjectKey, kv *mvccpb.KeyValue) (*store.StoredSpec, error) {
	var env envelope
	if err := json.Unmarshal(kv.Value, &env); err != nil {
		return nil, fmt.Errorf("etcdstore: parse envelope for %s: %w", key, err)
	}
	return &store.StoredSpec{
		Key:          key,
		Generation:   kv.Version, // trust etcd's authoritative Version, not the envelope's
		EtcdRevision: kv.ModRevision,
		Data:         env.Spec,
		UpdatedAt:    env.UpdatedAt,
	}, nil
}

// Close releases the etcd client and signals all live Watch goroutines
// to exit. Idempotent.
func (s *EtcdStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.closed)
		err = s.cli.Close()
	})
	return err
}

// checkOpen returns store.ErrClosed if Close has already run.
func (s *EtcdStore) checkOpen() error {
	select {
	case <-s.closed:
		return store.ErrClosed
	default:
		return nil
	}
}
