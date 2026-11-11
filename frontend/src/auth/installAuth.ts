import { setAuthHeadersResolver } from '@/api/client';
import { installDevAuth } from '@/auth/dev';
import { currentUser, isOidcAuthMode } from '@/auth/oidc';
import { installSessionSync } from '@/auth/session';

/** Wire the API client's auth header pipeline. Call once at startup. */
export function installAuth(): void {
  if (isOidcAuthMode()) {
    setAuthHeadersResolver(async (): Promise<Record<string, string>> => {
      // Token contract: the backend accepts access tokens whose audience is
      // the Tasks API (tasks-api), never the ID token.
      const user = await currentUser();
      if (!user?.access_token) {
        return {};
      }
      return { Authorization: `Bearer ${user.access_token}` };
    });
    installSessionSync();
    return;
  }
  installDevAuth();
}
