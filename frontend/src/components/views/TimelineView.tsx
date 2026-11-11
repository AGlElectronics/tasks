import { useState } from 'react';
import { Link } from '@tanstack/react-router';
import { ChevronLeft, ChevronRight, Lock } from 'lucide-react';
import type { Task } from '@/api/tasks';
import { Button } from '@/components/ui/Button';
import { addDays, bar, span, weekStart } from '@/lib/calendar';
import { cn } from '@/lib/utils';

const DAYS = 42; // six weeks

function todayStr(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Gantt-style timeline: one bar per task from start to due date. */
export function TimelineView({ tasks }: { tasks: Task[] }) {
  const [from, setFrom] = useState(() => addDays(weekStart(todayStr()), -7));
  const days = Array.from({ length: DAYS }, (_, i) => addDays(from, i));
  const rows = tasks
    .filter((t) => span(t))
    .sort((a, b) => span(a)!.from.localeCompare(span(b)!.from) || a.number - b.number);
  const now = todayStr();
  const col = `minmax(1.5rem, 1fr)`;
  return (
    <div>
      <div className="flex items-center gap-2 mb-3">
        <Button variant="ghost" size="sm" onClick={() => setFrom(addDays(from, -14))} aria-label="Earlier">
          <ChevronLeft className="w-4 h-4" />
        </Button>
        <Button variant="ghost" size="sm" onClick={() => setFrom(addDays(from, 14))} aria-label="Later">
          <ChevronRight className="w-4 h-4" />
        </Button>
        <Button variant="outline" size="sm" onClick={() => setFrom(addDays(weekStart(now), -7))}>
          Today
        </Button>
        <span className="text-xs text-muted-foreground ml-auto">
          {tasks.length - rows.length} without dates not shown · set a start and a due date to get a bar
        </span>
      </div>
      <div className="overflow-x-auto border border-border rounded-card">
        <div className="min-w-[56rem]" style={{ display: 'grid', gridTemplateColumns: `16rem repeat(${DAYS}, ${col})` }}>
          <div className="sticky left-0 bg-secondary/60 border-b border-border px-2 py-1 text-xs font-semibold">Task</div>
          {days.map((d) => (
            <div
              key={d}
              className={cn(
                'border-b border-l border-border text-[10px] text-center py-1',
                d === now ? 'bg-primary/15 font-bold' : 'bg-secondary/60',
                new Date(d).getUTCDay() === 1 && 'border-l-foreground/30'
              )}
              title={d}
            >
              {Number(d.slice(8))}
            </div>
          ))}
          {rows.map((t) => {
            const b = bar(t, from, DAYS);
            return (
              <div key={t.id} className="contents">
                <div className="sticky left-0 bg-card border-b border-border px-2 py-1 text-xs truncate flex items-center gap-1">
                  {t.blocked_by_open > 0 && t.status.category !== 'done' ? <Lock className="w-3 h-3 text-amber-600 shrink-0" /> : null}
                  <Link to="/t/$taskKey" params={{ taskKey: t.key }} className="truncate hover:underline" title={`${t.key} ${t.title}`}>
                    <span className="font-mono text-muted-foreground mr-1">{t.key}</span>
                    {t.title}
                  </Link>
                </div>
                <div className="border-b border-border relative" style={{ gridColumn: `2 / span ${DAYS}` }}>
                  {b ? (
                    <div
                      className={cn('absolute top-1 bottom-1 rounded', t.status.category === 'done' && 'opacity-50')}
                      style={{
                        left: `${(b.offset / DAYS) * 100}%`,
                        width: `${(b.length / DAYS) * 100}%`,
                        backgroundColor: t.status.color,
                      }}
                      title={`${span(t)!.from} → ${span(t)!.to}`}
                    />
                  ) : null}
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
