// Ctrl+K search (TASK-8): find tasks by title across everything you can
// see, or jump straight to a task by typing its KEY-N.
import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { Search } from 'lucide-react';
import { searchTasks } from '@/api/tasks';
import { cn } from '@/lib/utils';

const KEY_RE = /^[A-Za-z][A-Za-z0-9]{1,5}-\d+$/;

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setOpen((v) => !v);
        setQ('');
        setActive(0);
      }
      if (e.key === 'Escape') setOpen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  const results = useQuery({
    queryKey: ['search', q],
    queryFn: ({ signal }) => searchTasks(q, signal),
    enabled: open && q.trim().length >= 2,
    staleTime: 5_000,
    refetchInterval: false,
  });

  if (!open) return null;

  const hits = results.data ?? [];
  const go = (key: string) => {
    setOpen(false);
    void navigate({ to: '/t/$taskKey', params: { taskKey: key } });
  };

  return (
    <div
      className="fixed inset-0 z-[70] bg-black/40 backdrop-blur-[2px] flex items-start justify-center pt-[15vh] px-4"
      onClick={() => setOpen(false)}
    >
      <div
        className="w-full max-w-xl bg-card border border-border rounded-lg shadow-pop overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 px-4 py-3 border-b border-border">
          <Search className="w-4 h-4 text-muted-foreground shrink-0" />
          <input
            ref={inputRef}
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setActive(0);
            }}
            onKeyDown={(e) => {
              if (e.key === 'ArrowDown') {
                e.preventDefault();
                setActive((a) => Math.min(a + 1, hits.length - 1));
              } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                setActive((a) => Math.max(a - 1, 0));
              } else if (e.key === 'Enter') {
                e.preventDefault();
                if (KEY_RE.test(q.trim())) go(q.trim().toUpperCase());
                else if (hits[active]) go(hits[active].key);
              }
            }}
            placeholder="Search tasks, or type a task ID like OPS-12…"
            className="flex-1 bg-transparent outline-none text-sm"
          />
          <kbd className="text-[10px] text-muted-foreground border border-border rounded px-1.5 py-0.5">Esc</kbd>
        </div>
        <div className="max-h-[50vh] overflow-y-auto">
          {q.trim().length < 2 ? (
            <div className="px-4 py-6 text-sm text-muted-foreground text-center">
              Type at least two characters. Press Enter on a task ID to jump straight to it.
            </div>
          ) : hits.length === 0 && !results.isFetching ? (
            <div className="px-4 py-6 text-sm text-muted-foreground text-center">No tasks found.</div>
          ) : (
            hits.map((h, i) => (
              <button
                key={h.id}
                type="button"
                onClick={() => go(h.key)}
                onMouseEnter={() => setActive(i)}
                className={cn(
                  'w-full flex items-center gap-3 px-4 py-2.5 text-left text-sm transition-colors',
                  i === active ? 'bg-accent text-accent-foreground' : ''
                )}
              >
                <span className="font-mono text-xs text-muted-foreground shrink-0">{h.key}</span>
                <span className={cn('flex-1 truncate', h.done ? 'line-through opacity-60' : '')}>{h.title}</span>
                <span className="text-xs text-muted-foreground shrink-0">
                  {h.space_name} · {h.list_name}
                </span>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
