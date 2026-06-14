// useCounterStream — EventSource owner for /api/console/counters/stream.
//
// Pattern mirrors useTopologyStream exactly:
//   * one EventSource per useEffect mount; closes on unmount.
//   * tab-visibility pause + auto-resume.
//   * Last-Event-ID resume via query param on connect.
//   * exponential backoff with jitter on errors.
//
// Returns nothing — read state via useCountersStore selectors.

import { useEffect, useRef } from 'react';
import { useCountersStore, type CounterEvent } from '@/stores/counters-store';

const STREAM_PATH = '/api/console/counters/stream';
const TAB_HIDDEN_GRACE_MS = 60_000;
const RECONNECT_INITIAL_MS = 500;
const RECONNECT_MAX_MS = 15_000;

export interface UseCounterStreamOptions {
  /** If set, subscribe only to these DPU ids; empty = all. */
  dpuIds?: string[];
  /** If false (default true), the hook is a no-op (useful for tests). */
  enabled?: boolean;
}

export function useCounterStream(opts: UseCounterStreamOptions = {}) {
  const enabled = opts.enabled ?? true;
  const dpuIds = opts.dpuIds ?? [];
  const setConnection = useCountersStore((s) => s.setConnection);
  const apply = useCountersStore((s) => s.apply);

  const esRef = useRef<EventSource | null>(null);
  const reconnectTimer = useRef<number | null>(null);
  const reconnectAttempts = useRef(0);
  const hiddenTimer = useRef<number | null>(null);

  useEffect(() => {
    if (!enabled) return undefined;
    let cancelled = false;

    const open = () => {
      if (cancelled) return;
      const lastEventId = useCountersStore.getState().lastEventId;
      const params = new URLSearchParams();
      for (const id of dpuIds) if (id) params.append('dpu', id);
      if (lastEventId > 0) params.set('last_event_id', String(lastEventId));
      const url = params.toString().length > 0
        ? `${STREAM_PATH}?${params.toString()}`
        : STREAM_PATH;

      setConnection('connecting');
      const es = new EventSource(url, { withCredentials: false });
      esRef.current = es;

      const dispatch = (raw: string) => {
        try {
          const ev = JSON.parse(raw) as CounterEvent;
          apply(ev);
          reconnectAttempts.current = 0;
          setConnection('open');
        } catch (err) {
          setConnection('error', String(err));
        }
      };

      es.addEventListener('snapshot', (e) => dispatch((e as MessageEvent).data));
      es.addEventListener('report', (e) => dispatch((e as MessageEvent).data));
      es.addEventListener('keepalive', (e) => dispatch((e as MessageEvent).data));
      es.addEventListener('dropped', (e) => dispatch((e as MessageEvent).data));
      es.addEventListener('rate_limited', (e) => dispatch((e as MessageEvent).data));
      es.addEventListener('resync', (e) => dispatch((e as MessageEvent).data));

      es.onerror = () => {
        if (cancelled) return;
        setConnection('reconnecting');
        es.close();
        esRef.current = null;
        const attempt = reconnectAttempts.current++;
        const base = Math.min(RECONNECT_INITIAL_MS * 2 ** attempt, RECONNECT_MAX_MS);
        const jitter = Math.random() * base * 0.2;
        reconnectTimer.current = window.setTimeout(open, base + jitter);
      };
    };

    const close = (state: 'paused' | 'idle' = 'idle') => {
      if (reconnectTimer.current != null) {
        window.clearTimeout(reconnectTimer.current);
        reconnectTimer.current = null;
      }
      if (esRef.current) {
        esRef.current.close();
        esRef.current = null;
      }
      setConnection(state);
    };

    const onVisibilityChange = () => {
      if (document.hidden) {
        if (hiddenTimer.current != null) window.clearTimeout(hiddenTimer.current);
        hiddenTimer.current = window.setTimeout(() => close('paused'), TAB_HIDDEN_GRACE_MS);
      } else {
        if (hiddenTimer.current != null) {
          window.clearTimeout(hiddenTimer.current);
          hiddenTimer.current = null;
        }
        if (!esRef.current) open();
      }
    };

    open();
    document.addEventListener('visibilitychange', onVisibilityChange);

    return () => {
      cancelled = true;
      document.removeEventListener('visibilitychange', onVisibilityChange);
      if (hiddenTimer.current != null) window.clearTimeout(hiddenTimer.current);
      close('idle');
    };
    // dpuIds is intentionally not in the dep array — to change DPU
    // filter, unmount + remount the consumer (or call the hook with a
    // different key). Object identity diffing on arrays in useEffect
    // deps causes thrash.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled]);
}
