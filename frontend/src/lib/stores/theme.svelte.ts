const THEME_KEY = "uiautodev-theme";

export type Theme = "light" | "dark";
export type ThemeMode = Theme | "system";

const MODES: ThemeMode[] = ["light", "dark", "system"];

function isThemeMode(value: unknown): value is ThemeMode {
  return value === "light" || value === "dark" || value === "system";
}

function getStoredMode(): ThemeMode {
  if (typeof window === "undefined") return "system";
  try {
    const value = window.localStorage.getItem(THEME_KEY);
    return isThemeMode(value) ? value : "system";
  } catch {
    return "system";
  }
}

function getSystemTheme(): Theme {
  if (typeof window === "undefined") return "light";
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

class ThemeStore {
  mode = $state<ThemeMode>(getStoredMode());
  system = $state<Theme>(getSystemTheme());
  resolved = $derived<Theme>(this.mode === "system" ? this.system : this.mode);
}

export const themeStore = new ThemeStore();

// applyTheme reflects the resolved theme on <html>. It is driven by the store
// itself (not a component effect) so the class always updates the moment the
// mode or the system preference changes.
function applyTheme() {
  if (typeof document === "undefined") return;
  document.documentElement.classList.toggle("dark", themeStore.resolved === "dark");
}

applyTheme();

// Keep the system preference in sync so "system" mode reacts to OS changes.
if (typeof window !== "undefined" && window.matchMedia) {
  const query = window.matchMedia("(prefers-color-scheme: dark)");
  query.addEventListener("change", () => {
    themeStore.system = query.matches ? "dark" : "light";
    applyTheme();
  });
}

export function setTheme(mode: ThemeMode) {
  themeStore.mode = mode;
  if (typeof window !== "undefined") {
    try {
      window.localStorage.setItem(THEME_KEY, mode);
    } catch {
      // ignore storage errors (private mode, etc.)
    }
  }
  applyTheme();
}

export function cycleTheme() {
  const index = MODES.indexOf(themeStore.mode);
  setTheme(MODES[(index + 1) % MODES.length]);
}
