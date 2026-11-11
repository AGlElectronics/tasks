import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { LayoutGrid, Plus } from 'lucide-react';
import { ApiError } from '@/api/client';
import { createSpace, listSpaces } from '@/api/tasks';
import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Input, Label } from '@/components/ui/Input';
import { PageContainer, PageHeader } from '@/components/ui/PageHeader';
import { EmptyState, ErrorRow, LoadingRow } from '@/components/ui/States';
import { PERMS, useMe } from '@/hooks/useMe';

const COLORS = ['#6366f1', '#0ea5e9', '#22c55e', '#f59e0b', '#ec4899', '#14b8a6', '#a855f7', '#ef4444'];

function suggestKey(name: string) {
  const words = name.toUpperCase().replace(/[^A-Z0-9 ]/g, '').split(/\s+/).filter(Boolean);
  if (words.length === 0) return '';
  if (words.length === 1) return words[0].slice(0, 4);
  return words.map((w) => w[0]).join('').slice(0, 6);
}

function NewSpaceForm({ onDone }: { onDone: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState('');
  const [key, setKey] = useState('');
  const [keyTouched, setKeyTouched] = useState(false);
  const [color, setColor] = useState(COLORS[0]);
  const effectiveKey = keyTouched ? key : suggestKey(name);

  const create = useMutation({
    mutationFn: () => createSpace({ name, key: effectiveKey, color }),
    onSuccess: (sp) => {
      qc.invalidateQueries({ queryKey: ['spaces'] });
      onDone();
      navigate({ to: '/spaces/$spaceId', params: { spaceId: sp.id } });
    },
  });

  return (
    <Card className="mb-6">
      <CardHeader>
        <CardTitle>New space</CardTitle>
      </CardHeader>
      <CardContent>
        <form
          className="grid gap-4 sm:grid-cols-[1fr_8rem]"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <div>
            <Label htmlFor="space-name">Name</Label>
            <Input id="space-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Security team" autoFocus />
          </div>
          <div>
            <Label htmlFor="space-key">Key</Label>
            <Input
              id="space-key"
              value={effectiveKey}
              onChange={(e) => {
                setKeyTouched(true);
                setKey(e.target.value.toUpperCase());
              }}
              maxLength={6}
              placeholder="SEC"
            />
          </div>
          <div className="sm:col-span-2">
            <Label>Colour</Label>
            <div className="flex gap-2">
              {COLORS.map((c) => (
                <button
                  key={c}
                  type="button"
                  onClick={() => setColor(c)}
                  className={`w-7 h-7 rounded-full ${color === c ? 'ring-2 ring-offset-2 ring-ring ring-offset-background' : ''}`}
                  style={{ backgroundColor: c }}
                  aria-label={c}
                />
              ))}
            </div>
            <p className="text-xs text-muted-foreground mt-2">
              Task IDs in this space look like {effectiveKey || 'KEY'}-1. New spaces get To do, In progress, Review and Done
              statuses and a General list.
            </p>
          </div>
          {create.error ? (
            <p className="sm:col-span-2 text-sm text-destructive">
              {create.error instanceof ApiError ? create.error.message : 'Could not create the space.'}
            </p>
          ) : null}
          <div className="sm:col-span-2 flex gap-2">
            <Button type="submit" disabled={!name.trim() || !effectiveKey || create.isPending}>
              Create space
            </Button>
            <Button type="button" variant="secondary" onClick={onDone}>
              Cancel
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

export function SpacesPage() {
  const { canAny } = useMe();
  const canCreate = canAny(PERMS.spaceCreate, PERMS.spaceManage);
  const [creating, setCreating] = useState(false);
  const [showArchived, setShowArchived] = useState(false);
  const spaces = useQuery({ queryKey: ['spaces'], queryFn: ({ signal }) => listSpaces(signal) });
  const archived = useQuery({
    queryKey: ['spaces', 'archived'],
    queryFn: ({ signal }) => listSpaces(signal, true).then((all) => all.filter((sp) => sp.archived)),
    enabled: showArchived,
  });

  return (
    <PageContainer>
      <PageHeader
        title="Spaces"
        description="Each space holds lists of tasks for a team or project."
        actions={
          canCreate && !creating ? (
            <Button onClick={() => setCreating(true)}>
              <Plus className="w-4 h-4" /> New space
            </Button>
          ) : null
        }
      />
      {creating ? <NewSpaceForm onDone={() => setCreating(false)} /> : null}
      {spaces.isLoading ? (
        <LoadingRow />
      ) : spaces.isError ? (
        <ErrorRow message="Could not load spaces." />
      ) : spaces.data!.length === 0 ? (
        <Card>
          <EmptyState
            icon={LayoutGrid}
            title="No spaces yet"
            description={canCreate ? 'Create the first space to start tracking work.' : 'Ask a Tasks admin to create a space.'}
          />
        </Card>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {spaces.data!.map((sp) => {
            const open = sp.lists.reduce((n, l) => n + l.open_tasks, 0);
            const done = sp.lists.reduce((n, l) => n + l.done_tasks, 0);
            const total = open + done;
            return (
              <Link key={sp.id} to="/spaces/$spaceId" params={{ spaceId: sp.id }}>
                <Card clickable className="p-5 h-full">
                  <div className="flex items-center gap-3">
                    <span
                      className="inline-flex items-center justify-center w-10 h-10 rounded-lg text-sm font-bold text-white"
                      style={{ backgroundColor: sp.color }}
                    >
                      {sp.key.slice(0, 3)}
                    </span>
                    <div className="min-w-0">
                      <div className="font-heading font-bold truncate">{sp.name}</div>
                      <div className="text-xs text-muted-foreground">
                        {sp.lists.length} {sp.lists.length === 1 ? 'list' : 'lists'} · {open} open
                      </div>
                    </div>
                  </div>
                  {sp.description ? <p className="text-sm text-muted-foreground mt-3 line-clamp-2">{sp.description}</p> : null}
                  <div className="mt-4 h-1.5 rounded-full bg-secondary overflow-hidden">
                    <div
                      className="h-full rounded-full"
                      style={{ width: total ? `${(done / total) * 100}%` : '0%', backgroundColor: sp.color }}
                    />
                  </div>
                </Card>
              </Link>
            );
          })}
        </div>
      )}
      <div className="mt-8">
        <button
          type="button"
          onClick={() => setShowArchived((v) => !v)}
          className="text-xs font-semibold text-muted-foreground hover:text-foreground"
        >
          {showArchived ? 'Hide archived spaces' : 'Show archived spaces'}
        </button>
        {showArchived ? (
          archived.isLoading ? (
            <LoadingRow />
          ) : (archived.data ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground mt-2">No archived spaces.</p>
          ) : (
            <ul className="mt-2 space-y-1">
              {archived.data!.map((sp) => (
                <li key={sp.id} className="text-sm">
                  <Link to="/spaces/$spaceId" params={{ spaceId: sp.id }} className="hover:underline">
                    {sp.name}
                  </Link>{' '}
                  <span className="text-xs text-muted-foreground">
                    {sp.key} · archived{sp.department_id ? ' with its HR department' : ''}
                  </span>
                </li>
              ))}
            </ul>
          )
        ) : null}
      </div>
    </PageContainer>
  );
}
