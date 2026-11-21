import type { TapPoint } from "$lib/api/api.js";

export interface ScreenTapEvent extends TapPoint {
  /** Device the tap was captured on, used to route events to the right plugin. */
  deviceId: string;
  /** Event time as `Date.now()` (milliseconds since epoch). */
  timestamp: number;
}

type ScreenTapListener = (event: ScreenTapEvent) => void;

const listeners = new Set<ScreenTapListener>();

export function emitScreenTap(event: ScreenTapEvent): void {
  for (const listener of listeners) {
    try {
      listener(event);
    } catch (err) {
      console.error("[screen-events] onScreenTap listener error:", err);
    }
  }
}

export function onScreenTap(listener: ScreenTapListener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
