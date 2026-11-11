import { describe, expect, it } from 'vitest';
import { addDays, bar, monthGrid, monthRange, shiftMonth, span, tasksByDay, weekStart, workload } from './calendar';

describe('dates', () => {
  it('finds Monday and adds days across month ends', () => {
    expect(weekStart('2027-01-14')).toBe('2027-01-11'); // Thursday → Monday
    expect(weekStart('2027-01-11')).toBe('2027-01-11');
    expect(addDays('2027-02-28', 1)).toBe('2027-03-01');
  });

  it('builds a month of Monday-first weeks', () => {
    const grid = monthGrid(2027, 2); // Feb 2027 starts on a Monday
    expect(grid[0][0]).toBe('2027-02-01');
    expect(grid.at(-1)!.at(-1)).toBe('2027-02-28');
    expect(grid).toHaveLength(4);
    expect(monthGrid(2027, 3)[0][0]).toBe('2027-03-01');
    expect(monthGrid(2027, 5)[0][0]).toBe('2027-04-26');
  });

  it('moves between months across years', () => {
    expect(shiftMonth({ year: 2027, month: 1 }, -1)).toEqual({ year: 2026, month: 12 });
    expect(shiftMonth({ year: 2026, month: 12 }, 1)).toEqual({ year: 2027, month: 1 });
    expect(monthRange({ year: 2027, month: 2 })).toEqual({ from: '2027-02-01', to: '2027-02-28' });
  });
});

describe('placing tasks', () => {
  const t = (id: string, start_at: string | null, due_at: string | null) => ({ id, start_at, due_at });

  it('spans start to due, a single date is one day, and swaps reversed dates', () => {
    expect(span(t('a', '2027-01-02', '2027-01-04'))).toEqual({ from: '2027-01-02', to: '2027-01-04' });
    expect(span(t('b', null, '2027-01-04'))).toEqual({ from: '2027-01-04', to: '2027-01-04' });
    expect(span(t('c', '2027-01-09', '2027-01-04'))).toEqual({ from: '2027-01-04', to: '2027-01-09' });
    expect(span(t('d', null, null))).toBeNull();
  });

  it('puts multi-day tasks on each visible day only', () => {
    const days = tasksByDay([t('a', '2026-12-30', '2027-01-02'), t('b', null, '2027-01-05'), t('c', null, null)], '2027-01-01', '2027-01-31');
    expect([...days.keys()].sort()).toEqual(['2027-01-01', '2027-01-02', '2027-01-05']);
  });

  it('clips timeline bars to the window', () => {
    expect(bar(t('a', '2027-01-03', '2027-01-05'), '2027-01-01', 14)).toEqual({ offset: 2, length: 3 });
    expect(bar(t('a', '2026-12-20', '2027-01-02'), '2027-01-01', 14)).toEqual({ offset: 0, length: 2 });
    expect(bar(t('a', '2027-02-01', '2027-02-02'), '2027-01-01', 14)).toBeNull();
  });
});

describe('workload', () => {
  const task = (id: string, due: string, who: string[], est: number | null, done = false) => ({
    id,
    start_at: null,
    due_at: due,
    assignees: who.map((w) => ({ id: w, display_name: w.toUpperCase() })),
    estimate_minutes: est,
    done,
  });

  it('sums open tasks and estimates per person and week', () => {
    const rows = workload(
      [
        task('1', '2027-01-12', ['ann'], 60),
        task('2', '2027-01-14', ['ann', 'bo'], 30),
        task('3', '2027-01-20', [], null),
        task('4', '2027-01-13', ['bo'], 90, true),
      ],
      '2027-01-11',
      2
    );
    expect(rows.map((r) => r.name)).toEqual(['ANN', 'BO', 'Unassigned']);
    expect(rows[0].weeks[0]).toEqual({ tasks: 2, minutes: 90 });
    expect(rows[1].weeks[0]).toEqual({ tasks: 1, minutes: 30 });
    expect(rows[2].weeks[1]).toEqual({ tasks: 1, minutes: 0 });
  });
});
