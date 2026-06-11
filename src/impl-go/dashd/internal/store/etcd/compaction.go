// Watch + compaction recovery.
//
// Watch contract:
//   1. Take a snapshot of all keys under KeyPrefix at revision R.
//   2. Emit store.EventPut for every key in the snapshot.
//   3. Subscribe to etcd events with WithRev(R+1).
//   4. Translate each etcd event to store.DesiredEvent.
//
// On etcd compaction (the cluster GC'd revisions older than R+1 before
// we resumed):
//   1. Emit store.EventResync so the consumer knows it must drop its
//      cache and re-list.
//   2. Take a new snapshot at the cluster's current revision.
//   3. Resume watching from that revision.
//
// Compaction is rare in practice — etcd's default auto-compaction is
// 1h retention — but our recovery loop turns "rare" into "always
// handled". The compaction codepath is the same code as initial snapshot
// + watch, so the test for it is also the test for the snapshot path.

package etcd

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// watchSendTimeout caps how long broadcast() blocks before declaring the
// subscriber too slow. Matches the file backend's "send EventResync and
// drop the listener" pattern in spirit.
const watchSendTimeout = 100 * time.Millisecond

// Watch returns a channel that first receives an EventPut per existing
// key (the snapshot at the current etcd revision), then live mutations.
// The channel is closed when:
//   - ctx is cancelled by the caller, OR
//   - Close() is called on this store, OR
//   - the consumer is so slow that even EventResync cannot be delivered.
//
// Concurrent Watch callers are independent — each gets their own
// snapshot + etcd watch channel.
func (s *EtcdStore) Watch(ctx context.Context) (<-chan store.DesiredEvent, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}

	// Buffer matches file backend (64). Larger reduces back-pressure
	// but trades memory + masks slow subscribers; 64 is the same
	// generous-but-not-pathological value.
	out := make(chan store.DesiredEvent, 64)

	// Take the initial snapshot synchronously so the caller knows the
	// store was reachable before Watch returned. Errors here are
	// propagated to the caller.
	resp, err := s.cli.Get(ctx, s.keyPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	startRev := resp.Header.Revision

	// Background goroutine: emit the snapshot, then subscribe and
	// translate etcd events forever (or until ctx/closed fires).
	go s.runWatch(ctx, out, resp.Kvs, startRev)

	return out, nil
}

// runWatch is the watch-loop goroutine. It owns `out` and is the only
// site that ever sends or closes it.
func (s *EtcdStore) runWatch(ctx context.Context, out chan<- store.DesiredEvent, initialKvs []*mvccpb.KeyValue, startRev int64) {
	defer close(out)

	// Emit the initial snapshot.
	if !s.emitSnapshot(ctx, out, initialKvs) {
		// Send context cancelled (or closed/slow subscriber). Done.
		return
	}

	// Subscribe and stream from revision startRev+1.
	for {
		watchCh := s.cli.Watch(ctx, s.keyPrefix,
			clientv3.WithPrefix(),
			clientv3.WithRev(startRev+1),
			clientv3.WithPrevKV(),
		)

		if !s.consumeWatch(ctx, out, watchCh) {
			return // ctx/closed/slow
		}

		// consumeWatch returned without ctx cancel — that means the
		// watch failed compaction. Re-snapshot and resume.
		newRev, ok := s.resnapshot(ctx, out)
		if !ok {
			return
		}
		startRev = newRev
	}
}

// emitSnapshot sends an EventPut for each kv. Returns false if ctx is
// cancelled or the subscriber is so slow we have to abort.
func (s *EtcdStore) emitSnapshot(ctx context.Context, out chan<- store.DesiredEvent, kvs []*mvccpb.KeyValue) bool {
	for _, kv := range kvs {
		objKey, ok := s.parseEtcdKey(string(kv.Key))
		if !ok {
			continue
		}
		spec, err := decodeKV(objKey, kv)
		if err != nil {
			slog.Warn("etcdstore: snapshot decode failed", "key", string(kv.Key), "error", err)
			continue
		}
		if !s.send(ctx, out, store.DesiredEvent{Type: store.EventPut, Key: objKey, Spec: spec}) {
			return false
		}
	}
	return true
}

// consumeWatch drains the etcd watch channel and translates events.
// Returns false on terminal exit (ctx done, store closed, subscriber
// terminally slow). Returns true after a compaction event — caller
// must re-snapshot.
func (s *EtcdStore) consumeWatch(ctx context.Context, out chan<- store.DesiredEvent, watchCh clientv3.WatchChan) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-s.closed:
			return false
		case wr, ok := <-watchCh:
			if !ok {
				// etcd closed the channel — typically a clean Close
				// or session expiry; treat as a re-snapshot trigger
				// so we re-establish a fresh watch.
				return true
			}
			if err := wr.Err(); err != nil {
				if errors.Is(err, rpctypes.ErrCompacted) {
					slog.Info("etcdstore: watch compacted, resynchronising")
					// Signal the consumer.
					if !s.send(ctx, out, store.DesiredEvent{Type: store.EventResync}) {
						return false
					}
					return true
				}
				// Other errors — log and treat as a re-snapshot
				// trigger so we don't lose convergence.
				slog.Warn("etcdstore: watch error, resynchronising", "error", err)
				if !s.send(ctx, out, store.DesiredEvent{Type: store.EventResync}) {
					return false
				}
				return true
			}
			for _, ev := range wr.Events {
				if !s.translateAndSend(ctx, out, ev) {
					return false
				}
			}
		}
	}
}

// translateAndSend maps one etcd event to a store.DesiredEvent and
// sends it. Returns false on terminal exit.
func (s *EtcdStore) translateAndSend(ctx context.Context, out chan<- store.DesiredEvent, ev *clientv3.Event) bool {
	objKey, ok := s.parseEtcdKey(string(ev.Kv.Key))
	if !ok {
		// Foreign key under our prefix — ignore.
		return true
	}
	switch {
	case ev.Type == clientv3.EventTypePut:
		spec, err := decodeKV(objKey, ev.Kv)
		if err != nil {
			slog.Warn("etcdstore: PUT event decode failed", "key", string(ev.Kv.Key), "error", err)
			return true
		}
		return s.send(ctx, out, store.DesiredEvent{Type: store.EventPut, Key: objKey, Spec: spec})

	case ev.Type == clientv3.EventTypeDelete:
		return s.send(ctx, out, store.DesiredEvent{Type: store.EventDelete, Key: objKey})
	}
	return true
}

// resnapshot takes a fresh snapshot after compaction. Returns the new
// startRev for the next watch, and false if the operation aborted.
func (s *EtcdStore) resnapshot(ctx context.Context, out chan<- store.DesiredEvent) (int64, bool) {
	resp, err := s.cli.Get(ctx, s.keyPrefix, clientv3.WithPrefix())
	if err != nil {
		slog.Error("etcdstore: resnapshot failed; closing watch", "error", err)
		return 0, false
	}
	if !s.emitSnapshot(ctx, out, resp.Kvs) {
		return 0, false
	}
	return resp.Header.Revision, true
}

// send delivers an event with a short timeout. On timeout it sends
// EventResync (a cheaper signal) before giving up. Returns false on
// ctx done / store closed / subscriber that can't even receive
// EventResync.
func (s *EtcdStore) send(ctx context.Context, out chan<- store.DesiredEvent, ev store.DesiredEvent) bool {
	// Fast path: non-blocking send.
	select {
	case out <- ev:
		return true
	default:
	}

	// Slow path: timed send.
	timer := time.NewTimer(watchSendTimeout)
	defer timer.Stop()
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	case <-s.closed:
		return false
	case <-timer.C:
		// Subscriber too slow. Last-ditch: send EventResync. If even
		// that fails the channel is irrecoverable — close it.
		select {
		case out <- store.DesiredEvent{Type: store.EventResync}:
			slog.Warn("etcdstore: subscriber slow, sent EventResync in place of event", "event_type", ev.Type)
			return true
		default:
			slog.Warn("etcdstore: subscriber irrecoverably slow, dropping watch")
			return false
		}
	}
}
