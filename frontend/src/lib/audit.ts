import type { AuditEntry } from '@/api/admin';

/** One-line summary of what an audit entry's snapshot says. */
export function auditSummary(e: Pick<AuditEntry, 'entity' | 'data'>): string {
  const d = e.data;
  const s = (k: string) => (typeof d[k] === 'string' ? (d[k] as string) : '');
  switch (e.entity) {
    case 'task':
      return [s('key'), s('title')].filter(Boolean).join(' ');
    case 'grant':
      return `${s('subject_type')} ${s('subject')} → ${s('level')}${s('source') ? ' (HR)' : ''}`;
    case 'comment':
      return s('body');
    case 'attachment':
      return s('filename');
    case 'settings':
      return Object.keys(d).join(', ');
    default:
      return [s('key'), s('name')].filter(Boolean).join(' ');
  }
}
