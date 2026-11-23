import type { Me } from "$lib/api/schemas";
import { getMe } from "$lib/api/api";

/**
 * identityStore holds the current user as reported by /api/v1/me. The identity
 * may be anonymous when no third-party auth is configured; in that case it is
 * still stable per browser because the backend keys it on a session cookie.
 */
export const identityStore = $state({
  me: null as Me | null,
  loading: false,
  loaded: false,

  async ensure(): Promise<Me | null> {
    if (this.loaded) return this.me;
    if (this.loading) return this.me;
    this.loading = true;
    try {
      this.me = await getMe();
      this.loaded = true;
    } catch (e) {
      // Identity is optional for the UI: a missing endpoint (older server) or a
      // transient failure must not break the page.
      console.error("failed to load identity", e);
    } finally {
      this.loading = false;
    }
    return this.me;
  },

  async refresh(): Promise<Me | null> {
    this.loaded = false;
    return this.ensure();
  },
});
