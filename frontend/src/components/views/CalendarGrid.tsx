import { Link } from '@tanstack/react-router';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import type { Task } from '@/api/tasks';
import { Button } from '@/components/ui/Button';
import { monthGrid, monthRange, shiftMonth, tasksByDay, thisMonth, type Month } from '@/lib/calendar';
import { cn } from '@/lib/utils';

const WEEKDAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

function today(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Month calendar of tasks by their start-to-due span. */
export function CalendarGrid({
  tasks,
  month,
  onMonth,
  showList,
}: {
  tasks: Task[];
  month: Month;
  onMonth: (m: Month) => void;
  showList?: boolean;
}) {
  const grid = monthGrid(month.year, month.month);
  const { from, to } = monthRange(month);
  const byDay = tasksByDay(tasks, from, to);
  const label = new Date(month.year, month.month - 1, 1).toLocaleDateString(undefined, { month: 'long', year: 'numeric' });
  const now = today();
  const undated = tasks.filter((t) => !t.start_at && !t.due_at).length;
  return (
    <div>
      <div className="flex items-center gap-2 mb-3">
        <Button variant="ghost" size="sm" onClick={() => onMonth(shiftMonth(month, -1))} aria-label="Previous month">
          <ChevronLeft className="w-4 h-4" />
        </Button>
        <span className="font-heading font-bold w-40 text-center">{label}</span>
        <Button variant="ghost" size="sm" onClick={() => onMonth(shiftMonth(month, 1))} aria-label="Next month">
          <ChevronRight className="w-4 h-4" />
        </Button>
        <Button variant="outline" size="sm" onClick={() => onMonth(thisMonth())}>
          Today
        </Button>
        {undated ? <span className="text-xs text-muted-foreground ml-auto">{undated} without dates not shown</span> : null}
      </div>
      <div className="grid grid-cols-7 border-l border-t border-border rounded-card overflow-hidden text-xs">
        {WEEKDAYS.map((d) => (
          <div key={d} className="border-r border-b border-border bg-secondary/60 px-2 py-1 font-semibold text-muted-foreground">
            {d}
          </div>
        ))}
        {grid.flat().map((day) => {
          const items = byDay.get(day) ?? [];
          const inMonth = Number(day.slice(5, 7)) === month.month;
          return (
            <div
              key={day}
              className={cn('border-r border-b border-border min-h-24 p-1 space-y-0.5', !inMonth && 'bg-secondary/30 text-muted-foreground')}
            >
              <div className={cn('text-right px-1', day === now && 'font-bold text-primary')}>{Number(day.slice(8))}</div>
              {items.slice(0, 4).map((t) => (
                <Link
                  key={t.id}
                  to="/t/$taskKey"
                  params={{ taskKey: t.key }}
                  title={`${t.key} ${t.title}${showList ? ` — ${t.list_name}` : ''}`}
                  className={cn(
                    'block truncate rounded px-1 py-0.5 hover:opacity-80',
                    t.status.category === 'done' && 'line-through opacity-60'
                  )}
                  style={{ backgroundColor: `${t.status.color}22`, color: t.status.color }}
                >
                  {t.title}
                </Link>
              ))}
              {items.length > 4 ? <div className="px-1 text-muted-foreground">+{items.length - 4} more</div> : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}
