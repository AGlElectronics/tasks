import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Lock, Trash2, X } from 'lucide-react';
import { ApiError } from '@/api/client';
import {
  addDependency,
  deleteTimeEntry,
  logTime,
  removeDependency,
  type DepRef,
  type Recurrence,
  type TaskDetail,
} from '@/api/tasks';
import { Button } from '@/components/ui/Button';
import { Label } from '@/components/ui/Input';
import { formatMinutes, parseDuration } from '@/lib/format';
import { StatusPill } from '@/components/TaskBits';

function errText(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback;
}

function DepRow({ d, onRemove }: { d: DepRef; onRemove?: () => void }) {
  return (
    <li className="flex items-center gap-2 py-1 text-sm">
      <span className="font-mono text-xs text-muted-foreground w-16 shrink-0">{d.key}</span>
      <Link to="/t/$taskKey" params={{ taskKey: d.key }} className={d.done ? 'line-through text-muted-foreground truncate' : 'truncate hover:underline'}>
        {d.title}
      </Link>
      <StatusPill status={d.status} />
      {onRemove ? (
        <button type="button" onClick={onRemove} className="ml-auto text-muted-foreground hover:text-destructive" title="Remove dependency">
          <X className="w-3.5 h-3.5" />
        </button>
      ) : null}
    </li>
  );
}

/** "Waiting for" and "Blocking" on the task page. */
export function DependenciesSection({ task, refresh }: { task: TaskDetail; refresh: () => void }) {
  const [ref, setRef] = useState('');
  const add = useMutation({
    mutationFn: () => addDependency(task.id, ref.trim()),
    onSuccess: () => {
      setRef('');
      refresh();
    },
  });
  const remove = useMutation({ mutationFn: (other: string) => removeDependency(task.id, other), onSuccess: refresh });
  const openBlockers = task.blocked_by.filter((d) => !d.done).length;
  return (
    <div>
      <h2 className="text-sm font-bold mb-2 flex items-center gap-1.5">
        Waiting for
        {openBlockers > 0 && task.status.category !== 'done' ? (
          <span className="inline-flex items-center gap-1 text-xs text-amber-600">
            <Lock className="w-3.5 h-3.5" /> blocked by {openBlockers}
          </span>
        ) : null}
      </h2>
      <ul className="divide-y divide-border">
        {task.blocked_by.map((d) => (
          <DepRow key={d.id} d={d} onRemove={() => remove.mutate(d.id)} />
        ))}
      </ul>
      <form
        className="flex gap-2 mt-1"
        onSubmit={(e) => {
          e.preventDefault();
          if (ref.trim()) add.mutate();
        }}
      >
        <input
          value={ref}
          onChange={(e) => setRef(e.target.value)}
          placeholder="Task ID it waits for, e.g. CREW-12"
          className="flex-1 h-8 rounded-md border border-input bg-background px-2 text-sm"
          aria-label="Task it waits for"
        />
        <Button type="submit" size="sm" variant="outline" disabled={!ref.trim() || add.isPending}>
          Add
        </Button>
      </form>
      {add.error ? <p className="text-xs text-destructive mt-1">{errText(add.error, 'Could not add it.')}</p> : null}
      {task.blocking.length > 0 ? (
        <>
          <h3 className="text-xs font-bold text-muted-foreground mt-3 mb-1">Blocking</h3>
          <ul className="divide-y divide-border">
            {task.blocking.map((d) => (
              <DepRow key={d.id} d={d} />
            ))}
          </ul>
        </>
      ) : null}
      {task.hidden_dependencies > 0 ? (
        <p className="text-xs text-muted-foreground mt-1">
          {task.hidden_dependencies} more in spaces you cannot see.
        </p>
      ) : null}
    </div>
  );
}

/** Repeat rule picker for the task sidebar. */
export function RecurrencePicker({
  value,
  disabled,
  onChange,
}: {
  value: Recurrence | null;
  disabled?: boolean;
  onChange: (r: Recurrence | null) => void;
}) {
  const cls = 'h-9 rounded-md border border-input bg-background px-2 text-sm';
  return (
    <div>
      <Label>Repeat</Label>
      <div className="flex gap-2">
        <input
          type="number"
          min={1}
          max={365}
          disabled={disabled || !value}
          value={value?.interval ?? 1}
          onChange={(e) => value && onChange({ ...value, interval: Math.max(1, Math.min(365, Number(e.target.value) || 1)) })}
          className={`${cls} w-16`}
          aria-label="Every"
        />
        <select
          value={value?.freq ?? ''}
          disabled={disabled}
          onChange={(e) =>
            onChange(e.target.value ? { freq: e.target.value as Recurrence['freq'], interval: value?.interval ?? 1 } : null)
          }
          className={`${cls} flex-1`}
          aria-label="Repeat"
        >
          <option value="">Does not repeat</option>
          <option value="daily">day(s)</option>
          <option value="weekly">week(s)</option>
          <option value="monthly">month(s)</option>
          <option value="yearly">year(s)</option>
        </select>
      </div>
      {value ? (
        <p className="text-[11px] text-muted-foreground mt-1">Completing it creates the next one with its dates moved on.</p>
      ) : null}
    </div>
  );
}

/** Estimate and logged time on the task page. */
export function TimeSection({
  task,
  meId,
  refresh,
  onEstimate,
}: {
  task: TaskDetail;
  meId?: string;
  refresh: () => void;
  onEstimate: (minutes: number | null) => void;
}) {
  const [spent, setSpent] = useState('');
  const [note, setNote] = useState('');
  const [estimate, setEstimate] = useState(task.estimate_minutes ? formatMinutes(task.estimate_minutes) : '');
  const log = useMutation({
    mutationFn: (minutes: number) => logTime(task.id, { minutes, note: note.trim() || undefined }),
    onSuccess: () => {
      setSpent('');
      setNote('');
      refresh();
    },
  });
  const remove = useMutation({ mutationFn: deleteTimeEntry, onSuccess: refresh });
  const minutes = parseDuration(spent);
  return (
    <div>
      <h2 className="text-sm font-bold mb-2">
        Time{' '}
        <span className="text-muted-foreground font-medium">
          {formatMinutes(task.time_spent_minutes)}
          {task.estimate_minutes ? ` of ${formatMinutes(task.estimate_minutes)} estimated` : ''}
        </span>
      </h2>
      <div className="grid gap-2 sm:grid-cols-[8rem_1fr_auto] items-end">
        <div>
          <Label htmlFor="estimate">Estimate</Label>
          <input
            id="estimate"
            value={estimate}
            onChange={(e) => setEstimate(e.target.value)}
            onBlur={() => {
              const v = estimate.trim() ? parseDuration(estimate) : null;
              if (estimate.trim() && v === null) return;
              if (v !== task.estimate_minutes) onEstimate(v);
            }}
            placeholder="e.g. 2h"
            className="w-full h-8 rounded-md border border-input bg-background px-2 text-sm"
          />
        </div>
        <form
          className="contents"
          onSubmit={(e) => {
            e.preventDefault();
            if (minutes) log.mutate(minutes);
          }}
        >
          <div className="flex gap-2">
            <input
              value={spent}
              onChange={(e) => setSpent(e.target.value)}
              placeholder="Spent, e.g. 1h30"
              className="w-28 h-8 rounded-md border border-input bg-background px-2 text-sm"
              aria-label="Time spent"
            />
            <input
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="What on (optional)"
              className="flex-1 h-8 rounded-md border border-input bg-background px-2 text-sm"
              aria-label="Note"
            />
          </div>
          <Button type="submit" size="sm" variant="outline" disabled={!minutes || log.isPending}>
            Log time
          </Button>
        </form>
      </div>
      {log.error ? <p className="text-xs text-destructive mt-1">{errText(log.error, 'Could not log time.')}</p> : null}
      {task.time_entries.length > 0 ? (
        <ul className="mt-2 divide-y divide-border text-sm">
          {task.time_entries.map((e) => (
            <li key={e.id} className="flex items-center gap-2 py-1">
              <span className="w-20 shrink-0 text-xs text-muted-foreground">{e.spent_on}</span>
              <span className="w-24 shrink-0">{formatMinutes(e.minutes)}</span>
              <span className="truncate text-muted-foreground">
                {e.user_name}
                {e.note ? ` — ${e.note}` : ''}
              </span>
              {e.user_id === meId ? (
                <button
                  type="button"
                  onClick={() => remove.mutate(e.id)}
                  className="ml-auto text-muted-foreground hover:text-destructive"
                  title="Remove entry"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
