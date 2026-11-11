import { useEffect, useState } from 'react';
import { Link, Outlet, useRouterState } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Bell, Folder as FolderIcon, Inbox, LayoutGrid, ListChecks, LogOut, Menu, Settings, Star, X } from 'lucide-react';
import { getUnreadCount, listFavorites, listSpaces, updateList, type List, type Space } from '@/api/tasks';
import { isOidcAuthMode, signOutOidc } from '@/auth/oidc';
import { CommandPalette } from '@/components/CommandPalette';
import { DenConnection } from '@/components/DenConnection';
import { ThemeToggle } from '@/components/ThemeToggle';
import { useBrand } from '@/brand/BrandProvider';
import { isDenPairingRequired, useDenState } from '@/hooks/useDenState';
import { useMe, PERMS } from '@/hooks/useMe';
import { dismissNotice, useNotices } from '@/lib/notify';
import { startRealtime } from '@/lib/realtime';
import { useThemeSync } from '@/lib/theme';
import { cn } from '@/lib/utils';

/** Floating strip for app-wide notices (version conflicts, failed saves). */
function Notices() {
  const notices = useNotices();
  if (notices.length === 0) return null;
  return (
    <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-[60] space-y-2 w-[min(28rem,90vw)]">
      {notices.map((n) => (
        <div
          key={n.id}
          role="alert"
          className={cn(
            'flex items-start gap-2 rounded-md border px-3 py-2 text-sm shadow-pop bg-card',
            n.tone === 'error' ? 'border-destructive/50 text-destructive' : 'border-border'
          )}
        >
          <span className="flex-1">{n.message}</span>
          <button
            type="button"
            aria-label="Dismiss"
            className="shrink-0 opacity-60 hover:opacity-100"
            onClick={() => dismissNotice(n.id)}
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      ))}
    </div>
  );
}

type NavItem = {
  to: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  perms: string[]; // empty = always visible
};

const NAV: NavItem[] = [
  { to: '/my-work', label: 'My work', icon: Inbox, perms: [] },
  { to: '/inbox', label: 'Inbox', icon: Bell, perms: [] },
  { to: '/spaces', label: 'Spaces', icon: LayoutGrid, perms: [] },
  { to: '/settings', label: 'Settings', icon: Settings, perms: [PERMS.settingsManage] },
];

/** Unread-count pill on the Inbox nav item (TASK-5). */
function UnreadBadge() {
  const count = useQuery({ queryKey: ['inbox-unread'], queryFn: ({ signal }) => getUnreadCount(signal) });
  if (!count.data) return null;
  return (
    <span className="ml-auto min-w-5 h-5 px-1.5 inline-flex items-center justify-center rounded-pill bg-primary text-primary-foreground text-[11px] font-bold">
      {count.data > 99 ? '99+' : count.data}
    </span>
  );
}

function Wordmark() {
  const { brand } = useBrand();
  return (
    <div className="flex items-center gap-2.5">
      {brand.logoUrl ? (
        <img src={brand.logoUrl} alt="" className="h-8 w-8 rounded-md object-contain" />
      ) : (
        <span className="rounded-pill bg-primary-soft p-1.5">
          <ListChecks className="w-5 h-5 text-primary" />
        </span>
      )}
      <span className="leading-none">
        <span className="block font-heading text-xl font-extrabold tracking-tight">
          {brand.productName}
        </span>
        {brand.organizationName ? (
          <span className="block text-[11px] font-medium text-muted-foreground mt-0.5">
            {brand.organizationName}
          </span>
        ) : null}
      </span>
    </div>
  );
}

function NavLinks({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const { canAny } = useMe();
  return (
    <div className="space-y-0.5">
      {NAV.map((item) => {
        const visible = item.perms.length === 0 || canAny(...item.perms);
        if (!visible) return null;
        const isActive =
          pathname === item.to || (item.to !== '/' && pathname.startsWith(item.to + '/'));
        return (
          <Link
            key={`${item.to}-${item.label}`}
            to={item.to}
            onClick={onNavigate}
            className={cn(
              'flex items-center gap-2.5 px-3 py-2 rounded-pill text-sm font-medium whitespace-nowrap transition-colors',
              isActive
                ? 'bg-accent text-accent-foreground'
                : 'text-muted-foreground hover:bg-secondary hover:text-foreground'
            )}
          >
            <item.icon className="w-4 h-4 shrink-0" />
            {item.label}
            {item.to === '/inbox' ? <UnreadBadge /> : null}
          </Link>
        );
      })}
      <Favorites onNavigate={onNavigate} />
      <SpaceTree onNavigate={onNavigate} />
    </div>
  );
}

/** Pinned spaces and lists (TASK-11). */
function Favorites({ onNavigate }: { onNavigate?: () => void }) {
  const favs = useQuery({ queryKey: ['favorites'], queryFn: ({ signal }) => listFavorites(signal) });
  const spaces = useQuery({ queryKey: ['spaces'], queryFn: ({ signal }) => listSpaces(signal) });
  if (!favs.data || favs.data.length === 0 || !spaces.data) return null;
  const spaceById = new Map(spaces.data.map((s) => [s.id, s]));
  const listById = new Map(spaces.data.flatMap((s) => s.lists.map((l) => [l.id, l] as const)));
  return (
    <div className="pt-4">
      <div className="px-3 pb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground flex items-center gap-1">
        <Star className="w-3 h-3" /> Favourites
      </div>
      {favs.data.map((f) => {
        if (f.space_id) {
          const sp = spaceById.get(f.space_id);
          if (!sp) return null;
          return (
            <Link
              key={`s-${f.space_id}`}
              to="/spaces/$spaceId"
              params={{ spaceId: sp.id }}
              onClick={onNavigate}
              className="flex items-center gap-2 px-3 py-1.5 rounded-pill text-sm hover:bg-secondary transition-colors"
            >
              <span
                className="inline-flex items-center justify-center w-4 h-4 rounded text-[8px] font-bold text-white shrink-0"
                style={{ backgroundColor: sp.color }}
              >
                {sp.key.slice(0, 2)}
              </span>
              <span className="truncate">{sp.name}</span>
            </Link>
          );
        }
        const l = f.list_id ? listById.get(f.list_id) : undefined;
        if (!l) return null;
        return (
          <Link
            key={`l-${f.list_id}`}
            to="/lists/$listId"
            params={{ listId: l.id }}
            onClick={onNavigate}
            className="flex items-center gap-2 px-3 py-1.5 rounded-pill text-sm text-muted-foreground hover:bg-secondary hover:text-foreground transition-colors"
          >
            <span className="truncate">{l.name}</span>
          </Link>
        );
      })}
    </div>
  );
}

/**
 * One sidebar list row. Managers can drag it onto a folder (or the space
 * header) to regroup lists without opening settings (TASK-9).
 */
function ListRow({
  list,
  draggable,
  indent,
  onNavigate,
}: {
  list: List;
  draggable: boolean;
  indent: boolean;
  onNavigate?: () => void;
}) {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  return (
    <Link
      to="/lists/$listId"
      params={{ listId: list.id }}
      onClick={onNavigate}
      draggable={draggable}
      onDragStart={(e) => {
        e.dataTransfer.setData('application/x-tasks-list', list.id);
        e.dataTransfer.effectAllowed = 'move';
      }}
      className={cn(
        'flex items-center justify-between gap-2 pr-3 py-1 rounded-pill text-sm transition-colors',
        indent ? 'pl-14' : 'pl-10',
        pathname === `/lists/${list.id}`
          ? 'bg-accent text-accent-foreground'
          : 'text-muted-foreground hover:bg-secondary hover:text-foreground'
      )}
    >
      <span className="truncate">{list.name}</span>
      {list.open_tasks > 0 ? <span className="text-xs">{list.open_tasks}</span> : null}
    </Link>
  );
}

function useListDrop() {
  const qc = useQueryClient();
  const move = useMutation({
    mutationFn: ({ listId, folderId }: { listId: string; folderId: string | null }) =>
      updateList(listId, { folder_id: folderId }),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['spaces'] }),
  });
  const dropProps = (folderId: string | null, enabled: boolean) =>
    enabled
      ? {
          onDragOver: (e: React.DragEvent) => {
            if (e.dataTransfer.types.includes('application/x-tasks-list')) e.preventDefault();
          },
          onDrop: (e: React.DragEvent) => {
            const listId = e.dataTransfer.getData('application/x-tasks-list');
            if (listId) {
              e.preventDefault();
              move.mutate({ listId, folderId });
            }
          },
        }
      : {};
  return dropProps;
}

function SpaceEntry({ sp, onNavigate }: { sp: Space; onNavigate?: () => void }) {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const dropProps = useListDrop();
  const canDrag = sp.can_manage;
  const rootLists = sp.lists.filter((l) => !l.folder_id);
  const byFolder = new Map<string, List[]>();
  for (const l of sp.lists) {
    if (!l.folder_id) continue;
    byFolder.set(l.folder_id, [...(byFolder.get(l.folder_id) ?? []), l]);
  }
  return (
    <div className="pb-1">
      <Link
        to="/spaces/$spaceId"
        params={{ spaceId: sp.id }}
        onClick={onNavigate}
        {...dropProps(null, canDrag)}
        className={cn(
          'flex items-center gap-2 px-3 py-1.5 rounded-pill text-sm font-semibold transition-colors',
          pathname === `/spaces/${sp.id}` ? 'bg-accent text-accent-foreground' : 'hover:bg-secondary'
        )}
      >
        <span
          className="inline-flex items-center justify-center w-5 h-5 rounded text-[9px] font-bold text-white shrink-0"
          style={{ backgroundColor: sp.color }}
        >
          {sp.key.slice(0, 2)}
        </span>
        <span className="truncate">{sp.name}</span>
        {sp.shared_only ? <span className="text-[10px] text-muted-foreground ml-auto">shared</span> : null}
      </Link>
      {rootLists.map((l) => (
        <ListRow key={l.id} list={l} draggable={canDrag} indent={false} onNavigate={onNavigate} />
      ))}
      {sp.folders.map((f) => (
        <div key={f.id}>
          <div
            {...dropProps(f.id, canDrag)}
            className="flex items-center gap-1.5 pl-10 pr-3 py-1 text-xs font-semibold text-muted-foreground"
          >
            <FolderIcon className="w-3 h-3" />
            <span className="truncate">{f.name}</span>
          </div>
          {(byFolder.get(f.id) ?? []).map((l) => (
            <ListRow key={l.id} list={l} draggable={canDrag} indent onNavigate={onNavigate} />
          ))}
        </div>
      ))}
    </div>
  );
}

function SpaceTree({ onNavigate }: { onNavigate?: () => void }) {
  const spaces = useQuery({ queryKey: ['spaces'], queryFn: ({ signal }) => listSpaces(signal) });
  if (!spaces.data || spaces.data.length === 0) return null;
  return (
    <div className="pt-4">
      <div className="px-3 pb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
        Spaces
      </div>
      {spaces.data.map((sp) => (
        <SpaceEntry key={sp.id} sp={sp} onNavigate={onNavigate} />
      ))}
    </div>
  );
}

/** Shown instead of the app while the backend runs with DEN_MODE=required and is not paired. */
function DenPairingGate({ canManage }: { canManage: boolean }) {
  const qc = useQueryClient();
  return (
    <div className="min-h-screen grid place-items-center bg-background text-foreground p-6">
      <div className="max-w-xl w-full rounded-2xl border border-border bg-card p-6 space-y-4">
        <div className="flex justify-center">
          <Wordmark />
        </div>
        {canManage ? (
          <>
            <h1 className="font-heading text-lg font-bold text-center">Connect this module to Den</h1>
            <ol className="list-decimal space-y-1 pl-5 text-sm">
              <li>In Den, open Modules and click Pair next to Tasks</li>
              <li>Copy the pairing URL (valid 15 minutes)</li>
              <li>Paste it below</li>
            </ol>
            <DenConnection base="/api/v1/den" onPaired={() => void qc.invalidateQueries()} />
          </>
        ) : (
          <div className="space-y-1 text-center">
            <h1 className="font-heading text-lg font-bold">Waiting for Den</h1>
            <p className="text-sm text-muted-foreground">
              This module is waiting to be connected to Den. Ask an administrator to pair it.
            </p>
          </div>
        )}
        <div className="flex justify-center gap-2">
          <ThemeToggle className="rounded-pill" />
          {isOidcAuthMode() ? (
            <button
              type="button"
              className="inline-flex items-center gap-2 text-sm px-3 py-2 rounded-pill text-muted-foreground hover:bg-secondary hover:text-foreground transition-colors"
              onClick={() => {
                signOutOidc().catch(() => undefined);
              }}
            >
              <LogOut className="w-4 h-4 shrink-0" />
              Sign out
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export function Layout() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const { me, can } = useMe();
  const denState = useDenState();
  const [drawerOpen, setDrawerOpen] = useState(false);
  useThemeSync('/api/v1/me/preferences');

  // Collapse the mobile drawer whenever the route changes.
  useEffect(() => {
    setDrawerOpen(false);
  }, [pathname]);

  // Live updates once signed in; polling is the fallback.
  const qc = useQueryClient();
  const signedIn = !!me;
  useEffect(() => {
    if (!signedIn) return;
    return startRealtime(qc);
  }, [signedIn, qc]);

  if (me && isDenPairingRequired(denState.data)) {
    return <DenPairingGate canManage={can(PERMS.settingsManage)} />;
  }

  const footer = (
    <div className="mt-auto p-2 border-t border-border space-y-0.5">
      <ThemeToggle className="rounded-pill" />
      {isOidcAuthMode() ? (
        <button
          type="button"
          className="w-full flex items-center gap-2 text-left text-sm px-3 py-2 rounded-pill text-muted-foreground hover:bg-secondary hover:text-foreground transition-colors"
          onClick={() => {
            signOutOidc().catch(() => undefined);
          }}
        >
          <LogOut className="w-4 h-4 shrink-0" />
          Sign out
        </button>
      ) : null}
    </div>
  );

  return (
    <div className="min-h-screen w-full flex flex-col md:flex-row bg-background text-foreground">
      <header className="md:hidden sticky top-0 z-40 flex items-center gap-2 px-3 py-2.5 border-b border-border bg-card">
        <button
          type="button"
          className="p-2 rounded-pill hover:bg-secondary transition-colors"
          onClick={() => setDrawerOpen(true)}
          aria-label="Open menu"
        >
          <Menu className="w-5 h-5" />
        </button>
        <Wordmark />
      </header>

      {drawerOpen ? (
        <div
          className="md:hidden fixed inset-0 z-50 bg-black/50 backdrop-blur-[2px]"
          onClick={() => setDrawerOpen(false)}
        >
          <aside
            className="absolute inset-y-0 left-0 w-72 max-w-[85vw] bg-card border-r border-border flex flex-col"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="px-4 py-4 flex items-center justify-between border-b border-border">
              <Wordmark />
              <button
                type="button"
                className="p-2 rounded-pill hover:bg-secondary transition-colors"
                onClick={() => setDrawerOpen(false)}
                aria-label="Close menu"
              >
                <X className="w-5 h-5" />
              </button>
            </div>
            <nav className="p-2 flex-1 overflow-y-auto">
              <NavLinks onNavigate={() => setDrawerOpen(false)} />
            </nav>
            {footer}
          </aside>
        </div>
      ) : null}

      <aside className="hidden md:flex shrink-0 md:w-60 md:min-h-screen border-r border-border bg-card flex-col">
        <div className="px-4 py-4 border-b border-border">
          <Wordmark />
        </div>
        <nav className="p-2 md:flex-1 overflow-y-auto">
          <NavLinks />
        </nav>
        {footer}
      </aside>

      <main className="flex-1 min-w-0 min-h-0 flex flex-col w-full md:min-h-screen">
        <Outlet />
      </main>
      <Notices />
      <CommandPalette />
    </div>
  );
}
