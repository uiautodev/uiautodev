import { useLocalStorage } from "$lib/utils/localstorage.svelte";
import type { Plugin } from "$lib/api/schemas";

/** Persisted plugin id order for the two masonry columns. */
export interface PluginColumns {
  left: string[];
  right: string[];
}

const orderStore = useLocalStorage<PluginColumns>("uiautodev-plugin-order", {
  left: [],
  right: [],
});

function sortById(plugins: Plugin[]): Plugin[] {
  return [...plugins].sort((a, b) => a.id.localeCompare(b.id));
}

/**
 * Reconcile the stored column order with the currently available plugins:
 * drop ids that no longer exist, then append new plugins (sorted by id) to the
 * shorter column. Returns id lists ready to be mapped back to plugins.
 */
export function reconcileColumns(plugins: Plugin[]): PluginColumns {
  const validIds = new Set(plugins.map((p) => p.id));
  const seen = new Set<string>();
  const left: string[] = [];
  const right: string[] = [];

  for (const id of orderStore.current.left ?? []) {
    if (validIds.has(id) && !seen.has(id)) {
      left.push(id);
      seen.add(id);
    }
  }
  for (const id of orderStore.current.right ?? []) {
    if (validIds.has(id) && !seen.has(id)) {
      right.push(id);
      seen.add(id);
    }
  }

  for (const plugin of sortById(plugins.filter((p) => !seen.has(p.id)))) {
    if (left.length <= right.length) left.push(plugin.id);
    else right.push(plugin.id);
  }

  return { left, right };
}

export function savePluginColumns(columns: PluginColumns): void {
  orderStore.current = columns;
}
