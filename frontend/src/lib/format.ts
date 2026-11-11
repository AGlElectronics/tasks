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

/** "1 h 30 min", "45 min", "0 min". */
export function formatMinutes(total: number): string {
  const h = Math.floor(total / 60);
  const m = total % 60;
  if (h && m) return `${h} h ${m} min`;
  if (h) return `${h} h`;
  return `${m} min`;
}

/** Parses "90", "1h30", "1:30", "1.5h", "45m" into minutes; null if unreadable. */
export function parseDuration(input: string): number | null {
  const s = input.trim().toLowerCase().replace(/\s+/g, '');
  if (!s) return null;
  let m = /^(\d+):(\d{1,2})$/.exec(s);
  if (m) return Number(m[1]) * 60 + Number(m[2]);
  m = /^(\d+(?:[.,]\d+)?)h(?:(\d+)m?(?:in)?)?$/.exec(s);
  if (m) return Math.round(Number(m[1].replace(',', '.')) * 60) + (m[2] ? Number(m[2]) : 0);
  m = /^(\d+)(?:m|min)?$/.exec(s);
  if (m) return Number(m[1]);
  return null;
}

/** "every 2 weeks". */
export function describeRecurrence(r: { freq: string; interval: number }): string {
  const unit = { daily: 'day', weekly: 'week', monthly: 'month', yearly: 'year' }[r.freq] ?? r.freq;
  return r.interval === 1 ? `Repeats every ${unit}` : `Repeats every ${r.interval} ${unit}s`;
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
