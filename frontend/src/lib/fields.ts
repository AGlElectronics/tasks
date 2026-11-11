import type { CustomField, FieldValue } from '@/api/tasks';

/** Human-readable form of a custom field value ("" when unset). */
export function formatFieldValue(f: CustomField, v: FieldValue | undefined): string {
  if (v === undefined || v === null || v === '') return '';
  switch (f.type) {
    case 'select':
      return f.options.find((o) => o.id === v)?.name ?? '';
    case 'checkbox':
      return v ? 'Yes' : 'No';
    case 'number':
      return typeof v === 'number' ? v.toLocaleString() : String(v);
    default:
      return String(v);
  }
}

/** Parses what an input produced into the value the API expects; null clears. */
export function parseFieldInput(f: CustomField, raw: string | boolean): FieldValue | null {
  if (f.type === 'checkbox') return Boolean(raw);
  const s = String(raw).trim();
  if (s === '') return null;
  if (f.type === 'number') {
    const n = Number(s.replace(',', '.'));
    return Number.isFinite(n) ? n : null;
  }
  return s;
}
