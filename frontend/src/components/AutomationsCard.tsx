// Automations builder (TASK-22). Rules read as a sentence: "When <trigger>,
// if <conditions>, then <actions>". Templates pre-fill the form for the
// common cases, and each rule shows its run history on demand.
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ChevronDown, ChevronRight, History, Plus, Trash2, Zap } from 'lucide-react';
import { ApiError } from '@/api/client';
import {
  PRIORITIES,
  automationRuns,
  createAutomation,
  deleteAutomation,
  listAutomations,
  listPeople,
  listTags,
  updateAutomation,
  type Automation,
  type AutomationAction,
  type AutomationCondition,
  type AutomationInput,
  type AutomationTrigger,
  type Space,
} from '@/api/tasks';
import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { Input, Label } from '@/components/ui/Input';
import { timeAgo } from '@/lib/format';
import { cn } from '@/lib/utils';

const TRIGGER_LABEL: Record<string, string> = {
  task_created: 'a task is created',
  status_changed: 'a task changes status',
  assignee_added: 'someone is assigned',
  comment_added: 'a comment is added',
  checklist_completed: 'the checklist is completed',
  due_approaching: 'the due date is approaching',
};

const ACTION_LABEL: Record<string, string> = {
  set_status: 'set status to',
  assign: 'assign',
  set_priority: 'set priority to',
  set_due: 'set due date in (days)',
  add_tag: 'add tag',
  move_list: 'move to list',
  create_subtask: 'create a subtask titled',
  add_checklist_item: 'add a checklist item',
  add_comment: 'add a comment',
  notify: 'notify followers',
};

const CONDITION_LABEL: Record<string, string> = {
  list: 'the list is',
  status: 'the status is',
  priority: 'the priority is',
  tag: 'it has the tag',
  assignee: 'it is assigned to',
};

type Template = { label: string; input: (space: Space) => AutomationInput | null };

const TEMPLATES: Template[] = [
  {
    label: 'When a task is done, tell its followers',
    input: (sp) => {
      const done = sp.statuses.find((s) => s.category === 'done');
      return done
        ? {
            name: 'Tell followers when a task is done',
            trigger: { type: 'status_changed', status_id: done.id },
            actions: [{ type: 'notify', message: 'This task was completed' }],
          }
        : null;
    },
  },
  {
    label: 'When a task is due tomorrow, raise priority to urgent',
    input: () => ({
      name: 'Due tomorrow → urgent',
      trigger: { type: 'due_approaching', days_before: 1 },
      actions: [{ type: 'set_priority', priority: 'urgent' }],
    }),
  },
  {
    label: 'When a task is created, add a triage checklist',
    input: () => ({
      name: 'Triage new tasks',
      trigger: { type: 'task_created' },
      actions: [{ type: 'add_checklist_item', body: 'Review and estimate' }],
    }),
  },
  {
    label: 'When the checklist is completed, mark the task done',
    input: (sp) => {
      const done = sp.statuses.find((s) => s.category === 'done');
      return done
        ? {
            name: 'Checklist done → task done',
            trigger: { type: 'checklist_completed' },
            actions: [{ type: 'set_status', status_id: done.id }],
          }
        : null;
    },
  },
];

const selCls = 'h-8 rounded-md border border-input bg-background px-2 text-xs';

function RuleSentence({ a, space }: { a: Automation; space: Space }) {
  const names = useLookups(space);
  const trig =
    a.trigger.type === 'status_changed' && a.trigger.status_id
      ? `a task moves to ${names.status(a.trigger.status_id)}`
      : a.trigger.type === 'due_approaching'
        ? `the due date is ${a.trigger.days_before ?? 1} day(s) away`
        : TRIGGER_LABEL[a.trigger.type] ?? a.trigger.type;
  const conds = a.conditions.map((c) => `${CONDITION_LABEL[c.field]} ${names.condValue(c)}`).join(' and ');
  const acts = a.actions.map((x) => actionSentence(x, names)).join(', then ');
  return (
    <span className="text-xs text-muted-foreground">
      When {trig}
      {conds ? `, if ${conds}` : ''}, then {acts}.
    </span>
  );
}

type Lookups = {
  status: (id?: string) => string;
  list: (id?: string) => string;
  tag: (id?: string) => string;
  person: (id?: string) => string;
  condValue: (c: AutomationCondition) => string;
};

function useLookups(space: Space): Lookups {
  const people = useQuery({ queryKey: ['people'], queryFn: ({ signal }) => listPeople(signal) });
  const tags = useQuery({ queryKey: ['tags', space.id], queryFn: ({ signal }) => listTags(space.id, signal) });
  const status = (id?: string) => space.statuses.find((s) => s.id === id)?.name ?? '?';
  const list = (id?: string) => space.lists.find((l) => l.id === id)?.name ?? '?';
  const tag = (id?: string) => (tags.data ?? []).find((t) => t.id === id)?.name ?? '?';
  const person = (id?: string) => (people.data ?? []).find((p) => p.id === id)?.display_name ?? '?';
  return {
    status,
    list,
    tag,
    person,
    condValue: (c) =>
      c.field === 'list' ? list(c.value) : c.field === 'status' ? status(c.value) : c.field === 'tag' ? tag(c.value) : c.field === 'assignee' ? person(c.value) : c.value,
  };
}

function actionSentence(x: AutomationAction, names: Lookups): string {
  switch (x.type) {
    case 'set_status':
      return `set status to ${names.status(x.status_id)}`;
    case 'assign':
      return `assign ${names.person(x.user_id)}`;
    case 'set_priority':
      return `set priority to ${x.priority}`;
    case 'set_due':
      return `set the due date ${x.days} day(s) out`;
    case 'add_tag':
      return `add the tag ${names.tag(x.tag_id)}`;
    case 'move_list':
      return `move it to ${names.list(x.list_id)}`;
    case 'create_subtask':
      return `create a subtask “${x.title}”`;
    case 'add_checklist_item':
      return `add checklist item “${x.body}”`;
    case 'add_comment':
      return `comment “${x.body}”`;
    case 'notify':
      return `notify followers “${x.message}”`;
    default:
      return x.type;
  }
}

function RunHistory({ automationId }: { automationId: string }) {
  const runs = useQuery({
    queryKey: ['automation-runs', automationId],
    queryFn: ({ signal }) => automationRuns(automationId, signal),
  });
  if (runs.isLoading) return <p className="text-xs text-muted-foreground pl-6 py-1">Loading runs…</p>;
  if (!runs.data || runs.data.length === 0)
    return <p className="text-xs text-muted-foreground pl-6 py-1">This rule has not run yet.</p>;
  return (
    <div className="pl-6 py-1 space-y-0.5">
      {runs.data.map((r) => (
        <div key={r.id} className="text-xs flex items-center gap-2">
          <span className={cn('w-1.5 h-1.5 rounded-full shrink-0', r.status === 'ok' ? 'bg-green-500' : 'bg-destructive')} />
          <span className="font-mono text-muted-foreground">{r.task_key}</span>
          <span className="truncate flex-1 text-muted-foreground">{r.detail}</span>
          <span className="text-muted-foreground shrink-0">{timeAgo(r.created_at)}</span>
        </div>
      ))}
    </div>
  );
}

/** One editable action row in the builder. */
function ActionRow({
  action,
  space,
  onChange,
  onRemove,
}: {
  action: AutomationAction;
  space: Space;
  onChange: (a: AutomationAction) => void;
  onRemove: () => void;
}) {
  const people = useQuery({ queryKey: ['people'], queryFn: ({ signal }) => listPeople(signal) });
  const tags = useQuery({ queryKey: ['tags', space.id], queryFn: ({ signal }) => listTags(space.id, signal) });
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-xs text-muted-foreground">then</span>
      <select
        value={action.type}
        onChange={(e) => onChange({ type: e.target.value as AutomationAction['type'] })}
        className={selCls}
      >
        {Object.entries(ACTION_LABEL).map(([v, label]) => (
          <option key={v} value={v}>
            {label}
          </option>
        ))}
      </select>
      {action.type === 'set_status' ? (
        <select value={action.status_id ?? ''} onChange={(e) => onChange({ ...action, status_id: e.target.value })} className={selCls}>
          <option value="">status…</option>
          {space.statuses.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      ) : null}
      {action.type === 'assign' ? (
        <select value={action.user_id ?? ''} onChange={(e) => onChange({ ...action, user_id: e.target.value })} className={selCls}>
          <option value="">person…</option>
          {(people.data ?? []).map((p) => (
            <option key={p.id} value={p.id}>
              {p.display_name}
            </option>
          ))}
        </select>
      ) : null}
      {action.type === 'set_priority' ? (
        <select value={action.priority ?? ''} onChange={(e) => onChange({ ...action, priority: e.target.value })} className={cn(selCls, 'capitalize')}>
          <option value="">priority…</option>
          {PRIORITIES.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
      ) : null}
      {action.type === 'set_due' ? (
        <input
          type="number"
          min={0}
          max={3650}
          value={action.days ?? ''}
          onChange={(e) => onChange({ ...action, days: e.target.value === '' ? undefined : Number(e.target.value) })}
          placeholder="days"
          className={cn(selCls, 'w-20')}
        />
      ) : null}
      {action.type === 'add_tag' ? (
        <select value={action.tag_id ?? ''} onChange={(e) => onChange({ ...action, tag_id: e.target.value })} className={selCls}>
          <option value="">tag…</option>
          {(tags.data ?? []).map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
            </option>
          ))}
        </select>
      ) : null}
      {action.type === 'move_list' ? (
        <select value={action.list_id ?? ''} onChange={(e) => onChange({ ...action, list_id: e.target.value })} className={selCls}>
          <option value="">list…</option>
          {space.lists.map((l) => (
            <option key={l.id} value={l.id}>
              {l.name}
            </option>
          ))}
        </select>
      ) : null}
      {action.type === 'create_subtask' ? (
        <input
          value={action.title ?? ''}
          onChange={(e) => onChange({ ...action, title: e.target.value })}
          placeholder="subtask title"
          className={cn(selCls, 'flex-1 min-w-32')}
        />
      ) : null}
      {action.type === 'add_checklist_item' || action.type === 'add_comment' ? (
        <input
          value={action.body ?? ''}
          onChange={(e) => onChange({ ...action, body: e.target.value })}
          placeholder="text"
          className={cn(selCls, 'flex-1 min-w-32')}
        />
      ) : null}
      {action.type === 'notify' ? (
        <input
          value={action.message ?? ''}
          onChange={(e) => onChange({ ...action, message: e.target.value })}
          placeholder="message"
          className={cn(selCls, 'flex-1 min-w-32')}
        />
      ) : null}
      <button type="button" onClick={onRemove} className="text-muted-foreground hover:text-destructive">
        <Trash2 className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}

/** One editable condition row. */
function ConditionRow({
  cond,
  space,
  onChange,
  onRemove,
}: {
  cond: AutomationCondition;
  space: Space;
  onChange: (c: AutomationCondition) => void;
  onRemove: () => void;
}) {
  const people = useQuery({ queryKey: ['people'], queryFn: ({ signal }) => listPeople(signal) });
  const tags = useQuery({ queryKey: ['tags', space.id], queryFn: ({ signal }) => listTags(space.id, signal) });
  const options =
    cond.field === 'list'
      ? space.lists.map((l) => [l.id, l.name])
      : cond.field === 'status'
        ? space.statuses.map((s) => [s.id, s.name])
        : cond.field === 'priority'
          ? PRIORITIES.map((p) => [p, p])
          : cond.field === 'tag'
            ? (tags.data ?? []).map((t) => [t.id, t.name])
            : (people.data ?? []).map((p) => [p.id, p.display_name]);
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-xs text-muted-foreground">if</span>
      <select
        value={cond.field}
        onChange={(e) => onChange({ field: e.target.value as AutomationCondition['field'], value: '' })}
        className={selCls}
      >
        {Object.entries(CONDITION_LABEL).map(([v, label]) => (
          <option key={v} value={v}>
            {label}
          </option>
        ))}
      </select>
      <select value={cond.value} onChange={(e) => onChange({ ...cond, value: e.target.value })} className={selCls}>
        <option value="">choose…</option>
        {options.map(([v, label]) => (
          <option key={v} value={v}>
            {label}
          </option>
        ))}
      </select>
      <button type="button" onClick={onRemove} className="text-muted-foreground hover:text-destructive">
        <Trash2 className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}

function Builder({ space, onDone }: { space: Space; onDone: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState('');
  const [listId, setListId] = useState('');
  const [trigger, setTrigger] = useState<AutomationTrigger>({ type: 'task_created' });
  const [conditions, setConditions] = useState<AutomationCondition[]>([]);
  const [actions, setActions] = useState<AutomationAction[]>([{ type: 'set_status' }]);

  const save = useMutation({
    mutationFn: () =>
      createAutomation(space.id, {
        name: name.trim(),
        list_id: listId || null,
        trigger,
        conditions: conditions.filter((c) => c.value),
        actions,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['automations', space.id] });
      onDone();
    },
  });

  const applyTemplate = (t: Template) => {
    const input = t.input(space);
    if (!input) return;
    setName(input.name ?? '');
    setTrigger(input.trigger ?? { type: 'task_created' });
    setConditions(input.conditions ?? []);
    setActions(input.actions ?? []);
  };

  return (
    <div className="space-y-3 rounded-md border border-border p-3 bg-secondary/40">
      <div>
        <Label>Start from a template</Label>
        <select
          value=""
          onChange={(e) => {
            const t = TEMPLATES[Number(e.target.value)];
            if (t) applyTemplate(t);
          }}
          className="w-full h-9 rounded-md border border-input bg-background px-2 text-sm"
        >
          <option value="">Blank rule…</option>
          {TEMPLATES.map((t, i) => (
            <option key={t.label} value={i}>
              {t.label}
            </option>
          ))}
        </select>
      </div>
      <div className="grid gap-2 sm:grid-cols-2">
        <div>
          <Label htmlFor="auto-name">Name</Label>
          <Input id="auto-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="What this rule does" />
        </div>
        <div>
          <Label htmlFor="auto-scope">Applies to</Label>
          <select
            id="auto-scope"
            value={listId}
            onChange={(e) => setListId(e.target.value)}
            className="w-full h-9 rounded-md border border-input bg-background px-2 text-sm"
          >
            <option value="">The whole space</option>
            {space.lists.map((l) => (
              <option key={l.id} value={l.id}>
                Only “{l.name}”
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-xs text-muted-foreground">When</span>
        <select
          value={trigger.type}
          onChange={(e) => setTrigger({ type: e.target.value as AutomationTrigger['type'] })}
          className={selCls}
        >
          {Object.entries(TRIGGER_LABEL).map(([v, label]) => (
            <option key={v} value={v}>
              {label}
            </option>
          ))}
        </select>
        {trigger.type === 'status_changed' ? (
          <select
            value={trigger.status_id ?? ''}
            onChange={(e) => setTrigger({ ...trigger, status_id: e.target.value || undefined })}
            className={selCls}
          >
            <option value="">to any status</option>
            {space.statuses.map((s) => (
              <option key={s.id} value={s.id}>
                to {s.name}
              </option>
            ))}
          </select>
        ) : null}
        {trigger.type === 'due_approaching' ? (
          <span className="flex items-center gap-1 text-xs text-muted-foreground">
            <input
              type="number"
              min={0}
              max={365}
              value={trigger.days_before ?? 1}
              onChange={(e) => setTrigger({ ...trigger, days_before: Number(e.target.value) })}
              className={cn(selCls, 'w-16')}
            />
            day(s) before
          </span>
        ) : null}
      </div>
      {conditions.map((c, i) => (
        <ConditionRow
          key={i}
          cond={c}
          space={space}
          onChange={(next) => setConditions((cs) => cs.map((x, j) => (j === i ? next : x)))}
          onRemove={() => setConditions((cs) => cs.filter((_, j) => j !== i))}
        />
      ))}
      {actions.map((a, i) => (
        <ActionRow
          key={i}
          action={a}
          space={space}
          onChange={(next) => setActions((as) => as.map((x, j) => (j === i ? next : x)))}
          onRemove={() => setActions((as) => as.filter((_, j) => j !== i))}
        />
      ))}
      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={() => setConditions((cs) => [...cs, { field: 'priority', value: '' }])}>
          <Plus className="w-3.5 h-3.5" /> Condition
        </Button>
        <Button type="button" variant="ghost" size="sm" onClick={() => setActions((as) => [...as, { type: 'add_comment' }])}>
          <Plus className="w-3.5 h-3.5" /> Action
        </Button>
        <span className="flex-1" />
        <Button type="button" variant="ghost" size="sm" onClick={onDone}>
          Cancel
        </Button>
        <Button type="button" size="sm" disabled={!name.trim() || actions.length === 0 || save.isPending} onClick={() => save.mutate()}>
          Save rule
        </Button>
      </div>
      {save.error ? (
        <p className="text-sm text-destructive">
          {save.error instanceof ApiError ? save.error.message : 'Could not save the rule.'}
        </p>
      ) : null}
    </div>
  );
}

export function AutomationsCard({ space }: { space: Space }) {
  const qc = useQueryClient();
  const [building, setBuilding] = useState(false);
  const [openRuns, setOpenRuns] = useState<string | null>(null);
  const autos = useQuery({ queryKey: ['automations', space.id], queryFn: ({ signal }) => listAutomations(space.id, signal) });
  const invalidate = () => void qc.invalidateQueries({ queryKey: ['automations', space.id] });
  const toggle = useMutation({
    mutationFn: (a: Automation) => updateAutomation(a.id, { enabled: !a.enabled }),
    onSuccess: invalidate,
  });
  const remove = useMutation({ mutationFn: (id: string) => deleteAutomation(id), onSuccess: invalidate });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Zap className="w-4 h-4" /> Automations
        </CardTitle>
        <CardDescription>Rules that act on tasks for you: “when X happens, do Y”.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {(autos.data ?? []).map((a) => (
          <div key={a.id} className={cn('rounded-md border border-border p-2.5', !a.enabled && 'opacity-60')}>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setOpenRuns((v) => (v === a.id ? null : a.id))}
                className="text-muted-foreground hover:text-foreground"
                title="Run history"
              >
                {openRuns === a.id ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
              </button>
              <span className="text-sm font-semibold flex-1 truncate">
                {a.name}
                {a.list_id ? (
                  <span className="text-xs text-muted-foreground font-normal">
                    {' '}
                    · only “{space.lists.find((l) => l.id === a.list_id)?.name ?? '?'}”
                  </span>
                ) : null}
              </span>
              <button
                type="button"
                onClick={() => toggle.mutate(a)}
                className={cn(
                  'text-[11px] font-bold rounded-pill px-2 py-0.5',
                  a.enabled ? 'bg-green-500/15 text-green-600' : 'bg-secondary text-muted-foreground'
                )}
                title={a.enabled ? 'Click to pause' : 'Click to enable'}
              >
                {a.enabled ? 'On' : 'Off'}
              </button>
              <button
                type="button"
                onClick={() => setOpenRuns((v) => (v === a.id ? null : a.id))}
                className="text-muted-foreground hover:text-foreground"
                title="Run history"
              >
                <History className="w-3.5 h-3.5" />
              </button>
              <button
                type="button"
                onClick={() => confirm(`Delete the rule "${a.name}"?`) && remove.mutate(a.id)}
                className="text-muted-foreground hover:text-destructive"
                title="Delete rule"
              >
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            </div>
            <div className="pl-6">
              <RuleSentence a={a} space={space} />
            </div>
            {openRuns === a.id ? <RunHistory automationId={a.id} /> : null}
          </div>
        ))}
        {autos.data && autos.data.length === 0 && !building ? (
          <p className="text-sm text-muted-foreground">
            No rules yet. Automations take the busywork out of boards — try “due tomorrow → urgent”.
          </p>
        ) : null}
        {building ? (
          <Builder space={space} onDone={() => setBuilding(false)} />
        ) : (
          <Button type="button" variant="secondary" size="sm" onClick={() => setBuilding(true)}>
            <Plus className="w-4 h-4" /> New rule
          </Button>
        )}
      </CardContent>
    </Card>
  );
}
