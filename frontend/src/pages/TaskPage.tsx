import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import {
  Bell,
  BellOff,
  ChevronDown,
  ChevronRight,
  Download,
  Paperclip,
  Pencil,
  Plus,
  Tag as TagIcon,
  Trash2,
  UserPlus,
} from 'lucide-react';
import { ApiError } from '@/api/client';
import {
  PRIORITIES,
  addChecklistItem,
  addComment,
  createTask,
  deleteAttachment,
  deleteChecklistItem,
  deleteComment,
  deleteTask,
  downloadAttachmentFile,
  getTask,
  listPeople,
  listSpaces,
  listTags,
  taskHistory,
  unwatchTask,
  updateChecklistItem,
  updateComment,
  updateTask,
  uploadAttachment,
  watchTask,
  type Attachment,
  type TaskDetail,
  type TaskEvent,
  type TaskPatch,
} from '@/api/tasks';
import { Button } from '@/components/ui/Button';
import { Card, CardContent } from '@/components/ui/Card';
import { Label } from '@/components/ui/Input';
import { PageContainer } from '@/components/ui/PageHeader';
import { ErrorRow, LoadingRow } from '@/components/ui/States';
import { Avatar, DueBadge, PriorityFlag, StatusSelect, TaskLink } from '@/components/TaskBits';
import { useMe } from '@/hooks/useMe';
import { notify } from '@/lib/notify';
import { timeAgo } from '@/lib/format';
import { cn } from '@/lib/utils';

function str(v: unknown) {
  return typeof v === 'string' && v ? v : null;
}

function describe(e: TaskEvent): string {
  const base = describeBase(e);
  const auto = str(e.data.automation);
  return auto ? `${base} — automation “${auto}”` : base;
}

function describeBase(e: TaskEvent): string {
  const from = str(e.data.from);
  const to = str(e.data.to);
  switch (e.kind) {
    case 'created':
      return 'created this task';
    case 'title':
      return `renamed it to "${to}"`;
    case 'description':
      return 'updated the description';
    case 'status':
      return `changed status from ${from} to ${to}`;
    case 'list':
      return `moved it from ${from} to ${to}`;
    case 'priority':
      return to ? `set priority to ${to}` : 'cleared the priority';
    case 'due_at':
      return to ? `set the due date to ${to}` : 'removed the due date';
    case 'start_at':
      return to ? `set the start date to ${to}` : 'removed the start date';
    case 'assigned':
      return `assigned ${str(e.data.user)}`;
    case 'unassigned':
      return `unassigned ${str(e.data.user)}`;
    case 'tags':
      return 'changed the tags';
    case 'attachment':
      return `attached ${str(e.data.filename) ?? 'a file'}`;
    case 'checklist_completed':
      return 'completed the checklist';
    case 'deleted':
      return 'moved it to the trash';
    case 'restored':
      return 'restored it from the trash';
    case 'completed':
      return 'completed it (its status became a done status)';
    case 'reopened':
      return 'reopened it (its status is no longer a done status)';
    default:
      return e.kind;
  }
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

function AssigneePicker({ task, onChange }: { task: TaskDetail; onChange: (ids: string[]) => void }) {
  const people = useQuery({ queryKey: ['people'], queryFn: ({ signal }) => listPeople(signal) });
  const [open, setOpen] = useState(false);
  const selected = new Set(task.assignees.map((a) => a.id));
  return (
    <div className="relative">
      <div className="flex flex-wrap items-center gap-1.5">
        {task.assignees.map((a) => (
          <span key={a.id} className="inline-flex items-center gap-1.5 rounded-pill bg-secondary pl-0.5 pr-2 py-0.5 text-xs">
            <Avatar person={a} />
            {a.display_name}
          </span>
        ))}
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="inline-flex items-center gap-1 rounded-pill border border-dashed border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
        >
          <UserPlus className="w-3.5 h-3.5" /> {task.assignees.length ? 'Edit' : 'Assign'}
        </button>
      </div>
      {open ? (
        <div className="absolute z-20 mt-1 w-64 max-h-72 overflow-y-auto rounded-md border border-border bg-card shadow-pop p-1">
          {(people.data ?? []).map((p) => (
            <label key={p.id} className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-secondary text-sm cursor-pointer">
              <input
                type="checkbox"
                checked={selected.has(p.id)}
                onChange={(e) => {
                  const next = new Set(selected);
                  if (e.target.checked) next.add(p.id);
                  else next.delete(p.id);
                  onChange([...next]);
                }}
              />
              <Avatar person={p} />
              <span className="truncate">{p.display_name}</span>
            </label>
          ))}
          {people.data?.length === 0 ? (
            <p className="text-xs text-muted-foreground p-2">People appear here after they sign in to Tasks once.</p>
          ) : null}
          <button type="button" className="w-full text-xs text-muted-foreground py-1.5 hover:text-foreground" onClick={() => setOpen(false)}>
            Close
          </button>
        </div>
      ) : null}
    </div>
  );
}

/** Space-level tags on this task (TASK-4). */
function TagPicker({ task, onChange }: { task: TaskDetail; onChange: (ids: string[]) => void }) {
  const tags = useQuery({ queryKey: ['tags', task.space_id], queryFn: ({ signal }) => listTags(task.space_id, signal) });
  const [open, setOpen] = useState(false);
  const selected = new Set(task.tags.map((t) => t.id));
  return (
    <div className="relative">
      <div className="flex flex-wrap items-center gap-1.5">
        {task.tags.map((tg) => (
          <span
            key={tg.id}
            className="inline-flex items-center rounded-pill px-2 py-0.5 text-xs font-semibold text-white"
            style={{ backgroundColor: tg.color }}
          >
            {tg.name}
          </span>
        ))}
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="inline-flex items-center gap-1 rounded-pill border border-dashed border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
        >
          <TagIcon className="w-3.5 h-3.5" /> {task.tags.length ? 'Edit' : 'Tag'}
        </button>
      </div>
      {open ? (
        <div className="absolute z-20 mt-1 w-56 max-h-64 overflow-y-auto rounded-md border border-border bg-card shadow-pop p-1">
          {(tags.data ?? []).map((tg) => (
            <label key={tg.id} className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-secondary text-sm cursor-pointer">
              <input
                type="checkbox"
                checked={selected.has(tg.id)}
                onChange={(e) => {
                  const next = new Set(selected);
                  if (e.target.checked) next.add(tg.id);
                  else next.delete(tg.id);
                  onChange([...next]);
                }}
              />
              <span className="w-2.5 h-2.5 rounded-full shrink-0" style={{ backgroundColor: tg.color }} />
              <span className="truncate">{tg.name}</span>
            </label>
          ))}
          {tags.data?.length === 0 ? (
            <p className="text-xs text-muted-foreground p-2">No tags in this space yet — a space manager can add them on the space page.</p>
          ) : null}
          <button type="button" className="w-full text-xs text-muted-foreground py-1.5 hover:text-foreground" onClick={() => setOpen(false)}>
            Close
          </button>
        </div>
      ) : null}
    </div>
  );
}

/** Checklist (TASK-6): lightweight steps inside one task. */
function ChecklistSection({ task, refresh }: { task: TaskDetail; refresh: () => void }) {
  const [draft, setDraft] = useState('');
  const add = useMutation({
    mutationFn: (body: string) => addChecklistItem(task.id, body),
    onSuccess: () => {
      setDraft('');
      refresh();
    },
  });
  const toggle = useMutation({
    mutationFn: ({ id, done }: { id: string; done: boolean }) => updateChecklistItem(id, { done }),
    onSuccess: refresh,
  });
  const remove = useMutation({ mutationFn: (id: string) => deleteChecklistItem(id), onSuccess: refresh });

  if (task.checklist.length === 0 && !draft) {
    // Collapsed to a single affordance until it's used, keeping the page calm.
    return (
      <button
        type="button"
        onClick={() => setDraft(' ')}
        className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
      >
        <Plus className="w-3.5 h-3.5" /> Add a checklist
      </button>
    );
  }
  return (
    <div>
      <h2 className="text-sm font-bold mb-2">
        Checklist{' '}
        {task.checklist_total ? (
          <span className="text-muted-foreground font-medium">
            {task.checklist_done}/{task.checklist_total}
          </span>
        ) : null}
      </h2>
      <Card className="divide-y divide-border">
        {task.checklist.map((item) => (
          <div key={item.id} className="group flex items-center gap-3 px-4 py-2">
            <input
              type="checkbox"
              checked={item.done}
              onChange={(e) => toggle.mutate({ id: item.id, done: e.target.checked })}
            />
            <span className={cn('flex-1 text-sm', item.done ? 'line-through text-muted-foreground' : '')}>{item.body}</span>
            <button
              type="button"
              onClick={() => remove.mutate(item.id)}
              className="opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-destructive"
              title="Remove item"
            >
              <Trash2 className="w-3.5 h-3.5" />
            </button>
          </div>
        ))}
        <form
          className="flex items-center gap-2 px-3 py-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (draft.trim()) add.mutate(draft.trim());
          }}
        >
          <Plus className="w-4 h-4 text-muted-foreground" />
          <input
            value={draft.trimStart()}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="Add a step, then Enter"
            className="flex-1 bg-transparent text-sm focus-visible:outline-none"
            autoFocus={task.checklist.length === 0}
          />
        </form>
      </Card>
    </div>
  );
}

/** Attachments (TASK-7): stored in S3, streamed through the backend. */
function AttachmentsSection({ task, refresh }: { task: TaskDetail; refresh: () => void }) {
  const { me } = useMe();
  const fileInput = useRef<HTMLInputElement>(null);
  const upload = useMutation({
    mutationFn: (file: File) => uploadAttachment(task.id, file),
    onSuccess: refresh,
    onError: (e) => {
      if (e instanceof ApiError && e.status === 503) notify('File storage is not configured on this server.');
      else notify(e instanceof ApiError ? `Upload failed: ${e.message}` : 'Upload failed.');
    },
  });
  const remove = useMutation({
    mutationFn: (id: string) => deleteAttachment(id),
    onSuccess: refresh,
    onError: (e) => notify(e instanceof ApiError ? e.message : 'Could not delete the file.'),
  });
  const download = (a: Attachment) =>
    downloadAttachmentFile(a).catch(() => notify('Could not download the file.'));

  return (
    <div>
      <input
        ref={fileInput}
        type="file"
        className="hidden"
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) upload.mutate(f);
          e.target.value = '';
        }}
      />
      {task.attachments.length === 0 ? (
        <button
          type="button"
          onClick={() => fileInput.current?.click()}
          disabled={upload.isPending}
          className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground disabled:opacity-50"
        >
          <Paperclip className="w-3.5 h-3.5" /> {upload.isPending ? 'Uploading…' : 'Attach a file'}
        </button>
      ) : (
        <>
          <h2 className="text-sm font-bold mb-2">Attachments</h2>
          <Card className="divide-y divide-border">
            {task.attachments.map((a) => (
              <div key={a.id} className="group flex items-center gap-3 px-4 py-2">
                <Paperclip className="w-4 h-4 text-muted-foreground shrink-0" />
                <button
                  type="button"
                  onClick={() => void download(a)}
                  className="flex-1 min-w-0 text-left text-sm truncate hover:underline"
                  title={`Download ${a.filename}`}
                >
                  {a.filename}
                </button>
                <span className="text-xs text-muted-foreground shrink-0">{formatBytes(a.size_bytes)}</span>
                <button type="button" onClick={() => void download(a)} className="text-muted-foreground hover:text-foreground" title="Download">
                  <Download className="w-3.5 h-3.5" />
                </button>
                {a.uploaded_by === me?.id ? (
                  <button
                    type="button"
                    onClick={() => confirm(`Delete ${a.filename}?`) && remove.mutate(a.id)}
                    className="opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-destructive"
                    title="Delete file"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                ) : null}
              </div>
            ))}
            <div className="px-3 py-2">
              <button
                type="button"
                onClick={() => fileInput.current?.click()}
                disabled={upload.isPending}
                className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground disabled:opacity-50"
              >
                <Plus className="w-3.5 h-3.5" /> {upload.isPending ? 'Uploading…' : 'Add another file'}
              </button>
            </div>
          </Card>
        </>
      )}
    </div>
  );
}

export function TaskPage() {
  const { taskKey } = useParams({ strict: false }) as { taskKey: string };
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { me } = useMe();
  const task = useQuery({ queryKey: ['task', taskKey], queryFn: ({ signal }) => getTask(taskKey, signal) });
  const spaces = useQuery({ queryKey: ['spaces'], queryFn: ({ signal }) => listSpaces(signal) });
  const people = useQuery({ queryKey: ['people'], queryFn: ({ signal }) => listPeople(signal) });
  const [fullHistory, setFullHistory] = useState(false);
  const taskId = task.data?.id;
  const history = useQuery({
    queryKey: ['task', taskKey, 'history', taskId],
    queryFn: ({ signal }) => taskHistory(taskId!, signal),
    enabled: fullHistory && !!taskId,
  });

  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [comment, setComment] = useState('');
  const [subtask, setSubtask] = useState('');
  const [editingComment, setEditingComment] = useState<{ id: string; body: string } | null>(null);
  const [moreFields, setMoreFields] = useState(false);
  useEffect(() => {
    if (task.data) {
      setTitle(task.data.title);
      setDescription(task.data.description);
    }
  }, [task.data?.id, task.data?.version]); // eslint-disable-line react-hooks/exhaustive-deps

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['task'] });
    qc.invalidateQueries({ queryKey: ['tasks'] });
    qc.invalidateQueries({ queryKey: ['spaces'] });
    qc.invalidateQueries({ queryKey: ['my-work'] });
  };

  const patch = useMutation({
    mutationFn: ({ id, body }: { id: string; body: TaskPatch }) => updateTask(id, body),
    onSuccess: refresh,
    onError: (e) => {
      if (e instanceof ApiError && e.status === 409) {
        // Reload so the sidebar and the title/description editors show the
        // other person's change; the inline error explains what happened.
        refresh();
      }
    },
  });
  const addSub = useMutation({
    mutationFn: (t: string) => createTask({ parent_id: task.data!.id, title: t }),
    onSuccess: () => {
      setSubtask('');
      refresh();
    },
  });
  const postComment = useMutation({
    mutationFn: () => {
      // "@Full Name" in the text notifies that person (TASK-5).
      const lower = comment.toLowerCase();
      const mentions = (people.data ?? [])
        .filter((p) => lower.includes(`@${p.display_name.toLowerCase()}`))
        .map((p) => p.id);
      return addComment(task.data!.id, comment, mentions);
    },
    onSuccess: () => {
      setComment('');
      refresh();
    },
  });
  const saveComment = useMutation({
    mutationFn: ({ id, body }: { id: string; body: string }) => updateComment(id, body),
    onSuccess: () => {
      setEditingComment(null);
      refresh();
    },
  });
  const removeComment = useMutation({ mutationFn: (id: string) => deleteComment(id), onSuccess: refresh });
  const toggleWatch = useMutation({
    mutationFn: (watching: boolean) => (watching ? unwatchTask(task.data!.id) : watchTask(task.data!.id)),
    onSuccess: refresh,
  });
  const remove = useMutation({
    mutationFn: () => deleteTask(task.data!.id),
    onSuccess: () => {
      const t = task.data!;
      refresh();
      if (t.parent_key) navigate({ to: '/t/$taskKey', params: { taskKey: t.parent_key } });
      else navigate({ to: '/lists/$listId', params: { listId: t.list_id } });
    },
  });

  if (task.isLoading) return <LoadingRow />;
  if (task.isError || !task.data) {
    return <ErrorRow message={task.error instanceof ApiError ? task.error.message : 'Task not found.'} />;
  }
  const t = task.data;
  const space = spaces.data?.find((s) => s.id === t.space_id);
  const set = (body: TaskPatch) => patch.mutate({ id: t.id, body: { ...body, version: t.version } });
  const watching = !!me && t.watchers.some((w) => w.id === me.id);
  // Fields the Vikunja feedback called noise stay behind "More" until used.
  const showMore = moreFields || !!t.priority || !!t.start_at;

  const hidden = t.comments_total - t.comments.length + (t.events_total - t.events.length);
  const source = fullHistory && history.data ? history.data : t;
  const timeline = [
    ...source.comments.map((c) => ({ at: c.created_at, comment: c, event: null as TaskEvent | null })),
    ...source.events.map((e) => ({ at: e.created_at, comment: null, event: e })),
  ].sort((a, b) => a.at.localeCompare(b.at));

  return (
    <PageContainer className="max-w-6xl">
      <nav className="flex items-center gap-1 text-sm text-muted-foreground mb-3 flex-wrap">
        {space ? (
          <Link to="/spaces/$spaceId" params={{ spaceId: space.id }} className="hover:text-foreground">
            {space.name}
          </Link>
        ) : null}
        <ChevronRight className="w-3.5 h-3.5" />
        <Link to="/lists/$listId" params={{ listId: t.list_id }} search={{}} className="hover:text-foreground">
          {t.list_name}
        </Link>
        {t.parent_key ? (
          <>
            <ChevronRight className="w-3.5 h-3.5" />
            <Link to="/t/$taskKey" params={{ taskKey: t.parent_key }} className="hover:text-foreground">
              {t.parent_key} {t.parent_title}
            </Link>
          </>
        ) : null}
        <ChevronRight className="w-3.5 h-3.5" />
        <span className="font-mono">{t.key}</span>
      </nav>

      <div className="grid gap-6 lg:grid-cols-[1fr_18rem] items-start">
        <div className="space-y-6 min-w-0">
          <input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            onBlur={() => title.trim() && title !== t.title && set({ title })}
            onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
            className="w-full bg-transparent font-heading text-2xl font-extrabold tracking-tight focus-visible:outline-none border-b border-transparent focus:border-border"
          />
          <div>
            <Label htmlFor="desc">Description</Label>
            <textarea
              id="desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              onBlur={() => description !== t.description && set({ description })}
              rows={Math.max(4, description.split('\n').length + 1)}
              placeholder="Add details…"
              className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:border-primary"
            />
          </div>

          <ChecklistSection task={t} refresh={refresh} />

          {!t.parent_id ? (
            <div>
              <h2 className="text-sm font-bold mb-2">
                Subtasks{' '}
                {t.subtask_count ? (
                  <span className="text-muted-foreground font-medium">
                    {t.subtask_done}/{t.subtask_count}
                  </span>
                ) : null}
              </h2>
              <Card className="divide-y divide-border">
                {t.subtasks.map((s) => (
                  <div key={s.id} className="flex items-center gap-3 px-4 py-2">
                    <StatusSelect
                      statuses={t.statuses}
                      value={s.status.id}
                      onChange={(id) => patch.mutate({ id: s.id, body: { status_id: id, version: s.version } })}
                      className="w-28 shrink-0"
                    />
                    <span className="text-xs text-muted-foreground font-mono">{s.key}</span>
                    <TaskLink task={s} className="flex-1 truncate text-sm" />
                    <PriorityFlag priority={s.priority} />
                    <DueBadge date={s.due_at} done={s.status.category === 'done'} />
                  </div>
                ))}
                <form
                  className="flex items-center gap-2 px-3 py-2"
                  onSubmit={(e) => {
                    e.preventDefault();
                    if (subtask.trim()) addSub.mutate(subtask.trim());
                  }}
                >
                  <Plus className="w-4 h-4 text-muted-foreground" />
                  <input
                    value={subtask}
                    onChange={(e) => setSubtask(e.target.value)}
                    placeholder="Add a subtask, then Enter"
                    className="flex-1 bg-transparent text-sm focus-visible:outline-none"
                  />
                </form>
              </Card>
            </div>
          ) : null}

          <AttachmentsSection task={t} refresh={refresh} />

          <div>
            <h2 className="text-sm font-bold mb-2">Activity</h2>
            {hidden > 0 && !(fullHistory && history.data) ? (
              <button
                type="button"
                disabled={history.isFetching}
                onClick={() => setFullHistory(true)}
                className="mb-3 text-xs font-semibold text-primary hover:underline disabled:opacity-50"
              >
                {history.isFetching ? 'Loading…' : `Show ${hidden} older entr${hidden === 1 ? 'y' : 'ies'}`}
              </button>
            ) : null}
            <div className="space-y-3">
              {timeline.map((item) =>
                item.comment ? (
                  <Card key={item.comment.id}>
                    <CardContent className="py-3">
                      <div className="flex items-center gap-2 text-sm">
                        <span className="font-semibold">{item.comment.author_name}</span>
                        <span className="text-xs text-muted-foreground">{timeAgo(item.comment.created_at)}</span>
                        {item.comment.edited_at ? <span className="text-xs text-muted-foreground">(edited)</span> : null}
                        {item.comment.author_id === me?.id ? (
                          <span className="ml-auto flex items-center gap-2">
                            <button
                              type="button"
                              className="text-muted-foreground hover:text-foreground"
                              onClick={() => setEditingComment({ id: item.comment!.id, body: item.comment!.body })}
                              title="Edit comment"
                            >
                              <Pencil className="w-3.5 h-3.5" />
                            </button>
                            <button
                              type="button"
                              className="text-muted-foreground hover:text-destructive"
                              onClick={() => removeComment.mutate(item.comment!.id)}
                              title="Delete comment"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </span>
                        ) : null}
                      </div>
                      {editingComment?.id === item.comment.id ? (
                        <div className="mt-2 space-y-2">
                          <textarea
                            value={editingComment.body}
                            onChange={(e) => setEditingComment({ id: item.comment!.id, body: e.target.value })}
                            rows={3}
                            className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:border-primary"
                            autoFocus
                          />
                          <div className="flex gap-2">
                            <Button
                              size="sm"
                              disabled={!editingComment.body.trim() || saveComment.isPending}
                              onClick={() => saveComment.mutate({ id: editingComment.id, body: editingComment.body })}
                            >
                              Save
                            </Button>
                            <Button size="sm" variant="ghost" onClick={() => setEditingComment(null)}>
                              Cancel
                            </Button>
                          </div>
                        </div>
                      ) : (
                        <p className="text-sm mt-1 whitespace-pre-wrap">{item.comment.body}</p>
                      )}
                    </CardContent>
                  </Card>
                ) : (
                  <div key={item.event!.id} className="text-xs text-muted-foreground pl-4">
                    <span className="font-semibold text-foreground/80">{item.event!.actor_name}</span> {describe(item.event!)} ·{' '}
                    {timeAgo(item.event!.created_at)}
                  </div>
                )
              )}
            </div>
            <form
              className="mt-4 space-y-2"
              onSubmit={(e) => {
                e.preventDefault();
                if (comment.trim()) postComment.mutate();
              }}
            >
              <textarea
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && (e.ctrlKey || e.metaKey) && comment.trim()) postComment.mutate();
                }}
                rows={3}
                placeholder="Write a comment… @Full Name to notify someone (Ctrl+Enter to send)"
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:border-primary"
              />
              <Button type="submit" size="sm" disabled={!comment.trim() || postComment.isPending}>
                Comment
              </Button>
            </form>
          </div>
        </div>

        <Card className="lg:sticky lg:top-6">
          <CardContent className="space-y-4">
            <Button
              variant={watching ? 'secondary' : 'outline'}
              size="sm"
              className="w-full"
              disabled={toggleWatch.isPending}
              onClick={() => toggleWatch.mutate(watching)}
            >
              {watching ? <BellOff className="w-4 h-4" /> : <Bell className="w-4 h-4" />}
              {watching ? 'Stop watching' : 'Watch'}
            </Button>
            {t.watchers.length > 0 ? (
              <div className="flex items-center gap-1 flex-wrap">
                <span className="text-xs text-muted-foreground mr-1">Watching:</span>
                {t.watchers.map((w) => (
                  <Avatar key={w.id} person={w} />
                ))}
              </div>
            ) : null}
            <div>
              <Label>Status</Label>
              <StatusSelect statuses={t.statuses} value={t.status.id} onChange={(id) => set({ status_id: id })} className="w-full py-1.5" />
            </div>
            <div>
              <Label>Assignees</Label>
              <AssigneePicker task={t} onChange={(ids) => set({ assignee_ids: ids })} />
            </div>
            <div>
              <Label htmlFor="due">Due</Label>
              <input
                id="due"
                type="date"
                value={t.due_at ?? ''}
                onChange={(e) => set({ due_at: e.target.value || null })}
                className="w-full h-9 rounded-md border border-input bg-background px-2 text-sm"
              />
            </div>
            <div>
              <Label>Tags</Label>
              <TagPicker task={t} onChange={(ids) => set({ tag_ids: ids })} />
            </div>

            {!showMore ? (
              <button
                type="button"
                onClick={() => setMoreFields(true)}
                className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
              >
                <ChevronDown className="w-3.5 h-3.5" /> More fields
              </button>
            ) : (
              <>
                <div>
                  <Label htmlFor="prio">Priority</Label>
                  <select
                    id="prio"
                    value={t.priority ?? ''}
                    onChange={(e) => set({ priority: (e.target.value || null) as TaskPatch['priority'] })}
                    className="w-full h-9 rounded-md border border-input bg-background px-2 text-sm capitalize"
                  >
                    <option value="">None</option>
                    {PRIORITIES.map((p) => (
                      <option key={p} value={p}>
                        {p}
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <Label htmlFor="start">Start</Label>
                  <input
                    id="start"
                    type="date"
                    value={t.start_at ?? ''}
                    onChange={(e) => set({ start_at: e.target.value || null })}
                    className="w-full h-9 rounded-md border border-input bg-background px-2 text-sm"
                  />
                </div>
              </>
            )}

            {!t.parent_id && space ? (
              <div>
                <Label htmlFor="list">List</Label>
                <select
                  id="list"
                  value={t.list_id}
                  onChange={(e) => set({ list_id: e.target.value })}
                  className="w-full h-9 rounded-md border border-input bg-background px-2 text-sm"
                >
                  {(spaces.data ?? []).map((sp) =>
                    sp.can_edit ? (
                      <optgroup key={sp.id} label={sp.name}>
                        {sp.lists.map((l) => (
                          <option key={l.id} value={l.id}>
                            {l.name}
                          </option>
                        ))}
                      </optgroup>
                    ) : null
                  )}
                </select>
                {spaces.data && spaces.data.some((sp) => sp.id !== t.space_id && sp.can_edit) ? (
                  <p className="text-[11px] text-muted-foreground mt-1">
                    Moving to another space renumbers the task; old links keep working.
                  </p>
                ) : null}
              </div>
            ) : null}
            {patch.error ? (
              <p className="text-sm text-destructive">
                {patch.error instanceof ApiError ? patch.error.message : 'Could not save.'}
              </p>
            ) : null}
            <div className="pt-2 border-t border-border text-xs text-muted-foreground space-y-1">
              <div>Created {timeAgo(t.created_at)}</div>
              <div>Updated {timeAgo(t.updated_at)}</div>
              {t.completed_at ? <div>Completed {timeAgo(t.completed_at)}</div> : null}
            </div>
            <Button
              variant="outline"
              size="sm"
              className="w-full text-destructive"
              disabled={remove.isPending}
              onClick={() => {
                const extra = t.subtask_count ? ` and its ${t.subtask_count} subtasks` : '';
                if (confirm(`Move ${t.key}${extra} to the trash? A space manager can restore it.`)) remove.mutate();
              }}
            >
              <Trash2 className="w-4 h-4" /> Delete task
            </Button>
          </CardContent>
        </Card>
      </div>
    </PageContainer>
  );
}
