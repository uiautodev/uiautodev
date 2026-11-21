import type { TapPoint } from "$lib/api/api.js";

export interface ScrcpyTapInjector {
  /** Whether a live scrcpy control channel is currently available. */
  available(): boolean;
  /** Injects a single tap through the scrcpy control channel. */
  tap(point: TapPoint): Promise<void>;
}

const injectors = new Map<string, ScrcpyTapInjector>();

export function registerScrcpyTapInjector(deviceId: string, injector: ScrcpyTapInjector): () => void {
  injectors.set(deviceId, injector);
  return () => {
    if (injectors.get(deviceId) === injector) injectors.delete(deviceId);
  };
}

export function getScrcpyTapInjector(deviceId: string): ScrcpyTapInjector | undefined {
  return injectors.get(deviceId);
}
