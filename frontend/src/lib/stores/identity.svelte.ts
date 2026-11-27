import type { Me } from "$lib/api/schemas";
import { getMe } from "$lib/api/api";

// pending is the in-flight /me request shared by every caller. Without it a
// second caller (e.g. DeviceGuard racing the account menu) got `null` instead
// of waiting and fell back to a presence flow the desktop app does not use.
let pending: Promise<Me | null> | null = null;

// requestGeneration increments for every /me request. A response is applied
// only when it belongs to the newest request, so a slow reply cannot overwrite
// a fresh identity or clear its pending state.
let requestGeneration = 0;

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
    if (pending) return pending;
    return this.load();
  },

  async refresh(): Promise<Me | null> {
    // Do not clear pending: the older request would then clear the new one's
    // pending flag, and its late response could overwrite the fresh identity.
    this.loaded = false;
    return this.load();
  },

  load(): Promise<Me | null> {
    const generation = ++requestGeneration;
    this.loading = true;
    let request: Promise<Me | null>;
    request = getMe()
      .then((me) => {
        if (generation === requestGeneration) {
          this.me = me;
          this.loaded = true;
        }
        return me;
      })
      .catch((e) => {
        // Identity is optional for the UI: a missing endpoint (older server)
        // or a transient failure must not break the page.
        console.error("failed to load identity", e);
        return null;
      })
      .finally(() => {
        if (pending === request) {
          pending = null;
          this.loading = false;
        }
      });
    pending = request;
    return request;
  },
});
