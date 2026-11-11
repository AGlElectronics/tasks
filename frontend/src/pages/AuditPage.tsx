import { useState } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ApiError } from '@/api/client';
import { AUDIT_ENTITIES, getAudit } from '@/api/admin';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { PageContainer, PageHeader } from '@/components/ui/PageHeader';
import { EmptyState, ErrorRow, LoadingRow } from '@/components/ui/States';
import { auditSummary } from '@/lib/audit';
import { ScrollText } from 'lucide-react';

export function AuditPage() {
  const [entity, setEntity] = useState('');
  const [action, setAction] = useState('');
  const [since, setSince] = useState('');
  const feed = useInfiniteQuery({
    queryKey: ['audit', entity, action, since],
    queryFn: ({ pageParam, signal }) =>
      getAudit({ entity: entity || undefined, action: action || undefined, since: since || undefined, cursor: pageParam }, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next,
    refetchInterval: false,
  });
  const entries = feed.data?.pages.flatMap((p) => p.entries) ?? [];
  const sel = 'h-8 rounded-pill border border-border bg-card px-2.5 text-xs';

  return (
    <PageContainer>
      <PageHeader
        title="Audit log"
        description="Deletes, purges, access changes, archived spaces and settings changes, newest first. Only Tasks admins can see this page."
        actions={
          <Link to="/settings" className="text-sm font-semibold text-muted-foreground hover:text-foreground">
            Back to settings
          </Link>
        }
      />
      <div className="flex flex-wrap items-center gap-2 mb-4">
        <select value={entity} onChange={(e) => setEntity(e.target.value)} className={sel} aria-label="Entity">
          <option value="">Everything</option>
          {AUDIT_ENTITIES.map((x) => (
            <option key={x} value={x}>
              {x}
            </option>
          ))}
        </select>
        <select value={action} onChange={(e) => setAction(e.target.value)} className={sel} aria-label="Action">
          <option value="">Any action</option>
          {['deleted', 'purged', 'granted', 'revoked', 'archived', 'restored', 'updated'].map((x) => (
            <option key={x} value={x}>
              {x}
            </option>
          ))}
        </select>
        <label className="text-xs text-muted-foreground flex items-center gap-1">
          Since
          <input type="date" value={since} onChange={(e) => setSince(e.target.value)} className={sel} />
        </label>
      </div>
      {feed.isLoading ? (
        <LoadingRow />
      ) : feed.isError ? (
        <ErrorRow
          message={
            feed.error instanceof ApiError && feed.error.status === 403
              ? 'Only Tasks admins (settings.manage) can read the audit log.'
              : 'Could not load the audit log.'
          }
        />
      ) : entries.length === 0 ? (
        <Card>
          <EmptyState icon={ScrollText} title="Nothing recorded" description="No entries match these filters." />
        </Card>
      ) : (
        <Card className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-xs text-muted-foreground text-left">
              <tr>
                <th className="px-4 py-2 font-semibold">When</th>
                <th className="px-4 py-2 font-semibold">Who</th>
                <th className="px-4 py-2 font-semibold">What</th>
                <th className="px-4 py-2 font-semibold">Details</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {entries.map((e) => (
                <tr key={e.id}>
                  <td className="px-4 py-2 whitespace-nowrap text-xs">{new Date(e.created_at).toLocaleString()}</td>
                  <td className="px-4 py-2 whitespace-nowrap">{e.actor_name}</td>
                  <td className="px-4 py-2 whitespace-nowrap">
                    {e.entity} {e.action}
                  </td>
                  <td className="px-4 py-2 text-muted-foreground truncate max-w-md" title={JSON.stringify(e.data)}>
                    {auditSummary(e)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
      {feed.hasNextPage ? (
        <div className="mt-4">
          <Button variant="outline" size="sm" disabled={feed.isFetchingNextPage} onClick={() => feed.fetchNextPage()}>
            {feed.isFetchingNextPage ? 'Loading…' : 'Load older entries'}
          </Button>
        </div>
      ) : null}
    </PageContainer>
  );
}
