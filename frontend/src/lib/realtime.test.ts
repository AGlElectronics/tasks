import { describe, expect, it } from 'vitest';
import { keysFor, parseSSE } from './realtime';

describe('parseSSE', () => {
  it('splits complete events and keeps the tail', () => {
    const { events, rest } = parseSSE(
      'retry: 5000\nevent: ready\ndata: {}\n\n: ping\n\nevent: change\ndata: {"type":"inbox"}\n\nevent: chan'
    );
    expect(events).toEqual([
      { event: 'ready', data: '{}' },
      { event: 'change', data: '{"type":"inbox"}' },
    ]);
    expect(rest).toBe('event: chan');
  });
  it('handles CRLF and multi-line data', () => {
    const { events } = parseSSE('event: change\r\ndata: a\r\ndata: b\r\n\r\n');
    expect(events).toEqual([{ event: 'change', data: 'a\nb' }]);
  });
});

describe('keysFor', () => {
  it('refetches the list, its tasks and task pages for a list change', () => {
    const keys = keysFor({ type: 'list', list_id: 'L1', space_id: 'S1' });
    expect(keys).toContainEqual(['tasks', 'L1']);
    expect(keys).toContainEqual(['list', 'L1']);
    expect(keys).toContainEqual(['task']);
  });
  it('refetches the inbox for inbox changes only', () => {
    expect(keysFor({ type: 'inbox' })).toEqual([['inbox'], ['inbox-unread'], ['my-work']]);
  });
});
