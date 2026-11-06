// Sharing editor for a space or a single list (TASK-1). Grants give a
// person or an IdP group view, edit or manage access. Sharing one list is
// the cross-team path: the other team sees just that list, not the space.
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Trash2, X } from 'lucide-react';
import { ApiError } from '@/api/client';
import {
  deleteGrant,
  listGrants,
  listPeople,
  putGrant,
  type AccessLevel,
  type GrantScope,
} from '@/api/tasks';
import { Button } from '@/components/ui/Button';
import { Input, Label } from '@/components/ui/Input';

const LEVELS: AccessLevel[] = ['view', 'edit', 'manage'];

export function ShareDialog({
  scope,
  title,
  onClose,
}: {
  scope: GrantScope;
  title: string;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const key = ['grants', scope.spaceId ?? scope.listId];
  const grants = useQuery({ queryKey: key, queryFn: ({ signal }) => listGrants(scope, signal) });
  const [subjectType, setSubjectType] = useState<'user' | 'group'>('user');
  const [userId, setUserId] = useState('');
  const [group, setGroup] = useState('');
  const [level, setLevel] = useState<AccessLevel>('edit');
  const people = useQuery({ queryKey: ['people'], queryFn: ({ signal }) => listPeople(signal) });

  const refresh = () => {
    qc.invalidateQueries({ queryKey: key });
    qc.invalidateQueries({ queryKey: ['spaces'] });
  };
  const add = useMutation({
    mutationFn: () =>
      putGrant(scope, {
        subject_type: subjectType,
        subject: subjectType === 'user' ? userId : group.trim(),
        level,
      }),
    onSuccess: () => {
      setUserId('');
      setGroup('');
      refresh();
    },
  });
  const remove = useMutation({ mutationFn: (id: string) => deleteGrant(scope, id), onSuccess: refresh });

  return (
    <div className="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-4" onClick={onClose}>
      <div
        className="w-full max-w-lg rounded-card border border-border bg-card shadow-pop p-5 space-y-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 className="font-heading text-lg font-extrabold">Share {title}</h2>
          <button type="button" aria-label="Close" className="opacity-60 hover:opacity-100" onClick={onClose}>
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="space-y-1.5">
          {grants.isLoading ? <p className="text-sm text-muted-foreground">Loading…</p> : null}
          {grants.data?.length === 0 ? (
            <p className="text-sm text-muted-foreground">Not shared with anyone yet.</p>
          ) : null}
          {(grants.data ?? []).map((g) => (
            <div key={g.id} className="flex items-center gap-2 text-sm">
              <span className="flex-1 truncate">
                {g.display_name}
                <span className="text-xs text-muted-foreground ml-1.5">{g.subject_type === 'group' ? 'group' : ''}</span>
              </span>
              <span className="text-xs rounded-pill bg-secondary px-2 py-0.5 capitalize">{g.level}</span>
              <button
                type="button"
                className="text-muted-foreground hover:text-destructive"
                title="Remove access"
                onClick={() => remove.mutate(g.id)}
              >
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            </div>
          ))}
        </div>

        <form
          className="space-y-2 border-t border-border pt-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (subjectType === 'user' ? userId : group.trim()) add.mutate();
          }}
        >
          <div className="flex gap-2">
            <select
              value={subjectType}
              onChange={(e) => setSubjectType(e.target.value as 'user' | 'group')}
              className="h-9 rounded-md border border-input bg-background px-2 text-sm"
            >
              <option value="user">Person</option>
              <option value="group">Group</option>
            </select>
            {subjectType === 'user' ? (
              <select
                value={userId}
                onChange={(e) => setUserId(e.target.value)}
                className="h-9 flex-1 rounded-md border border-input bg-background px-2 text-sm"
              >
                <option value="">Choose a person…</option>
                {(people.data ?? []).map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.display_name}
                  </option>
                ))}
              </select>
            ) : (
              <Input
                value={group}
                onChange={(e) => setGroup(e.target.value)}
                placeholder="IdP group name, e.g. hr-logistics"
                className="flex-1"
              />
            )}
            <select
              value={level}
              onChange={(e) => setLevel(e.target.value as AccessLevel)}
              className="h-9 rounded-md border border-input bg-background px-2 text-sm capitalize"
            >
              {LEVELS.map((l) => (
                <option key={l} value={l}>
                  {l}
                </option>
              ))}
            </select>
            <Button type="submit" disabled={add.isPending || (subjectType === 'user' ? !userId : !group.trim())}>
              Share
            </Button>
          </div>
          <Label className="text-xs text-muted-foreground font-normal">
            People get access when they match either way; the strongest level wins.
          </Label>
          {add.error ? (
            <p className="text-sm text-destructive">
              {add.error instanceof ApiError ? add.error.message : 'Could not share.'}
            </p>
          ) : null}
        </form>
      </div>
    </div>
  );
}
