import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiError } from '@/api/client';
import { getSettings, putSettings, sendTestEmail, splitKey, type SettingsPatch } from '@/api/admin';
import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Input, Label } from '@/components/ui/Input';
import { ErrorRow, LoadingRow } from '@/components/ui/States';

const LABELS: Record<string, string> = {
  'retention.trash_days': 'Trash kept (days)',
  'retention.notification_days': 'Read notifications kept (days)',
  'retention.audit_days': 'Audit log kept (days)',
  'retention.automation_run_days': 'Automation history kept (days)',
  'reminders.due_soon_days': 'Due-soon reminder (days before)',
};

const SOURCE: Record<string, string> = { default: 'default', tasks: 'set here', den: 'set in Den' };

/** Retention, reminders and email: Tasks' own values, or Den's when paired. */
export function ModuleSettingsCard() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ['module-settings'], queryFn: ({ signal }) => getSettings(signal) });
  const [draft, setDraft] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!q.data) return;
    const next: Record<string, string> = {};
    for (const d of q.data.definitions) {
      const [s, f] = splitKey(d.key);
      next[d.key] = String(q.data.settings[s]?.[f] ?? d.default);
    }
    setDraft(next);
  }, [q.data]);
  const save = useMutation({
    mutationFn: (body: SettingsPatch) => putSettings(body),
    onSuccess: (data) => qc.setQueryData(['module-settings'], data),
  });
  const test = useMutation({ mutationFn: sendTestEmail });

  if (q.isLoading) return <LoadingRow />;
  if (q.isError || !q.data) return <ErrorRow message="Could not load the module settings." />;
  const s = q.data;

  const submit = () => {
    const body: SettingsPatch = {};
    for (const d of s.definitions) {
      if (s.sources[d.key] === 'den') continue;
      const [sec, f] = splitKey(d.key);
      const cur = s.settings[sec]?.[f];
      const v = Number(draft[d.key]);
      if (Number.isFinite(v) && v !== cur) (body[sec] ??= {})[f] = v;
    }
    save.mutate(body);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Retention, reminders and email</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-xs text-muted-foreground">
          0 keeps data forever. The defaults are placeholders pending legal review: set what your data-protection rules
          say. Values marked “set in Den” come from Den’s module settings and can only be changed there.
        </p>
        <form
          className="grid gap-3 sm:grid-cols-2"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          {s.definitions.map((d) => (
            <div key={d.key}>
              <Label htmlFor={d.key}>{LABELS[d.key] ?? d.key}</Label>
              <Input
                id={d.key}
                type="number"
                min={d.min}
                max={d.max}
                value={draft[d.key] ?? ''}
                disabled={s.sources[d.key] === 'den'}
                onChange={(e) => setDraft((x) => ({ ...x, [d.key]: e.target.value }))}
                title={d.description}
              />
              <p className="text-[11px] text-muted-foreground mt-0.5">
                {SOURCE[s.sources[d.key]]} · default {d.default}
              </p>
            </div>
          ))}
          <div className="sm:col-span-2 flex items-center gap-3">
            <Button type="submit" size="sm" disabled={save.isPending}>
              Save
            </Button>
            {save.error ? (
              <span className="text-sm text-destructive">
                {save.error instanceof ApiError ? save.error.message : 'Could not save.'}
              </span>
            ) : save.isSuccess ? (
              <span className="text-sm text-muted-foreground">Saved.</span>
            ) : null}
          </div>
        </form>
        <div className="border-t border-border pt-4 text-sm">
          <div className="font-semibold mb-1">Email</div>
          {s.email.configured ? (
            <p className="text-muted-foreground">
              Sent through {s.email.host}:{s.email.port} ({s.email.security}) as {s.email.from} — settings from{' '}
              {s.email.source === 'den' ? 'Den' : 'Tasks’ SMTP_* environment'}.
            </p>
          ) : (
            <p className="text-muted-foreground">
              Not configured. Set SMTP_HOST and SMTP_FROM on the backend, or pair with a Den that sends SMTP settings.
              Notifications stay in the Inbox.
            </p>
          )}
          <div className="flex items-center gap-3 mt-2">
            <Button size="sm" variant="outline" disabled={!s.email.configured || test.isPending} onClick={() => test.mutate()}>
              Send a test email to me
            </Button>
            {test.error ? (
              <span className="text-sm text-destructive">
                {test.error instanceof ApiError ? test.error.message : 'Could not send.'}
              </span>
            ) : test.data ? (
              <span className="text-sm text-muted-foreground">Sent to {test.data.sent_to}.</span>
            ) : null}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
