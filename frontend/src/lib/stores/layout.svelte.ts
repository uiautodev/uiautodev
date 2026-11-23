const LAYOUT_KEY = "uiautodev-device-layout";

function getStoredLayout(): "horizontal" | "vertical" {
  if (typeof window === "undefined") return "horizontal";
  try {
    return window.localStorage.getItem(LAYOUT_KEY) === "vertical" ? "vertical" : "horizontal";
  } catch {
    return "horizontal";
  }
}

export const layoutStore = $state({
  value: getStoredLayout(),
});

export function toggleLayout() {
  layoutStore.value = layoutStore.value === "horizontal" ? "vertical" : "horizontal";
  if (typeof window !== "undefined") {
    try {
      window.localStorage.setItem(LAYOUT_KEY, layoutStore.value);
    } catch {
      // Ignore storage write failures (quota / private mode / disabled storage)
    }
  }
}
