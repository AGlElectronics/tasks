// The Inbox (TASK-5): mentions, assignments and activity on tasks you
// watch or are assigned to. Clicking a row opens the task and marks the
// notification read.
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { AtSign, Bell, CalendarClock, CheckCheck, Mail, MessageSquare, UserPlus } from 'lucide-react';
import { getInbox, markRead, type Notification } from '@/api/tasks';
import { EmailPrefs } from '@/components/EmailPrefs';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { PageContainer, PageHeader } from '@/components/ui/PageHeader';
import { EmptyState, ErrorRow, LoadingRow } from '@/components/ui/States';
import { timeAgo } from '@/lib/format';
import { cn } from '@/lib/utils';

function kindIcon(kind: string) {
  switch (kind) {
    case 'mention':
      return AtSign;
    case 'assigned':
      return UserPlus;
    case 'comment':
      return MessageSquare;
    case 'due_soon':
      return CalendarClock;
    default:
      return Bell;
  }
}

function Row({ n, onOpen }: { n: Notification; onOpen: (n: Notification) => void }) {
  const Icon = kindIcon(n.kind);
  const inner = (
    <>
      <Icon className="w-4 h-4 shrink-0 mt-0.5 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <div className="text-sm">
          <span className="font-medium">{n.actor_name}</span>{' '}
          <span className="text-muted-foreground">{n.message}</span>
        </div>
        <div className="text-xs text-muted-foreground mt-0.5 truncate">
          <span className="font-mono">{n.task_key}</span> · {n.task_title}
        </div>
      </div>
      <span className="text-xs text-muted-foreground shrink-0">{timeAgo(n.created_at)}</span>
      {!n.read_at ? <span className="w-2 h-2 rounded-full bg-primary shrink-0 mt-1.5" aria-label="Unread" /> : null}
    </>
  );
  const className = cn(
    'flex items-start gap-3 px-4 py-3 transition-colors',
    n.read_at ? 'opacity-70' : 'bg-primary-soft/30',
    n.task_id ? 'hover:bg-secondary cursor-pointer' : ''
  );
  if (!n.task_id) {
    return <div className={className}>{inner}</div>;
  }
  return (
    <Link to="/t/$taskKey" params={{ taskKey: n.task_key }} className={className} onClick={() => onOpen(n)}>
      {inner}
    </Link>
  );
}

export function InboxPage() {
  const qc = useQueryClient();
  const [unreadOnly, setUnreadOnly] = useState(false);
  const [showEmail, setShowEmail] = useState(false);
  const inbox = useQuery({
    queryKey: ['inbox', unreadOnly],
    queryFn: ({ signal }) => getInbox(signal, unreadOnly),
  });
  const read = useMutation({
    mutationFn: (ids?: string[]) => markRead(ids),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['inbox'] });
      void qc.invalidateQueries({ queryKey: ['inbox-unread'] });
    },
  });

  return (
    <PageContainer>
      <PageHeader
        title="Inbox"
        description="Mentions, assignments and updates on tasks you follow."
        actions={
          <>
            <Button variant="ghost" size="sm" onClick={() => setShowEmail((v) => !v)}>
              <Mail className="w-4 h-4" /> Email settings
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setUnreadOnly((v) => !v)}>
              {unreadOnly ? 'Show all' : 'Unread only'}
            </Button>
            <Button variant="secondary" size="sm" onClick={() => read.mutate(undefined)}>
              <CheckCheck className="w-4 h-4" /> Mark all read
            </Button>
          </>
        }
      />
      {showEmail ? <EmailPrefs /> : null}
      {inbox.isLoading ? (
        <LoadingRow />
      ) : inbox.isError ? (
        <ErrorRow message="Could not load your inbox." />
      ) : inbox.data!.length === 0 ? (
        <Card>
          <EmptyState icon={Bell} title="Nothing here" description="Watch a task or get assigned, and its updates show up here." />
        </Card>
      ) : (
        <Card className="divide-y divide-border p-0 overflow-hidden">
          {inbox.data!.map((n) => (
            <Row key={n.id} n={n} onOpen={(x) => (!x.read_at ? read.mutate([x.id]) : undefined)} />
          ))}
        </Card>
      )}
    </PageContainer>
  );
}
