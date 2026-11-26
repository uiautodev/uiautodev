// Bun's test runner types are not part of the SvelteKit tsconfig.
// @ts-nocheck
import { expect, test } from "bun:test";

// The occupancy avatars render an absolutely positioned badge with z-10. Without
// an isolated stacking context on the badge root that badge paints above the
// sticky actions column, so a lone green dot shows next to the observe button
// once the table scrolls horizontally. Pin the guard here.
test("PresenceBadge isolates its badge from the sticky actions column", async () => {
  const source = await Bun.file(new URL("./PresenceBadge.svelte", import.meta.url)).text();
  const rootClass = source.match(/<div class="([^"]*)"/)?.[1] ?? "";
  expect(rootClass.split(/\s+/)).toContain("isolate");
});
