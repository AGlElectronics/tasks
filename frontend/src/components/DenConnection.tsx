import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { apiRequest } from '@/api/client';

type DenStatus = {
  paired: boolean;
  den_base_url?: string;
  paired_at?: string;
  synced_at?: string;
  last_error?: string;
  /** Den rejected this module's credential; an admin has to pair again. */
  revoked?: boolean;
  revoked_at?: string;
};

/**
 * Pairing with Den. Paired: brand and organisation settings come from Den.
 * Standalone: this module's own settings apply. Revoked: Den rejected the
 * stored credential; the last settings from Den stay in use until an admin
 * pairs again with a new pairing URL (the module never re-pairs by itself).
 */
export function DenConnection({ base, onPaired }: { base: string; onPaired?: () => void }) {
  const qc = useQueryClient();
  const status = useQuery({ queryKey: ['den-status'], queryFn: () => apiRequest<DenStatus>(`${base}/`) });
  const [url, setUrl] = useState('');
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['den-status'] });
    void qc.invalidateQueries({ queryKey: ['den-state'] });
    void qc.invalidateQueries({ queryKey: ['settings'] });
    void qc.invalidateQueries({ queryKey: ['module-brand'] });
  };
  const pair = useMutation({
    mutationFn: () => apiRequest<DenStatus>(`${base}/pair`, { method: 'POST', body: { pairing_url: url } }),
    onSuccess: () => {
      setUrl('');
      refresh();
      onPaired?.();
    },
  });
  const sync = useMutation({ mutationFn: () => apiRequest<DenStatus>(`${base}/sync`, { method: 'POST' }), onSettled: refresh });
  const unpair = useMutation({ mutationFn: () => apiRequest<void>(`${base}/pair`, { method: 'DELETE' }), onSuccess: refresh });
  const s = status.data;
  const error = (pair.error ?? sync.error ?? unpair.error) as Error | null;

  const pairForm = (intro: string) => (
    <form className="space-y-2"
      onSubmit={(e) => {
        e.preventDefault();
        pair.mutate();
      }}>
      <p className="text-muted-foreground">{intro}</p>
      <div className="flex gap-2">
        <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://den.example/api/v1/integrations/pairing/redeem?token=…"
          aria-label="Pairing URL"
          className="min-w-0 flex-1 rounded-md border border-input bg-background px-3 py-2 font-mono text-xs" />
        <button type="submit" disabled={!url.trim() || pair.isPending}
          className="rounded-pill bg-primary px-4 py-2 font-semibold text-primary-foreground disabled:opacity-60">
          {pair.isPending ? 'Connecting…' : 'Connect'}
        </button>
      </div>
    </form>
  );

  const disconnect = (
    <button type="button" disabled={unpair.isPending}
      onClick={() => {
        if (confirm('Disconnect from Den? This module goes back to its own settings and default look.')) unpair.mutate();
      }}
      className="rounded-pill border border-destructive/40 px-3 py-1.5 text-destructive disabled:opacity-60">
      Disconnect
    </button>
  );

  return (
    <section className="space-y-3 rounded-lg border border-border bg-card p-4 text-sm">
      <div>
        <h2 className="font-semibold">Den connection</h2>
        <p className="text-xs text-muted-foreground">
          Connected modules take their look and organisation settings from Den and pick up changes within a minute.
        </p>
      </div>
      {status.isPending ? (
        <p className="text-muted-foreground">Checking…</p>
      ) : s?.paired && s.revoked ? (
        <>
          <div role="alert" className="space-y-1 rounded-md border border-destructive/40 bg-destructive/10 p-3">
            <p className="font-semibold text-destructive">Den rejected this module's credential</p>
            <p>
              Den at <code className="font-mono">{s.den_base_url}</code> no longer accepts this module
              {s.revoked_at && <> (since {new Date(s.revoked_at).toLocaleString()})</>}: the pairing was revoked or
              the module was removed in Den. The last settings from Den stay in use. Tasks does not pair again by
              itself; pair again manually below.
            </p>
          </div>
          {pairForm('In Den, open Admin → Modules, create a new pairing URL for this module and paste it here.')}
          <div className="flex gap-2">{disconnect}</div>
        </>
      ) : s?.paired ? (
        <>
          <p>
            Connected to <code className="font-mono">{s.den_base_url}</code>
            {s.synced_at && <> · last sync {new Date(s.synced_at).toLocaleString()}</>}
          </p>
          {s.last_error && <p className="text-destructive">Last sync failed: {s.last_error}</p>}
          <div className="flex gap-2">
            <button type="button" onClick={() => sync.mutate()} disabled={sync.isPending}
              className="rounded-pill border border-border px-3 py-1.5 disabled:opacity-60">
              {sync.isPending ? 'Syncing…' : 'Sync now'}
            </button>
            {disconnect}
          </div>
        </>
      ) : (
        pairForm('Standalone. To connect, open Den → Admin → Modules, create a pairing URL for this module and paste it here.')
      )}
      {error && <p className="text-destructive">{error.message}</p>}
    </section>
  );
}
