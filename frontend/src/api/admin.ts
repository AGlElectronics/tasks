import { apiRequestWithHeaders } from './client';

// Admin-only endpoints (settings.manage).

export type AuditEntity = 'task' | 'list' | 'space' | 'comment' | 'attachment' | 'grant' | 'settings';
export const AUDIT_ENTITIES: AuditEntity[] = ['task', 'list', 'space', 'comment', 'attachment', 'grant', 'settings'];

export type AuditEntry = {
  id: string;
  created_at: string;
  actor_id: string | null;
  /** "System" for the retention sweep and HR's department hook. */
  actor_name: string;
  entity: AuditEntity;
  entity_id: string;
  action: string;
  data: Record<string, unknown>;
};

export type AuditQuery = { entity?: string; action?: string; since?: string; until?: string; cursor?: string };

export async function getAudit(q: AuditQuery, signal?: AbortSignal) {
  const { data, headers } = await apiRequestWithHeaders<{ entries: AuditEntry[] }>('/api/v1/audit', {
    signal,
    query: { ...q, limit: 100 },
  });
  return { entries: data.entries, next: headers.get('X-Next-Cursor') ?? undefined };
}
