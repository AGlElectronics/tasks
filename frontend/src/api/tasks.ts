import { apiBlob, apiRequest, apiRequestWithHeaders, apiUpload } from './client';

export type Category = 'not_started' | 'active' | 'done';
export type Priority = 'urgent' | 'high' | 'normal' | 'low';

export const PRIORITIES: Priority[] = ['urgent', 'high', 'normal', 'low'];

export type Status = {
  id: string;
  name: string;
  color: string;
  category: Category;
  position: number;
};

export type List = {
  id: string;
  space_id: string;
  folder_id: string | null;
  name: string;
  description: string;
  position: number;
  private: boolean;
  archived: boolean;
  open_tasks: number;
  done_tasks: number;
};

export type Folder = {
  id: string;
  space_id: string;
  name: string;
  position: number;
};

export type Tag = { id: string; name: string; color: string };

export type AccessLevel = 'view' | 'edit' | 'manage';

export type Space = {
  id: string;
  name: string;
  key: string;
  color: string;
  description: string;
  created_by?: string;
  created_at: string;
  everyone_level: AccessLevel | null;
  /** Hidden from the sidebar; HR's department hook archives department spaces. */
  archived: boolean;
  /** HR department this space belongs to, or null for spaces made by hand. */
  department_id: string | null;
  lists: List[];
  folders: Folder[];
  statuses: Status[];
  my_level: AccessLevel | '';
  can_edit: boolean;
  can_manage: boolean;
  shared_only?: boolean;
};

export type Grant = {
  id: string;
  space_id?: string;
  list_id?: string;
  subject_type: 'user' | 'group';
  /** User id, or the group name as typed. */
  subject: string;
  /** What the grant matches on: user id, or the normalised group name. */
  subject_key: string;
  display_name: string;
  level: AccessLevel;
};

export type Person = { id: string; display_name: string; email?: string };

export type Task = {
  id: string;
  key: string;
  number: number;
  space_id: string;
  list_id: string;
  list_name: string;
  parent_id: string | null;
  parent_key: string | null;
  parent_title: string | null;
  title: string;
  description: string;
  status: { id: string; name: string; color: string; category: Category };
  priority: Priority | null;
  start_at: string | null;
  due_at: string | null;
  position: number;
  completed_at: string | null;
  version: number;
  created_at: string;
  updated_at: string;
  created_by: string | null;
  assignees: Person[];
  tags: Tag[];
  subtask_count: number;
  subtask_done: number;
  comment_count: number;
  checklist_total: number;
  checklist_done: number;
  /** Custom field id → value (string, number, boolean, YYYY-MM-DD or select option id). */
  fields: Record<string, FieldValue>;
  recurrence: Recurrence | null;
  /** Unfinished tasks this one waits for. */
  blocked_by_open: number;
  estimate_minutes: number | null;
  time_spent_minutes: number;
};

export type Recurrence = { freq: 'daily' | 'weekly' | 'monthly' | 'yearly'; interval: number };

export type DepRef = {
  id: string;
  key: string;
  title: string;
  status: { id: string; name: string; color: string; category: Category };
  done: boolean;
  list_id: string;
  space_id: string;
};

export type TimeEntry = {
  id: string;
  task_id: string;
  user_id: string | null;
  user_name: string;
  minutes: number;
  spent_on: string;
  note: string;
  created_at: string;
};

export type FieldType = 'text' | 'number' | 'date' | 'select' | 'checkbox' | 'url';
export const FIELD_TYPES: FieldType[] = ['text', 'number', 'date', 'select', 'checkbox', 'url'];
export type FieldValue = string | number | boolean;
export type FieldOption = { id: string; name: string; color: string };
export type CustomField = {
  id: string;
  space_id: string;
  name: string;
  type: FieldType;
  options: FieldOption[];
  position: number;
};
export type FieldInput = {
  name?: string;
  type?: FieldType;
  options?: { id?: string; name: string; color?: string }[];
  position?: number;
};

export const listFields = (spaceId: string, signal?: AbortSignal) =>
  apiRequest<{ fields: CustomField[] }>(`/api/v1/spaces/${spaceId}/fields`, { signal }).then((r) => r.fields);
export const createField = (spaceId: string, body: FieldInput) =>
  apiRequest<CustomField>(`/api/v1/spaces/${spaceId}/fields`, { method: 'POST', body });
export const updateField = (id: string, body: FieldInput) =>
  apiRequest<CustomField>(`/api/v1/fields/${id}`, { method: 'PATCH', body });
export const deleteField = (id: string) => apiRequest<void>(`/api/v1/fields/${id}`, { method: 'DELETE' });

export type ChecklistItem = {
  id: string;
  task_id: string;
  body: string;
  done: boolean;
  position: number;
};

export type Attachment = {
  id: string;
  task_id: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  uploaded_by: string | null;
  created_at: string;
};

export type Notification = {
  id: string;
  task_id: string | null;
  task_key: string;
  task_title: string;
  actor_name: string;
  kind: string;
  message: string;
  read_at: string | null;
  created_at: string;
};

export type SearchHit = {
  id: string;
  key: string;
  title: string;
  list_name: string;
  space_name: string;
  status: string;
  done: boolean;
};

export type TrashedTask = {
  id: string;
  key: string;
  title: string;
  list_name: string;
  deleted_at: string;
  deleted_by: string | null;
};

export type Favorite = { space_id?: string; list_id?: string };

export type Comment = {
  id: string;
  author_id: string | null;
  author_name: string;
  body: string;
  created_at: string;
  edited_at: string | null;
};

export type TaskEvent = {
  id: string;
  actor_name: string;
  kind: string;
  data: Record<string, unknown>;
  created_at: string;
};

export type TaskDetail = Task & {
  subtasks: Task[];
  comments: Comment[];
  events: TaskEvent[];
  statuses: Status[];
  checklist: ChecklistItem[];
  watchers: Person[];
  attachments: Attachment[];
  /** Totals; comments and events hold only the newest 100 and 200. */
  comments_total: number;
  events_total: number;
  blocked_by: DepRef[];
  blocking: DepRef[];
  /** Dependencies on tasks you cannot see. */
  hidden_dependencies: number;
  time_entries: TimeEntry[];
};

export type ListWithSpace = List & { space: Space; my_level: AccessLevel | ''; can_edit: boolean };

export type SpaceInput = {
  name?: string;
  key?: string;
  color?: string;
  description?: string;
  everyone_level?: AccessLevel | null;
  archived?: boolean;
};
export type StatusInput = { id?: string; name: string; color: string; category: Category };

export type TaskInput = {
  list_id?: string;
  parent_id?: string;
  title: string;
  description?: string;
  status_id?: string;
  priority?: Priority | null;
  start_at?: string | null;
  due_at?: string | null;
  assignee_ids?: string[];
};

export type TaskPatch = {
  title?: string;
  description?: string;
  status_id?: string;
  list_id?: string;
  position?: number;
  priority?: Priority | null;
  start_at?: string | null;
  due_at?: string | null;
  assignee_ids?: string[];
  tag_ids?: string[];
  /** Custom field id → value; null clears it. */
  fields?: Record<string, FieldValue | null>;
  recurrence?: Recurrence | null;
  estimate_minutes?: number | null;
  version?: number;
};

// Dependencies
export const addDependency = (taskId: string, dependsOn: string) =>
  apiRequest<DepRef>(`/api/v1/tasks/${taskId}/dependencies`, { method: 'POST', body: { depends_on: dependsOn } });
export const removeDependency = (taskId: string, other: string) =>
  apiRequest<void>(`/api/v1/tasks/${taskId}/dependencies/${encodeURIComponent(other)}`, { method: 'DELETE' });

// Time tracking
export const logTime = (taskId: string, body: { minutes: number; spent_on?: string; note?: string }) =>
  apiRequest<TimeEntry>(`/api/v1/tasks/${taskId}/time`, { method: 'POST', body });
export const deleteTimeEntry = (id: string) => apiRequest<void>(`/api/v1/time/${id}`, { method: 'DELETE' });
export const listTimeReport = (listId: string, q: { from?: string; to?: string }, signal?: AbortSignal) =>
  apiRequest<{ people: { user_id: string | null; user_name: string; minutes: number }[]; total_minutes: number }>(
    `/api/v1/lists/${listId}/time-report`,
    { signal, query: q }
  );

export const listSpaces = (signal?: AbortSignal, includeArchived?: boolean) =>
  apiRequest<{ spaces: Space[]; truncated: boolean }>('/api/v1/spaces', {
    signal,
    query: { archived: includeArchived ? 1 : undefined },
  }).then((r) => r.spaces);
export const getSpace = (id: string, signal?: AbortSignal) =>
  apiRequest<Space>(`/api/v1/spaces/${id}`, { signal });
export const createSpace = (body: SpaceInput) =>
  apiRequest<Space>('/api/v1/spaces', { method: 'POST', body });
export const updateSpace = (id: string, body: SpaceInput) =>
  apiRequest<Space>(`/api/v1/spaces/${id}`, { method: 'PATCH', body });
export const deleteSpace = (id: string) => apiRequest<void>(`/api/v1/spaces/${id}`, { method: 'DELETE' });
export const replaceStatuses = (id: string, statuses: StatusInput[]) =>
  apiRequest<{ statuses: Status[] }>(`/api/v1/spaces/${id}/statuses`, { method: 'PUT', body: { statuses } });

export const createList = (spaceId: string, body: { name: string; description?: string }) =>
  apiRequest<List>(`/api/v1/spaces/${spaceId}/lists`, { method: 'POST', body });
export const getList = (id: string, signal?: AbortSignal) =>
  apiRequest<ListWithSpace>(`/api/v1/lists/${id}`, { signal });
export const updateList = (
  id: string,
  body: {
    name?: string;
    description?: string;
    private?: boolean;
    position?: number;
    folder_id?: string | null;
    archived?: boolean;
  }
) => apiRequest<List>(`/api/v1/lists/${id}`, { method: 'PATCH', body });
export const deleteList = (id: string) => apiRequest<void>(`/api/v1/lists/${id}`, { method: 'DELETE' });
/**
 * Follows X-Next-Cursor pages (TASK-38) so views keep seeing the whole
 * collection. The page cap only guards against a server bug; at 200 tasks a
 * page it allows 10,000 tasks per view.
 */
async function allPagesOf<T>(path: string, key: string, signal?: AbortSignal): Promise<T[]> {
  const all: T[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < 50; page++) {
    const { data, headers } = await apiRequestWithHeaders<Record<string, T[]>>(path, {
      signal,
      query: { cursor },
    });
    all.push(...(data[key] ?? []));
    cursor = headers.get('X-Next-Cursor') ?? undefined;
    if (!cursor) break;
  }
  return all;
}
const allPages = (path: string, signal?: AbortSignal) => allPagesOf<Task>(path, 'tasks', signal);

/** Every comment and event of a task; the task detail embeds only the newest. */
export const taskHistory = async (taskId: string, signal?: AbortSignal) => {
  const [comments, events] = await Promise.all([
    allPagesOf<Comment>(`/api/v1/tasks/${taskId}/comments`, 'comments', signal),
    allPagesOf<TaskEvent>(`/api/v1/tasks/${taskId}/events`, 'events', signal),
  ]);
  return { comments, events };
};

export const listTasks = (listId: string, signal?: AbortSignal) => allPages(`/api/v1/lists/${listId}/tasks`, signal);

export const myWork = (signal?: AbortSignal) => allPages('/api/v1/my-work', signal);
export const createTask = (body: TaskInput) => apiRequest<Task>('/api/v1/tasks', { method: 'POST', body });
export const getTask = (ref: string, signal?: AbortSignal) =>
  apiRequest<TaskDetail>(`/api/v1/tasks/${encodeURIComponent(ref)}`, { signal });
export const updateTask = (id: string, body: TaskPatch) =>
  apiRequest<Task>(`/api/v1/tasks/${id}`, { method: 'PATCH', body });
export const deleteTask = (id: string) => apiRequest<void>(`/api/v1/tasks/${id}`, { method: 'DELETE' });
export const addComment = (taskId: string, body: string, mentions?: string[]) =>
  apiRequest<Comment>(`/api/v1/tasks/${taskId}/comments`, { method: 'POST', body: { body, mentions } });
export const deleteComment = (id: string) => apiRequest<void>(`/api/v1/comments/${id}`, { method: 'DELETE' });
export const updateComment = (id: string, body: string) =>
  apiRequest<Comment>(`/api/v1/comments/${id}`, { method: 'PATCH', body: { body } });

export const listPeople = (signal?: AbortSignal, q?: string) =>
  apiRequest<{ users: Person[] }>('/api/v1/users', { signal, query: { q } }).then((r) => r.users);

// Sharing. Scope is a space or a list.
export type GrantScope = { spaceId?: string; listId?: string };
const grantsPath = (s: GrantScope) =>
  s.spaceId ? `/api/v1/spaces/${s.spaceId}/grants` : `/api/v1/lists/${s.listId}/grants`;

export const listGrants = (scope: GrantScope, signal?: AbortSignal) =>
  apiRequest<{ grants: Grant[] }>(grantsPath(scope), { signal }).then((r) => r.grants);
export const putGrant = (scope: GrantScope, body: { subject_type: 'user' | 'group'; subject: string; level: AccessLevel }) =>
  apiRequest<Grant>(grantsPath(scope), { method: 'PUT', body });
export const deleteGrant = (scope: GrantScope, id: string) =>
  apiRequest<void>(`${grantsPath(scope)}/${id}`, { method: 'DELETE' });

// Tags (TASK-4)
export const listTags = (spaceId: string, signal?: AbortSignal) =>
  apiRequest<{ tags: Tag[] }>(`/api/v1/spaces/${spaceId}/tags`, { signal }).then((r) => r.tags);
export const createTag = (spaceId: string, body: { name: string; color?: string }) =>
  apiRequest<Tag>(`/api/v1/spaces/${spaceId}/tags`, { method: 'POST', body });
export const updateTag = (spaceId: string, tagId: string, body: { name?: string; color?: string }) =>
  apiRequest<Tag>(`/api/v1/spaces/${spaceId}/tags/${tagId}`, { method: 'PATCH', body });
export const deleteTag = (spaceId: string, tagId: string) =>
  apiRequest<void>(`/api/v1/spaces/${spaceId}/tags/${tagId}`, { method: 'DELETE' });

// Checklists (TASK-6)
export const addChecklistItem = (taskId: string, body: string) =>
  apiRequest<ChecklistItem>(`/api/v1/tasks/${taskId}/checklist`, { method: 'POST', body: { body } });
export const updateChecklistItem = (itemId: string, body: { body?: string; done?: boolean; position?: number }) =>
  apiRequest<ChecklistItem>(`/api/v1/checklist/${itemId}`, { method: 'PATCH', body });
export const deleteChecklistItem = (itemId: string) =>
  apiRequest<void>(`/api/v1/checklist/${itemId}`, { method: 'DELETE' });

// Watchers and the Inbox (TASK-5)
export const watchTask = (taskId: string) => apiRequest<void>(`/api/v1/tasks/${taskId}/watch`, { method: 'POST', body: {} });
export const unwatchTask = (taskId: string) => apiRequest<void>(`/api/v1/tasks/${taskId}/watch`, { method: 'DELETE' });
export const getInbox = (signal?: AbortSignal, unreadOnly?: boolean) =>
  apiRequest<{ notifications: Notification[] }>('/api/v1/inbox', {
    signal,
    query: { unread: unreadOnly ? 1 : undefined },
  }).then((r) => r.notifications);
export const getUnreadCount = (signal?: AbortSignal) =>
  apiRequest<{ count: number }>('/api/v1/inbox/unread-count', { signal }).then((r) => r.count);
export const markRead = (ids?: string[]) =>
  apiRequest<void>('/api/v1/inbox/read', { method: 'POST', body: { ids } });

// Email notifications: which Inbox kinds also come by email.
export const NOTIFICATION_KINDS = ['assigned', 'mention', 'comment', 'status', 'due_soon', 'automation'] as const;
export type NotificationPrefs = { email: boolean; email_kinds: string[]; has_address?: boolean };
export const getNotificationPrefs = (signal?: AbortSignal) =>
  apiRequest<NotificationPrefs>('/api/v1/me/notifications', { signal });
export const setNotificationPrefs = (p: NotificationPrefs) =>
  apiRequest<NotificationPrefs>('/api/v1/me/notifications', {
    method: 'PUT',
    body: { email: p.email, email_kinds: p.email_kinds },
  });

// Search (TASK-8)
export const searchTasks = (q: string, signal?: AbortSignal) =>
  apiRequest<{ results: SearchHit[] }>('/api/v1/search', { signal, query: { q } }).then((r) => r.results);

// Folders (TASK-9)
export const createFolder = (spaceId: string, name: string) =>
  apiRequest<Folder>(`/api/v1/spaces/${spaceId}/folders`, { method: 'POST', body: { name } });
export const updateFolder = (folderId: string, body: { name?: string; position?: number }) =>
  apiRequest<Folder>(`/api/v1/folders/${folderId}`, { method: 'PATCH', body });
export const deleteFolder = (folderId: string) =>
  apiRequest<void>(`/api/v1/folders/${folderId}`, { method: 'DELETE' });

// Favourites (TASK-11)
export const listFavorites = (signal?: AbortSignal) =>
  apiRequest<{ favorites: Favorite[] }>('/api/v1/favorites', { signal }).then((r) => r.favorites);
export const addFavorite = (f: Favorite) => apiRequest<void>('/api/v1/favorites', { method: 'PUT', body: f });
export const removeFavorite = (f: Favorite) =>
  apiRequest<void>('/api/v1/favorites/remove', { method: 'POST', body: f });

// Automations (TASK-22)
export type AutomationTrigger = {
  type:
    | 'task_created'
    | 'status_changed'
    | 'assignee_added'
    | 'comment_added'
    | 'checklist_completed'
    | 'due_approaching';
  status_id?: string;
  category?: string;
  days_before?: number;
};
export type AutomationCondition = {
  field: 'list' | 'status' | 'priority' | 'tag' | 'assignee';
  value: string;
};
export type AutomationAction = {
  type:
    | 'set_status'
    | 'assign'
    | 'set_priority'
    | 'set_due'
    | 'add_tag'
    | 'move_list'
    | 'create_subtask'
    | 'add_checklist_item'
    | 'add_comment'
    | 'notify';
  status_id?: string;
  user_id?: string;
  priority?: string;
  days?: number;
  tag_id?: string;
  list_id?: string;
  title?: string;
  body?: string;
  message?: string;
};
export type Automation = {
  id: string;
  space_id: string;
  list_id: string | null;
  name: string;
  enabled: boolean;
  trigger: AutomationTrigger;
  conditions: AutomationCondition[];
  actions: AutomationAction[];
  created_at: string;
  updated_at: string;
};
export type AutomationRun = {
  id: string;
  task_id: string | null;
  task_key: string;
  status: 'ok' | 'error';
  detail: string;
  created_at: string;
};
export type AutomationInput = {
  name?: string;
  enabled?: boolean;
  list_id?: string | null;
  trigger?: AutomationTrigger;
  conditions?: AutomationCondition[];
  actions?: AutomationAction[];
};

export const listAutomations = (spaceId: string, signal?: AbortSignal) =>
  apiRequest<{ automations: Automation[] }>(`/api/v1/spaces/${spaceId}/automations`, { signal }).then(
    (r) => r.automations
  );
export const createAutomation = (spaceId: string, body: AutomationInput) =>
  apiRequest<Automation>(`/api/v1/spaces/${spaceId}/automations`, { method: 'POST', body });
export const updateAutomation = (id: string, body: AutomationInput) =>
  apiRequest<Automation>(`/api/v1/automations/${id}`, { method: 'PATCH', body });
export const deleteAutomation = (id: string) =>
  apiRequest<void>(`/api/v1/automations/${id}`, { method: 'DELETE' });
export const automationRuns = (id: string, signal?: AbortSignal) =>
  apiRequest<{ runs: AutomationRun[] }>(`/api/v1/automations/${id}/runs`, { signal }).then((r) => r.runs);

// Archived lists (TASK-48)
export const archivedLists = (spaceId: string, signal?: AbortSignal) =>
  apiRequest<{ lists: List[] }>(`/api/v1/spaces/${spaceId}/archived-lists`, { signal }).then((r) => r.lists);

// Trash (TASK-48)
export const spaceTrash = (spaceId: string, signal?: AbortSignal) =>
  apiRequest<{ tasks: TrashedTask[] }>(`/api/v1/spaces/${spaceId}/trash`, { signal }).then((r) => r.tasks);
export const restoreTask = (id: string) => apiRequest<Task>(`/api/v1/tasks/${id}/restore`, { method: 'POST', body: {} });
export const purgeTask = (id: string) => apiRequest<void>(`/api/v1/tasks/${id}/purge`, { method: 'DELETE' });

// Attachments (TASK-7). Multipart, so this bypasses apiRequest's JSON body.
export async function uploadAttachment(taskId: string, file: File): Promise<Attachment> {
  const form = new FormData();
  form.append('file', file);
  return apiUpload<Attachment>(`/api/v1/tasks/${taskId}/attachments`, form);
}
export const deleteAttachment = (id: string) => apiRequest<void>(`/api/v1/attachments/${id}`, { method: 'DELETE' });

/** Fetches the bytes with auth headers and hands them to the browser. */
export async function downloadAttachmentFile(a: Attachment): Promise<void> {
  const blob = await apiBlob(`/api/v1/attachments/${a.id}`);
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = a.filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30_000);
}
