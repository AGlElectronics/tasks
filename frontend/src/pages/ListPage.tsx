import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router';
import {
  ArrowUpDown,
  CalendarDays,
  Columns3,
  GanttChart,
  List as ListIcon,
  Plus,
  Search as SearchIcon,
  Star,
  Upload,
  Users,
  X,
} from 'lucide-react';
import { ImportDialog } from '@/components/ImportDialog';
import { ApiError } from '@/api/client';
import {
  PRIORITIES,
  addFavorite,
  createTask,
  deleteTask,
  getList,
  listFavorites,
  listPeople,
  listSpaces,
  listTags,
  listTasks,
  removeFavorite,
  updateTask,
  type Priority,
  type Status,
  type Task,
  type TaskPatch,
} from '@/api/tasks';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { PageContainer } from '@/components/ui/PageHeader';
import { ErrorRow, LoadingRow } from '@/components/ui/States';
import { Avatars, DueBadge, PriorityFlag, StatusSelect, TaskLink, TaskMeta } from '@/components/TaskBits';
import { FieldChips, useFields } from '@/components/CustomFields';
import { CalendarGrid } from '@/components/views/CalendarGrid';
import { thisMonth, type Month } from '@/lib/calendar';
import { TimelineView } from '@/components/views/TimelineView';
import { WorkloadView } from '@/components/views/WorkloadView';
import { SORTS, dropPatch, sortTasks, type SortKey } from '@/lib/board';
import { notify } from '@/lib/notify';
import { cn } from '@/lib/utils';

export const LIST_VIEWS = ['list', 'board', 'calendar', 'timeline', 'workload'] as const;
export type ListView = (typeof LIST_VIEWS)[number];
export type ListSearch = { view?: ListView };

type Filters = {
  text: string;
  statusId: string;
  priority: Priority | '';
  assigneeId: string;
  tagId: string;
  /** "<field id>=<option id>" for select custom fields. */
  field: string;
};

const EMPTY_FILTERS: Filters = { text: '', statusId: '', priority: '', assigneeId: '', tagId: '', field: '' };

function applyFilters(tasks: Task[], f: Filters): Task[] {
  const text = f.text.trim().toLowerCase();
  const [fieldId, fieldValue] = f.field ? f.field.split('=') : ['', ''];
  return tasks.filter((t) => {
    if (text && !t.title.toLowerCase().includes(text) && !t.key.toLowerCase().includes(text)) return false;
    if (f.statusId && t.status.id !== f.statusId) return false;
    if (f.priority && t.priority !== f.priority) return false;
    if (f.assigneeId && !t.assignees.some((a) => a.id === f.assigneeId)) return false;
    if (f.tagId && !t.tags.some((tg) => tg.id === f.tagId)) return false;
    if (fieldId && t.fields?.[fieldId] !== fieldValue) return false;
    return true;
  });
}

/** Filters and sort are remembered per list in this browser. */
type SavedView = { filters: Filters; sort: SortKey };
const storageKey = (listId: string) => `tasks:list-view:${listId}`;

function loadSaved(listId: string): SavedView {
  try {
    const raw = localStorage.getItem(storageKey(listId));
    if (raw) {
      const v = JSON.parse(raw) as Partial<SavedView>;
      return {
        filters: { ...EMPTY_FILTERS, ...(v.filters ?? {}) },
        sort: SORTS.some((s) => s.key === v.sort) ? (v.sort as SortKey) : 'smart',
      };
    }
  } catch {
    // unreadable: fall back to defaults
  }
  return { filters: EMPTY_FILTERS, sort: 'smart' };
}

function FilterBar({
  spaceId,
  statuses,
  filters,
  onChange,
  sort,
  onSort,
}: {
  spaceId: string;
  statuses: Status[];
  filters: Filters;
  onChange: (f: Filters) => void;
  sort: SortKey;
  onSort: (s: SortKey) => void;
}) {
  const people = useQuery({ queryKey: ['people'], queryFn: ({ signal }) => listPeople(signal) });
  const tags = useQuery({ queryKey: ['tags', spaceId], queryFn: ({ signal }) => listTags(spaceId, signal) });
  const fields = useFields(spaceId);
  const selectFields = (fields.data ?? []).filter((f) => f.type === 'select');
  const active = Object.values(filters).some((v) => v !== '');
  const sel = 'h-8 rounded-pill border border-border bg-card px-2.5 text-xs';
  return (
    <div className="flex flex-wrap items-center gap-2 mb-4">
      <div className="relative">
        <SearchIcon className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" />
        <input
          value={filters.text}
          onChange={(e) => onChange({ ...filters, text: e.target.value })}
          placeholder="Filter tasks…"
          className="h-8 w-44 rounded-pill border border-border bg-card pl-8 pr-2.5 text-xs focus-visible:outline-none focus-visible:border-primary"
        />
      </div>
      <select value={filters.statusId} onChange={(e) => onChange({ ...filters, statusId: e.target.value })} className={sel}>
        <option value="">Any status</option>
        {statuses.map((s) => (
          <option key={s.id} value={s.id}>
            {s.name}
          </option>
        ))}
      </select>
      <select
        value={filters.priority}
        onChange={(e) => onChange({ ...filters, priority: e.target.value as Priority | '' })}
        className={cn(sel, 'capitalize')}
      >
        <option value="">Any priority</option>
        {PRIORITIES.map((p) => (
          <option key={p} value={p}>
            {p}
          </option>
        ))}
      </select>
      <select value={filters.assigneeId} onChange={(e) => onChange({ ...filters, assigneeId: e.target.value })} className={sel}>
        <option value="">Anyone</option>
        {(people.data ?? []).map((p) => (
          <option key={p.id} value={p.id}>
            {p.display_name}
          </option>
        ))}
      </select>
      {(tags.data ?? []).length > 0 ? (
        <select value={filters.tagId} onChange={(e) => onChange({ ...filters, tagId: e.target.value })} className={sel}>
          <option value="">Any tag</option>
          {(tags.data ?? []).map((tg) => (
            <option key={tg.id} value={tg.id}>
              {tg.name}
            </option>
          ))}
        </select>
      ) : null}
      {selectFields.length > 0 ? (
        <select value={filters.field} onChange={(e) => onChange({ ...filters, field: e.target.value })} className={sel} aria-label="Field">
          <option value="">Any field value</option>
          {selectFields.map((f) => (
            <optgroup key={f.id} label={f.name}>
              {f.options.map((o) => (
                <option key={o.id} value={`${f.id}=${o.id}`}>
                  {f.name}: {o.name}
                </option>
              ))}
            </optgroup>
          ))}
        </select>
      ) : null}
      {active ? (
        <button
          type="button"
          onClick={() => onChange(EMPTY_FILTERS)}
          className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
        >
          <X className="w-3.5 h-3.5" /> Clear
        </button>
      ) : null}
      <label className="ml-auto inline-flex items-center gap-1.5 text-xs text-muted-foreground">
        <ArrowUpDown className="w-3.5 h-3.5" />
        <select value={sort} onChange={(e) => onSort(e.target.value as SortKey)} className={sel} aria-label="Sort">
          {SORTS.map((s) => (
            <option key={s.key} value={s.key}>
              {s.label}
            </option>
          ))}
        </select>
      </label>
    </div>
  );
}

/** Bulk actions (TASK-3): act on every selected task in one go. */
function BulkBar({
  selected,
  tasks,
  statuses,
  spaceId,
  listId,
  onDone,
}: {
  selected: Set<string>;
  tasks: Task[];
  statuses: Status[];
  spaceId: string;
  listId: string;
  onDone: () => void;
}) {
  const qc = useQueryClient();
  const people = useQuery({ queryKey: ['people'], queryFn: ({ signal }) => listPeople(signal) });
  const spaces = useQuery({ queryKey: ['spaces'], queryFn: ({ signal }) => listSpaces(signal) });
  const byId = new Map(tasks.map((t) => [t.id, t]));
  const run = useMutation({
    mutationFn: async (fn: (t: Task) => Promise<unknown>) => {
      let failed = 0;
      for (const id of selected) {
        const t = byId.get(id);
        if (!t) continue;
        try {
          await fn(t);
        } catch {
          failed++;
        }
      }
      if (failed) notify(`${failed} task${failed === 1 ? '' : 's'} could not be updated.`);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['tasks', listId] });
      qc.invalidateQueries({ queryKey: ['spaces'] });
      qc.invalidateQueries({ queryKey: ['my-work'] });
      onDone();
    },
  });
  const sel = 'h-8 rounded-pill border border-border bg-card px-2.5 text-xs';
  const space = spaces.data?.find((s) => s.id === spaceId);
  return (
    <div className="sticky bottom-4 z-30 mx-auto w-fit max-w-full flex flex-wrap items-center gap-2 rounded-pill border border-border bg-card shadow-pop px-4 py-2">
      <span className="text-xs font-bold">{selected.size} selected</span>
      <select
        value=""
        disabled={run.isPending}
        onChange={(e) => {
          const id = e.target.value;
          if (id) run.mutate((t) => updateTask(t.id, { status_id: id, version: t.version }));
        }}
        className={sel}
      >
        <option value="">Set status…</option>
        {statuses.map((s) => (
          <option key={s.id} value={s.id}>
            {s.name}
          </option>
        ))}
      </select>
      <select
        value=""
        disabled={run.isPending}
        onChange={(e) => {
          const id = e.target.value;
          if (id)
            run.mutate((t) =>
              updateTask(t.id, { assignee_ids: [...new Set([...t.assignees.map((a) => a.id), id])], version: t.version })
            );
        }}
        className={sel}
      >
        <option value="">Assign…</option>
        {(people.data ?? []).map((p) => (
          <option key={p.id} value={p.id}>
            {p.display_name}
          </option>
        ))}
      </select>
      {space && space.lists.filter((l) => l.id !== listId).length > 0 ? (
        <select
          value=""
          disabled={run.isPending}
          onChange={(e) => {
            const id = e.target.value;
            if (id) run.mutate((t) => updateTask(t.id, { list_id: id, version: t.version }));
          }}
          className={sel}
        >
          <option value="">Move to…</option>
          {space.lists
            .filter((l) => l.id !== listId)
            .map((l) => (
              <option key={l.id} value={l.id}>
                {l.name}
              </option>
            ))}
        </select>
      ) : null}
      <Button
        variant="ghost"
        size="sm"
        className="text-destructive"
        disabled={run.isPending}
        onClick={() => {
          if (confirm(`Move ${selected.size} task${selected.size === 1 ? '' : 's'} to the trash?`))
            run.mutate((t) => deleteTask(t.id));
        }}
      >
        Delete
      </Button>
      <Button variant="ghost" size="sm" disabled={run.isPending} onClick={onDone}>
        Cancel
      </Button>
    </div>
  );
}

function useTaskMutations(listId: string) {
  const qc = useQueryClient();
  const key = ['tasks', listId];
  const patch = useMutation({
    mutationFn: ({ id, body }: { id: string; body: TaskPatch }) => updateTask(id, body),
    onMutate: async ({ id, body }) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Task[]>(key);
      qc.setQueryData<Task[]>(key, (tasks) =>
        tasks?.map((t) =>
          t.id === id
            ? {
                ...t,
                position: body.position ?? t.position,
                status: body.status_id ? { ...t.status, id: body.status_id } : t.status,
              }
            : t
        )
      );
      return { prev };
    },
    onError: (e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
      if (e instanceof ApiError && e.status === 409) {
        notify('Someone else changed that task — the view has been refreshed with their change.');
      } else {
        notify(e instanceof ApiError ? `Could not save: ${e.message}` : 'Could not save.');
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: key });
      qc.invalidateQueries({ queryKey: ['spaces'] });
      qc.invalidateQueries({ queryKey: ['my-work'] });
    },
  });
  const create = useMutation({
    mutationFn: (body: { title: string; status_id: string }) => createTask({ list_id: listId, ...body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: key });
      qc.invalidateQueries({ queryKey: ['spaces'] });
    },
  });
  return { patch, create };
}

function QuickAdd({ onAdd, pending }: { onAdd: (title: string) => void; pending: boolean }) {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState('');
  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground px-3 py-2"
      >
        <Plus className="w-4 h-4" /> Add task
      </button>
    );
  }
  return (
    <form
      className="px-2 py-1.5"
      onSubmit={(e) => {
        e.preventDefault();
        if (!title.trim()) return;
        onAdd(title.trim());
        setTitle('');
      }}
    >
      <input
        autoFocus
        value={title}
        disabled={pending}
        onChange={(e) => setTitle(e.target.value)}
        onBlur={() => !title && setOpen(false)}
        onKeyDown={(e) => e.key === 'Escape' && setOpen(false)}
        placeholder="Task name, then Enter"
        className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm focus-visible:outline-none focus-visible:border-primary"
      />
    </form>
  );
}

function ListRows({
  statuses,
  tasks,
  listId,
  spaceId,
  canEdit,
  selected,
  onToggleSelect,
  sort,
}: {
  statuses: Status[];
  tasks: Task[];
  listId: string;
  spaceId: string;
  canEdit: boolean;
  selected: Set<string>;
  onToggleSelect: (id: string) => void;
  sort: SortKey;
}) {
  const { patch, create } = useTaskMutations(listId);
  const fields = useFields(spaceId);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  return (
    <div className="space-y-5">
      {statuses.map((st) => {
        const rows = sortTasks(
          tasks.filter((t) => t.status.id === st.id),
          sort
        );
        const isCollapsed = collapsed[st.id] ?? (st.category === 'done' && rows.length > 5);
        return (
          <section key={st.id}>
            <button
              type="button"
              className="flex items-center gap-2 mb-2 text-sm font-bold"
              onClick={() => setCollapsed((c) => ({ ...c, [st.id]: !isCollapsed }))}
            >
              <span className="w-2.5 h-2.5 rounded-full" style={{ backgroundColor: st.color }} />
              <span style={{ color: st.color }}>{st.name}</span>
              <span className="text-muted-foreground font-medium">{rows.length}</span>
            </button>
            {isCollapsed ? null : (
              <Card className="divide-y divide-border">
                {rows.map((t) => (
                  <div key={t.id} className={cn('flex items-center gap-3 px-4 py-2', selected.has(t.id) && 'bg-primary-soft/40')}>
                    {canEdit ? (
                      <input
                        type="checkbox"
                        checked={selected.has(t.id)}
                        onChange={() => onToggleSelect(t.id)}
                        aria-label={`Select ${t.key}`}
                      />
                    ) : null}
                    <StatusSelect
                      statuses={statuses}
                      value={t.status.id}
                      onChange={(id) => patch.mutate({ id: t.id, body: { status_id: id, version: t.version } })}
                      className="w-28 shrink-0"
                    />
                    <span className="text-xs text-muted-foreground font-mono shrink-0 w-16">{t.key}</span>
                    <div className="flex-1 min-w-0 flex items-center gap-3">
                      <TaskLink task={t} className="truncate" />
                      {t.tags.map((tg) => (
                        <span
                          key={tg.id}
                          className="hidden sm:inline-flex rounded-pill px-1.5 py-0.5 text-[10px] font-semibold text-white shrink-0"
                          style={{ backgroundColor: tg.color }}
                        >
                          {tg.name}
                        </span>
                      ))}
                      <FieldChips fields={fields.data ?? []} values={t.fields ?? {}} />
                      <TaskMeta task={t} />
                    </div>
                    <PriorityFlag priority={t.priority} />
                    <DueBadge date={t.due_at} done={t.status.category === 'done'} />
                    <span className="w-20 flex justify-end">
                      <Avatars people={t.assignees} />
                    </span>
                  </div>
                ))}
                {canEdit ? (
                  <QuickAdd pending={create.isPending} onAdd={(title) => create.mutate({ title, status_id: st.id })} />
                ) : null}
              </Card>
            )}
          </section>
        );
      })}
    </div>
  );
}

function BoardView({
  statuses,
  tasks,
  listId,
  canEdit,
}: {
  statuses: Status[];
  tasks: Task[];
  listId: string;
  canEdit: boolean;
}) {
  const { patch, create } = useTaskMutations(listId);
  const [dragId, setDragId] = useState<string | null>(null);
  const [over, setOver] = useState<{ status: string; before: string | null } | null>(null);

  const drop = (statusId: string, beforeId: string | null) => {
    const dragged = tasks.find((t) => t.id === dragId);
    setDragId(null);
    setOver(null);
    if (!dragged) return;
    patch.mutate({ id: dragged.id, body: dropPatch(tasks, dragged, statusId, beforeId) });
  };

  return (
    <div className="flex gap-4 overflow-x-auto pb-4 items-start">
      {statuses.map((st) => {
        const col = tasks.filter((t) => t.status.id === st.id);
        return (
          <div
            key={st.id}
            className={cn(
              'w-72 shrink-0 rounded-card bg-secondary/60 border border-border flex flex-col max-h-[calc(100vh-12rem)]',
              over?.status === st.id && 'ring-2 ring-primary/40'
            )}
            onDragOver={(e) => {
              e.preventDefault();
              if (over?.status !== st.id || over.before !== null) {
                if (!(e.target as HTMLElement).closest('[data-card]')) setOver({ status: st.id, before: null });
              }
            }}
            onDrop={(e) => {
              e.preventDefault();
              drop(st.id, over?.status === st.id ? over.before : null);
            }}
          >
            <div className="flex items-center gap-2 px-3 py-2.5 border-b border-border" style={{ borderTop: `3px solid ${st.color}` }}>
              <span className="text-sm font-bold">{st.name}</span>
              <span className="text-xs text-muted-foreground">{col.length}</span>
            </div>
            <div className="p-2 space-y-2 overflow-y-auto">
              {col.map((t) => (
                <div
                  key={t.id}
                  data-card
                  draggable
                  onDragStart={(e) => {
                    setDragId(t.id);
                    e.dataTransfer.effectAllowed = 'move';
                  }}
                  onDragEnd={() => {
                    setDragId(null);
                    setOver(null);
                  }}
                  onDragOver={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    if (over?.before !== t.id) setOver({ status: st.id, before: t.id });
                  }}
                  className={cn(
                    'rounded-md border border-border bg-card p-3 shadow-sm cursor-grab active:cursor-grabbing',
                    dragId === t.id && 'opacity-40',
                    over?.before === t.id && dragId !== t.id && 'border-t-2 border-t-primary'
                  )}
                >
                  <div className="text-xs text-muted-foreground font-mono mb-1">{t.key}</div>
                  <TaskLink task={t} className="text-sm block" />
                  <div className="flex items-center gap-2 mt-2 flex-wrap">
                    <PriorityFlag priority={t.priority} />
                    <DueBadge date={t.due_at} done={t.status.category === 'done'} />
                    <TaskMeta task={t} />
                    <span className="ml-auto">
                      <Avatars people={t.assignees} />
                    </span>
                  </div>
                </div>
              ))}
            </div>
            {canEdit ? (
              <QuickAdd pending={create.isPending} onAdd={(title) => create.mutate({ title, status_id: st.id })} />
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

/** Pin this list to the sidebar favourites (TASK-11). */
function ListFavoriteStar({ listId }: { listId: string }) {
  const qc = useQueryClient();
  const favs = useQuery({ queryKey: ['favorites'], queryFn: ({ signal }) => listFavorites(signal) });
  const pinned = (favs.data ?? []).some((f) => f.list_id === listId);
  const toggle = useMutation({
    mutationFn: () => (pinned ? removeFavorite({ list_id: listId }) : addFavorite({ list_id: listId })),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['favorites'] }),
  });
  return (
    <button
      type="button"
      disabled={toggle.isPending || favs.isLoading}
      onClick={() => toggle.mutate()}
      title={pinned ? 'Remove from favourites' : 'Add to favourites'}
      className="p-1.5 rounded-pill hover:bg-secondary disabled:opacity-50"
    >
      <Star className={pinned ? 'w-4 h-4 fill-amber-400 text-amber-400' : 'w-4 h-4 text-muted-foreground'} />
    </button>
  );
}

export function ListPage() {
  const { listId } = useParams({ strict: false }) as { listId: string };
  const search = useSearch({ strict: false }) as ListSearch;
  const view = search.view ?? 'list';
  const navigate = useNavigate();

  const list = useQuery({ queryKey: ['list', listId], queryFn: ({ signal }) => getList(listId, signal) });
  const tasks = useQuery({ queryKey: ['tasks', listId], queryFn: ({ signal }) => listTasks(listId, signal) });
  const [saved, setSaved] = useState<SavedView>(() => loadSaved(listId));
  const [savedFor, setSavedFor] = useState(listId);
  if (savedFor !== listId) {
    setSavedFor(listId);
    setSaved(loadSaved(listId));
  }
  const { filters, sort } = saved;
  const setFilters = (f: Filters) => setSaved((s) => ({ ...s, filters: f }));
  const setSort = (k: SortKey) => setSaved((s) => ({ ...s, sort: k }));
  useEffect(() => {
    try {
      localStorage.setItem(storageKey(savedFor), JSON.stringify(saved));
    } catch {
      // storage full or disabled: the view just is not remembered
    }
  }, [saved, savedFor]);
  const [month, setMonth] = useState<Month>(thisMonth);
  const [importing, setImporting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const topLevel = useMemo(
    () =>
      applyFilters(
        (tasks.data ?? []).filter((t) => !t.parent_id),
        filters
      ).sort((a, b) => a.position - b.position),
    [tasks.data, filters]
  );
  // Calendar, timeline and workload include subtasks: they have dates too.
  const filtered = useMemo(() => applyFilters(tasks.data ?? [], filters), [tasks.data, filters]);

  if (list.isLoading) return <LoadingRow />;
  if (list.isError || !list.data) {
    return <ErrorRow message={list.error instanceof ApiError ? list.error.message : 'List not found.'} />;
  }
  const l = list.data;
  const statuses = l.space.statuses;
  const toggleSelect = (id: string) =>
    setSelected((s) => {
      const next = new Set(s);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  return (
    <PageContainer>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between mb-6">
        <div>
          <Link
            to="/spaces/$spaceId"
            params={{ spaceId: l.space.id }}
            className="text-sm font-semibold text-muted-foreground hover:text-foreground"
          >
            {l.space.name}
          </Link>
          <span className="flex items-center gap-1">
            <h1 className="font-heading text-2xl font-extrabold leading-tight tracking-tight">{l.name}</h1>
            <ListFavoriteStar listId={l.id} />
            {l.can_edit ? (
              <button
                type="button"
                onClick={() => setImporting(true)}
                className="p-1.5 rounded-pill hover:bg-secondary text-muted-foreground"
                title="Import tasks from CSV, ClickUp or Vikunja"
              >
                <Upload className="w-4 h-4" />
              </button>
            ) : null}
          </span>
          {importing ? <ImportDialog listId={l.id} onClose={() => setImporting(false)} /> : null}
        </div>
        <div className="inline-flex rounded-pill border border-border p-0.5 bg-card">
          {(
            [
              ['list', 'List', ListIcon],
              ['board', 'Board', Columns3],
              ['calendar', 'Calendar', CalendarDays],
              ['timeline', 'Timeline', GanttChart],
              ['workload', 'Workload', Users],
            ] as const
          ).map(([v, label, Icon]) => (
            <button
              key={v}
              type="button"
              onClick={() => navigate({ to: '/lists/$listId', params: { listId }, search: { view: v } })}
              className={cn(
                'inline-flex items-center gap-1.5 rounded-pill px-3 py-1.5 text-sm font-semibold transition-colors',
                view === v ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground'
              )}
            >
              <Icon className="w-4 h-4" /> {label}
            </button>
          ))}
        </div>
      </div>
      <FilterBar spaceId={l.space.id} statuses={statuses} filters={filters} onChange={setFilters} sort={sort} onSort={setSort} />
      {tasks.isLoading ? (
        <LoadingRow />
      ) : tasks.isError ? (
        <ErrorRow message="Could not load tasks." />
      ) : view === 'board' ? (
        <BoardView statuses={statuses} tasks={topLevel} listId={listId} canEdit={l.can_edit} />
      ) : view === 'calendar' ? (
        <CalendarGrid tasks={filtered} month={month} onMonth={setMonth} />
      ) : view === 'timeline' ? (
        <TimelineView tasks={filtered} />
      ) : view === 'workload' ? (
        <WorkloadView tasks={filtered} listId={listId} />
      ) : (
        <ListRows
          statuses={statuses}
          tasks={topLevel}
          listId={listId}
          spaceId={l.space.id}
          canEdit={l.can_edit}
          selected={selected}
          onToggleSelect={toggleSelect}
          sort={sort}
        />
      )}
      {selected.size > 0 ? (
        <BulkBar
          selected={selected}
          tasks={topLevel}
          statuses={statuses}
          spaceId={l.space.id}
          listId={listId}
          onDone={() => setSelected(new Set())}
        />
      ) : null}
    </PageContainer>
  );
}
