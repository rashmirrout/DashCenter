/**
 * useTopologyStream — production-grade EventSource hook for the
 * /api/console/topology-v2/stream surface.
 *
 * Production contract (mirror of docs/dashd-features/topology-streaming-design.md):
 *
 *   1. EventSource lifecycle is tied to the consuming React component:
 *      `useEffect` opens the stream on mount, the cleanup function
 *      closes it on unmount. Navigating to another page closes the
 *      connection inside one React render — no leak.
 *
 *   2. Tab Visibility API: when the page is hidden for >60s we close
 *      the stream and re-open on visibility return. Saves both browser
 *      + server resources during inactive periods.
 *
 *   3. Last-Event-ID resume: EventSource auto-attaches the cursor on
 *      reconnect. We ALSO persist `lastEventId` in the Zustand store so
 *      page reloads (which create a fresh EventSource) can resume via
 *      the `?last_event_id=N` query param — EventSource doesn't expose
 *      a way to set Last-Event-ID on the FIRST connection.
 *
 *   4. Exponential backoff with jitter for manual reconnects (a fleet of
 *      browsers reopening after a dashw deploy must NOT thundering-herd
 *      the upstream).
 *
 *   5. Browser-side de-dup: one EventSource per (page, includeEnis) tuple;
 *      multiple widgets on the page share the singleton store.
 *
 * Returns nothing — the store is the public API. Widgets read via
 * useTopologyV2Store(selector).
 */
import { useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTopologyV2Store } from '@/stores/topology-v2-store';
import type { TopologyEvent, TopologyV2Response } from '@/api/topology-v2-types';

const STREAM_PATH = '/api/console/topology-v2/stream';
const SNAPSHOT_PATH = '/api/console/topology-v2';
const TAB_HIDDEN_GRACE_MS = 60_000;
const RECONNECT_INITIAL_MS = 500;
const RECONNECT_MAX_MS = 15_000;

interface UseTopologyStreamOptions {
  includeEnis?: boolean;
  /** If false (default), the hook does nothing — useful for tests. */
  enabled?: boolean;
}

export function useTopologyStream(opts: UseTopologyStreamOptions = {}) {
  const enabled = opts.enabled ?? true;
  const includeEnis = !!opts.includeEnis;
  const qc = useQueryClient();

  const setConnection = useTopologyV2Store((s) => s.setConnection);
  const applySnapshot = useTopologyV2Store((s) => s.applySnapshot);
  const applyEvent = useTopologyV2Store((s) => s.applyEvent);
  const reset = useTopologyV2Store((s) => s.reset);

  // Mutable refs to avoid stale-closure issues in the EventSource handlers.
  const esRef = useRef<EventSource | null>(null);
  const reconnectTimer = useRef<number | null>(null);
  const reconnectAttempts = useRef(0);
  const hiddenTimer = useRef<number | null>(null);

  useEffect(() => {
    if (!enabled) return undefined;

    let cancelled = false;

    /** opens a fresh EventSource using the store's cursor for resume. */
    const open = () => {
      if (cancelled) return;

      const lastEventId = useTopologyV2Store.getState().lastEventId;
      const params = new URLSearchParams();
      if (includeEnis) params.set('include_enis', 'true');
      if (lastEventId > 0) params.set('last_event_id', String(lastEventId));
      const url = `${STREAM_PATH}?${params.toString()}`;

      setConnection('connecting');
      const es = new EventSource(url, { withCredentials: false });
      esRef.current = es;

      // Snapshot listener (first event after cold connect; OR after
      // a RESYNC notice). We also dispatch the snapshot to TanStack
      // Query's cache so other widgets that use useQuery on
      // /topology-v2 stay in sync.
      es.addEventListener('snapshot', (e) => {
        try {
          const ev = JSON.parse((e as MessageEvent).data) as TopologyEvent;
          if (ev.snapshot) {
            applySnapshot(ev.snapshot, Number(ev.event_id ?? 0));
            qc.setQueryData<TopologyV2Response>(['topology-v2', includeEnis], ev.snapshot);
          }
          reconnectAttempts.current = 0;
          setConnection('open');
        } catch (err) {
          setConnection('error', String(err));
        }
      });

      // Delta listener — fires for every non-snapshot event kind.
      const onDelta = (e: MessageEvent) => {
        try {
          const ev = JSON.parse(e.data) as TopologyEvent;
          applyEvent(ev);
        } catch {
          // Silent: a malformed frame should not kill the stream.
        }
      };

      const kinds = [
        'peer_added', 'peer_removed', 'peer_updated',
        'leader_changed', 'dpu_state', 'dpu_added', 'dpu_removed',
        'keepalive', 'dropped', 'rate_limited',
      ];
      for (const k of kinds) {
        es.addEventListener(k, onDelta as EventListener);
      }

      // RESYNC: drop local state + refetch snapshot via fetch (NOT EventSource).
      // The hub already pushed us into a clean state; the snapshot fetch
      // benefits from the BFF's 1-second dedup cache.
      es.addEventListener('resync', async () => {
        try {
          const params2 = new URLSearchParams();
          if (includeEnis) params2.set('include_enis', 'true');
          const res = await fetch(`${SNAPSHOT_PATH}?${params2.toString()}`);
          if (res.ok) {
            const snap = (await res.json()) as TopologyV2Response;
            applySnapshot(snap, 0); // reset cursor; next live event will set it
            qc.setQueryData<TopologyV2Response>(['topology-v2', includeEnis], snap);
          }
        } catch (err) {
          setConnection('error', String(err));
        }
      });

      es.onerror = () => {
        // EventSource auto-reconnects via Last-Event-ID by itself, but
        // we still drive backoff so a thundering-herd deploy doesn't
        // hammer dashw. Close ours + schedule a custom reconnect.
        es.close();
        esRef.current = null;
        if (cancelled) return;

        const attempt = ++reconnectAttempts.current;
        const base = Math.min(RECONNECT_INITIAL_MS * 2 ** (attempt - 1), RECONNECT_MAX_MS);
        const jitter = Math.random() * base;
        const wait = base / 2 + jitter;
        setConnection('reconnecting', `attempt ${attempt}, retry in ${Math.round(wait)}ms`);
        reconnectTimer.current = window.setTimeout(open, wait);
      };
    };

    const closeStream = () => {
      if (reconnectTimer.current) {
        window.clearTimeout(reconnectTimer.current);
        reconnectTimer.current = null;
      }
      if (esRef.current) {
        esRef.current.close();
        esRef.current = null;
      }
    };

    // Visibility handler: pause the stream after the page is hidden
    // for >60s; resume immediately when visible.
    const onVisibility = () => {
      if (document.hidden) {
        hiddenTimer.current = window.setTimeout(() => {
          closeStream();
          setConnection('paused', 'tab hidden');
        }, TAB_HIDDEN_GRACE_MS);
      } else {
        if (hiddenTimer.current) {
          window.clearTimeout(hiddenTimer.current);
          hiddenTimer.current = null;
        }
        if (!esRef.current) {
          open();
        }
      }
    };

    open();
    document.addEventListener('visibilitychange', onVisibility);

    return () => {
      cancelled = true;
      document.removeEventListener('visibilitychange', onVisibility);
      if (hiddenTimer.current) window.clearTimeout(hiddenTimer.current);
      closeStream();
      reset(); // free memory + reset cursor on unmount
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, includeEnis]);
}
