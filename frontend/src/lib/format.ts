/** "3 min ago", "yesterday", or a date for anything older than a week. */
export function timeAgo(iso: string): string {
  const then = new Date(iso).getTime();
  const s = Math.round((Date.now() - then) / 1000);
  if (s < 45) return 'just now';
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} h ago`;
  const d = Math.round(h / 24);
  if (d === 1) return 'yesterday';
  if (d < 7) return `${d} days ago`;
  return new Date(iso).toLocaleDateString();
}

function localToday(): string {
  const now = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

/** Due-date label and tone: overdue, today, or a short date. */
export function dueLabel(date: string, done: boolean): { text: string; tone: 'overdue' | 'today' | 'normal' } {
  const today = localToday();
  const [y, m, d] = date.split('-').map(Number);
  const text = new Date(y, m - 1, d).toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
  if (done) return { text, tone: 'normal' };
  if (date < today) return { text, tone: 'overdue' };
  if (date === today) return { text: 'Today', tone: 'today' };
  return { text, tone: 'normal' };
}
