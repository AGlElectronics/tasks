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

type Droppable = Positioned & { status: { id: string }; version: number };

/**
 * dropPatch is the PATCH body for dropping `dragged` into the column of
 * `statusId` above `beforeId` (null = at the end). `tasks` is the whole
 * board. Dropping into another column also changes the status.
 */
export function dropPatch<T extends Droppable>(
  tasks: T[],
  dragged: T,
  statusId: string,
  beforeId: string | null
): { position: number; version: number; status_id?: string } {
  const col = tasks.filter((t) => t.status.id === statusId && t.id !== dragged.id);
  const body: { position: number; version: number; status_id?: string } = {
    position: dropPosition(col, beforeId),
    version: dragged.version,
  };
  if (statusId !== dragged.status.id) body.status_id = statusId;
  return body;
}

export type OrderableTask = {
  priority: 'urgent' | 'high' | 'normal' | 'low' | null;
  due_at: string | null;
  position: number;
};

/** Sort orders a person can pick in the list view. */
export type SortKey = 'smart' | 'manual' | 'due' | 'priority' | 'title' | 'created' | 'updated';
export const SORTS: { key: SortKey; label: string }[] = [
  { key: 'smart', label: 'Priority, then due date' },
  { key: 'manual', label: 'Manual (board order)' },
  { key: 'due', label: 'Due date' },
  { key: 'priority', label: 'Priority' },
  { key: 'title', label: 'Title' },
  { key: 'created', label: 'Newest first' },
  { key: 'updated', label: 'Recently updated' },
];

type SortableTask = OrderableTask & { title: string; created_at: string; updated_at: string };

/** sortTasks orders rows by the chosen key; ties fall back to position. */
export function sortTasks<T extends SortableTask>(rows: T[], key: SortKey): T[] {
  if (key === 'smart') return orderTasks(rows);
  const byPos = (a: T, b: T) => a.position - b.position;
  const cmp: Record<Exclude<SortKey, 'smart'>, (a: T, b: T) => number> = {
    manual: byPos,
    due: (a, b) => (a.due_at ?? '9999').localeCompare(b.due_at ?? '9999') || byPos(a, b),
    priority: (a, b) =>
      (a.priority ? PRIORITY_RANK[a.priority] : 4) - (b.priority ? PRIORITY_RANK[b.priority] : 4) || byPos(a, b),
    title: (a, b) => a.title.localeCompare(b.title, undefined, { sensitivity: 'base' }) || byPos(a, b),
    created: (a, b) => b.created_at.localeCompare(a.created_at) || byPos(a, b),
    updated: (a, b) => b.updated_at.localeCompare(a.updated_at) || byPos(a, b),
  };
  return [...rows].sort(cmp[key]);
}

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
