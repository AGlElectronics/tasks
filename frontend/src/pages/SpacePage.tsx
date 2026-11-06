import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { Archive, ArchiveRestore, ArrowDown, ArrowUp, Folder as FolderIcon, Lock, Plus, Star, Tag as TagIcon, Trash2, Users } from 'lucide-react';
import { ApiError } from '@/api/client';
import {
  addFavorite,
  archivedLists,
  createFolder,
  createList,
  createTag,
  deleteFolder,
  deleteList,
  deleteSpace,
  deleteTag,
  getSpace,
  listFavorites,
  listTags,
  purgeTask,
  removeFavorite,
  replaceStatuses,
  restoreTask,
  spaceTrash,
  updateFolder,
  updateList,
  updateSpace,
  updateTag,
  type AccessLevel,
  type Category,
  type Space,
  type StatusInput,
} from '@/api/tasks';
import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { notify } from '@/lib/notify';
import { Input, Label } from '@/components/ui/Input';
import { PageContainer, PageHeader } from '@/components/ui/PageHeader';
import { ErrorRow, LoadingRow } from '@/components/ui/States';
import { ShareDialog } from '@/components/ShareDialog';

function errText(err: unknown, fallback: string) {
  return err instanceof ApiError ? err.message : fallback;
}

function ListsCard({ space }: { space: Space }) {
  const qc = useQueryClient();
  const [name, setName] = useState('');
  const [shareList, setShareList] = useState<{ id: string; name: string } | null>(null);
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['spaces'] });
    qc.invalidateQueries({ queryKey: ['space', space.id] });
  };
  const add = useMutation({
    mutationFn: () => createList(space.id, { name }),
    onSuccess: () => {
      setName('');
      invalidate();
    },
  });
  const remove = useMutation({ mutationFn: (id: string) => deleteList(id), onSuccess: invalidate });
  const togglePrivate = useMutation({
    mutationFn: ({ id, next }: { id: string; next: boolean }) => updateList(id, { private: next }),
    onSuccess: invalidate,
  });
  const archive = useMutation({
    mutationFn: (id: string) => updateList(id, { archived: true }),
    onSuccess: invalidate,
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Lists</CardTitle>
      </CardHeader>
      <div className="divide-y divide-border">
        {space.lists.map((l) => {
          const total = l.open_tasks + l.done_tasks;
          return (
            <div key={l.id} className="flex items-center gap-3 px-5 py-3">
              <Link to="/lists/$listId" params={{ listId: l.id }} className="flex-1 min-w-0 font-medium hover:text-primary">
                {l.name}
                {l.private ? <Lock className="inline w-3 h-3 ml-1.5 text-muted-foreground" /> : null}
              </Link>
              <span className="text-xs text-muted-foreground w-28 text-right">
                {l.open_tasks} open · {l.done_tasks} done
              </span>
              <div className="w-24 h-1.5 rounded-full bg-secondary overflow-hidden">
                <div
                  className="h-full rounded-full"
                  style={{ width: total ? `${(l.done_tasks / total) * 100}%` : '0%', backgroundColor: space.color }}
                />
              </div>
              {space.can_manage ? (
                <>
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-foreground"
                    title="Share this list (the other team sees only this list)"
                    onClick={() => setShareList({ id: l.id, name: l.name })}
                  >
                    <Users className="w-4 h-4" />
                  </button>
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-foreground"
                    title={l.private ? 'Private: only shared people see it. Click to open it to the space.' : 'Visible to the space. Click to make it private.'}
                    onClick={() => togglePrivate.mutate({ id: l.id, next: !l.private })}
                  >
                    <Lock className={l.private ? 'w-4 h-4 text-amber-500' : 'w-4 h-4'} />
                  </button>
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-foreground disabled:opacity-30"
                    disabled={space.lists.length <= 1 || archive.isPending}
                    title={space.lists.length <= 1 ? 'A space needs at least one list' : 'Archive this list (tasks are kept, hidden from the sidebar)'}
                    onClick={() => archive.mutate(l.id)}
                  >
                    <Archive className="w-4 h-4" />
                  </button>
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-destructive disabled:opacity-30"
                    disabled={space.lists.length <= 1 || remove.isPending}
                    title={space.lists.length <= 1 ? 'A space needs at least one list' : 'Delete list and its tasks'}
                    onClick={() => {
                      if (confirm(`Delete "${l.name}" and its ${total} tasks?`)) remove.mutate(l.id);
                    }}
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </>
              ) : null}
            </div>
          );
        })}
      </div>
      {shareList ? (
        <ShareDialog scope={{ listId: shareList.id }} title={`list "${shareList.name}"`} onClose={() => setShareList(null)} />
      ) : null}
      {space.can_manage ? (
        <CardContent className="border-t border-border">
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (name.trim()) add.mutate();
            }}
          >
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="New list name" />
            <Button type="submit" disabled={!name.trim() || add.isPending}>
              <Plus className="w-4 h-4" /> Add list
            </Button>
          </form>
          {add.error ? <p className="text-sm text-destructive mt-2">{errText(add.error, 'Could not add the list.')}</p> : null}
        </CardContent>
      ) : null}
    </Card>
  );
}

const CATEGORY_LABEL: Record<Category, string> = {
  not_started: 'Not started',
  active: 'Active',
  done: 'Done',
};

const EVERYONE_LABEL: Record<string, string> = {
  '': 'No access (closed space)',
  view: 'Can view',
  edit: 'Can edit',
  manage: 'Can manage',
};

function SharingCard({ space }: { space: Space }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const everyone = useMutation({
    mutationFn: (level: AccessLevel | null) => updateSpace(space.id, { everyone_level: level }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['spaces'] });
      qc.invalidateQueries({ queryKey: ['space', space.id] });
    },
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Sharing</CardTitle>
        <CardDescription>
          Who can use this space. Share a single list instead when another team only needs that list.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div>
          <Label htmlFor="sp-everyone">Everyone in the organization</Label>
          <select
            id="sp-everyone"
            value={space.everyone_level ?? ''}
            onChange={(e) => everyone.mutate((e.target.value || null) as AccessLevel | null)}
            className="w-full h-9 rounded-md border border-input bg-background px-2 text-sm"
          >
            {Object.entries(EVERYONE_LABEL).map(([v, label]) => (
              <option key={v} value={v}>
                {label}
              </option>
            ))}
          </select>
        </div>
        <Button type="button" variant="secondary" onClick={() => setOpen(true)}>
          <Users className="w-4 h-4" /> People and groups
        </Button>
        {everyone.error ? (
          <p className="text-sm text-destructive">{errText(everyone.error, 'Could not change access.')}</p>
        ) : null}
      </CardContent>
      {open ? <ShareDialog scope={{ spaceId: space.id }} title={`"${space.name}"`} onClose={() => setOpen(false)} /> : null}
    </Card>
  );
}

function StatusesCard({ space }: { space: Space }) {
  const qc = useQueryClient();
  const [rows, setRows] = useState<StatusInput[]>([]);
  useEffect(() => {
    setRows(space.statuses.map((s) => ({ id: s.id, name: s.name, color: s.color, category: s.category })));
  }, [space.statuses]);

  const save = useMutation({
    mutationFn: () => replaceStatuses(space.id, rows),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['spaces'] });
      qc.invalidateQueries({ queryKey: ['space', space.id] });
      qc.invalidateQueries({ queryKey: ['tasks'] });
    },
  });

  const set = (i: number, patch: Partial<StatusInput>) =>
    setRows((r) => r.map((row, j) => (j === i ? { ...row, ...patch } : row)));
  const move = (i: number, d: number) =>
    setRows((r) => {
      const next = [...r];
      const [x] = next.splice(i, 1);
      next.splice(i + d, 0, x);
      return next;
    });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Statuses</CardTitle>
        <CardDescription>
          Board columns, in order. Tasks on a removed status move to another status of the same kind.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-2">
        {rows.map((row, i) => (
          <div key={row.id ?? `new-${i}`} className="flex items-center gap-2">
            <input
              type="color"
              value={row.color}
              onChange={(e) => set(i, { color: e.target.value })}
              className="w-9 h-9 rounded border border-border bg-background p-1"
            />
            <Input value={row.name} onChange={(e) => set(i, { name: e.target.value })} className="flex-1" />
            <select
              value={row.category}
              onChange={(e) => set(i, { category: e.target.value as Category })}
              className="h-9 rounded-md border border-input bg-background px-2 text-sm"
            >
              {(Object.keys(CATEGORY_LABEL) as Category[]).map((c) => (
                <option key={c} value={c}>
                  {CATEGORY_LABEL[c]}
                </option>
              ))}
            </select>
            <Button type="button" variant="ghost" size="icon" disabled={i === 0} onClick={() => move(i, -1)}>
              <ArrowUp className="w-4 h-4" />
            </Button>
            <Button type="button" variant="ghost" size="icon" disabled={i === rows.length - 1} onClick={() => move(i, 1)}>
              <ArrowDown className="w-4 h-4" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              disabled={rows.length <= 2}
              onClick={() => setRows((r) => r.filter((_, j) => j !== i))}
            >
              <Trash2 className="w-4 h-4" />
            </Button>
          </div>
        ))}
        <div className="flex gap-2 pt-2">
          <Button
            type="button"
            variant="secondary"
            onClick={() => setRows((r) => [...r, { name: 'New status', color: '#64748b', category: 'active' }])}
          >
            <Plus className="w-4 h-4" /> Add status
          </Button>
          <Button type="button" onClick={() => save.mutate()} disabled={save.isPending}>
            Save statuses
          </Button>
        </div>
        {save.error ? <p className="text-sm text-destructive">{errText(save.error, 'Could not save statuses.')}</p> : null}
        {save.isSuccess ? <p className="text-sm text-muted-foreground">Saved.</p> : null}
      </CardContent>
    </Card>
  );
}

function SpaceSettingsCard({ space }: { space: Space }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState(space.name);
  const [key, setKey] = useState(space.key);
  const [color, setColor] = useState(space.color);
  const [description, setDescription] = useState(space.description);
  useEffect(() => {
    setName(space.name);
    setKey(space.key);
    setColor(space.color);
    setDescription(space.description);
  }, [space]);

  const save = useMutation({
    mutationFn: () => updateSpace(space.id, { name, key, color, description }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['spaces'] });
      qc.invalidateQueries({ queryKey: ['space', space.id] });
    },
  });
  const remove = useMutation({
    mutationFn: () => deleteSpace(space.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['spaces'] });
      navigate({ to: '/spaces' });
    },
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Space settings</CardTitle>
      </CardHeader>
      <CardContent>
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <div className="grid gap-3 sm:grid-cols-[1fr_7rem_4rem]">
            <div>
              <Label htmlFor="sp-name">Name</Label>
              <Input id="sp-name" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="sp-key">Key</Label>
              <Input id="sp-key" value={key} maxLength={6} onChange={(e) => setKey(e.target.value.toUpperCase())} />
            </div>
            <div>
              <Label htmlFor="sp-color">Colour</Label>
              <input
                id="sp-color"
                type="color"
                value={color}
                onChange={(e) => setColor(e.target.value)}
                className="w-full h-9 rounded border border-border bg-background p-1"
              />
            </div>
          </div>
          <div>
            <Label htmlFor="sp-desc">Description</Label>
            <textarea
              id="sp-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={2}
              className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
            />
          </div>
          {key !== space.key ? (
            <p className="text-xs text-amber-600">Changing the key renames every task ID in this space.</p>
          ) : null}
          {save.error ? <p className="text-sm text-destructive">{errText(save.error, 'Could not save.')}</p> : null}
          <div className="flex justify-between">
            <Button type="submit" disabled={save.isPending}>
              Save
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => {
                if (confirm(`Delete the space "${space.name}" with every list and task in it? This cannot be undone.`))
                  remove.mutate();
              }}
            >
              <Trash2 className="w-4 h-4" /> Delete space
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

/** Space tags (TASK-4): shared labels tasks in this space can wear. */
function TagsCard({ space }: { space: Space }) {
  const qc = useQueryClient();
  const tags = useQuery({ queryKey: ['tags', space.id], queryFn: ({ signal }) => listTags(space.id, signal) });
  const [name, setName] = useState('');
  const [color, setColor] = useState('#6366f1');
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['tags', space.id] });
    qc.invalidateQueries({ queryKey: ['tasks'] });
    qc.invalidateQueries({ queryKey: ['task'] });
  };
  const add = useMutation({
    mutationFn: () => createTag(space.id, { name: name.trim(), color }),
    onSuccess: () => {
      setName('');
      invalidate();
    },
  });
  const recolor = useMutation({
    mutationFn: ({ id, c }: { id: string; c: string }) => updateTag(space.id, id, { color: c }),
    onSuccess: invalidate,
  });
  const remove = useMutation({ mutationFn: (id: string) => deleteTag(space.id, id), onSuccess: invalidate });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Tags</CardTitle>
        <CardDescription>Labels anyone with edit access can put on tasks in this space.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-2">
        {(tags.data ?? []).map((tg) => (
          <div key={tg.id} className="flex items-center gap-2">
            <input
              type="color"
              value={tg.color}
              onChange={(e) => recolor.mutate({ id: tg.id, c: e.target.value })}
              className="w-9 h-9 rounded border border-border bg-background p-1"
            />
            <span
              className="inline-flex rounded-pill px-2 py-0.5 text-xs font-semibold text-white"
              style={{ backgroundColor: tg.color }}
            >
              {tg.name}
            </span>
            <button
              type="button"
              className="ml-auto text-muted-foreground hover:text-destructive"
              title="Delete tag (removed from every task)"
              onClick={() => confirm(`Delete the tag "${tg.name}"? It is removed from every task.`) && remove.mutate(tg.id)}
            >
              <Trash2 className="w-4 h-4" />
            </button>
          </div>
        ))}
        <form
          className="flex gap-2 pt-1"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) add.mutate();
          }}
        >
          <input
            type="color"
            value={color}
            onChange={(e) => setColor(e.target.value)}
            className="w-9 h-9 rounded border border-border bg-background p-1 shrink-0"
          />
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="New tag name" maxLength={50} />
          <Button type="submit" disabled={!name.trim() || add.isPending}>
            <TagIcon className="w-4 h-4" /> Add
          </Button>
        </form>
        {add.error ? <p className="text-sm text-destructive">{errText(add.error, 'Could not add the tag.')}</p> : null}
      </CardContent>
    </Card>
  );
}

/** Folders (TASK-9): sidebar grouping for lists; drag lists onto them. */
function FoldersCard({ space }: { space: Space }) {
  const qc = useQueryClient();
  const [name, setName] = useState('');
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['spaces'] });
    qc.invalidateQueries({ queryKey: ['space', space.id] });
  };
  const add = useMutation({
    mutationFn: () => createFolder(space.id, name.trim()),
    onSuccess: () => {
      setName('');
      invalidate();
    },
  });
  const rename = useMutation({
    mutationFn: ({ id, n }: { id: string; n: string }) => updateFolder(id, { name: n }),
    onSuccess: invalidate,
  });
  const remove = useMutation({ mutationFn: (id: string) => deleteFolder(id), onSuccess: invalidate });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Folders</CardTitle>
        <CardDescription>Group lists in the sidebar — drag a list onto a folder there.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-2">
        {space.folders.map((f) => (
          <div key={f.id} className="flex items-center gap-2">
            <FolderIcon className="w-4 h-4 text-muted-foreground shrink-0" />
            <Input
              defaultValue={f.name}
              onBlur={(e) => {
                const n = e.target.value.trim();
                if (n && n !== f.name) rename.mutate({ id: f.id, n });
              }}
              className="flex-1"
            />
            <button
              type="button"
              className="text-muted-foreground hover:text-destructive"
              title="Delete folder (its lists stay, back at the space root)"
              onClick={() => remove.mutate(f.id)}
            >
              <Trash2 className="w-4 h-4" />
            </button>
          </div>
        ))}
        <form
          className="flex gap-2 pt-1"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) add.mutate();
          }}
        >
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="New folder name" />
          <Button type="submit" disabled={!name.trim() || add.isPending}>
            <Plus className="w-4 h-4" /> Add
          </Button>
        </form>
        {add.error ? <p className="text-sm text-destructive">{errText(add.error, 'Could not add the folder.')}</p> : null}
      </CardContent>
    </Card>
  );
}

/** Archived lists + trashed tasks (TASK-48): everything restorable in one place. */
function RestoreCard({ space }: { space: Space }) {
  const qc = useQueryClient();
  const archived = useQuery({
    queryKey: ['archived-lists', space.id],
    queryFn: ({ signal }) => archivedLists(space.id, signal),
  });
  const trash = useQuery({ queryKey: ['trash', space.id], queryFn: ({ signal }) => spaceTrash(space.id, signal) });
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['archived-lists', space.id] });
    qc.invalidateQueries({ queryKey: ['trash', space.id] });
    qc.invalidateQueries({ queryKey: ['spaces'] });
    qc.invalidateQueries({ queryKey: ['space', space.id] });
    qc.invalidateQueries({ queryKey: ['tasks'] });
  };
  const unarchive = useMutation({ mutationFn: (id: string) => updateList(id, { archived: false }), onSuccess: invalidate });
  const restore = useMutation({
    mutationFn: (id: string) => restoreTask(id),
    onSuccess: invalidate,
    onError: (e) => notifyErr(e, 'Could not restore the task.'),
  });
  const purge = useMutation({ mutationFn: (id: string) => purgeTask(id), onSuccess: invalidate });

  const hasArchived = (archived.data ?? []).length > 0;
  const hasTrash = (trash.data ?? []).length > 0;
  if (!hasArchived && !hasTrash) return null;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Archive and trash</CardTitle>
        <CardDescription>Archived lists and deleted tasks can be brought back from here.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {hasArchived ? (
          <div className="space-y-2">
            <h3 className="text-xs font-bold uppercase tracking-wide text-muted-foreground">Archived lists</h3>
            {archived.data!.map((l) => (
              <div key={l.id} className="flex items-center gap-2 text-sm">
                <Archive className="w-4 h-4 text-muted-foreground shrink-0" />
                <span className="flex-1 truncate">{l.name}</span>
                <span className="text-xs text-muted-foreground">{l.open_tasks + l.done_tasks} tasks</span>
                <Button variant="secondary" size="sm" disabled={unarchive.isPending} onClick={() => unarchive.mutate(l.id)}>
                  <ArchiveRestore className="w-3.5 h-3.5" /> Restore
                </Button>
              </div>
            ))}
          </div>
        ) : null}
        {hasTrash ? (
          <div className="space-y-2">
            <h3 className="text-xs font-bold uppercase tracking-wide text-muted-foreground">Trashed tasks</h3>
            {trash.data!.map((t) => (
              <div key={t.id} className="flex items-center gap-2 text-sm">
                <span className="font-mono text-xs text-muted-foreground shrink-0">{t.key}</span>
                <span className="flex-1 truncate">{t.title}</span>
                <span className="text-xs text-muted-foreground shrink-0">from {t.list_name}</span>
                <Button variant="secondary" size="sm" disabled={restore.isPending} onClick={() => restore.mutate(t.id)}>
                  Restore
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="text-destructive"
                  disabled={purge.isPending}
                  onClick={() => confirm(`Permanently delete ${t.key}? This cannot be undone.`) && purge.mutate(t.id)}
                >
                  Purge
                </Button>
              </div>
            ))}
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function notifyErr(e: unknown, fallback: string) {
  notify(e instanceof ApiError ? e.message : fallback);
}

/** Star in the page header: pin or unpin this space in the sidebar (TASK-11). */
function FavoriteStar({ spaceId }: { spaceId: string }) {
  const qc = useQueryClient();
  const favs = useQuery({ queryKey: ['favorites'], queryFn: ({ signal }) => listFavorites(signal) });
  const pinned = (favs.data ?? []).some((f) => f.space_id === spaceId);
  const toggle = useMutation({
    mutationFn: () => (pinned ? removeFavorite({ space_id: spaceId }) : addFavorite({ space_id: spaceId })),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['favorites'] }),
  });
  return (
    <Button
      variant="ghost"
      size="icon"
      disabled={toggle.isPending || favs.isLoading}
      onClick={() => toggle.mutate()}
      title={pinned ? 'Remove from favourites' : 'Add to favourites'}
    >
      <Star className={pinned ? 'w-5 h-5 fill-amber-400 text-amber-400' : 'w-5 h-5 text-muted-foreground'} />
    </Button>
  );
}

export function SpacePage() {
  const { spaceId } = useParams({ strict: false }) as { spaceId: string };
  const space = useQuery({ queryKey: ['space', spaceId], queryFn: ({ signal }) => getSpace(spaceId, signal) });


  if (space.isLoading) return <LoadingRow />;
  if (space.isError || !space.data) return <ErrorRow message="Space not found." />;
  const sp = space.data;

  return (
    <PageContainer>
      <PageHeader
        title={sp.name}
        description={sp.description || `Task IDs: ${sp.key}-1, ${sp.key}-2, …`}
        actions={<FavoriteStar spaceId={sp.id} />}
      />
      <div className="grid gap-6 lg:grid-cols-2 items-start">
        <div className="space-y-6">
          <ListsCard space={sp} />
          {sp.can_manage ? <RestoreCard space={sp} /> : null}
        </div>
        {sp.can_manage ? (
          <div className="space-y-6">
            <SharingCard space={sp} />
            <TagsCard space={sp} />
            <FoldersCard space={sp} />
            <StatusesCard space={sp} />
            <SpaceSettingsCard space={sp} />
          </div>
        ) : null}
      </div>
    </PageContainer>
  );
}
