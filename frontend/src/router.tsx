import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from '@tanstack/react-router';

import { Layout } from '@/components/Layout';
import { AuditPage } from '@/pages/AuditPage';
import { InboxPage } from '@/pages/InboxPage';
import { ListPage, type ListSearch } from '@/pages/ListPage';
import { LoginPage } from '@/pages/LoginPage';
import { MyWorkPage } from '@/pages/MyWorkPage';
import { OidcCallbackPage } from '@/pages/OidcCallbackPage';
import { SettingsPage } from '@/pages/SettingsPage';
import { SpacePage } from '@/pages/SpacePage';
import { SpacesPage } from '@/pages/SpacesPage';
import { TaskPage } from '@/pages/TaskPage';
import { getUserManager, isOidcAuthMode } from '@/auth/oidc';

// In OIDC mode a route behind the Layout checks there is a cached user
// before letting the component mount. If not, we redirect to /login and
// carry the attempted path so the post-login redirect lands there.
async function requireAuth(fullPath: string) {
  if (!isOidcAuthMode()) {
    return; // dev mode: nothing to check
  }
  const mgr = getUserManager();
  if (!mgr) {
    throw redirect({ to: '/login', search: { redirect: fullPath } });
  }
  const user = await mgr.getUser();
  if (!user || user.expired) {
    throw redirect({ to: '/login', search: { redirect: fullPath } });
  }
}

const rootRoute = createRootRoute({
  component: () => <Outlet />,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  component: LoginPage,
  validateSearch: (search: Record<string, unknown>) => ({
    redirect: typeof search.redirect === 'string' ? search.redirect : undefined,
  }),
});

const authCallbackRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/auth/callback',
  component: OidcCallbackPage,
});

const appLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'app',
  component: Layout,
});

const indexRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/',
  beforeLoad: async () => {
    await requireAuth('/my-work');
    throw redirect({ to: '/my-work' });
  },
});

const myWorkRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/my-work',
  beforeLoad: ({ location }) => requireAuth(location.pathname),
  component: MyWorkPage,
});

const inboxRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/inbox',
  beforeLoad: ({ location }) => requireAuth(location.pathname),
  component: InboxPage,
});

const spacesRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/spaces',
  beforeLoad: ({ location }) => requireAuth(location.pathname),
  component: SpacesPage,
});

const spaceRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/spaces/$spaceId',
  beforeLoad: ({ location }) => requireAuth(location.pathname),
  component: SpacePage,
});

const listRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/lists/$listId',
  beforeLoad: ({ location }) => requireAuth(location.pathname),
  validateSearch: (search: Record<string, unknown>): ListSearch => ({
    view: search.view === 'board' || search.view === 'list' ? search.view : undefined,
  }),
  component: ListPage,
});

const taskRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/t/$taskKey',
  beforeLoad: ({ location }) => requireAuth(location.pathname),
  component: TaskPage,
});

const settingsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/settings',
  beforeLoad: ({ location }) => requireAuth(location.pathname),
  component: SettingsPage,
});

const auditRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/settings/audit',
  beforeLoad: ({ location }) => requireAuth(location.pathname),
  component: AuditPage,
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  authCallbackRoute,
  appLayoutRoute.addChildren([
    indexRoute,
    myWorkRoute,
    inboxRoute,
    spacesRoute,
    spaceRoute,
    listRoute,
    taskRoute,
    settingsRoute,
    auditRoute,
  ]),
]);

export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
