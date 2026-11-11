import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Trash2 } from 'lucide-react';
import { ApiError } from '@/api/client';
import {
  FIELD_TYPES,
  createField,
  deleteField,
  listFields,
  type CustomField,
  type FieldType,
  type FieldValue,
  type Space,
} from '@/api/tasks';
import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Input, Label } from '@/components/ui/Input';
import { formatFieldValue, parseFieldInput } from '@/lib/fields';

export function useFields(spaceId: string | undefined) {
  return useQuery({
    queryKey: ['fields', spaceId],
    queryFn: ({ signal }) => listFields(spaceId!, signal),
    enabled: !!spaceId,
  });
}

/** One editor for a custom field value on the task page. */
function FieldEditor({
  field,
  value,
  onChange,
}: {
  field: CustomField;
  value: FieldValue | undefined;
  onChange: (v: FieldValue | null) => void;
}) {
  const [draft, setDraft] = useState(value === undefined ? '' : String(value));
  useEffect(() => setDraft(value === undefined ? '' : String(value)), [value]);
  const cls = 'w-full h-9 rounded-md border border-input bg-background px-2 text-sm';
  switch (field.type) {
    case 'checkbox':
      return (
        <input type="checkbox" checked={value === true} onChange={(e) => onChange(e.target.checked)} aria-label={field.name} />
      );
    case 'select':
      return (
        <select value={typeof value === 'string' ? value : ''} onChange={(e) => onChange(e.target.value || null)} className={cls}>
          <option value="">—</option>
          {field.options.map((o) => (
            <option key={o.id} value={o.id}>
              {o.name}
            </option>
          ))}
        </select>
      );
    case 'date':
      return (
        <input
          type="date"
          value={typeof value === 'string' ? value : ''}
          onChange={(e) => onChange(e.target.value || null)}
          className={cls}
        />
      );
    default:
      return (
        <input
          type={field.type === 'number' ? 'text' : field.type === 'url' ? 'url' : 'text'}
          inputMode={field.type === 'number' ? 'decimal' : undefined}
          value={draft}
          placeholder={field.type === 'url' ? 'https://…' : ''}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={() => {
            const next = parseFieldInput(field, draft);
            if (next !== (value ?? null)) onChange(next);
          }}
          onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
          className={cls}
        />
      );
  }
}

/** The task page's custom fields block. */
export function TaskFields({
  spaceId,
  values,
  onChange,
}: {
  spaceId: string;
  values: Record<string, FieldValue>;
  onChange: (fieldId: string, v: FieldValue | null) => void;
}) {
  const fields = useFields(spaceId);
  if (!fields.data?.length) return null;
  return (
    <div className="space-y-3">
      {fields.data.map((f) => (
        <div key={f.id}>
          <Label>{f.name}</Label>
          <FieldEditor field={f} value={values[f.id]} onChange={(v) => onChange(f.id, v)} />
          {f.type === 'url' && typeof values[f.id] === 'string' ? (
            <a href={values[f.id] as string} target="_blank" rel="noreferrer noopener" className="text-xs text-primary hover:underline break-all">
              Open link
            </a>
          ) : null}
        </div>
      ))}
    </div>
  );
}

/** Compact chips for list rows and board cards. */
export function FieldChips({ fields, values }: { fields: CustomField[]; values: Record<string, FieldValue> }) {
  const shown = fields.filter((f) => formatFieldValue(f, values[f.id]) !== '').slice(0, 3);
  if (shown.length === 0) return null;
  return (
    <>
      {shown.map((f) => {
        const option = f.type === 'select' ? f.options.find((o) => o.id === values[f.id]) : undefined;
        return (
          <span
            key={f.id}
            title={f.name}
            className="hidden md:inline-flex rounded-pill border border-border px-1.5 py-0.5 text-[10px] text-muted-foreground shrink-0"
            style={option ? { borderColor: option.color, color: option.color } : undefined}
          >
            {f.type === 'checkbox' ? f.name : formatFieldValue(f, values[f.id])}
          </span>
        );
      })}
    </>
  );
}

/** Space settings: define the space's custom fields (managers). */
export function FieldsCard({ space }: { space: Space }) {
  const qc = useQueryClient();
  const fields = useFields(space.id);
  const [name, setName] = useState('');
  const [type, setType] = useState<FieldType>('text');
  const [options, setOptions] = useState('');
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['fields', space.id] });
    void qc.invalidateQueries({ queryKey: ['tasks'] });
  };
  const create = useMutation({
    mutationFn: () =>
      createField(space.id, {
        name,
        type,
        options:
          type === 'select'
            ? options
                .split(',')
                .map((o) => o.trim())
                .filter(Boolean)
                .map((o) => ({ name: o }))
            : undefined,
      }),
    onSuccess: () => {
      setName('');
      setOptions('');
      refresh();
    },
  });
  const remove = useMutation({ mutationFn: deleteField, onSuccess: refresh });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Custom fields</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-xs text-muted-foreground">
          Extra fields every task in this space can fill in (cost, vendor, stage…). Deleting a field deletes its values.
        </p>
        <ul className="divide-y divide-border">
          {(fields.data ?? []).map((f) => (
            <li key={f.id} className="flex items-center gap-2 py-1.5 text-sm">
              <span className="font-medium">{f.name}</span>
              <span className="text-xs text-muted-foreground">
                {f.type}
                {f.type === 'select' ? `: ${f.options.map((o) => o.name).join(', ')}` : ''}
              </span>
              <button
                type="button"
                className="ml-auto text-muted-foreground hover:text-destructive"
                title="Delete field"
                onClick={() => confirm(`Delete the field "${f.name}" and its value on every task?`) && remove.mutate(f.id)}
              >
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            </li>
          ))}
        </ul>
        <form
          className="grid gap-2 sm:grid-cols-[1fr_8rem]"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) create.mutate();
          }}
        >
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Field name" aria-label="Field name" />
          <select
            value={type}
            onChange={(e) => setType(e.target.value as FieldType)}
            className="h-9 rounded-md border border-input bg-background px-2 text-sm"
            aria-label="Field type"
          >
            {FIELD_TYPES.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
          {type === 'select' ? (
            <Input
              className="sm:col-span-2"
              value={options}
              onChange={(e) => setOptions(e.target.value)}
              placeholder="Options, comma-separated (Ordered, Delivered)"
              aria-label="Options"
            />
          ) : null}
          <div className="sm:col-span-2 flex items-center gap-2">
            <Button type="submit" size="sm" disabled={!name.trim() || create.isPending}>
              Add field
            </Button>
            {create.error ? (
              <span className="text-sm text-destructive">
                {create.error instanceof ApiError ? create.error.message : 'Could not add the field.'}
              </span>
            ) : null}
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
