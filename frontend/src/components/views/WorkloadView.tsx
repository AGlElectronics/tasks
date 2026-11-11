import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { listTimeReport, type Task } from '@/api/tasks';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { addDays, weekStart, workload } from '@/lib/calendar';
import { formatMinutes } from '@/lib/format';
import { cn } from '@/lib/utils';

const WEEKS = 6;

function todayStr(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Open tasks and estimated hours per person per week, plus logged time. */
export function WorkloadView({ tasks, listId }: { tasks: Task[]; listId: string }) {
  const [from, setFrom] = useState(() => weekStart(todayStr()));
  const rows = workload(
    tasks.map((t) => ({ ...t, done: t.status.category === 'done' })),
    from,
    WEEKS
  );
  const weeks = Array.from({ length: WEEKS }, (_, i) => addDays(from, i * 7));
  const report = useQuery({
    queryKey: ['time-report', listId, from],
    queryFn: ({ signal }) => listTimeReport(listId, { from, to: addDays(from, WEEKS * 7 - 1) }, signal),
  });
  const max = Math.max(1, ...rows.flatMap((r) => r.weeks.map((w) => w.tasks)));
  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={() => setFrom(addDays(from, -7 * WEEKS))} aria-label="Earlier">
          <ChevronLeft className="w-4 h-4" />
        </Button>
        <Button variant="ghost" size="sm" onClick={() => setFrom(addDays(from, 7 * WEEKS))} aria-label="Later">
          <ChevronRight className="w-4 h-4" />
        </Button>
        <Button variant="outline" size="sm" onClick={() => setFrom(weekStart(todayStr()))}>
          This week
        </Button>
        <span className="text-xs text-muted-foreground ml-auto">
          Open tasks by due date (or start date) · estimates count fully for each assignee
        </span>
      </div>
      <Card className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="text-xs text-muted-foreground">
            <tr>
              <th className="text-left px-4 py-2 font-semibold">Person</th>
              {weeks.map((w) => (
                <th key={w} className="px-2 py-2 font-semibold text-center">
                  Week of {new Date(w).toLocaleDateString(undefined, { day: 'numeric', month: 'short', timeZone: 'UTC' })}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {rows.length === 0 ? (
              <tr>
                <td colSpan={WEEKS + 1} className="px-4 py-6 text-center text-muted-foreground">
                  No open tasks with dates in these weeks.
                </td>
              </tr>
            ) : (
              rows.map((r) => (
                <tr key={r.id}>
                  <td className={cn('px-4 py-2 whitespace-nowrap', r.id === '' && 'text-muted-foreground italic')}>{r.name}</td>
                  {r.weeks.map((c, i) => (
                    <td key={i} className="px-2 py-2 text-center">
                      {c.tasks ? (
                        <div
                          className="rounded px-1 py-0.5"
                          style={{ backgroundColor: `hsl(var(--primary) / ${0.1 + (0.5 * c.tasks) / max})` }}
                          title={`${c.tasks} task(s)${c.minutes ? `, ${formatMinutes(c.minutes)} estimated` : ''}`}
                        >
                          {c.tasks}
                          {c.minutes ? <span className="block text-[10px] text-muted-foreground">{formatMinutes(c.minutes)}</span> : null}
                        </div>
                      ) : (
                        <span className="text-muted-foreground/40">·</span>
                      )}
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </Card>
      <div>
        <h2 className="text-sm font-bold mb-2">Time logged in these weeks</h2>
        {report.data && report.data.people.length > 0 ? (
          <ul className="text-sm space-y-1">
            {report.data.people.map((p) => (
              <li key={p.user_id ?? 'former'} className="flex gap-3">
                <span className="w-48 truncate">{p.user_name}</span>
                <span>{formatMinutes(p.minutes)}</span>
              </li>
            ))}
            <li className="flex gap-3 font-semibold border-t border-border pt-1">
              <span className="w-48">Total</span>
              <span>{formatMinutes(report.data.total_minutes)}</span>
            </li>
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">No time logged.</p>
        )}
      </div>
    </div>
  );
}
