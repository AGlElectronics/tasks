// Live updates over Server-Sent Events (GET /api/v1/stream). The stream
// only says what changed (a list, a space, my inbox); this module turns that
// into query invalidations, so views refetch through the normal API.
//
// EventSource cannot send an Authorization header, so the stream is read
// with fetch. While connected, background polling drops to every two
// minutes; without a stream it stays at 15 seconds.

import type { QueryClient } from '@tanstack/react-query';
import { apiUrl, authHeaders } from '@/api/client';

export type StreamEvent = {
  type: 'list' | 'space' | 'inbox' | 'resync';
  list_id?: string;
  space_id?: string;
};

export const POLL_CONNECTED = 120_000;
export const POLL_FALLBACK = 15_000;

/** Query keys to refetch for one event. */
export function keysFor(ev: StreamEvent): unknown[][] {
  switch (ev.type) {
    case 'list':
      return [['tasks', ev.list_id], ['list', ev.list_id], ['task'], ['my-work'], ['spaces'], ['calendar']];
    case 'space':
      return [['spaces'], ['space', ev.space_id], ['list'], ['tags', ev.space_id], ['fields', ev.space_id]];
    case 'inbox':
      return [['inbox'], ['inbox-unread'], ['my-work']];
    default:
      return [[]];
  }
}

/** Splits an SSE byte stream into events; keeps the unfinished tail. */
export function parseSSE(buffer: string): { events: { event: string; data: string }[]; rest: string } {
  const events: { event: string; data: string }[] = [];
  const blocks = buffer.replace(/\r\n/g, '\n').split('\n\n');
  const rest = blocks.pop() ?? '';
  for (const block of blocks) {
    let event = 'message';
    const data: string[] = [];
    for (const line of block.split('\n')) {
      if (line.startsWith(':')) continue;
      if (line.startsWith('event:')) event = line.slice(6).trim();
      else if (line.startsWith('data:')) data.push(line.slice(5).trimStart());
    }
    if (data.length) events.push({ event, data: data.join('\n') });
  }
  return { events, rest };
}

/** Connects and keeps reconnecting until the returned stop() is called. */
export function startRealtime(qc: QueryClient): () => void {
  let stopped = false;
  let controller: AbortController | null = null;
  let delay = 2_000;
  let pending = new Map<string, unknown[]>();
  let flush: ReturnType<typeof setTimeout> | null = null;

  const setPolling = (ms: number) =>
    qc.setDefaultOptions({ ...qc.getDefaultOptions(), queries: { ...qc.getDefaultOptions().queries, refetchInterval: ms } });

  // Many changes arrive in bursts (a bulk edit, an automation chain); refetch
  // once per burst.
  const invalidate = (ev: StreamEvent) => {
    if (ev.type === 'resync') {
      void qc.invalidateQueries();
      return;
    }
    for (const key of keysFor(ev)) pending.set(JSON.stringify(key), key);
    if (flush) return;
    flush = setTimeout(() => {
      const keys = [...pending.values()];
      pending = new Map();
      flush = null;
      for (const queryKey of keys) void qc.invalidateQueries({ queryKey });
    }, 300);
  };

  const connect = async () => {
    while (!stopped) {
      controller = new AbortController();
      try {
        const res = await fetch(apiUrl('/api/v1/stream'), {
          headers: { Accept: 'text/event-stream', ...(await authHeaders()) },
          credentials: 'include',
          signal: controller.signal,
        });
        if (res.status === 503 || res.status === 404) return; // live updates are off
        if (!res.ok || !res.body) throw new Error(`stream ${res.status}`);
        setPolling(POLL_CONNECTED);
        delay = 2_000;
        const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
        let buffer = '';
        let first = true;
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buffer += value;
          const parsed = parseSSE(buffer);
          buffer = parsed.rest;
          for (const e of parsed.events) {
            if (e.event === 'ready') {
              // Catch up on whatever changed while disconnected.
              if (!first) void qc.invalidateQueries();
              first = false;
            } else if (e.event === 'change') {
              try {
                invalidate(JSON.parse(e.data) as StreamEvent);
              } catch {
                // ignore a malformed event
              }
            }
          }
        }
      } catch {
        if (stopped) return;
      }
      setPolling(POLL_FALLBACK);
      if (stopped) return;
      await new Promise((r) => setTimeout(r, delay));
      delay = Math.min(delay * 2, 60_000);
      void qc.invalidateQueries();
    }
  };
  void connect();
  return () => {
    stopped = true;
    controller?.abort();
    if (flush) clearTimeout(flush);
    setPolling(POLL_FALLBACK);
  };
}
