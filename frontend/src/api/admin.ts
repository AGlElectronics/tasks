import { apiRequest, apiRequestWithHeaders } from './client';

export type SettingSource = 'default' | 'tasks' | 'den';
export type SettingDef = { key: string; default: number; min: number; max: number; description: string };
export type ModuleSettings = {
  settings: Record<string, Record<string, number>>;
  sources: Record<string, SettingSource>;
  definitions: SettingDef[];
  email: { configured: boolean; source: 'den' | 'tasks' | 'none'; host?: string; port?: number; from?: string; security?: string };
};
/** Nested partial: {retention: {trash_days: 14}}; null resets to the default. */
export type SettingsPatch = Record<string, Record<string, number | null>>;

export const getSettings = (signal?: AbortSignal) => apiRequest<ModuleSettings>('/api/v1/settings', { signal });
export const putSettings = (body: SettingsPatch) => apiRequest<ModuleSettings>('/api/v1/settings', { method: 'PUT', body });
export const sendTestEmail = () =>
  apiRequest<{ sent_to: string }>('/api/v1/settings/email/test', { method: 'POST', body: {} });

/** Splits "retention.trash_days" into its section and field. */
export function splitKey(key: string): [string, string] {
  const i = key.indexOf('.');
  return [key.slice(0, i), key.slice(i + 1)];
}

// Admin-only endpoints (settings.manage).

export type AuditEntity = 'task' | 'list' | 'space' | 'comment' | 'attachment' | 'grant' | 'settings' | 'field';
export const AUDIT_ENTITIES: AuditEntity[] = ['task', 'list', 'space', 'comment', 'attachment', 'grant', 'settings', 'field'];

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
