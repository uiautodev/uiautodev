import type { OccupancyState } from "$lib/api/schemas";

/**
 * i18n key of the occupancy label shown in the device list and the device page.
 * Holding the device yourself (mine/shared) or seeing it free is the normal
 * case, so those stay unlabeled; only a read-only session and other people's
 * occupancy need a hint.
 */
export function presenceLabelKey(state: OccupancyState | undefined): string | null {
  switch (state) {
    case "viewing":
      return "presence.state.viewing";
    case "busy":
      return "presence.state.busy";
    case "preempted":
      return "presence.state.preempted";
    default:
      return null;
  }
}
