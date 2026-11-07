// TASK-55: board drop positions and the default row ordering.
import { describe, expect, it } from 'vitest';
import { dropPosition, orderTasks } from './board';

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
