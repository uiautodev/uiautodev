import { DEFAULT_WIDTH, isWidthMode, type WidthMode } from "./width";

const WIDTH_KEY = "uiautodev-width";

function getStoredWidth(): WidthMode {
  if (typeof window === "undefined") return DEFAULT_WIDTH;
  try {
    const value = window.localStorage.getItem(WIDTH_KEY);
    return isWidthMode(value) ? value : DEFAULT_WIDTH;
  } catch {
    return DEFAULT_WIDTH;
  }
}

export const widthStore = $state<{ value: WidthMode }>({
  value: getStoredWidth(),
});

export function toggleWidth() {
  widthStore.value = widthStore.value === "wide" ? "narrow" : "wide";
  if (typeof window !== "undefined") {
    try {
      window.localStorage.setItem(WIDTH_KEY, widthStore.value);
    } catch {
      // Ignore storage write failures (quota / private mode / disabled storage)
    }
  }
}
