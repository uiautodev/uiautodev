export enum ScreenMode {
  Control = "control",
  Readonly = "readonly",
  Recording = "recording",
}

export const screenModeStore = $state<{ modes: Record<string, ScreenMode> }>({
  modes: {},
});

type ScreenModeChangeListener = (mode: ScreenMode) => void;

const listeners = new Map<string, Set<ScreenModeChangeListener>>();

function listenersFor(deviceId: string): Set<ScreenModeChangeListener> {
  let set = listeners.get(deviceId);
  if (!set) {
    set = new Set();
    listeners.set(deviceId, set);
  }
  return set;
}

export function getScreenMode(deviceId: string): ScreenMode {
  return screenModeStore.modes[deviceId] ?? ScreenMode.Control;
}

export function emitScreenModeChange(deviceId: string, mode: ScreenMode): void {
  const set = listeners.get(deviceId);
  if (!set) return;
  for (const listener of set) {
    try {
      listener(mode);
    } catch (err) {
      console.error("[screen-mode] onScreenModeChange listener error:", err);
    }
  }
}

export function onScreenModeChange(deviceId: string, listener: ScreenModeChangeListener): () => void {
  const set = listenersFor(deviceId);
  set.add(listener);
  return () => {
    set.delete(listener);
    if (set.size === 0) listeners.delete(deviceId);
  };
}

export function setScreenMode(deviceId: string, mode: ScreenMode): void {
  if (getScreenMode(deviceId) === mode) return;
  if (mode === ScreenMode.Control) {
    delete screenModeStore.modes[deviceId];
  } else {
    screenModeStore.modes[deviceId] = mode;
  }
  emitScreenModeChange(deviceId, mode);
}
