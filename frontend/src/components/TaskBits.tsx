import { Link } from '@tanstack/react-router';
import { CalendarDays, Flag, ListTree, MessageSquare } from 'lucide-react';
import type { Person, Priority, Status, Task } from '@/api/tasks';
import { dueLabel } from '@/lib/format';
import { cn } from '@/lib/utils';

const PRIORITY_COLORS: Record<Priority, string> = {
  urgent: 'text-red-500',
  high: 'text-amber-500',
  normal: 'text-sky-500',
  low: 'text-muted-foreground',
};

export function PriorityFlag({ priority, withLabel }: { priority: Priority | null; withLabel?: boolean }) {
  if (!priority) return null;
  return (
    <span className={cn('inline-flex items-center gap-1 text-xs font-medium', PRIORITY_COLORS[priority])} title={`Priority: ${priority}`}>
      <Flag className="w-3.5 h-3.5" fill="currentColor" />
      {withLabel ? <span className="capitalize">{priority}</span> : null}
    </span>
  );
}

export function StatusPill({ status }: { status: Pick<Status, 'name' | 'color'> }) {
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-pill px-2 py-0.5 text-xs font-semibold"
      style={{ backgroundColor: `${status.color}22`, color: status.color }}
    >
      <span className="w-2 h-2 rounded-full" style={{ backgroundColor: status.color }} />
      {status.name}
    </span>
  );
}

export function StatusSelect({
  statuses,
  value,
  onChange,
  className,
}: {
  statuses: Status[];
  value: string;
  onChange: (id: string) => void;
  className?: string;
}) {
  const current = statuses.find((s) => s.id === value);
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onClick={(e) => e.stopPropagation()}
      className={cn('rounded-pill border border-border bg-background px-2 py-0.5 text-xs font-semibold', className)}
      style={current ? { color: current.color } : undefined}
    >
      {statuses.map((s) => (
        <option key={s.id} value={s.id}>
          {s.name}
        </option>
      ))}
    </select>
  );
}

function initials(name: string) {
  const parts = name.trim().split(/\s+/);
  return ((parts[0]?.[0] ?? '') + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toUpperCase() || '?';
}

const AVATAR_COLORS = ['#6366f1', '#0ea5e9', '#22c55e', '#f59e0b', '#ec4899', '#14b8a6', '#a855f7', '#ef4444'];

function colorFor(id: string) {
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return AVATAR_COLORS[h % AVATAR_COLORS.length];
}

export function Avatar({ person, size = 'sm' }: { person: Person; size?: 'sm' | 'md' }) {
  return (
    <span
      title={person.display_name}
      className={cn(
        'inline-flex items-center justify-center rounded-full font-bold text-white ring-2 ring-card',
        size === 'sm' ? 'w-6 h-6 text-[10px]' : 'w-8 h-8 text-xs'
      )}
      style={{ backgroundColor: colorFor(person.id) }}
    >
      {initials(person.display_name)}
    </span>
  );
}

export function Avatars({ people }: { people: Person[] }) {
  if (people.length === 0) return null;
  return (
    <span className="inline-flex -space-x-1.5">
      {people.slice(0, 3).map((p) => (
        <Avatar key={p.id} person={p} />
      ))}
      {people.length > 3 ? (
        <span className="inline-flex items-center justify-center w-6 h-6 rounded-full bg-secondary text-[10px] font-bold ring-2 ring-card">
          +{people.length - 3}
        </span>
      ) : null}
    </span>
  );
}

export function DueBadge({ date, done }: { date: string | null; done: boolean }) {
  if (!date) return null;
  const { text, tone } = dueLabel(date, done);
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 text-xs',
        tone === 'overdue' && 'text-destructive font-semibold',
        tone === 'today' && 'text-amber-600 font-semibold',
        tone === 'normal' && 'text-muted-foreground'
      )}
    >
      <CalendarDays className="w-3.5 h-3.5" />
      {text}
    </span>
  );
}

/** Small counters shown under a task title (subtasks, comments). */
export function TaskMeta({ task }: { task: Task }) {
  return (
    <>
      {task.subtask_count > 0 ? (
        <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
          <ListTree className="w-3.5 h-3.5" />
          {task.subtask_done}/{task.subtask_count}
        </span>
      ) : null}
      {task.comment_count > 0 ? (
        <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
          <MessageSquare className="w-3.5 h-3.5" />
          {task.comment_count}
        </span>
      ) : null}
    </>
  );
}

export function TaskLink({ task, className }: { task: Task; className?: string }) {
  return (
    <Link
      to="/t/$taskKey"
      params={{ taskKey: task.key }}
      className={cn(
        'font-medium hover:text-primary hover:underline underline-offset-2',
        task.status.category === 'done' && 'line-through text-muted-foreground',
        className
      )}
    >
      {task.title}
    </Link>
  );
}
