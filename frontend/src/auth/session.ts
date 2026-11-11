import type { User } from 'oidc-client-ts';

import { setUnauthorizedHandler } from '@/api/client';
import { getUserManager } from '@/auth/oidc';

/*
 * Keeps every tab on the identity Keycloak currently has. Tokens live in
 * each tab's memory only (token contract), so tabs tell each other who is
 * signed in over a BroadcastChannel; only the subject is sent, never a token:
 *  - another tab signs out            -> this tab goes to the login page
 *  - another tab signs in as someone  -> this tab reloads as that person
 *  - Keycloak session ends elsewhere   -> session monitor signs this tab out
 *  - the API answers 401               -> back to the login page
 * Without this, a tab kept a stale admin token after a user switch.
 */

const CHANNEL = 'tasks-auth';

type SessionMessage = { sub: string | null };

export function goToLogin(): void {
  const path = `${window.location.pathname}${window.location.search}`;
  if (path.startsWith('/login') || path.startsWith('/auth/')) return;
  window.location.assign(`/login?redirect=${encodeURIComponent(path)}`);
}

let installed = false;

export function installSessionSync(): void {
  const mgr = getUserManager();
  if (!mgr || installed || typeof window === 'undefined') return;
  installed = true;

  const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(CHANNEL);
  const announce = (sub: string | null) => channel?.postMessage({ sub } satisfies SessionMessage);

  let currentSub: string | undefined;
  void mgr.getUser().then((u) => {
    currentSub = u?.profile.sub;
  });
  mgr.events.addUserLoaded((u: User) => {
    currentSub = u.profile.sub;
    announce(u.profile.sub);
  });
  mgr.events.addUserUnloaded(() => {
    currentSub = undefined;
    announce(null);
  });

  channel?.addEventListener('message', (e: MessageEvent<SessionMessage>) => {
    const next = e.data?.sub ?? undefined;
    if (next === currentSub) return; // token refresh for the same person
    if (next === undefined) {
      void mgr.removeUser().finally(goToLogin);
    } else if (currentSub !== undefined) {
      window.location.reload();
    }
  });

  const endSession = () => {
    void mgr.removeUser().finally(goToLogin);
  };
  mgr.events.addUserSignedOut(endSession);
  // Same person, new Keycloak session (signed out and back in elsewhere): adopt
  // it silently. A reload would loop, because the stored user keeps the old
  // session_state and the monitor reports "changed" again on every load.
  mgr.events.addUserSessionChanged(() => {
    void mgr.signinSilent().catch(endSession);
  });
  mgr.events.addSilentRenewError((err) => {
    if (/login_required|invalid_grant|session not active/i.test(String(err?.message ?? err))) endSession();
  });

  let redirecting = false;
  setUnauthorizedHandler(() => {
    if (redirecting) return;
    redirecting = true;
    endSession();
  });
}
