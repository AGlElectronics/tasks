import type { ReactNode } from 'react';
import { X } from 'lucide-react';
import { NO_LABEL_URGENCY_FILTER, PRIORITIES, type LabelUrgencyFilter as Value } from '@/api/tasks';
import { useAllLabels } from '@/hooks/useAllLabels';
import { cn } from '@/lib/utils';

const sel = 'h-8 rounded-pill border border-border bg-card px-2.5 text-xs';

/** Labels in a task's row: coloured pills, like the task page. */
export function LabelPills({ tags }: { tags: { id: string; name: string; color: string }[] }) {
  if (tags.length === 0) return null;
  return (
    <span className="inline-flex flex-wrap items-center gap-1">
      {tags.map((tg) => (
        <span
          key={tg.id}
          className="inline-flex items-center rounded-pill px-1.5 py-px text-[10px] font-semibold text-white"
          style={{ backgroundColor: tg.color }}
        >
          {tg.name}
        </span>
      ))}
    </span>
  );
}

/**
 * Urgency and label filter for pages that read across spaces (My Work,
 * Inbox). The filter runs on the server, so paging stays correct.
 */
export function LabelUrgencyFilter({
  value,
  onChange,
  children,
}: {
  value: Value;
  onChange: (v: Value) => void;
  /** Extra controls on the same row (a sort picker, say). */
  children?: ReactNode;
}) {
  const labels = useAllLabels();
  const active = value.priority !== '' || value.tag !== '';
  return (
    <div className="flex flex-wrap items-center gap-2 mb-4">
      <select
        value={value.priority}
        onChange={(e) => onChange({ ...value, priority: e.target.value as Value['priority'] })}
        className={cn(sel, 'capitalize')}
        aria-label="Urgency"
      >
        <option value="">Any urgency</option>
        {PRIORITIES.map((p) => (
          <option key={p} value={p}>
            {p}
          </option>
        ))}
        <option value="none">No urgency set</option>
      </select>
      {labels.length > 0 || value.tag ? (
        <select value={value.tag} onChange={(e) => onChange({ ...value, tag: e.target.value })} className={sel} aria-label="Label">
          <option value="">Any label</option>
          {value.tag && !labels.some((l) => l.name.toLowerCase() === value.tag.toLowerCase()) ? (
            <option value={value.tag}>{value.tag}</option>
          ) : null}
          {labels.map((l) => (
            <option key={l.name} value={l.name}>
              {l.name}
            </option>
          ))}
        </select>
      ) : null}
      {active ? (
        <button
          type="button"
          onClick={() => onChange(NO_LABEL_URGENCY_FILTER)}
          className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
        >
          <X className="w-3.5 h-3.5" /> Clear
        </button>
      ) : null}
      {children}
    </div>
  );
}
