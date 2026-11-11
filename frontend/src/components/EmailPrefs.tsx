import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { getNotificationPrefs, setNotificationPrefs, NOTIFICATION_KINDS, type NotificationPrefs } from '@/api/tasks';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';

const KIND_LABELS: Record<string, string> = {
  assigned: 'I am assigned',
  mention: 'Someone mentions me',
  comment: 'New comments on tasks I follow',
  status: 'Status changes on tasks I follow',
  due_soon: 'A task of mine is due soon',
  automation: 'Automation notices',
};

/** A person's choice of which Inbox notifications also come by email. */
export function EmailPrefs() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ['notification-prefs'], queryFn: ({ signal }) => getNotificationPrefs(signal) });
  const save = useMutation({
    mutationFn: (p: NotificationPrefs) => setNotificationPrefs(p),
    onSuccess: (p) => qc.setQueryData(['notification-prefs'], p),
  });
  if (!q.data) return null;
  const p = q.data;
  const toggleKind = (k: string) =>
    save.mutate({
      ...p,
      email_kinds: p.email_kinds.includes(k) ? p.email_kinds.filter((x) => x !== k) : [...p.email_kinds, k],
    });
  return (
    <Card className="mb-6">
      <CardHeader>
        <CardTitle>Email settings</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        {!p.has_address ? (
          <p className="text-muted-foreground">Your account has no email address, so notifications stay here.</p>
        ) : null}
        <label className="flex items-center gap-2 font-semibold">
          <input type="checkbox" checked={p.email} disabled={save.isPending} onChange={() => save.mutate({ ...p, email: !p.email })} />
          Also send me notifications by email
        </label>
        <div className="grid sm:grid-cols-2 gap-1 pl-6">
          {NOTIFICATION_KINDS.map((k) => (
            <label key={k} className="flex items-center gap-2 text-muted-foreground">
              <input
                type="checkbox"
                checked={p.email_kinds.includes(k)}
                disabled={!p.email || save.isPending}
                onChange={() => toggleKind(k)}
              />
              {KIND_LABELS[k] ?? k}
            </label>
          ))}
        </div>
        <p className="text-xs text-muted-foreground">
          Emails wait two minutes and bundle what happened meanwhile; anything you already read here is not emailed.
        </p>
      </CardContent>
    </Card>
  );
}
