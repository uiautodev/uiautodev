// Bun's test runner types are not part of the SvelteKit tsconfig.
// @ts-nocheck
import { expect, test } from "bun:test";
import { presenceLabelKey } from "./presence-label";

// Own occupancy is the normal case and must stay quiet; the device page used to
// print "in use by me" next to the screen for no benefit.
test("own and free devices have no occupancy label", () => {
  expect(presenceLabelKey("mine")).toBeNull();
  expect(presenceLabelKey("shared")).toBeNull();
  expect(presenceLabelKey("free")).toBeNull();
  expect(presenceLabelKey(undefined)).toBeNull();
});

test("read-only and other people's sessions keep a label", () => {
  expect(presenceLabelKey("viewing")).toBe("presence.state.viewing");
  expect(presenceLabelKey("busy")).toBe("presence.state.busy");
  expect(presenceLabelKey("preempted")).toBe("presence.state.preempted");
});
