import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CheckCircle2, Circle, Inbox } from 'lucide-react';
import { ApiError } from '@/api/client';
import { listSpaces, myWork, updateTask, type Space, type Task } from '@/api/tasks';
import { notify } from '@/lib/notify';
import { Card } from '@/components/ui/Card';
import { PageContainer, PageHeader } from '@/components/ui/PageHeader';
import { EmptyState, ErrorRow, LoadingRow } from '@/components/ui/States';
import { Avatars, DueBadge, PriorityFlag, StatusPill, TaskLink, TaskMeta } from '@/components/TaskBits';

function localToday() {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

function inDays(n: number) {
  const d = new Date();
  d.setDate(d.getDate() + n);
  const pad = (x: number) => String(x).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

type Group = { id: string; title: string; tasks: Task[] };

function groupTasks(tasks: Task[]): Group[] {
  const today = localToday();
  const week = inDays(7);
  const groups: Group[] = [
    { id: 'overdue', title: 'Overdue', tasks: [] },
    { id: 'today', title: 'Today', tasks: [] },
    { id: 'week', title: 'Next 7 days', tasks: [] },
    { id: 'later', title: 'Later', tasks: [] },
    { id: 'nodate', title: 'No due date', tasks: [] },
    { id: 'done', title: 'Done this week', tasks: [] },
  ];
  const by = Object.fromEntries(groups.map((g) => [g.id, g]));
  for (const t of tasks) {
    if (t.status.category === 'done') by.done.tasks.push(t);
    else if (!t.due_at) by.nodate.tasks.push(t);
    else if (t.due_at < today) by.overdue.tasks.push(t);
    else if (t.due_at === today) by.today.tasks.push(t);
    else if (t.due_at <= week) by.week.tasks.push(t);
    else by.later.tasks.push(t);
  }
  return groups.filter((g) => g.tasks.length > 0);
}

export function MyWorkPage() {
  const qc = useQueryClient();
  const tasks = useQuery({ queryKey: ['my-work'], queryFn: ({ signal }) => myWork(signal) });
  const spaces = useQuery({ queryKey: ['spaces'], queryFn: ({ signal }) => listSpaces(signal) });

  const toggle = useMutation({
    mutationFn: ({ task, space }: { task: Task; space: Space }) => {
      const target =
        task.status.category === 'done'
          ? space.statuses.find((s) => s.category === 'not_started') ?? space.statuses[0]
          : space.statuses.find((s) => s.category === 'done');
      return updateTask(task.id, { status_id: target!.id, version: task.version });
    },
    onError: (e) => {
      if (e instanceof ApiError && e.status === 409) {
        notify('Someone else changed that task — the list has been refreshed with their change.');
      } else {
        notify(e instanceof ApiError ? `Could not save: ${e.message}` : 'Could not save.');
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['my-work'] });
      qc.invalidateQueries({ queryKey: ['spaces'] });
    },
  });

  const spaceById = new Map((spaces.data ?? []).map((s) => [s.id, s]));

  return (
    <PageContainer>
      <PageHeader title="My work" description="Tasks assigned to you, by due date." />
      {tasks.isLoading ? (
        <LoadingRow />
      ) : tasks.isError ? (
        <ErrorRow message="Could not load your tasks." />
      ) : tasks.data!.length === 0 ? (
        <Card>
          <EmptyState
            icon={Inbox}
            title="Nothing assigned to you"
            description="When someone assigns you a task it shows up here."
            action={
              <Link to="/spaces" className="text-sm font-semibold text-primary hover:underline">
                Browse spaces
              </Link>
            }
          />
        </Card>
      ) : (
        <div className="space-y-6">
          {groupTasks(tasks.data!).map((g) => (
            <section key={g.id}>
              <h2 className={g.id === 'overdue' ? 'text-sm font-bold text-destructive mb-2' : 'text-sm font-bold mb-2'}>
                {g.title} <span className="text-muted-foreground font-medium">{g.tasks.length}</span>
              </h2>
              <Card className="divide-y divide-border">
                {g.tasks.map((t) => {
                  const space = spaceById.get(t.space_id);
                  const done = t.status.category === 'done';
                  return (
                    <div key={t.id} className="flex items-center gap-3 px-4 py-2.5">
                      <button
                        type="button"
                        disabled={!space || toggle.isPending}
                        onClick={() => space && toggle.mutate({ task: t, space })}
                        className="text-muted-foreground hover:text-primary"
                        title={done ? 'Reopen' : 'Mark done'}
                      >
                        {done ? <CheckCircle2 className="w-5 h-5 text-green-500" /> : <Circle className="w-5 h-5" />}
                      </button>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-xs text-muted-foreground font-mono shrink-0">{t.key}</span>
                          <TaskLink task={t} className="truncate" />
                        </div>
                        <div className="flex items-center gap-3 mt-0.5 text-xs text-muted-foreground">
                          <span>
                            {space?.name ?? ''} · {t.list_name}
                          </span>
                          <TaskMeta task={t} />
                        </div>
                      </div>
                      <PriorityFlag priority={t.priority} />
                      <DueBadge date={t.due_at} done={done} />
                      <StatusPill status={t.status} />
                      <Avatars people={t.assignees} />
                    </div>
                  );
                })}
              </Card>
            </section>
          ))}
        </div>
      )}
    </PageContainer>
  );
}
