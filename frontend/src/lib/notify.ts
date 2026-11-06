// Tiny app-wide notice store (no dependency): mutations call notify() and
// the <Notices /> strip in the Layout renders whatever is live. Used for
// version conflicts (TASK-12) and other save failures that happen outside
// a form the user is looking at.
import { useSyncExternalStore } from 'react';

export type Notice = { id: number; message: string; tone: 'error' | 'info' };

let notices: Notice[] = [];
let nextId = 1;
const listeners = new Set<() => void>();

function emit() {
  for (const l of listeners) l();
}

export function notify(message: string, tone: Notice['tone'] = 'error', ttlMs = 6000) {
  const id = nextId++;
  notices = [...notices, { id, message, tone }];
  emit();
  window.setTimeout(() => dismissNotice(id), ttlMs);
}

export function dismissNotice(id: number) {
  if (!notices.some((n) => n.id === id)) return;
  notices = notices.filter((n) => n.id !== id);
  emit();
}

export function useNotices(): Notice[] {
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    () => notices
  );
}
