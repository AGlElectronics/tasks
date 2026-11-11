import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { apiRequest } from '@/api/client';
import type { Task } from '@/api/tasks';
import { PageContainer, PageHeader } from '@/components/ui/PageHeader';
import { ErrorRow, LoadingRow } from '@/components/ui/States';
import { CalendarGrid } from '@/components/views/CalendarGrid';
import { monthRange, thisMonth, type Month } from '@/lib/calendar';

const getCalendar = (from: string, to: string, mine: boolean, signal?: AbortSignal) =>
  apiRequest<{ tasks: Task[]; truncated: boolean }>('/api/v1/calendar', {
    signal,
    query: { from, to, mine: mine ? 1 : undefined },
  });

/** Every dated task you can see, across spaces, by month. */
export function CalendarPage() {
  const [month, setMonth] = useState<Month>(thisMonth);
  const [mine, setMine] = useState(false);
  const { from, to } = monthRange(month);
  const q = useQuery({
    queryKey: ['calendar', from, to, mine],
    queryFn: ({ signal }) => getCalendar(from, to, mine, signal),
  });
  return (
    <PageContainer>
      <PageHeader
        title="Calendar"
        description="Tasks with a start or due date in every space you can see."
        actions={
          <label className="inline-flex items-center gap-2 text-sm">
            <input type="checkbox" checked={mine} onChange={(e) => setMine(e.target.checked)} /> Only mine
          </label>
        }
      />
      {q.isLoading ? (
        <LoadingRow />
      ) : q.isError ? (
        <ErrorRow message="Could not load the calendar." />
      ) : (
        <>
          {q.data!.truncated ? (
            <p className="text-xs text-amber-600 mb-2">This month has more tasks than the calendar shows; tick “Only mine” or open a list.</p>
          ) : null}
          <CalendarGrid tasks={q.data!.tasks} month={month} onMonth={setMonth} showList />
        </>
      )}
    </PageContainer>
  );
}
