// Pure date logic for the calendar, timeline and workload views. Dates are
// "YYYY-MM-DD" strings (task dates have no time of day) handled in UTC so
// no time zone or daylight-saving shift moves a task to another day.

export type Dated = {
  id: string;
  start_at: string | null;
  due_at: string | null;
};

const DAY = 86_400_000;

export function toDate(s: string): Date {
  const [y, m, d] = s.split('-').map(Number);
  return new Date(Date.UTC(y, m - 1, d));
}

export function fmt(d: Date): string {
  return d.toISOString().slice(0, 10);
}

export function addDays(s: string, n: number): string {
  return fmt(new Date(toDate(s).getTime() + n * DAY));
}

export function daysBetween(a: string, b: string): number {
  return Math.round((toDate(b).getTime() - toDate(a).getTime()) / DAY);
}

/** Monday of the week containing s. */
export function weekStart(s: string): string {
  const dow = (toDate(s).getUTCDay() + 6) % 7; // Monday = 0
  return addDays(s, -dow);
}

/** The weeks (Monday to Sunday) covering a month; month is 1-12. */
export function monthGrid(year: number, month: number): string[][] {
  const first = fmt(new Date(Date.UTC(year, month - 1, 1)));
  const last = fmt(new Date(Date.UTC(year, month, 0)));
  const weeks: string[][] = [];
  for (let day = weekStart(first); day <= last; day = addDays(day, 7)) {
    weeks.push(Array.from({ length: 7 }, (_, i) => addDays(day, i)));
  }
  return weeks;
}

export type Month = { year: number; month: number }; // month 1-12

export function thisMonth(): Month {
  const d = new Date();
  return { year: d.getFullYear(), month: d.getMonth() + 1 };
}

export function shiftMonth(m: Month, by: number): Month {
  const i = m.year * 12 + (m.month - 1) + by;
  return { year: Math.floor(i / 12), month: (i % 12) + 1 };
}

/** First and last day shown for a month (whole weeks). */
export function monthRange(m: Month): { from: string; to: string } {
  const grid = monthGrid(m.year, m.month);
  return { from: grid[0][0], to: grid[grid.length - 1][6] };
}

/** A task's span: start to due; a single date makes a one-day span. */
export function span(t: Dated): { from: string; to: string } | null {
  const from = t.start_at ?? t.due_at;
  const to = t.due_at ?? t.start_at;
  if (!from || !to) return null;
  return from <= to ? { from, to } : { from: to, to: from };
}

/**
 * tasksByDay places each task on every day of its span that falls in
 * [from, to] (calendar cells). Tasks without dates are left out.
 */
export function tasksByDay<T extends Dated>(tasks: T[], from: string, to: string): Map<string, T[]> {
  const out = new Map<string, T[]>();
  for (const t of tasks) {
    const s = span(t);
    if (!s || s.to < from || s.from > to) continue;
    const start = s.from < from ? from : s.from;
    const end = s.to > to ? to : s.to;
    for (let d = start; d <= end; d = addDays(d, 1)) {
      const list = out.get(d) ?? [];
      list.push(t);
      out.set(d, list);
    }
  }
  return out;
}

/** Timeline bar of a task inside [from, from + days): offset and length in days. */
export function bar(t: Dated, from: string, days: number): { offset: number; length: number } | null {
  const s = span(t);
  if (!s) return null;
  const end = addDays(from, days - 1);
  if (s.to < from || s.from > end) return null;
  const a = s.from < from ? from : s.from;
  const b = s.to > end ? end : s.to;
  return { offset: daysBetween(from, a), length: daysBetween(a, b) + 1 };
}

type Workable = Dated & {
  assignees: { id: string; display_name: string }[];
  estimate_minutes: number | null;
  done: boolean;
};

export type WorkloadCell = { tasks: number; minutes: number };

/**
 * workload sums open tasks per assignee per week (by due date, or start
 * date when there is no due date) over `weeks` weeks from `fromWeek`.
 * Unassigned work goes to the "" row.
 */
export function workload(tasks: Workable[], fromWeek: string, weeks: number) {
  const people = new Map<string, string>();
  const cells = new Map<string, WorkloadCell[]>();
  const row = (id: string) => {
    let r = cells.get(id);
    if (!r) {
      r = Array.from({ length: weeks }, () => ({ tasks: 0, minutes: 0 }));
      cells.set(id, r);
    }
    return r;
  };
  for (const t of tasks) {
    if (t.done) continue;
    const date = t.due_at ?? t.start_at;
    if (!date) continue;
    const w = Math.floor(daysBetween(fromWeek, weekStart(date)) / 7);
    if (w < 0 || w >= weeks) continue;
    const who = t.assignees.length ? t.assignees : [{ id: '', display_name: 'Unassigned' }];
    for (const p of who) {
      people.set(p.id, p.display_name);
      const c = row(p.id)[w];
      c.tasks += 1;
      // Shared work counts fully for each assignee: nobody knows the split.
      c.minutes += t.estimate_minutes ?? 0;
    }
  }
  return [...cells.entries()]
    .map(([id, weeksRow]) => ({ id, name: people.get(id) ?? '', weeks: weeksRow }))
    .sort((a, b) => (a.id === '' ? 1 : b.id === '' ? -1 : a.name.localeCompare(b.name)));
}
