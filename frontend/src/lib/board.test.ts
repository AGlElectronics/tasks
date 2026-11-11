// TASK-55: board drop positions and the default row ordering.
import { describe, expect, it } from 'vitest';
import { dropPatch, dropPosition, orderTasks, sortTasks } from './board';

const col = [
  { id: 'a', position: 1024 },
  { id: 'b', position: 2048 },
  { id: 'c', position: 3072 },
];

describe('dropPosition', () => {
  it('drops at 1024 in an empty column', () => {
    expect(dropPosition([], null)).toBe(1024);
  });

  it('drops after the last card when no target is given', () => {
    expect(dropPosition(col, null)).toBe(3072 + 1024);
  });

  it('drops before the first card at next - 512', () => {
    expect(dropPosition(col, 'a')).toBe(1024 - 512);
  });

  it('drops between two cards at their midpoint', () => {
    expect(dropPosition(col, 'b')).toBe((1024 + 2048) / 2);
    expect(dropPosition(col, 'c')).toBe((2048 + 3072) / 2);
  });

  it('keeps halving on repeated drops into the same gap', () => {
    const first = dropPosition(col, 'b'); // 1536
    const second = dropPosition([{ id: 'a', position: 1024 }, { id: 'x', position: first }, ...col.slice(1)], 'x');
    expect(second).toBe((1024 + first) / 2);
    expect(second).toBeGreaterThan(1024);
    expect(second).toBeLessThan(first);
  });

  it('falls back to the end when the target id is not in the column', () => {
    expect(dropPosition(col, 'missing')).toBe(3072 + 1024);
  });
});

describe('dropPatch', () => {
  const card = (id: string, status: string, position: number) => ({ id, status: { id: status }, position, version: 3 });
  const board = [card('a', 'todo', 1024), card('b', 'todo', 2048), card('c', 'doing', 1024)];

  it('changes the status when a card lands in another column', () => {
    expect(dropPatch(board, board[0], 'doing', null)).toEqual({ position: 2048, version: 3, status_id: 'doing' });
    expect(dropPatch(board, board[0], 'doing', 'c')).toEqual({ position: 512, version: 3, status_id: 'doing' });
  });

  it('keeps the status when reordering inside a column', () => {
    const body = dropPatch(board, board[1], 'todo', 'a');
    expect(body.status_id).toBeUndefined();
    expect(body.position).toBe(512);
  });

  it('ignores the dragged card itself when computing the end of its column', () => {
    expect(dropPatch(board, board[1], 'todo', null).position).toBe(1024 + 1024);
  });
});

describe('sortTasks', () => {
  const row = (id: string, extra: Partial<{ title: string; due_at: string | null; created_at: string; position: number }>) => ({
    id,
    priority: null,
    due_at: null,
    title: id,
    created_at: '2027-01-01T00:00:00Z',
    updated_at: '2027-01-01T00:00:00Z',
    position: 0,
    ...extra,
  });
  it('sorts by title case-insensitively and by due date with undated last', () => {
    const rows = [row('b', { title: 'banana', due_at: null }), row('a', { title: 'Apple', due_at: '2027-02-01' }), row('c', { title: 'cherry', due_at: '2027-01-01' })];
    expect(sortTasks(rows, 'title').map((r) => r.id)).toEqual(['a', 'b', 'c']);
    expect(sortTasks(rows, 'due').map((r) => r.id)).toEqual(['c', 'a', 'b']);
  });
  it('puts the newest first for created', () => {
    const rows = [row('old', { created_at: '2027-01-01T00:00:00Z' }), row('new', { created_at: '2027-03-01T00:00:00Z' })];
    expect(sortTasks(rows, 'created').map((r) => r.id)).toEqual(['new', 'old']);
  });
});

describe('orderTasks', () => {
  const t = (id: string, priority: 'urgent' | 'high' | 'normal' | 'low' | null, due: string | null, position: number) => ({
    id,
    priority,
    due_at: due,
    position,
  });

  it('sorts by priority first (urgent > high > normal > low > none)', () => {
    const rows = [t('none', null, null, 1), t('low', 'low', null, 2), t('urgent', 'urgent', null, 3), t('high', 'high', null, 4)];
    expect(orderTasks(rows).map((r) => r.id)).toEqual(['urgent', 'high', 'low', 'none']);
  });

  it('breaks priority ties by due date, missing dates last', () => {
    const rows = [t('late', 'high', '2027-05-01', 1), t('soon', 'high', '2027-01-01', 2), t('nodate', 'high', null, 3)];
    expect(orderTasks(rows).map((r) => r.id)).toEqual(['soon', 'late', 'nodate']);
  });

  it('breaks full ties by manual position and does not mutate the input', () => {
    const rows = [t('second', null, null, 2048), t('first', null, null, 1024)];
    const sorted = orderTasks(rows);
    expect(sorted.map((r) => r.id)).toEqual(['first', 'second']);
    expect(rows.map((r) => r.id)).toEqual(['second', 'first']);
  });
});
