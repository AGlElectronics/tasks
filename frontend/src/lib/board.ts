// Pure board logic, extracted from ListPage so it can be unit-tested
// (TASK-55): where a dragged card lands and how rows are ordered.

type Positioned = { id: string; position: number };

/**
 * dropPosition computes the position value for a card dropped into a column.
 * `col` is the target column WITHOUT the dragged card, in board order;
 * `beforeId` is the card it was dropped above, or null for the end.
 * New gaps are halves/1024-steps; the backend renumbers when gaps vanish.
 */
export function dropPosition(col: Positioned[], beforeId: string | null): number {
  const idx = beforeId ? col.findIndex((t) => t.id === beforeId) : -1;
  if (idx === -1) {
    return col.length ? col[col.length - 1].position + 1024 : 1024;
  }
  const next = col[idx].position;
  const prev = idx > 0 ? col[idx - 1].position : next - 1024;
  return (prev + next) / 2;
}

export type OrderableTask = {
  priority: 'urgent' | 'high' | 'normal' | 'low' | null;
  due_at: string | null;
  position: number;
};

// Clear visual order inside a status group (part of the Vikunja feedback):
// urgent things first, then by due date, then manual position.
const PRIORITY_RANK: Record<string, number> = { urgent: 0, high: 1, normal: 2, low: 3 };

export function orderTasks<T extends OrderableTask>(rows: T[]): T[] {
  return [...rows].sort((a, b) => {
    const pa = a.priority ? PRIORITY_RANK[a.priority] : 4;
    const pb = b.priority ? PRIORITY_RANK[b.priority] : 4;
    if (pa !== pb) return pa - pb;
    if (a.due_at !== b.due_at) {
      if (!a.due_at) return 1;
      if (!b.due_at) return -1;
      return a.due_at.localeCompare(b.due_at);
    }
    return a.position - b.position;
  });
}
