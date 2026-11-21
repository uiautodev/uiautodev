type WailsWindow = Window & {
  _wails?: { environment?: { OS?: string } };
};

/**
 * True when the page is hosted inside the Wails desktop webview. The Wails
 * runtime injects `window._wails.environment` before the app scripts run, so
 * this is also safe to call at module initialisation time.
 */
export function isWailsEnv(): boolean {
  return typeof window !== "undefined" && Boolean((window as WailsWindow)._wails?.environment);
}
