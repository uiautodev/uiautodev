import { get } from "svelte/store";
import { t } from "svelte-i18n";
import { getLatestVersion, getUpdateInfo, getUpdateProgress, startUpdate } from "$lib/api/api";
import type { UpdateInfo, UpdateProgress } from "$lib/api/schemas";

const PROGRESS_POLL_MS = 500;
const RESTART_PROBE_DELAY_MS = 3000;
const RESTART_PROBE_ATTEMPTS = 60;
const RESTART_PROBE_INTERVAL_MS = 1000;

function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

class UpdateStore {
  latestVersion = $state("");
  available = $state(false);
  checking = $state(false);
  installing = $state(false);
  info = $state<UpdateInfo | null>(null);
  progress = $state<UpdateProgress | null>(null);
  error = $state("");

  #progressTimer: ReturnType<typeof setInterval> | null = null;

  get percent(): number {
    return Math.max(0, Math.min(100, this.progress?.percent ?? 0));
  }

  /** Cheap availability check backed by the cached version endpoint. */
  async checkVersion(): Promise<void> {
    if (this.checking) return;
    this.checking = true;
    try {
      const result = await getLatestVersion();
      this.latestVersion = result.latest_version;
      this.available = result.update_available;
    } catch {
      // Version check is best-effort; keep the previous state.
    } finally {
      this.checking = false;
    }
  }

  /** Load full update details (release notes, asset, desktop flag). */
  async check(): Promise<void> {
    if (this.checking || this.installing) return;
    this.checking = true;
    this.error = "";
    try {
      const info = await getUpdateInfo();
      this.info = info;
      this.latestVersion = info.latest;
      this.available = info.available;
    } catch (e) {
      this.info = null;
      this.error = errorMessage(e);
    } finally {
      this.checking = false;
    }
  }

  /** Start the desktop installer and follow its progress. */
  async install(): Promise<void> {
    if (this.installing || !this.info?.available || !this.info.desktop) return;
    this.installing = true;
    this.error = "";
    this.progress = { active: true, percent: 0 };
    try {
      await startUpdate();
      this.#startProgressPolling();
    } catch (e) {
      this.installing = false;
      this.progress = null;
      this.error = errorMessage(e);
    }
  }

  /** Resume an update started elsewhere (e.g. the native menu). */
  async resume(): Promise<boolean> {
    try {
      const progress = await getUpdateProgress();
      const finished = !progress.active && progress.percent >= 100 && !progress.error;
      if (!progress.active && !finished) return false;
      this.installing = true;
      this.progress = progress;
      try {
        this.info = await getUpdateInfo();
      } catch {
        // The update may have already restarted the app.
      }
      if (progress.active) {
        this.#startProgressPolling();
      } else {
        await this.#waitForRestart();
      }
      return true;
    } catch {
      return false;
    }
  }

  #startProgressPolling(): void {
    this.#stopProgressPolling();
    this.#progressTimer = setInterval(async () => {
      try {
        const progress = await getUpdateProgress();
        this.progress = progress;
        if (progress.error) {
          this.error = progress.error;
          this.installing = false;
          this.#stopProgressPolling();
        } else if (!progress.active && progress.percent >= 100) {
          this.#stopProgressPolling();
          await this.#waitForRestart();
        }
      } catch {
        // The server may be restarting; waitForRestart handles recovery.
      }
    }, PROGRESS_POLL_MS);
  }

  #stopProgressPolling(): void {
    if (this.#progressTimer) {
      clearInterval(this.#progressTimer);
      this.#progressTimer = null;
    }
  }

  async #waitForRestart(): Promise<void> {
    const previousVersion = this.info?.current;
    const targetVersion = this.info?.latest;
    const versionChanged = Boolean(previousVersion && targetVersion && previousVersion !== targetVersion);
    let sawFailure = false;

    // Give the old process time to quit and the relaunch helper to start the
    // new one before probing the API.
    await delay(RESTART_PROBE_DELAY_MS);
    for (let attempt = 0; attempt < RESTART_PROBE_ATTEMPTS; attempt++) {
      try {
        const info = await getUpdateInfo();
        const restarted = versionChanged
          ? info.current === targetVersion
          : previousVersion
            ? info.current !== previousVersion || sawFailure
            : sawFailure;
        if (restarted) {
          location.reload();
          return;
        }
      } catch {
        // The replacement process may not be listening yet.
        sawFailure = true;
      }
      await delay(RESTART_PROBE_INTERVAL_MS);
    }
    this.installing = false;
    this.error = get(t)("config.update.restartTimeout");
  }
}

export const updateStore = new UpdateStore();
